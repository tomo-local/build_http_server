# Day 2 — HTTP/1.1プロトコル + BufferedReader自作 + リクエストパーサー

## 今日のゴール

- HTTP/1.1のメッセージフォーマットを完全に理解する
- バッファリング読み取りの仕組みを自作して理解する
- `Request` 構造体を実装してヘッダー・ボディを取り出せる

---

## 1. HTTP/1.1 リクエストの構造

HTTPはTCPの上を流れるテキストプロトコル。生のバイト列はこのような形をしている。

```
POST /api/users HTTP/1.1\r\n
Host: localhost:8080\r\n
Content-Type: application/json\r\n
Content-Length: 27\r\n
\r\n
{"name": "Alice", "age": 30}
```

### 各パーツの役割

```
[リクエストライン]
POST /api/users HTTP/1.1\r\n
 ^      ^           ^
 |      |           HTTPバージョン
 |      パス（URI）
 HTTPメソッド

[ヘッダー] (0行以上、順不同)
Key: Value\r\n
Key: Value\r\n

[空行] ← ヘッダー終端の目印（必須）
\r\n

[ボディ] (Content-Lengthがある場合)
{"name": "Alice", "age": 30}
```

### 重要なヘッダー

| ヘッダー | 意味 |
|----------|------|
| `Host` | 接続先ホスト名（HTTP/1.1では必須） |
| `Content-Length` | ボディのバイト数 |
| `Content-Type` | ボディの種類 (`application/json`, `text/html` など) |
| `Connection` | `keep-alive` / `close` |

### HTTPメソッド

| メソッド | 用途 | ボディ |
|----------|------|--------|
| `GET` | リソースの取得 | なし |
| `POST` | リソースの作成 | あり |
| `PUT` | リソースの置き換え | あり |
| `DELETE` | リソースの削除 | なし |

---

## 2. なぜバッファリングが必要か

`conn.Read()` はOSのシステムコール `read()` を呼ぶ。
1バイトずつ呼ぶとシステムコールの回数が膨大になり遅い。

```
システムコールなし: ユーザー空間だけで処理 → 速い
システムコールあり: カーネルに切り替わる → 遅い（1回あたり数マイクロ秒）

1000バイトのヘッダーを1バイトずつ読む場合:
→ 1000回のシステムコール
→ バッファリングすれば 1〜2回で済む
```

解決策: 一度にまとめてTCPから読み込み、内部バッファに保存しておく。
行単位の読み取りはバッファから行う。

---

## 3. BufferedReader を自作する

標準ライブラリの `bufio` を使わず、同じ仕組みを自分で作る。

```go
// BufferedReader は net.Conn の上にバッファを乗せた読み取り器
type BufferedReader struct {
    conn net.Conn
    buf  [4096]byte // 内部バッファ（固定4KB）
    r    int        // 次に読む位置
    w    int        // バッファの書き込み済み終端
}

func NewBufferedReader(conn net.Conn) *BufferedReader {
    return &BufferedReader{conn: conn}
}

// fill は conn から読み込んでバッファを補充する
func (b *BufferedReader) fill() error {
    // 未読データを先頭に詰める
    if b.r > 0 {
        copy(b.buf[:], b.buf[b.r:b.w])
        b.w -= b.r
        b.r = 0
    }
    n, err := b.conn.Read(b.buf[b.w:])
    b.w += n
    return err
}

// ReadLine は \n まで読んで、末尾の \r\n を除いた行を返す
func (b *BufferedReader) ReadLine() (string, error) {
    for {
        // バッファ内を \n で検索
        for i := b.r; i < b.w; i++ {
            if b.buf[i] == '\n' {
                line := string(b.buf[b.r:i])
                b.r = i + 1
                // 末尾の \r を除去
                if len(line) > 0 && line[len(line)-1] == '\r' {
                    line = line[:len(line)-1]
                }
                return line, nil
            }
        }
        // \n が見つからなければ conn から追加で読む
        if err := b.fill(); err != nil {
            return "", err
        }
    }
}

// ReadFull は正確に n バイト読んで返す
func (b *BufferedReader) ReadFull(n int) ([]byte, error) {
    result := make([]byte, n)
    total := 0

    for total < n {
        // まずバッファにある分を使う
        buffered := b.w - b.r
        if buffered > 0 {
            take := buffered
            if take > n-total {
                take = n - total
            }
            copy(result[total:], b.buf[b.r:b.r+take])
            b.r += take
            total += take
            continue
        }
        // バッファが空なら conn から直接読む
        nn, err := b.conn.Read(result[total:])
        total += nn
        if err != nil && total < n {
            return result[:total], err
        }
    }

    return result, nil
}
```

