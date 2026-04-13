# Day 3 — HTTPレスポンスビルダー + ルーター & ハンドラー

## 今日のゴール

- HTTP レスポンスをバイト列として組み立てて送信できる
- 自作の JSON ビルダーで構造体をシリアライズできる
- パス・メソッドでハンドラーを振り分けるルーターを実装できる

---

## 1. HTTP/1.1 レスポンスの構造

```
HTTP/1.1 200 OK\r\n
Content-Type: text/plain; charset=utf-8\r\n
Content-Length: 13\r\n
\r\n
Hello, World!
```

```
[ステータスライン]
HTTP/1.1 200 OK\r\n
    ^      ^   ^
    |      |   理由フレーズ（テキスト）
    |      ステータスコード（3桁）
    HTTPバージョン

[レスポンスヘッダー]
Key: Value\r\n

[空行] ← ヘッダー終端
\r\n

[ボディ] (バイナリでもテキストでも可)
```

### よく使うステータスコード

| コード | 意味 | 使いどき |
|--------|------|----------|
| 200 OK | 成功 | GETで取得成功 |
| 201 Created | 作成成功 | POSTでリソース作成 |
| 204 No Content | 成功（ボディなし） | DELETEなど |
| 400 Bad Request | クライアントエラー | パラメータ不正 |
| 404 Not Found | リソースなし | パスに対応なし |
| 405 Method Not Allowed | メソッド不可 | パスはあるがメソッドが違う |
| 500 Internal Server Error | サーバーエラー | 予期しないpanic等 |

---

## 2. Response構造体とバイト列生成

レスポンスを `[]byte` として組み立てる。
文字列連結は都度アロケーションが起きるので、`[]byte` に `append` していく。

```go
package main

import (
    "net"
    "strconv"
)

type Response struct {
    StatusCode int
    Headers    map[string]string
    Body       []byte
}

func NewResponse(statusCode int) *Response {
    return &Response{
        StatusCode: statusCode,
        Headers:    make(map[string]string),
    }
}

func (r *Response) SetText(body string) *Response {
    r.Body = []byte(body)
    r.Headers["Content-Type"] = "text/plain; charset=utf-8"
    return r
}

func (r *Response) SetJSON(body []byte) *Response {
    r.Body = body
    r.Headers["Content-Type"] = "application/json"
    return r
}

// Bytes はHTTPレスポンス全体をバイト列として組み立てる
func (r *Response) Bytes() []byte {
    var buf []byte

    // ステータスライン: "HTTP/1.1 200 OK\r\n"
    buf = append(buf, "HTTP/1.1 "...)
    buf = strconv.AppendInt(buf, int64(r.StatusCode), 10)
    buf = append(buf, ' ')
    buf = append(buf, statusText(r.StatusCode)...)
    buf = append(buf, '\r', '\n')

    // Content-Length を自動セット
    r.Headers["Content-Length"] = strconv.Itoa(len(r.Body))

    // ヘッダー: "Key: Value\r\n"
    for k, v := range r.Headers {
        buf = append(buf, k...)
        buf = append(buf, ':', ' ')
        buf = append(buf, v...)
        buf = append(buf, '\r', '\n')
    }

    // 空行
    buf = append(buf, '\r', '\n')

    // ボディ
    buf = append(buf, r.Body...)

    return buf
}

func statusText(code int) string {
    switch code {
    case 200:
        return "OK"
    case 201:
        return "Created"
    case 204:
        return "No Content"
    case 400:
        return "Bad Request"
    case 404:
        return "Not Found"
    case 405:
        return "Method Not Allowed"
    case 500:
        return "Internal Server Error"
    default:
        return "Unknown"
    }
}
```

---

## 3. 自作 JSON ビルダー

`encoding/json` を使わず、よく使うパターンのJSON文字列を手で組み立てる。
JSONは単純なテキストフォーマットなので、基本的なケースは自分で書ける。

### JSONフォーマットのおさらい

```
オブジェクト: {"key": "value", "count": 42}
配列:         ["Alice", "Bob", "Charlie"]
文字列:       "Hello\nWorld"  ← 特殊文字はバックスラッシュエスケープ
数値:         42 / 3.14
真偽値:       true / false
null:         null
```

