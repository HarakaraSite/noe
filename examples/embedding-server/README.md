# Goホストにstdlibを組み込んで常駐させる

[実行例一覧](../README.md) · [最小の組み込み例](../embedding/README.md) · [Go組み込みガイド](../../docs/go-embedding.md)

GoのHTTPサーバーを起動し、リクエストごとに公開APIの `stdlib.Execute` でJavaScriptを実行する例です。Goプロセスは常駐しますが、**JavaScriptのVM・モジュールキャッシュ・環境変数・出力バッファは毎回新しく作ります**。NOEのCLI、agent Host、provider認証、外部サービスは不要です。

HTTP待受はGoの `net/http` が担当します。JS側でHTTPサーバーを起動したり、同じVMを複数リクエストで共有したりする例ではありません。

## 起動と確認

Go 1.27.2以降を使い、リポジトリのルートで実行します。Cツールチェーンは不要です。

```sh
CGO_ENABLED=0 go run ./examples/embedding-server

# 待受アドレスとJS実行期限を明示する場合（以下が既定値）
CGO_ENABLED=0 go run ./examples/embedding-server \
  -addr 127.0.0.1:8080 -request-timeout 5s
```

別の端末で確認します。

```sh
curl -fsS http://127.0.0.1:8080/health
# ok

curl -fsS -H 'Content-Type: application/json' \
  -d '{"values":[7,11,13]}' http://127.0.0.1:8080/sum
# {"stdout":"{\"count\":3,\"total\":31}\n","stderr":"","exitCode":0}
```

`POST /sum` は数値配列を含むJSONオブジェクトを1個受け取り、固定のJSで合計します。空配列も使用できます。入力は1 MiB以下です。Goで配列をJSONへ変換して `Environment.Env["INPUT_JSON"]` に渡し、JSで `JSON.parse` します。入力をJSコードへ文字列連結しません。

レスポンスの `stdout` と `stderr` は実行終了後の文字列で、ストリーミングではありません。通常はHTTP 200です。不正入力は400、JS実行期限切れは504、実行キャンセルは503、JS例外・非ゼロ終了コードは500です。非ゼロ終了コードの場合もJSONの `exitCode` で値を確認できます。JS例外などの詳細はホストのログへ出します。

## 実行と終了処理の流れ

1. ハンドラがリクエスト専用の `Profile` と出力バッファを作ります。
2. `r.Context()` からJS実行期限を持つcontextを作り、`Execute` へ渡します。クライアント切断・期限切れで実行をキャンセルします。
3. `Execute` がVMの作成・実行・終了処理を同じ所有goroutineで行います。レスポンスにはGoの文字列と終了コードだけを使い、JS値を持ち出しません。
4. SIGINT／SIGTERMを受けると、サーバーの `BaseContext` 経由で実行中のJSもキャンセルします。`http.Server.Shutdown` で受付を停止し、ハンドラとVMの終了処理を待ちます。
5. 終了待ちには、キャンセル済みcontextとは別の10秒の期限を使います。期限を超えた場合は接続を閉じ、エラー終了します。

`-request-timeout` はJSON入力を読み終えた後のJS実行期限です。HTTP入力の読取りには別途10秒の `ReadTimeout`、ヘッダーには5秒の期限を設定しています。

手元ではCtrl+Cで停止できます。SIGTERMやsystemdからの停止を確かめる場合は、`go run` のラッパーではなく、ビルドした実行ファイルを直接起動してください。

```sh
CGO_ENABLED=0 go build -o /tmp/noe-embedding-server ./examples/embedding-server
/tmp/noe-embedding-server -addr 127.0.0.1:8080
# 別端末から、この実行ファイルのPIDに kill -TERM <PID> を送る
```

## Linuxでsystemdユーザーサービスとして動かす

