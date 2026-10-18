# NOE インクリメント11 待機API

> この文書は実装当時の計画・検証記録です。現在の仕様は[APIリファレンス](api.md)と[stdlib移行記録](increment-12.md)を参照してください。旧`noe.*` API、独自I/O実装、当時の対象外・中断動作は現行仕様ではありません。

リトライや処理間隔をJSで書けるよう、`noe.sleep(ms)`を追加する。Go 1.27.2・既存go-quickjs・共通イベントループを使い、依存は追加しない。状態は実装・テスト・セルフレビュー・総合E2E完了。

## APIと実装

`noe.sleep(ms)`は指定ミリ秒を待つPromise<void>を返す。引数は0以上の整数のnumberで、文字列などの暗黙変換はしない。number以外はTypeError、負数・小数・NaN・Infinity・Goのtime.Duration上限を超える値はRangeErrorでPromiseをrejectする。最大値は9223372036854ms。0もPromiseを返し、解決結果はundefined。

Go側では所有goroutineで値を検証し、共通startAsyncWorkからGoのtime.NewTimerで待機する。workerはJS値に触れず、Promiseの解決を所有goroutineへ届ける。成功・キャンセルのどちらでもtimerをStopする。変換前に範囲を確認し、durationの桁あふれで大きな待機が即時完了しないようにする。

待機中は他のJSやI/Oを処理でき、未awaitのsleepも通常終了前に待つ。`--timeout`は既存どおり実行全体を中断してコード124、`noe.exit()`は待機を中断して指定コードで終了する。REPLのCtrl+Cではその入力のtimerと遅延completionを取り消し、変数・前回結果を保持して次の入力へ戻る。

## 作業範囲と受入条件

| 範囲 | 受入条件 |
| --- | --- |
| APIとtimer | 正のミリ秒の実時間待機、0、undefinedの解決結果、未await完了待ち、不正入力のPromise reject、duration上限の検証を確認 |
| キャンセルと実用例 | await有無のtimeout、明示終了、入力中断後の続行・遅延JS抑止、sleep中のHTTPハンドラーと別リクエストの並行処理を確認。sleep-demo.jsを実CLIで実行 |

小さな既存基盤への追加として、実装・テスト・通常／批判的観点のセルフレビューを親agentが行う。通常観点はAPIと実用例、批判的観点はduration変換、JS所有goroutine、timer解放、キャンセル後の続行抑止を確認する。

検証は対象runtimeテスト、実バイナリE2E、Linux PTYのCtrl+C、runtime/CLIのrace、CGO無効ビルド、go vet、最後の総合E2Eで行う。正の待機はJSが待機を開始した時刻から測り、実行時間の上限による速さの要求は置かない。最大durationは即座に明示終了して受理と取消しを確認し、実時間を待たない。

## 実用例と対象外

`examples/sleep-demo.js`は1秒間隔で3回表示し、正常終了する。無限の定期実行はスクリプト自身のループで書けるが、この例は有限にする。

setTimeout・setInterval・clearTimeout、timer ID、コールバック式のtimer、AbortSignal、リクエスト単位のJS中断は追加しない。TypeScriptと名称変更は保留、npm・Node/Deno互換は引き続き実装しない。

## 検証記録

対象runtimeテストと実CLIのsleep E2Eが成功。型・数値範囲・最大値の受理、0、実時間待機、未await完了、timeout、明示終了、REPLエラー回復、PTYのCtrl+Cと前回結果保持、HTTPハンドラーの並行処理を確認した。セルフレビューで、millisecondsの変換前チェック、workerのJS非参照、取消し時のtimer解放とcompletion抑止を確認し、必須修正なし。

HTTP中断時はGoが空の200を返してから接続を閉じる場合があるため、E2Eは通信エラーだけを必須とせず、Go側の待機が解放され、終了後のJSやハンドラーbodyが実行・送信されないことを確認する。

既存CLI/API/REPL/端末操作を含む最後の総合E2E、runtime/CLIのrace、CGO無効ビルド、go vetが成功。直下のnoeを更新した。検証環境はGo 1.27.2、Linux amd64。

```sh
go test ./internal/runtime -run '^TestSleep' -count=1 -v
go test -tags=e2e ./tests -run '^TestSleep' -count=1 -v
CGO_ENABLED=1 go test -race ./internal/... -count=1
CGO_ENABLED=0 go build ./...
go vet -tags=e2e ./...
go test -tags=e2e ./... -count=1 -v
CGO_ENABLED=0 go build -o noe ./cmd/noe
git diff --check
```

## 参照

- [製品方針とMVP](mvp.md)
- [共通イベントループ](increment-10.md)
- [API](api.md)
