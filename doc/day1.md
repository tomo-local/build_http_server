# Day 1 — TCP/IP基礎 + Goのnetパッケージ

## 今日のゴール

- TCP/IPプロトコルスタックの仕組みを理解する
- `net.Listen` / `Accept` / `Read` / `Write` を使いこなす
- telnet/nc でバイト列レベルの通信を体験する

---

## 1. TCP/IPプロトコルスタック

ネットワーク通信は4層に分かれている。

```
アプリケーション層  HTTP, DNS, SMTP ...
      ↓↑
トランスポート層    TCP / UDP
      ↓↑
インターネット層    IP (アドレッシング・ルーティング)
      ↓↑
ネットワーク層      Ethernet, Wi-Fi (物理的な伝送)
```

### TCP の特徴（UDPとの違い）

| 特性 | TCP | UDP |
|------|-----|-----|
| 接続 | コネクション型（3-wayハンドシェイク） | コネクションレス |
| 信頼性 | 順序保証・再送あり | なし |
| 速度 | 遅め | 速い |
| 用途 | HTTP, SSH, FTP | DNS, 動画配信, ゲーム |

### 3-wayハンドシェイク

```
Client          Server
  |---SYN-------->|   「接続したい」
  |<--SYN-ACK-----|   「OK、受け取った」
  |---ACK-------->|   「確認した」
  |               |   ← コネクション確立
```

### ポートとソケット

- **ポート**: プロセスを識別する 0〜65535 の番号
- **ソケット**: `(IPアドレス, ポート番号)` のペア = 通信の端点
- **コネクション**: `(クライアントソケット, サーバーソケット)` の4タプル

---

## 2. GoのTCPソケットAPI

Goの `net` パッケージはOSのソケットAPIを薄くラップしている。
内部では `socket()`, `bind()`, `listen()`, `accept()` というシステムコールが呼ばれている。

```
net.Listen()  → socket() + bind() + listen()
ln.Accept()   → accept()
conn.Read()   → read()
conn.Write()  → write()
conn.Close()  → close()
```

### サーバー側の流れ

```go
// 1. リスナーを作る（OSにポートを予約する）
ln, err := net.Listen("tcp", ":8080")

// 2. コネクションを待つ（クライアントが来るまでブロック）
conn, err := ln.Accept()

// 3. データを読み書きする
buf := make([]byte, 1024)
n, err := conn.Read(buf)    // クライアントからの受信（nバイト読めた）
conn.Write([]byte("pong"))  // クライアントへの送信

// 4. 必ずクローズする
conn.Close()
```

### 重要な型

```go
// net.Listener — ポートを監視するオブジェクト
type Listener interface {
    Accept() (Conn, error)  // 次のコネクションを待つ
    Close() error
    Addr() Addr
}

// net.Conn — 1本のTCPコネクション（双方向のストリーム）
type Conn interface {
    Read(b []byte) (n int, err error)   // 受信バッファからbへコピー
    Write(b []byte) (n int, err error)  // bを送信バッファへコピー
    Close() error
    LocalAddr() Addr
    RemoteAddr() Addr
    SetDeadline(t time.Time) error
}
```

### TCPはストリーム

重要: TCPは「パケット」ではなく「バイトのストリーム」を提供する。
`Read()` が何バイト返すかは保証されない。

```go
// NG: 一度のReadで全部届くとは限らない
n, _ := conn.Read(buf)  // 期待100バイト → 実際に47バイトしか来ないことがある

// OK: 必要なバイト数が揃うまで読み続ける（後日自作する）
readFull(conn, buf, 100)
```

### 複数コネクションの並行処理

```go
for {
    conn, err := ln.Accept()
    if err != nil {
        log.Println(err)
        continue
    }
    go handleConnection(conn) // goroutineで並行処理
}
```

---

## 3. ハンズオン: エコーサーバー

受け取ったバイト列をそのまま返す最小サーバーを書く。

```go
package main

import (
    "log"
    "net"
)

func main() {
    ln, err := net.Listen("tcp", ":8080")
    if err != nil {
        log.Fatal(err)
    }
    defer ln.Close()
    log.Println("Listening on :8080")

    for {
        conn, err := ln.Accept()
        if err != nil {
            log.Println("accept error:", err)
            continue
        }
        go echo(conn)
    }
}

func echo(conn net.Conn) {
    defer conn.Close()

    buf := make([]byte, 1024)
    for {
        n, err := conn.Read(buf)
        if err != nil {
            return // EOF またはエラー
        }
        // 受け取ったn バイトをそのまま返す
        conn.Write(buf[:n])
    }
}
```

### 動作確認

```bash
# ターミナル1: サーバー起動
go run main.go

# ターミナル2: telnet で接続
telnet localhost 8080
# "hello" と入力 → "hello" が返ってくる

# または nc で接続
echo "hello world" | nc localhost 8080
```

---

## 4. 理解を深める実験

### 実験1: goroutine なしで複数クライアント

`go echo(conn)` を `echo(conn)` に変えると、2つ目のクライアントがブロックされる。
→ goroutine の必要性を体感する。

### 実験2: パケットを観察する

```bash
# ループバック (lo0) をキャプチャ
sudo tcpdump -i lo0 -A port 8080
```

SYN → SYN-ACK → ACK のハンドシェイクと実際のデータが見える。

### 実験3: クライアントのIPアドレスを表示

```go
func echo(conn net.Conn) {
    log.Printf("New connection from %s", conn.RemoteAddr())
    defer conn.Close()
    // ...
}
```

### 実験4: Read が分割されることを確認

```go
func echo(conn net.Conn) {
    defer conn.Close()
    buf := make([]byte, 4) // 意図的に小さいバッファ
    for {
        n, err := conn.Read(buf)
        if err != nil {
            return
        }
        log.Printf("Read %d bytes: %q", n, buf[:n])
        conn.Write(buf[:n])
    }
}
```

大きなデータを送ると複数回に分割されて届くことが確認できる。

---

## 5. よくあるミス

| ミス | 問題 | 対処 |
|------|------|------|
| `conn.Close()` を忘れる | ファイルディスクリプタのリーク | `defer conn.Close()` を最初に書く |
| `go` を忘れて直列処理 | 2つ目の接続がブロック | `go handleConnection(conn)` |
| `ln.Accept()` エラーで `log.Fatal` | 一時エラーでサーバー停止 | `log.Println` + `continue` |
| `Read` が全データを返すと思う | TCPはストリーム、分割される | ループして必要バイト数分読む |

---

## 今日のチェックリスト

- [ ] TCP 3-wayハンドシェイクを図で説明できる
- [ ] `net.Listen` / `Accept` / `Read` / `Write` の役割を説明できる
- [ ] エコーサーバーを書いて `telnet` で動作確認できた
- [ ] goroutine で複数クライアントを同時処理できた
- [ ] `Read()` が期待より少ないバイトを返すことを実験で確認できた

---

## 参考

- `go doc net` — net パッケージのドキュメント
- RFC 793 — TCP仕様
- `man 2 socket` — システムコールレベルのAPIドキュメント
