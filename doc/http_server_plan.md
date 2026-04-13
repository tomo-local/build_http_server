# HTTP サーバー自作 学習プラン（5日間）

`net/http` を使わず、TCP ソケットから HTTP サーバーをゼロから組み上げる。

---

## 全体の流れ

```
Day 1: TCP/IP基礎 + Goのnetパッケージ
         ↓ TCPエコーサーバーが動く
Day 2: HTTPプロトコル + リクエストパーサー
         ↓ curlのリクエストをパースできる
Day 3: レスポンスビルダー + ルーター
         ↓ パス・メソッドでハンドラーを振り分けられる
Day 4: 並行処理・エラーハンドリング・テスト
         ↓ 安定して動く & テストが通る
Day 5: Keep-Alive・ミドルウェア・TLS
         ↓ 本物のHTTPサーバーに近い完成度
```

---

## 日別ドキュメント

| 日 | ファイル | テーマ |
|----|----------|--------|
| Day 1 | [day1.md](day1.md) | TCP/IP基礎 + `net.Listen` / `Accept` |
| Day 2 | [day2.md](day2.md) | HTTP/1.1フォーマット + リクエストパーサー |
| Day 3 | [day3.md](day3.md) | レスポンスビルダー + ルーター & ハンドラー |
| Day 4 | [day4.md](day4.md) | 並行処理・タイムアウト・統合テスト |
| Day 5 | [day5.md](day5.md) | Keep-Alive・ミドルウェア・HTTPS |

---

## 最終的なアーキテクチャ

```
net.Listen(":8080")
        |
    ln.Accept()
        |
  go handleConnection(conn)
        |
  ┌─────────────────────┐
  │   Keep-Alive ループ  │
  │  parseRequest(conn) │  ← bufio.Reader
  │  router.Dispatch()  │  ← path + method
  │  Chain(middlewares) │  ← Logger, Recovery ...
  │    HandlerFunc()    │  ← ビジネスロジック
  │  res.Bytes() → Write│
  └─────────────────────┘
```

---

## 参考

- RFC 7230 — HTTP/1.1 メッセージ構文
- RFC 793 — TCP
- `go doc net` / `go doc bufio` / `go doc crypto/tls`