```go
// jsonString は Go の string を JSON文字列リテラルに変換する
// 例: `He said "hi"` → `"He said \"hi\""`
func jsonString(s string) []byte {
    buf := make([]byte, 0, len(s)+2)
    buf = append(buf, '"')
    for i := 0; i < len(s); i++ {
        c := s[i]
        switch c {
        case '"':
            buf = append(buf, '\\', '"')
        case '\\':
            buf = append(buf, '\\', '\\')
        case '\n':
            buf = append(buf, '\\', 'n')
        case '\r':
            buf = append(buf, '\\', 'r')
        case '\t':
            buf = append(buf, '\\', 't')
        default:
            if c < 0x20 {
                // 制御文字は \uXXXX 表記
                buf = append(buf, '\\', 'u', '0', '0',
                    hexChar(c>>4), hexChar(c&0xf))
            } else {
                buf = append(buf, c)
            }
        }
    }
    buf = append(buf, '"')
    return buf
}

func hexChar(n byte) byte {
    if n < 10 {
        return '0' + n
    }
    return 'a' + n - 10
}

// JSONObject はキーと値のペアからJSONオブジェクトを組み立てる
// 値は既にJSON形式のバイト列として渡す
//
// 例:
//   JSONObject(
//       "name", jsonString("Alice"),
//       "age",  []byte("30"),
//   )
//   → {"name":"Alice","age":30}
func JSONObject(kvs ...interface{}) []byte {
    buf := []byte{'{'}
    first := true
    for i := 0; i+1 < len(kvs); i += 2 {
        key := kvs[i].(string)
        val := kvs[i+1].([]byte)
        if !first {
            buf = append(buf, ',')
        }
        buf = append(buf, jsonString(key)...)
        buf = append(buf, ':')
        buf = append(buf, val...)
        first = false
    }
    buf = append(buf, '}')
    return buf
}

// JSONArray は値の配列からJSON配列を組み立てる
func JSONArray(items ...[]byte) []byte {
    buf := []byte{'['}
    for i, item := range items {
        if i > 0 {
            buf = append(buf, ',')
        }
        buf = append(buf, item...)
    }
    buf = append(buf, ']')
    return buf
}

// JSONInt は整数をJSONのバイト列に変換する
func JSONInt(n int) []byte {
    return strconv.AppendInt(nil, int64(n), 10)
}

// JSONBool は真偽値をJSONのバイト列に変換する
func JSONBool(b bool) []byte {
    if b {
        return []byte("true")
    }
    return []byte("false")
}
```

### 使い方

```go
// {"status":"ok"}
body := JSONObject(
    "status", jsonString("ok"),
)

// {"id":1,"name":"Alice","active":true}
body = JSONObject(
    "id",     JSONInt(1),
    "name",   jsonString("Alice"),
    "active", JSONBool(true),
)

// [{"id":1,"name":"Alice"},{"id":2,"name":"Bob"}]
user1 := JSONObject("id", JSONInt(1), "name", jsonString("Alice"))
user2 := JSONObject("id", JSONInt(2), "name", jsonString("Bob"))
body = JSONArray(user1, user2)

res := NewResponse(200).SetJSON(body)
```

---

## 4. ルーターの実装

### HandlerFunc 型

```go
// ハンドラーの型: Requestを受け取り、Responseを返す
type HandlerFunc func(req *Request) *Response
```

### Router 構造体

```
router.routes = {
    "/":           { "GET": handler1 },
    "/api/users":  { "GET": handler2, "POST": handler3 },
    "/api/health": { "GET": handler4 },
}
```

```go
type Router struct {
    routes map[string]map[string]HandlerFunc
}

func NewRouter() *Router {
    return &Router{routes: make(map[string]map[string]HandlerFunc)}
}

func (r *Router) Handle(method, path string, h HandlerFunc) {
    if r.routes[path] == nil {
        r.routes[path] = make(map[string]HandlerFunc)
    }
    r.routes[path][toUpper(method)] = h
}

func (r *Router) Dispatch(req *Request) *Response {
    methods, pathExists := r.routes[req.Path]
    if !pathExists {
        return NewResponse(404).SetText("404 Not Found: " + req.Path)
    }

    h, methodExists := methods[req.Method]
    if !methodExists {
        // 登録済みメソッドをAllow ヘッダーに列挙（RFC要件）
        allowed := joinKeys(methods)
        res := NewResponse(405).SetText("405 Method Not Allowed")
        res.Headers["Allow"] = allowed
        return res
    }

    return h(req)
}

// toUpper はASCII文字を大文字に変換する
func toUpper(s string) string {
    b := make([]byte, len(s))
    for i := 0; i < len(s); i++ {
        c := s[i]
        if c >= 'a' && c <= 'z' {
            c -= 'a' - 'A'
        }
        b[i] = c
    }
    return string(b)
}

// joinKeys はmapのキーをカンマ区切りで連結する
func joinKeys(m map[string]HandlerFunc) string {
    buf := make([]byte, 0, 32)
    first := true
    for k := range m {
        if !first {
            buf = append(buf, ',', ' ')
        }
        buf = append(buf, k...)
        first = false
    }
    return string(buf)
}
```

