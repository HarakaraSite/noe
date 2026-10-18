# NOE インクリメント7 実行時間の制御

> **初期開発の履歴です。** 旧APIや当時の対象範囲・検証結果を含みます。現在の使い方は[利用ガイド一覧](../README.md)、記録の一覧は[archive](README.md)を参照してください。

> この文書は実装当時の計画・検証記録です。現在の仕様は[APIリファレンス](../api.md)と[stdlib移行記録](increment-12.md)を参照してください。旧`noe.*` API、独自I/O実装、当時の対象外・中断動作は現行仕様ではありません。

run/evalに任意の実行時間上限を追加する。長いJS評価や通信・標準入力待ちを、スクリプトを書き換えず終了できるようにする。既定では上限を設けない。Go 1.27.2・既存go-quickjs・CGO無効ビルドを維持し、依存を追加しない。状態は実装・テスト・レビュー・総合E2E完了。

## CLIの契約

```sh
noe run --timeout 5s script.js [args...]
noe eval --timeout=500ms 'while (true) {}'
```

- run/evalのコマンド名の後、スクリプト名・コードの前に`--timeout <duration>`または`--timeout=<duration>`を指定する。Goのtime.ParseDuration形式（例: 500ms、5s、1m）。正の値のみとし、空・不正・ゼロ・負・範囲外・重複はstderrの診断と終了コード2。
- 最初のスクリプト名・コード以降はオプション解析しない。runの後続引数は`--timeout`を含め、そのままnoe.argsへ渡す。先頭の`--`はオプション終端。`eval '-1'`など既存のコードを保つため、既知のtimeoutオプションだけを認識する。
- runのエントリソース読込み後、Runtime.Executeを呼ぶ直前に時間計測を開始する。初期化・JS評価・top-level await・開始した非同期I/Oと後続jobsの完了待ちを同じdeadlineで扱う。リクエストごとの時間制限は追加しない。
- 時間切れにより実行が失敗した場合、stderrに実行元と`execution timed out after <duration>`を表示し、コード124。成功0、通常の実行エラー1、使い方エラー2、noe.exitで指定した終了コードは従来どおり。正常完了後にタイマーが切れても成功や明示終了を時間切れへ変更しない。
- CLIへ渡された親contextのキャンセル・より早いdeadlineは従来のコード1を保持し、設定したtimeout自身の満了と区別する。
- REPLにはtimeoutオプションを追加しない。Ctrl+Cによる対話中断や既存SIGINTの終了コードも変えない。オプション未指定は従来どおり。

既存context経路でJSの割込み、Promise/jobs待ち、HTTPとstdin待ちの終了を扱う。自分のtimer由来の中断はcontext.WithTimeoutCauseのcauseで区別する。CLIのエントリソース読込みと同期OS syscallを強制停止する機能は追加しない（ローカルimportの読込みもOSの完了に依存する）。変更・stdoutの出力は巻き戻さない。stdinの借用Readerを閉じず、既存の終了時AsyncWorkキャンセルと遅延completion抑止を維持する。

## スライスと受入条件

| スライス | 範囲 | 完了条件 |
| --- | --- | --- |
| 1 | CLIオプション解析・実行context・終了コード・help・対象テスト | 両形式・duration検証・重複・終端・従来evalコード・run引数の保存を確認。JS無限ループと未await後続JSの無限ループを時間切れにする。期限内成功・通常エラー・明示終了・親contextキャンセルのコードを保持。必要なruntime変更は根拠がある場合のみ |
| 2 | README・実バイナリE2E | CPU評価、待機するローカルHTTP、EOFにならないstdin、未awaitの完了待ちを124で終了。HTTPリクエストのキャンセル、終了後のJS未実行、stdout/stderr、引数・既存CLI/REPL互換を確認 |

各スライスと計画を通常／批判的レビューする。オプションとスクリプト引数の境界、deadlineの分類、明示終了の優先、JS所有goroutine・context・I/Oの終了経路を独立確認する。初回15分、再確認は変更箇所・既存findingに限って一回15分以内。親agentがfinding採否と最終検証を担当する。

テストは一時ファイル・ローカルHTTPのみを使い、stdin pipeをEOFにせず待たせる。無限ループの確認には外側のプロセス期限も置く。極端に短い期限で実行開始を期待するテストは使わない。runtime/CLIのrace、CGO無効ビルド、go vetを確認し、全レビュー後の`go test -tags=e2e ./... -count=1 -v`で総合E2Eを行う。直下のnoeを更新する。

## 対象外

REPLの入力ごとの時間制限、HTTPオプション追加、AbortController、sleep/timer API、メモリ上限、実行サンドボックス、同期OS syscall強制停止、TypeScript、名称変更、npm・Node/Deno互換、Web全面互換は含めない。

## 実装・レビュー記録

| 段階 | 結果 |
| --- | --- |
| 計画 | 通常／批判的レビューで必須指摘なし。実装時の確認点だった親contextの早いdeadlineをコード1として保持することを明文化し、WithTimeoutCauseによる判別を採用 |
| スライス1 | CLI解析・context・124診断・helpを実装。duration両形式・境界・不正値・重複・終端、旧来のコード・run引数、成功・通常エラー・明示124・stdin待機でのtimeout・親deadlineの保持を確認。通常／批判的レビューで必須指摘なし。任意P3の空stderr期待の検証を補強し、対象テストと解消確認が成功 |
| スライス2 | README・実バイナリE2Eを追加。run/eval無限ループ、未await後続JSの無限ループ、await有無のHTTP待機・server側キャンセル、EOFのないstdin、SIGINT、正常終了・エラー・明示終了・引数・REPLオプション非対応を確認。通常／批判的レビューで必須指摘なし |
| 総合E2E | 全レビュー後、CLI・runtimeと既存API・REPL・端末操作を含む全E2Eが成功 |

runtime本体は変更せず、既存のAwait/RunJobsContext/drainとExecuteの終了時Closeによって時間切れとI/Oキャンセルを実現した。任意P3として、I/O完了後の未await .then内で無限ループする追加ケースが提案された。今回は同一contextを渡すRunJobsContextの経路確認と、未await I/O待機・後続JSループの実ケースで契約を確認しており、追加していない。実装上の未解消必須指摘はない。

検証環境はGo 1.27.2、Linux amd64。以下の確認が成功した。

```sh
go test ./internal/cli
CGO_ENABLED=0 go build -o /tmp/noe-increment7-cli ./cmd/noe
go test -tags=e2e ./tests -run '^TestExecutionTimeout' -count=1 -v
CGO_ENABLED=1 go test -race ./internal/... -count=1
go test -tags=e2e ./... -count=1 -v
go vet -tags=e2e ./...
CGO_ENABLED=0 go build -o noe ./cmd/noe
git diff --check
```

一時Go overlayでE2EのTestMainを実際のCLIのraceビルドへ切り替え、追加timeout E2Eも成功し、race検出なし。通常の全E2Eと直下のnoeはCGO無効でビルドする。overlayとログはrepositoryに追加しない。手元のnoeでも、500msの無限ループを標準エラー診断・コード124で終了することを確認した。

## 参照

- [製品方針・MVP](mvp.md)
- [インクリメント6](increment-6.md)
- [現在のCLI](../../README.md#build-and-run)