### バッファの動き方（図解）

```
初期状態:
buf: [................................]
      r=0, w=0

fill() 後（TCPから48バイト受信）:
buf: [GET / HTTP/1.1\r\nHost: loc...]
      r=0, w=48

ReadLine() で "GET / HTTP/1.1" を取り出した後:
buf: [GET / HTTP/1.1\r\nHost: loc...]
                       r=17, w=48
                       ↑ rが進む、データはそのまま残る
```

---

## 4. Request構造体の実装

```go
package main

import (
    "fmt"
    "net"
    "strconv"
    "strings"
)

type Request struct {
    Method  string
    Path    string
    Query   string            // "q=go&page=2" の部分（生文字列）
    Version string
    Headers map[string]string
    Body    []byte
}

func parseRequest(conn net.Conn) (*Request, error) {
    reader := NewBufferedReader(conn)

    // --- 1. リクエストラインを読む ---
    line, err := reader.ReadLine()
    if err != nil {
        return nil, fmt.Errorf("read request line: %w", err)
    }

    // "GET /path?q=1 HTTP/1.1" を3分割
    parts := splitN(line, ' ', 3)
    if len(parts) != 3 {
        return nil, fmt.Errorf("invalid request line: %q", line)
    }

    // パスとクエリを分離
    path, query := splitPath(parts[1])

    req := &Request{
        Method:  parts[0],
        Path:    path,
        Query:   query,
        Version: parts[2],
        Headers: make(map[string]string),
    }

    // --- 2. ヘッダーを読む ---
    for {
        line, err := reader.ReadLine()
        if err != nil {
            return nil, fmt.Errorf("read header: %w", err)
        }

        if line == "" {
            break // 空行 = ヘッダー終端
        }

        // "Content-Type: application/json" を key/value に分割
        idx := indexByte(line, ':')
        if idx < 0 {
            continue
        }
        key := toLower(trimSpace(line[:idx]))
        val := trimSpace(line[idx+1:])
        req.Headers[key] = val
    }

    // --- 3. ボディを読む ---
    if cl, ok := req.Headers["content-length"]; ok {
        length, err := strconv.Atoi(cl)
        if err != nil {
            return nil, fmt.Errorf("invalid Content-Length: %w", err)
        }
        req.Body, err = reader.ReadFull(length)
        if err != nil {
            return nil, fmt.Errorf("read body: %w", err)
        }
    }

    return req, nil
}
```

---

## 5. 自作ユーティリティ関数

標準ライブラリの `strings` パッケージに頼らず、必要な関数を自分で実装する。

```go
// indexByte は s の中で c が最初に現れる位置を返す（なければ -1）
func indexByte(s string, c byte) int {
    for i := 0; i < len(s); i++ {
        if s[i] == c {
            return i
        }
    }
    return -1
}

// trimSpace は先頭と末尾のスペース・タブを除去する
func trimSpace(s string) string {
    start := 0
    for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
        start++
    }
    end := len(s)
    for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
        end--
    }
    return s[start:end]
}

// toLower はASCII文字を小文字に変換する
func toLower(s string) string {
    b := make([]byte, len(s))
    for i := 0; i < len(s); i++ {
        c := s[i]
        if c >= 'A' && c <= 'Z' {
            c += 'a' - 'A'
        }
        b[i] = c
    }
    return string(b)
}

// splitN は sep で最大n個に分割する
func splitN(s string, sep byte, n int) []string {
    var result []string
    for len(result) < n-1 {
        idx := indexByte(s, sep)
        if idx < 0 {
            break
        }
        result = append(result, s[:idx])
        s = s[idx+1:]
    }
    result = append(result, s)
    return result
}

// splitPath は "/path?query" を ("/path", "query") に分ける
func splitPath(raw string) (path, query string) {
    idx := indexByte(raw, '?')
    if idx < 0 {
        return raw, ""
    }
    return raw[:idx], raw[idx+1:]
}
```

