# プロセス・標準I/O拡張の実装記録

2026-10-18。[実装計画](process-io-plan.md)に沿ったS1〜S5の実装・テスト・コードレビュー・総合検証を完了。stdlibの変更・export置換・prototype上書きは行わない。

## スライスの状態

| Slice | 状態 | 証拠・残作業 |
| --- | --- | --- |
| S1 | 完了 | 初回指摘を修正し、限定再レビューで必須指摘なし。hostioと所有者終了のfocused race test成功 |
| S2 | 完了 | 複数回通信、環境・cwd、終了・close・Worker環境を検証。レビューで必須修正なし |
| S3 | 完了 | 借用I/O・新旧出力・観察経路を検証。alias raceと明示abortを修正し、再レビューで必須指摘なし |
| S4 | 完了 | Worker、REPL、実run_jsのinterrupt/timeout/SIGINT、inherit/ignoreの回収、MCP SDK適合を確認。レビューで必須指摘なし |
| S5 | 完了 | 文書・全体レビュー・最終の総合テストが完了。必須指摘なし |

## S1の設計と検証

`internal/process`はPOSIXで現在のバイナリをGo専用の監督モードとして再実行する。予約引数と内部pipeのhandshakeで識別し、QuickJS・providerを起動しない。監督と対象は所有者と別のprocess groupを持つ。所有者のcontrol writerが閉じると対象groupを停止する。対象PID・終了値は別status pipeで返す。`Child.Wait`は対象の結果を受け取り、監督自身を回収してから完了する。WindowsではJobを使う。

Goテストの小さなtargetを使い、所有者のgroup全体へのSIGKILL後に対象・同groupの孫・監督が動作し続けないこと、read/write・EOF・実対象PID・別stderr・反復Wait/Closeを確認した。Linux上の参考値として、test executableによる5回のStart-to-ready中央値は約1.5 ms、監督RSSは約10 MiB、9回のStart/Close前後の所有者fd数は7で一定だった。これはテストバイナリの局所測定で、配布バイナリや他環境の性能値ではない。POSIXでは対象に加え監督1プロセスを使う。

`internal/hostio`は借用I/Oの取消しを扱い、Closeで元のreader/writerを閉じない。メモリI/O、明示的なContextReader/ContextWriter、OS別file adapterを使う。任意のblocking io.Reader/io.Writerを隠れたgoroutineで包まず、対応できない型は新しいcontext操作時にErrUnsupportedを返す。既存の同期I/Oは元の挙動を保つ。`SharedWriter`をNOE境界で一度作り、console・新Stream・Workerへ渡す。

`internal/nativeio`は既存のWeb Streamsで公開ストリームを構築し、stdinはhighWaterMark=0でread要求まで開始しない。Go処理はAsyncWorkで追跡し、Promise・Uint8Arrayの生成はruntime ownerで行う。終了回収はGoだけで完了し、JSの配送を待たない。`pipe()`のabortが待機read/writeを停止することを確認した。

## レビュー指摘と対応

| 指摘 | 対応・確認 |
| --- | --- |
| 監督のcontrol/status fdが対象へ継承される | helperで受領直後にCLOEXEC指定。対象が内部pipeを保有しない回帰テストを追加 |
| Windowsでsyscall開始直前のcancelを取り逃がす | CancelSynchronousIoのERROR_NOT_FOUNDで完了扱いにせず、処理完了まで取消しを再試行。開始前gateのテストを追加。native実行は未確認 |
| 複製fdのCLOEXEC指定までの間に並行spawnへ継承され得る | F_DUPFD_CLOEXECによる原子的な複製と回帰テストを追加 |
| Linux TTY/socketでPOLLOUT後の大きなblocking writeが停止しない | 文字デバイスは独立したnonblocking descriptor、socketはMSG_DONTWAITへ修正。canonical PTYで取消し・再利用・1 MiBの全文出力を確認 |
| 監督異常終了後、対象が残ってもCloseで停止できない | 所有者側fallbackの対象group停止と回帰テストを追加。監督の終了値は対象の結果へ代用しない |

起動ackを追加し、所有者が対象identityを取得する前に監督が即時終了の子をreapする競合を防いだ。25回の即時終了、起動途中のowner取消し、ack失敗時のpidfd解放を確認した。最終再レビューで既存指摘は解消した。

実NOEバイナリからtrueを10回spawnした参考値は起動成功まで2〜7 ms（中央値4 ms）。計測にはJSからnativeへの呼出し・ハンドル生成を含む。

## S2〜S4の確認