---

## 5. 全体をつなぐ

```go
package main

import (
    "log"
    "net"
)

func main() {
    router := NewRouter()

    router.Handle("GET", "/", func(req *Request) *Response {
        return NewResponse(200).SetText("Hello, World!")
    })

    router.Handle("GET", "/api/health", func(req *Request) *Response {
        body := JSONObject("status", jsonString("ok"))
        return NewResponse(200).SetJSON(body)
    })

    router.Handle("GET", "/api/users", func(req *Request) *Response {
        u1 := JSONObject("id", JSONInt(1), "name", jsonString("Alice"))
        u2 := JSONObject("id", JSONInt(2), "name", jsonString("Bob"))
        return NewResponse(200).SetJSON(JSONArray(u1, u2))
    })

    router.Handle("POST", "/api/users", func(req *Request) *Response {
        // ボディをそのまま返す（エコー）
        res := NewResponse(201)
        res.Headers["Content-Type"] = "application/json"
        res.Body = req.Body
        return res
    })

    ln, err := net.Listen("tcp", ":8080")
    if err != nil {
        log.Fatal(err)
    }
    defer ln.Close()
    log.Println("Server listening on :8080")

    for {
        conn, err := ln.Accept()
        if err != nil {
            log.Println("accept error:", err)
            continue
        }
        go handleConnection(conn, router)
    }
}

func handleConnection(conn net.Conn, router *Router) {
    defer conn.Close()

    req, err := parseRequest(conn)
    if err != nil {
        log.Println("parse error:", err)
        return
    }

    res := router.Dispatch(req)
    if _, err := conn.Write(res.Bytes()); err != nil {
        log.Println("write error:", err)
    }
}
```

---

## 6. 動作確認

```bash
# 正常系
curl -v http://localhost:8080/
curl -v http://localhost:8080/api/health
curl -v http://localhost:8080/api/users

# POSTリクエスト
curl -v -X POST \
  -H "Content-Type: application/json" \
  -d '{"name":"Charlie"}' \
  http://localhost:8080/api/users

# 404
curl -v http://localhost:8080/not-found

# 405: GETしか登録していないパスにDELETE
curl -v -X DELETE http://localhost:8080/api/health
```

### curlの `-v` で見るべきポイント

```
< HTTP/1.1 200 OK
< Content-Type: text/plain; charset=utf-8
< Content-Length: 13
<
Hello, World!
```

---

## 7. よくあるミス

| ミス | 問題 | 対処 |
|------|------|------|
| `Content-Length` を手動計算 | ボディ変更のたびに更新忘れ | `Bytes()` 内で自動計算 |
| JSONの特殊文字をエスケープしない | 壊れたJSON / XSS | `jsonString()` を必ず通す |
| 405でAllow ヘッダーなし | クライアントが何を送ればいいか不明 | `Allow: GET, POST` を返す（RFC要件）|
| `conn.Write()` のエラーを無視 | 送信失敗に気づかない | エラーをログに出す |

---

## 今日のチェックリスト

- [ ] `[]byte` への `append` でHTTPレスポンスを組み立てられる
- [ ] `jsonString()` でエスケープを正しく処理できる
- [ ] `JSONObject()` / `JSONArray()` で構造化データを組み立てられる
- [ ] `Router.Dispatch()` でパス・メソッドごとにハンドラーを呼べる
- [ ] 404 / 405 を正しく返せる

---

## 参考

- RFC 7231 — HTTP/1.1 セマンティクス（ステータスコードの定義）
- RFC 8259 — JSON
- `go doc strconv.AppendInt` — バッファへの整数書き込み