---

## 6. クエリパラメータのパース

`q=golang&page=2` のような文字列を `map[string]string` に変換する。

```go
// parseQuery は "key=value&key2=value2" をパースする
func parseQuery(query string) map[string]string {
    result := make(map[string]string)
    if query == "" {
        return result
    }

    // "&" で各ペアに分割
    pairs := splitAll(query, '&')
    for _, pair := range pairs {
        idx := indexByte(pair, '=')
        if idx < 0 {
            result[pair] = "" // 値なしキー
            continue
        }
        key := pair[:idx]
        val := pair[idx+1:]
        result[percentDecode(key)] = percentDecode(val)
    }
    return result
}

// splitAll は sep で全て分割する
func splitAll(s string, sep byte) []string {
    var result []string
    start := 0
    for i := 0; i < len(s); i++ {
        if s[i] == sep {
            result = append(result, s[start:i])
            start = i + 1
        }
    }
    result = append(result, s[start:])
    return result
}

// percentDecode は %XX エンコードをデコードする（例: %20 → " "）
func percentDecode(s string) string {
    result := make([]byte, 0, len(s))
    for i := 0; i < len(s); i++ {
        if s[i] == '+' {
            result = append(result, ' ') // form では + がスペースを意味する
        } else if s[i] == '%' && i+2 < len(s) {
            hi := hexVal(s[i+1])
            lo := hexVal(s[i+2])
            if hi >= 0 && lo >= 0 {
                result = append(result, byte(hi<<4|lo))
                i += 2
                continue
            }
        }
        result = append(result, s[i])
    }
    return string(result)
}

func hexVal(c byte) int {
    switch {
    case c >= '0' && c <= '9':
        return int(c - '0')
    case c >= 'a' && c <= 'f':
        return int(c-'a') + 10
    case c >= 'A' && c <= 'F':
        return int(c-'A') + 10
    }
    return -1
}

// 使い方
params := parseQuery(req.Query)
q := params["q"]     // "golang"
page := params["page"] // "2"
```

---

## 7. 動作確認

```bash
# サーバー起動
go run ./src/main.go

# GETリクエスト
curl -v http://localhost:8080/

# クエリパラメータ付き
curl -v "http://localhost:8080/search?q=golang&page=2"

# POSTリクエスト（ボディあり）
curl -v -X POST \
  -H "Content-Type: application/json" \
  -d '{"name":"Alice"}' \
  http://localhost:8080/api/users
```

---

## 8. よくあるミス

| ミス | 問題 | 対処 |
|------|------|------|
| `\r\n` でなく `\n` だけ確認 | ブラウザが`\r\n`を送る | ReadLine内で末尾の`\r`を除去 |
| ヘッダーキーの大文字小文字 | `Content-Length` と `content-length` を別扱い | `toLower()` で正規化 |
| バッファを使わず毎回 `conn.Read` | 次のReadで前のデータが消える | BufferedReader に統一 |
| ボディが途中で切れる | TCPの分割受信 | `ReadFull` で必要バイト数分ループ |

---

## 今日のチェックリスト

- [ ] HTTPリクエストの3つのパーツ（ライン・ヘッダー・ボディ）を説明できる
- [ ] バッファリングが必要な理由（システムコールコスト）を説明できる
- [ ] `BufferedReader` の `fill()` / `ReadLine()` / `ReadFull()` を実装できた
- [ ] `\r\n` の空行でヘッダー終端を検出できる
- [ ] クエリパラメータを `%` デコードしながらパースできる

---

## 参考

- RFC 7230 — HTTP/1.1 メッセージ構文
- `man 2 read` — read システムコール
- RFC 3986 — URI構文（パーセントエンコーディングの仕様）
