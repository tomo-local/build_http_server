# HTTP サーバー自作 学習プラン (3日間)

`net/http` を使わず、`net` パッケージの TCP ソケット通信から HTTP サーバーを組み上げる。

---

## Day 1 — TCP ソケット + HTTPリクエスト パーサー

### ゴール
- TCP コネクションの確立・受け入れができる
- 生バイト列を `Request` 構造体にパースできる

### TCP ソケット基礎

| # | やること |
|---|---------|
| 1 | `net.Listen("tcp", ":8080")` でサーバー起動 |
| 2 | `listener.Accept()` でコネクション受け入れ |
| 3 | `conn.Read()` / `conn.Write()` で生バイト送受信 |
| 4 | goroutine で複数コネクションを並行処理 |

**動作確認:** `telnet localhost 8080` や `nc` で接続してバイト列を確認

### HTTP リクエスト パーサー

HTTP/1.1 のフォーマットを理解して実装する:

```
GET /path HTTP/1.1\r\n
Host: localhost\r\n
\r\n
```

| # | やること |
|---|---------|
| 1 | `bufio.Reader` でリクエストラインを読む |
| 2 | メソッド・パス・HTTPバージョンをパース |
| 3 | ヘッダーを `map[string]string` に格納 |
| 4 | `Content-Length` があればボディを読む |

```go
type Request struct {
    Method  string
    Path    string
    Version string
    Headers map[string]string
    Body    []byte
}
```

---

## Day 2 — HTTP レスポンス ビルダー + ルーター & ハンドラー

### ゴール
- 構造体から HTTP レスポンスを組み立てて送信できる
- パスとメソッドでハンドラーを振り分けられる

### HTTP レスポンス ビルダー

```
HTTP/1.1 200 OK\r\n
Content-Type: text/plain\r\n
Content-Length: 13\r\n
\r\n
Hello, World!
```

| # | やること |
|---|---------|
| 1 | ステータスライン生成 |
| 2 | レスポンスヘッダー書き込み |
| 3 | `Content-Length` を自動計算 |
| 4 | `curl -v` で実際のブラウザ/ツールから動作確認 |

```go
type Response struct {
    StatusCode int
    Headers    map[string]string
    Body       []byte
}
```

### ルーター & ハンドラー

| # | やること |
|---|---------|
| 1 | `Handler` 型 (`func(req *Request) *Response`) を定義 |
| 2 | `map[string]Handler` でパスルーティング |
| 3 | メソッド (GET/POST) でも分岐 |
| 4 | 404 / 405 のエラーレスポンス |

```go
type Handler func(req *Request) *Response

type Router struct {
    routes map[string]map[string]Handler // path -> method -> handler
}

func (r *Router) Handle(method, path string, h Handler)
func (r *Router) Dispatch(req *Request) *Response
```

---

## Day 3 — 仕上げ & 深掘り

### ゴール
- 安定して動くサーバーに仕上げる
- 統合テストで品質を確認する

### 必須

| # | やること |
|---|---------|
| 1 | `defer conn.Close()` のリソース管理 |
| 2 | パニックリカバリー (1コネクションのクラッシュが全体を止めない) |
| 3 | 統合テスト (`net.Dial` でテストコードから叩く) |

### 発展 (興味に応じて)

| テーマ | 内容 |
|--------|------|
| Keep-Alive | `Connection: keep-alive` で1コネクション複数リクエスト |
| ミドルウェア | ロギング・認証を関数合成で挟む |
| クエリパラメータ | `/search?q=go` のパース |
| チャンク転送 | `Transfer-Encoding: chunked` |

---

## 全体の流れ

```
Day 1: TCP接続 + リクエスト解析
         ↓
Day 2: レスポンス生成 + ルーティング
         ↓
Day 3: 仕上げ・テスト・深掘り
```

## 参考資料

- [RFC 7230](https://datatracker.ietf.org/doc/html/rfc7230) — HTTP/1.1 メッセージ構文
- `go doc net` / `go doc bufio` — 標準ライブラリドキュメント
- `curl -v http://localhost:8080/` — レスポンスの生ヘッダー確認
