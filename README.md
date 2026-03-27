# HTTP Server 自作

`net/http` を使わず、`net` パッケージの TCP ソケット通信から HTTP/1.1 サーバーを自作するプロジェクト。

## 目的

- TCP ソケット通信の仕組みを理解する
- HTTP/1.1 プロトコルをバイトレベルで理解する
- Go の標準ライブラリ (`net`, `bufio`) の使い方を学ぶ

## 起動方法

```bash
cd src
go run main.go
```

## 動作確認

```bash
curl -v http://localhost:8080/
```

## ディレクトリ構成

```
.
├── doc/                  # ドキュメント
│   └── http_server_plan.md  # 学習プラン
└── src/                  # ソースコード
    └── main.go
```

## 学習プラン

[doc/http_server_plan.md](doc/http_server_plan.md) を参照。
