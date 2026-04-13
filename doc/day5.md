# Day 5 — Keep-Alive・ミドルウェア・TLS

## 今日のゴール

- `Connection: keep-alive` で1コネクション複数リクエストを処理できる
- ロギング・リカバリーをミドルウェアとして合成できる
- TLS の仕組みを理解して HTTPS サーバーを起動できる

---

## 1. Keep-Alive（持続的コネクション）

### なぜ必要か

HTTP/1.0 はリクエストごとにTCPコネクションを張り直していた。
3-wayハンドシェイクのオーバーヘッドが大きいため、
HTTP/1.1 ではデフォルトで1本のTCPコネクション上で複数リクエストを処理する。

```
HTTP/1.0（毎回コネクションを張り直す）
  TCP connect → GET / → TCP close
  TCP connect → GET /style.css → TCP close
  TCP connect → GET /logo.png → TCP close

HTTP/1.1 Keep-Alive（1本のTCPで連続処理）
  TCP connect → GET / → GET /style.css → GET /logo.png → TCP close
```

### 実装: リクエストループ

```go
func handleConnection(conn net.Conn, router *Router) {
    defer conn.Close()
    defer recoverPanic(conn)

    reader := NewBufferedReader(conn)

    for {
        // アイドル状態のタイムアウト: 60秒でコネクションを切る
        conn.SetReadDeadline(time.Now().Add(60 * time.Second))

        req, err := parseRequestFrom(reader)
        if err != nil {
            if err == io.EOF || isTimeout(err) {
                return // 正常終了
            }
            log.Println("parse error:", err)
            return
        }

        res := router.Dispatch(req)

        // クライアントが "Connection: close" を送ってきたら、このリクエストで終了
        connHeader := toLower(req.Headers["connection"])
        if connHeader == "close" {
            res.Headers["Connection"] = "close"
            conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
            conn.Write(res.Bytes())
            return
        }

        res.Headers["Connection"] = "keep-alive"
        conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
        if _, err := conn.Write(res.Bytes()); err != nil {
            return
        }
        // ループして次のリクエストを待つ
    }
}
```

### parseRequest を BufferedReader を外から受け取る形に変更

Keep-Aliveループでは同一の `BufferedReader` を複数リクエストで使い回す必要がある。
（バッファ内に次のリクエストの先頭が入っている場合があるため）

```go
// Day2 の parseRequest を BufferedReader を引数に取るよう変更
func parseRequestFrom(reader *BufferedReader) (*Request, error) {
    // リクエストライン
    line, err := reader.ReadLine()
    if err != nil {
        return nil, err
    }
    // ... (Day2と同じロジック)
}
```

---

## 2. ミドルウェアパターン

### ミドルウェアとは

ハンドラーの前後に処理を挟む仕組み。
ロギング・認証・リカバリーを「横断的関心事」として本体のロジックから分離する。

```
Request → [Logger] → [Auth] → [BusinessLogic] → [Logger] → Response
```

### 関数合成で実装する

```go
// Middleware はハンドラーを受け取り、ラップしたハンドラーを返す関数
type Middleware func(HandlerFunc) HandlerFunc

// Chain はミドルウェアを順に適用する
// Chain(h, A, B, C) → A(B(C(h)))
// リクエストの処理順: A → B → C → h → C → B → A
func Chain(h HandlerFunc, middlewares ...Middleware) HandlerFunc {
    for i := len(middlewares) - 1; i >= 0; i-- {
        h = middlewares[i](h)
    }
    return h
}
```

### ロギングミドルウェア

```go
func LoggerMiddleware(next HandlerFunc) HandlerFunc {
    return func(req *Request) *Response {
        start := time.Now()
        res := next(req)
        elapsed := time.Since(start)
        log.Printf("%s %s %d %v", req.Method, req.Path, res.StatusCode, elapsed)
        return res
    }
}
```

### パニックリカバリーミドルウェア

```go
func RecoveryMiddleware(next HandlerFunc) HandlerFunc {
    return func(req *Request) *Response {
        defer func() {
            if r := recover(); r != nil {
                log.Printf("panic in handler: %v", r)
            }
        }()
        return next(req)
    }
}
```

### 認証ミドルウェア（Bearer トークン）

