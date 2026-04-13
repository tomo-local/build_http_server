# Day 4 — 並行処理・エラーハンドリング・テスト

## 今日のゴール

- goroutineのリーク・パニック問題を防げる
- タイムアウト・デッドライン制御を実装できる
- `net.Dial` を使った統合テストを（外部ライブラリなしで）書ける

---

## 1. goroutineの注意点

### goroutineリークとは

goroutine は軽量だが、無限に増え続けると問題になる。
よくあるリーク: コネクションが切れてもgoroutineが残り続ける。

```go
// NG: conn.Close() を忘れると Read がブロックし続けてリーク
go func() {
    buf := make([]byte, 1024)
    for {
        n, err := conn.Read(buf) // EOFが来るまでブロック
        if err != nil {
            return
        }
        _ = n
    }
}()

// OK: defer で確実にクローズ
go func() {
    defer conn.Close() // ← 最初に書く
    // ...
}()
```

### アクティブなgoroutine数を確認

```go
import "runtime"

log.Printf("Goroutines: %d", runtime.NumGoroutine())
```

---

## 2. パニックリカバリー

1つのコネクション処理でpanicが起きてもサーバー全体が落ちてはいけない。
`recover()` はdeferの中でしか機能しない点に注意。

```go
func handleConnection(conn net.Conn, router *Router) {
    defer conn.Close()
    // ← defer はLIFOスタック。conn.Close()の前にrecoverが実行される
    defer func() {
        if r := recover(); r != nil {
            log.Printf("panic recovered: %v", r)
            // ベストエフォートで500を返す
            res := NewResponse(500).SetText("Internal Server Error")
            conn.Write(res.Bytes())
        }
    }()

    req, err := parseRequest(conn)
    if err != nil {
        conn.Write(NewResponse(400).SetText("Bad Request").Bytes())
        return
    }

    res := router.Dispatch(req)
    if _, err := conn.Write(res.Bytes()); err != nil {
        log.Println("write error:", err)
    }
}
```

### defer のLIFO実行順序

```go
func f() {
    defer fmt.Println("1") // 最後に実行
    defer fmt.Println("2")
    defer fmt.Println("3") // 最初に実行
    fmt.Println("body")
}
// 出力: body → 3 → 2 → 1
```

---

## 3. タイムアウト制御

クライアントが接続したまま何も送ってこない場合、goroutineが永遠にブロックする。
`SetDeadline` でOS側にタイムアウトを設定する。

```go
import "time"

func handleConnection(conn net.Conn, router *Router) {
    defer conn.Close()
    defer recoverPanic(conn)

    // リクエスト受信のタイムアウト: 30秒
    conn.SetReadDeadline(time.Now().Add(30 * time.Second))

    req, err := parseRequest(conn)
    if err != nil {
        if isTimeout(err) {
            log.Println("read timeout")
        } else {
            log.Println("parse error:", err)
        }
        return
    }

    // レスポンス送信のタイムアウト: 10秒
    conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

    res := router.Dispatch(req)
    conn.Write(res.Bytes())
}

// isTimeout はエラーがタイムアウトかどうかを判定する
func isTimeout(err error) bool {
    if err == nil {
        return false
    }
    // net.Error インターフェースを持っていればTimeout()で確認
    type timeoutErr interface {
        Timeout() bool
    }
    if te, ok := err.(timeoutErr); ok {
        return te.Timeout()
    }
    return false
}

func recoverPanic(conn net.Conn) {
    if r := recover(); r != nil {
        log.Printf("panic: %v", r)
        conn.Write(NewResponse(500).SetText("Internal Server Error").Bytes())
    }
}
```

### SetDeadline の種類

| メソッド | 対象 |
|----------|------|
| `SetDeadline` | 読み書き両方 |
| `SetReadDeadline` | 読み込みのみ |
| `SetWriteDeadline` | 書き込みのみ |

---

## 4. 統合テスト（外部ライブラリなし）

Go標準の `testing` パッケージと `net.Dial` だけで書く。
自作の `sendHTTP` ヘルパーで生のTCPリクエストを送る。

```go
// src/server_test.go
package main

import (
    "net"
    "testing"
    "time"
)

// startTestServer はランダムポートでサーバーを起動し、アドレスとstop関数を返す
func startTestServer(t *testing.T) (addr string, stop func()) {
    t.Helper()

    router := NewRouter()
    router.Handle("GET", "/", func(req *Request) *Response {
        return NewResponse(200).SetText("Hello, World!")
    })
    router.Handle("GET", "/api/health", func(req *Request) *Response {
        return NewResponse(200).SetJSON(JSONObject("status", jsonString("ok")))
    })
    router.Handle("POST", "/api/users", func(req *Request) *Response {
        return NewResponse(201).SetJSON(req.Body)
    })

    // ポート0 = OSが空きポートを自動で割り当てる
    ln, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        t.Fatal(err)
    }

    go func() {
        for {
            conn, err := ln.Accept()
            if err != nil {
                return // ln.Close()が呼ばれた
            }
            go handleConnection(conn, router)
        }
    }()

    return ln.Addr().String(), func() { ln.Close() }
}

// sendHTTP は生のHTTPリクエストを送ってレスポンス全体を返す
func sendHTTP(t *testing.T, addr, rawRequest string) []byte {
    t.Helper()

    conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
    if err != nil {
        t.Fatalf("dial failed: %v", err)
    }
    defer conn.Close()
    conn.SetDeadline(time.Now().Add(5 * time.Second))

    // リクエストを送信
    if _, err := conn.Write([]byte(rawRequest)); err != nil {
        t.Fatalf("write failed: %v", err)
    }

    // レスポンスを全部読む
    var buf []byte
    tmp := make([]byte, 1024)
    for {
        n, err := conn.Read(tmp)
        buf = append(buf, tmp[:n]...)
        if err != nil {
            break // EOFまたはタイムアウト
        }
    }
    return buf
}

// containsBytes は haystack が needle を含むか確認する
func containsBytes(haystack []byte, needle string) bool {
    h := string(haystack)
    n := needle
    for i := 0; i+len(n) <= len(h); i++ {
        if h[i:i+len(n)] == n {
            return true
        }
    }
    return false
}
```