S2は公開spawnの検証・環境・cwd・Worker環境・終了・kill/close、agentの共通scope adapterをレビューし、必須指摘なし。inheritの転送先が詰まっても対象exitedが解決してcloseでpumpを取り消せることと、転送先のエラーをcloseが返すことも確認した。

S3ではstdout/stderrに同じ出力先を指定すると別lockで書くraceを修正した。元の出力先が同一なら同じSharedWriterを渡す。main/Workerの新ストリームと従来process.stdout/stderrの混合出力を同じbytes.Bufferでrace検査した。観察モードの明示stream abortは、イベントが0byteも書かれていなければ実行全体を取り消さず、JSが理由をcatchして継続できるよう修正した。記録完了後の表示取消しは記録のseqを維持して継続する。部分的に書かれたJSONLや実際の出力障害は永続エラーとする。実際にsinkが待機するfixture、継続後のconsole、終了コード、JSONLを検証した。記録commit後に表示が取り消される場合も、続くイベントのseqが重複しない回帰テストを追加した。

S4は開いたままのstdinへ応答、40 MiBのバイト一致、run/eval、Workerの環境・terminate、REPL同一入力と取消しを確認した。ローカルfixture providerからrun_jsを生成してspawnでshellと孫を起動し、公開interrupt_agentによるrun中断に加え、親CLIのtimeout＋inherit、SIGINT＋ignoreでも全停止と出力収集完了を確認した。

## 検証環境

Linuxでnative実行。Darwin・Windowsは現時点ではクロスビルドのみで、native動作確認済みとは扱わない。実provider・認証情報・有料APIは使わない。MCP SDKを用いる検証は通常のGoテストから分け、版・準備方法・結果を以下に記録した。Darwinのpipeはpoll後に小さい操作を行うため、同じ入力を外部のreaderと競合消費する場合まで取消しを保証するものではない。文字デバイスを独立して開き直せない環境や、取消し非対応の埋込みI/OはErrUnsupportedを返す。

## MCP SDK適合確認

[再現用ソースと手順](../scripts/spikes/process-io-mcp/README.md)を追加した。公式MCP SDK 1.32.1のClientをesbuild 0.25.12で一時bundle化し、NOEのJS transportとローカルJS serverへ接続した。SDK本体へのパッチ、MCP専用Go API、製品へのSDK依存は追加していない。初期化、tool一覧、3件の並行呼出し、270000 byteのUTF-8入力、EOF後の終了・回収が成功した。

## 総合検証

以下はLinux環境で成功。Darwin/Windowsのnative実行は含まない。

| 検証 | 結果 |
| --- | --- |
| `go test ./...` | 成功 |
| `go test -tags=e2e ./...` | 成功 |
| `go vet -tags=e2e ./...` | 成功 |
| `GORACE=atexit_sleep_ms=0 go test -race -p 1 ./internal/hostio ./internal/nativeio ./internal/process ./internal/processscope ./internal/runtime ./internal/agent ./internal/cli ./internal/events -count=1 -timeout=120s` | 成功 |
| `GORACE=atexit_sleep_ms=0 go test -race -tags=e2e ./tests -run '^TestProcessIO' -count=1 -timeout=90s` | 成功 |
| CGO_ENABLED=0、Linux/Darwin/Windowsのamd64/arm64ビルド | 6構成すべて成功 |
| process-worker / process-pipeline実行例 | 成功 |
| 公式MCP SDK適合確認 | 成功（上記参照） |

最初の全process race実行はテストスイート全体の90秒上限に達した。再実行する子のrace終了待機が累積するため、終了待機を`GORACE=atexit_sleep_ms=0`で無効化して上記の検査を行い成功した。race検出は有効のままである。担当側の既定設定によるprocess全体のrace実行も約93秒で成功した。

## レビュー結果

S1は所有関係・fd・各OSの取消しを独立レビューし、指摘修正後の限定再レビューで必須指摘なし。S2は公開process APIと既存agent接続、S3はI/O・観察writerを別々にレビューした。S3のalias・abort・記録済みseqの指摘を修正し、限定再レビューで必須指摘なし。S4/S5は別のレビュアーが実経路・応用例・文書を横断し、必須指摘なしと評価した。主担当が最終テスト結果と指摘対応を確認し、S1〜S5を完了とした。

並行した最終race・ビルド実行中、既存CLIテストの300 ms期限がfixture接続前に到達し、code 124ながらstarted条件を満たさない失敗が1回あった。ソースやテストの受入条件は変更せず、最終package raceは`-p 1`で直列実行し、すべて成功した。