```go
func AuthMiddleware(next HandlerFunc) HandlerFunc {
    return func(req *Request) *Response {
        auth := req.Headers["authorization"]
        // "Bearer <token>" のプレフィックスを確認
        prefix := "Bearer "
        if len(auth) < len(prefix) || auth[:len(prefix)] != prefix {
            return NewResponse(401).SetText("Unauthorized")
        }
        token := auth[len(prefix):]
        if token != "secret-token" { // 実際はDBや環境変数と照合
            return NewResponse(403).SetText("Forbidden")
        }
        return next(req)
    }
}
```

### 使い方

```go
// 全ルートにLogging + Recoveryを適用
router.Handle("GET", "/api/users", Chain(
    usersHandler,
    LoggerMiddleware,
    RecoveryMiddleware,
))

// 管理系エンドポイントには認証も追加
router.Handle("POST", "/api/admin", Chain(
    adminHandler,
    LoggerMiddleware,
    RecoveryMiddleware,
    AuthMiddleware,
))
```

---

## 3. TLS / HTTPS の仕組み

### なぜTLSが必要か

TCP の通信は素のバイト列が流れるため、経路上で盗聴・改ざんができる。
TLS（Transport Layer Security）はTCPの上で暗号化層を提供する。

```
HTTPSの層構造:
HTTP（アプリケーションデータ）
  ↓↑ 暗号化・復号
TLS（ハンドシェイク・暗号化）
  ↓↑
TCP（信頼性のある伝送）
  ↓↑
IP（ルーティング）
```

### TLS ハンドシェイクの流れ（TLS 1.3）

```
Client                          Server
  |---ClientHello─────────────>|  対応暗号スイート・乱数を送る
  |<──ServerHello──────────────|  使用する暗号を選択
  |<──Certificate──────────────|  サーバーの公開鍵証明書
  |   [鍵交換 ECDH]             |  双方で同じ共有秘密鍵を計算
  |---Finished────────────────>|
  |<──Finished─────────────────|
  |=== 以降は暗号化された通信 ===|
```

### 重要な概念

- **証明書**: サーバーの公開鍵 + CA（認証局）の署名
- **ECDH**: 鍵交換アルゴリズム。お互いに秘密鍵を送らずに共通の鍵を生成
- **AES-GCM**: 実際のデータの暗号化に使う対称暗号

> TLS の暗号実装自体は数学的に非常に複雑なため、`crypto/tls` パッケージを使う。
> 自作サーバーでもTCPソケット部分は同じ — `tls.Listen` が TLS レイヤーを透過的に挟んでくれる。

---

## 4. 自己署名証明書の生成

```bash
# 開発・テスト用の自己署名証明書（有効期間365日）
openssl req -x509 -newkey rsa:4096 \
  -keyout key.pem -out cert.pem \
  -days 365 -nodes \
  -subj "/CN=localhost"
```

生成されるファイル:
- `cert.pem` — 証明書（公開鍵 + 自己署名）
- `key.pem` — 秘密鍵（絶対に公開しない）

---

## 5. TLS サーバーの実装

```go
import "crypto/tls"

func startTLSServer(router *Router) error {
    // 証明書と秘密鍵を読み込む
    cert, err := tls.LoadX509KeyPair("cert.pem", "key.pem")
    if err != nil {
        return err
    }

    tlsConfig := &tls.Config{
        Certificates: []tls.Certificate{cert},
        MinVersion:   tls.VersionTLS12,
    }

    // tls.Listen は net.Listen と同じインターフェースを返す
    ln, err := tls.Listen("tcp", ":8443", tlsConfig)
    if err != nil {
        return err
    }
    defer ln.Close()
    log.Println("HTTPS listening on :8443")

    for {
        conn, err := ln.Accept() // 返ってくる conn は net.Conn と互換
        if err != nil {
            continue
        }
        go handleConnection(conn, router) // 今まで書いたコードがそのまま動く
    }
}
```

### HTTPとHTTPSを同時に起動

```go
func main() {
    router := setupRouter()

    // HTTP: 8080
    go func() {
        ln, _ := net.Listen("tcp", ":8080")
        defer ln.Close()
        log.Println("HTTP  listening on :8080")
        for {
            conn, err := ln.Accept()
            if err != nil {
                return
            }
            go handleConnection(conn, router)
        }
    }()

    // HTTPS: 8443（こちらをメインに）
    if err := startTLSServer(router); err != nil {
        log.Fatal(err)
    }
}
```