### テストケース

```go
func TestGetRoot(t *testing.T) {
    addr, stop := startTestServer(t)
    defer stop()

    resp := sendHTTP(t, addr, "GET / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")

    if !containsBytes(resp, "HTTP/1.1 200 OK") {
        t.Errorf("expected 200 OK, got:\n%s", resp)
    }
    if !containsBytes(resp, "Hello, World!") {
        t.Errorf("expected body 'Hello, World!', got:\n%s", resp)
    }
}

func TestNotFound(t *testing.T) {
    addr, stop := startTestServer(t)
    defer stop()

    resp := sendHTTP(t, addr, "GET /no-such-path HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")

    if !containsBytes(resp, "HTTP/1.1 404") {
        t.Errorf("expected 404, got:\n%s", resp)
    }
}

func TestMethodNotAllowed(t *testing.T) {
    addr, stop := startTestServer(t)
    defer stop()

    resp := sendHTTP(t, addr, "DELETE / HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")

    if !containsBytes(resp, "HTTP/1.1 405") {
        t.Errorf("expected 405, got:\n%s", resp)
    }
    if !containsBytes(resp, "Allow:") {
        t.Errorf("expected Allow header in 405 response, got:\n%s", resp)
    }
}

func TestPostWithBody(t *testing.T) {
    addr, stop := startTestServer(t)
    defer stop()

    body := `{"name":"Alice"}`
    req := "POST /api/users HTTP/1.1\r\n" +
        "Host: localhost\r\n" +
        "Content-Type: application/json\r\n" +
        "Content-Length: " + itoa(len(body)) + "\r\n" +
        "Connection: close\r\n" +
        "\r\n" +
        body

    resp := sendHTTP(t, addr, req)

    if !containsBytes(resp, "HTTP/1.1 201") {
        t.Errorf("expected 201, got:\n%s", resp)
    }
}

// itoa は int を文字列に変換する（strconv.Itoa の代替）
func itoa(n int) string {
    if n == 0 {
        return "0"
    }
    buf := make([]byte, 0, 10)
    for n > 0 {
        buf = append([]byte{byte('0' + n%10)}, buf...)
        n /= 10
    }
    return string(buf)
}
```

### テスト実行

```bash
cd src
go test ./... -v
go test ./... -v -run TestGetRoot  # 特定のテストのみ
```

---

## 5. ネットワークエラーの種類と対処

```go
import "io"

func handleReadError(err error) {
    if err == nil {
        return
    }

    // EOF: クライアントが接続を切った（正常終了）
    if err == io.EOF {
        log.Println("client disconnected")
        return
    }

    // タイムアウト
    if isTimeout(err) {
        log.Println("connection timed out")
        return
    }

    // その他のネットワークエラー
    log.Printf("network error: %v", err)
}
```

---

## 6. グレースフルシャットダウン

Ctrl+C を押したとき、処理中のリクエストを完了してから終了する。

```go
import (
    "os"
    "os/signal"
    "syscall"
)

func main() {
    router := setupRouter()

    ln, err := net.Listen("tcp", ":8080")
    if err != nil {
        log.Fatal(err)
    }

    // SIGINT(Ctrl+C) / SIGTERM を受け取るチャンネル
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

    go func() {
        <-quit
        log.Println("Shutting down...")
        ln.Close() // Accept()がエラーを返しループが終わる
    }()

    log.Println("Server listening on :8080")
    for {
        conn, err := ln.Accept()
        if err != nil {
            log.Println("Server stopped:", err)
            return
        }
        go handleConnection(conn, router)
    }
}
```

---

## 今日のチェックリスト

- [ ] `defer conn.Close()` を全コードパスで保証できる
- [ ] `recover()` でpanicをキャッチして500を返せる
- [ ] `isTimeout()` でエラーの種類を判別できる
- [ ] `net.Listen("tcp", "127.0.0.1:0")` でテスト用サーバーを立てられる
- [ ] 生のHTTPリクエスト文字列を `conn.Write` で送るテストが書ける
- [ ] `go test ./... -v` でテストが通る

---

## 参考

- `go doc net.Error` — ネットワークエラーインターフェース
- `go doc testing` — テストパッケージ
- RFC 9110 §15 — HTTPエラーコードの正式な定義
