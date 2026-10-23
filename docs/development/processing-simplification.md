# 処理の冗長性整理

対象は v2.1.0 後のローカル変更。公開 API、所有権、キャンセルと履歴の整合性を保ち、処理の重複を減らす。コミット・公開・配置はこの作業に含めない。

## スライス

1. 画像参照の検証を data URL の生成から分離する。
2. コンテキスト準備で測定した履歴のサイズ・画像量・ブロック重みを再利用し、同一 JSON の比較を短絡する。
3. CLI 観測出力の I/O adapter を観測の寿命で再利用する。
4. ファイル観測の wait 中、同じ内容の再解析を省く。置換・切詰め・同サイズ書換えも検出する。
5. private SSE snapshot は不変 payload を共有し、外側の slice だけをコピーする。
6. タスク完了待ち前の未使用計算、profile 正規化中の不要コピーを除く。
7. 別スライス: 通常のモデル step で履歴を繰り返し JS → Go に転送する経路を整理する。
8. 別スライス: 型付き Go Client の JSON 往復を除き、既存の受付・権限・所有権・キャンセルを共通化する。

各スライスで関係する既存テストと契約確認を行い、最後に全体テスト、race、公開 consumer の検証を行う。外部プロバイダーの利用が必要なら、既存の利用許可の範囲を確認する。固定 fixture で確認できるものはそれを使う。

## 状態

8 スライスの実装と独立レビュー、および下記の追加依頼による Worker 終了待ち修正を完了。公開 API の変更はない。

- 通常の managed task は、begin / prepare / final prepare / model で履歴本文を再送せず、Go が保持する確定済み conversation を revision で参照する。古い revision と他 operation の参照は拒否する。トリム候補の承認、tool/final checkpoint の転送は残す。
- model service に渡す履歴 bytes はリクエスト所有のコピーにする。provider の opaque fields、通知履歴、steer、トリム、queue 継続は既存 fixture で確認した。
- Go Client は typed request を共通の受付へ渡す。入力と応答の snapshot、presence bits、構造化設定の検証、回答省略、不正 UTF-8 の従来 JSON と同じ正規化を維持する。
- Wait 観測は previous/scratch の二つの raw buffer を再利用する。未変更時もファイルを読み、完全一致なら JSON 解析を省く。mtime には依存しない。保持メモリはログサイズに比例する。

## 検証

以下は成功。

- `CGO_ENABLED=0 go test -tags=e2e ./... -count=1`
- `CGO_ENABLED=1 go test -race ./internal/... ./stdlib/... ./agent/... -count=1`
- slow integration の `TestGoClientLongModelAndToolWait`（モデルと tool の長時間待ち）
- `go vet -tags=e2e ./...`、6 release targets の `go list -export ./...`
- candidate build、engine smoke、HTTP/Worker/標準入出力等の local native fixture、`go mod verify`
- 外部 agent consumer の local replace / module artifact の portable / gohost 実行
- 全スライスの独立レビュー、typed UTF-8 追加差分の再レビュー。要対応 finding はない。

診断の一例（各5回、累計 allocation/回。RSS や end-to-end 測定ではない）:

| コンテキスト準備 | 変更前 | 変更後 |
| --- | ---: | ---: |
| 履歴 1 MiB | 10,974,312 bytes | 10,015,531 bytes |
| 画像 3 MiB | 33,645,156 bytes | 21,988 bytes |

画像は検証時の展開を除いた結果であり、実送信時の data URL 生成は必要なまま。

検証ログと比較用 probe は `/var/tmp/noe-processing-pE26Qi` に保存した。配布済み binary と version は変更していない。

## 初回検証で検出した既存問題

外部 stdlib consumer の artifact 実行が `execution ownership or initializer cleanup failed` で失敗した。診断を加えた同一 consumer を変更前 HEAD と変更後で比較した。

- 単独30回ずつは両方成功。
- 並行負荷では変更前100回中23回、変更後100回中22回失敗。全失敗で `initialized=2, cleaned=1`。stdout 内容と借用 stream の未 close は正常。
- consumer の Worker は `parentPort.postMessage(...); parentPort.close()` を行う。`internal/stdlib/messaging.go` の `portEnd.closed` は port の切断を `workerHandle.exited` に接続している。`internal/stdlib/worker.go` の `exited` は worker を管理 map から除くため、実際の Worker cleanup が終わる前に親の終了待ち対象から外れ得る。
- `endAll` 自体には `wh.done` の join があるが、既に map から除かれた Worker は待機対象に含まれない。今回の profile コピー削減に由来する回帰ではなく、変更前にも存在する終了通知・所有権の問題である。

この時点では外部 stdlib consumer を含む全検査が成功したとは扱わず、assert も緩和しなかった。以下は、この問題の修正を依頼された後の追加作業である。

## 追加修正: Worker の実終了を待つ

2026-10-23 の追加依頼に従い、port の切断通知と Worker の実終了を分離した。

- `parentPort.close()` による `msgClose` は、親の Worker 用 hidden queue と所有 handle を終了させない。
- Worker の online/error/exit は、切断後にも利用できる親 queue へ送る。以前の data message と同じ queue を使い、到着順を保つ。
- runtime・initializer cleanup が完了してから `msgWorkerExit` を送る。親は `wh.done` によって同時起動数の解放まで確認し、exit を通知して管理 map から除く。
- 親の終了では、実際に残っている handle を従来の `endAll` が join する。閉じた execution に JavaScript のイベントは送らない。

回帰テストでは initializer cleanup を channel で停止し、Worker の exit 待ちをする場合と、親の managed execution を終了する場合の両方で、cleanup 完了前に戻らないことを確認する。port を閉じた後の例外・明示終了コードと、それ以前の message の順序も確認する。

修正前の Worker 実装を overlay で戻す negative control は、上記の回帰テストで期待どおり失敗した。新しい実装では以下すべてが成功した。

- `CGO_ENABLED=0 go test -tags=e2e ./... -count=1`
- `CGO_ENABLED=1 go test -race ./internal/stdlib ./internal/jsexec ./stdlib -count=1`
- `go vet -tags=e2e ./...` と `git diff --check`
- 外部 stdlib consumer の local replace と module artifact の build / run
- 同じ artifact consumer を8並列で100回実行し、失敗0回（初回検証の変更前は23/100、終了待ち修正前は22/100）
- 上記 Worker 変更に限定した独立レビュー。要対応 finding はない。

追加修正の検証ログ、negative-control overlay、consumer artifact と反復実行結果は `/var/tmp/noe-worker-join-mudJVt` に保存した。コミット・公開・配置は行っていない。