### 動作確認

```bash
# -k で自己署名証明書のチェックをスキップ
curl -vk https://localhost:8443/

# 証明書の詳細を確認
openssl s_client -connect localhost:8443 -showcerts
```

---

## 6. チャンク転送（Transfer-Encoding: chunked）

ファイルサイズが事前に分からない場合に使う。
`Content-Length` なしでボディを送れる。

```
HTTP/1.1 200 OK\r\n
Transfer-Encoding: chunked\r\n
\r\n
7\r\n           ← チャンクのバイト数（16進数）
Mozilla\r\n     ← チャンクのデータ
9\r\n
Developer\r\n
0\r\n           ← 0 = 終端チャンク
\r\n
```

```go
// writeChunked は conn にチャンク転送形式でデータを書き出す
func writeChunked(conn net.Conn, data []byte, chunkSize int) error {
    for len(data) > 0 {
        n := chunkSize
        if n > len(data) {
            n = len(data)
        }
        // チャンクサイズを16進で書く
        conn.Write(intToHexBytes(n))
        conn.Write([]byte("\r\n"))
        conn.Write(data[:n])
        conn.Write([]byte("\r\n"))
        data = data[n:]
    }
    // 終端チャンク
    conn.Write([]byte("0\r\n\r\n"))
    return nil
}

// intToHexBytes は整数を16進文字列のバイト列に変換する
func intToHexBytes(n int) []byte {
    if n == 0 {
        return []byte("0")
    }
    buf := make([]byte, 0, 8)
    for n > 0 {
        nibble := byte(n & 0xf)
        if nibble < 10 {
            buf = append([]byte{nibble + '0'}, buf...)
        } else {
            buf = append([]byte{nibble - 10 + 'a'}, buf...)
        }
        n >>= 4
    }
    return buf
}
```

---

## 7. 5日間の全体アーキテクチャ

```
net.Listen(":8080") / tls.Listen(":8443")
        |
    ln.Accept()  ← ブロッキング、コネクションを待つ
        |
  go handleConnection(conn)  ← goroutineで並行処理
        |
  ┌─────────────────────────────────────┐
  │         Keep-Alive ループ            │
  │                 |                   │
  │  parseRequestFrom(reader)           │ ← 自作BufferedReader
  │    ReadLine() × N (ヘッダー)        │ ← 自作ユーティリティ
  │    ReadFull(N) (ボディ)             │
  │                 |                   │
  │  router.Dispatch(req)               │ ← path+methodで振り分け
  │                 |                   │
  │  Chain(handler,                     │
  │    LoggerMiddleware,                │ ← 関数合成
  │    RecoveryMiddleware,              │
  │    AuthMiddleware)                  │
  │                 |                   │
  │  HandlerFunc(req) → Response        │ ← ビジネスロジック
  │    JSONObject / JSONArray           │ ← 自作JSONビルダー
  │                 |                   │
  │  res.Bytes() → conn.Write()         │ ← []byteへのappend
  └─────────────────────────────────────┘
```

---

## 8. 次のステップ

| テーマ | 内容 |
|--------|------|
| HTTP/2 | バイナリフレーム・多重化・ヘッダー圧縮（HPACK） |
| WebSocket | HTTP→WS アップグレード・フレーム構造の自作 |
| 静的ファイル配信 | `os.Open` + Content-Type 判定 |
| レート制限 | トークンバケットをgoroutineで実装 |
| `net/http` と比較 | 自作サーバーと標準ライブラリのソースを読み比べる |

---

## 今日のチェックリスト

- [ ] Keep-Aliveループで同一コネクション上の複数リクエストを処理できる
- [ ] `BufferedReader` を使い回してリクエストを連続パースできる
- [ ] `Chain()` でミドルウェアを関数合成できる
- [ ] `LoggerMiddleware` / `RecoveryMiddleware` を実装できた
- [ ] 自己署名証明書を生成して TLS サーバーを起動できた
- [ ] 5日間で作ったアーキテクチャ全体を説明できる

---

## 参考

- RFC 8446 — TLS 1.3
- RFC 7230 §4.1 — Chunked Transfer Encoding
- `go doc crypto/tls` — TLS パッケージ
- [How HTTPS works](https://howhttps.works/) — TLSをビジュアルで解説