以下はsystemdのユーザーマネージャーが使えるLinux向けです。アプリ自身はdaemon化せず、**フォアグラウンドで動く実行ファイルをsystemdが管理**します。root権限やシステム全体のunitは不要です。

リポジトリのルートでビルド・配置します。

```sh
mkdir -p "$HOME/.local/bin" "$HOME/.local/share/noe-embedding-server" \
  "$HOME/.config/systemd/user"
CGO_ENABLED=0 go build -o "$HOME/.local/bin/noe-embedding-server" ./examples/embedding-server

cat > "$HOME/.config/systemd/user/noe-embedding-server.service" <<'EOF'
[Unit]
Description=NOE stdlib embedding example

[Service]
Type=simple
WorkingDirectory=%h/.local/share/noe-embedding-server
ExecStart=%h/.local/bin/noe-embedding-server -addr 127.0.0.1:8080 -request-timeout 5s
Restart=on-failure
RestartSec=2s
TimeoutStopSec=15s
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
EOF

systemctl --user daemon-reload
systemctl --user enable --now noe-embedding-server.service
systemctl --user status noe-embedding-server.service
```

手動起動のプロセスが8080番ポートを使用していれば、先に停止してください。サービスはログインユーザーの権限で動き、作業ディレクトリは上記 `WorkingDirectory` です。この例のJSにはホストの環境変数を継承せず、`INPUT_JSON` だけを渡します。認証情報の設定は不要です。

```sh
# ログの確認（終了するにはCtrl+C。サービス自体は停止しない）
journalctl --user -u noe-embedding-server.service -f

# 停止。SIGTERMによるJSキャンセルと終了処理が走る
systemctl --user stop noe-embedding-server.service

# 再開
systemctl --user start noe-embedding-server.service

# 自動起動を解除して停止
systemctl --user disable --now noe-embedding-server.service
```

`TimeoutStopSec=15s` はアプリの10秒の終了待ちより長くしています。`Restart=on-failure` は失敗終了時の再起動で、明示的な `systemctl stop` では再起動しません。

ユーザーサービスのログアウト後の継続や未ログイン時の起動は、ホストのlinger設定に依存します。必要なら管理方針を確認し、`loginctl enable-linger "$USER"` を設定します（環境によって管理者の許可が必要です）。

## 別のGo moduleで使う

`main.go` と `go.mod.template` をリポジトリ外のディレクトリへコピーし、テンプレートを `go.mod` に変更します。`replace` をNOEのcheckoutの絶対パスへ書き換えて、次を実行します。

```sh
GOWORK=off go mod tidy
GOWORK=off CGO_ENABLED=0 go run .
```

テンプレートの `v2.0.0-dev.0` はローカル置換専用です。公開タグを使う場合は `replace` を外して `v2.0.0` を指定します。詳しくは[v2移行ガイド](../../docs/v2-migration.md)を参照してください。

## 確認用テスト

リポジトリのルートで実行します。

```sh
go test ./examples/embedding-server -count=1
```

並行リクエストの入力・出力分離、新しいVMでの実行、例外と終了コード、同期処理・非同期処理の期限切れ、リクエストのキャンセル、サーバー停止時の実行キャンセルとVM cleanup完了待ちを確認します。

## 制約

- trustedなローカル利用向けの例で、認証やTLSはありません。既定のloopback待受を維持してください。
- HTTPクライアントから任意のJSを受け取るAPIではありません。実行するコードはGo側の `sumSource` に固定しています。
- 選択する追加機能は `ProcessInfo` だけです。ファイル・コマンド・ネットワーク・Workerは有効にしていません。ただし、NOE全体としてサンドボックスを保証するものではありません。
- 数値計算はJavaScriptの `Number` によります。厳密な整数・金額計算用ではありません。
- リクエストごとにVMを作るため初期化コストがあります。同時実行数の制御、CPU・メモリ・出力の上限、永続化は実装していません。systemd管理にしても、本番運用や長期連続稼働の保証にはなりません。
