# NOE版stdlib：ステップ5・要求受信streamingの実装と検証記録

2026-10-19。[計画](stdlib-step5-receive-plan.md)に沿ってS5-A〜Dの実装・関連テスト・独立コードレビューと再レビュー、その後の総合テスト・実プロバイダの基本フローまで完了しました。[構造化結果](stdlib-step5-receive-results.json)にコマンド、比較、manifest、環境上の失敗と回復、実接続の課題・tool code・結果を保存しています。

## 結果と変更範囲

基点はv1.11.0公開後の`41e40d46d304a2f996e58459cd861d7d6a376d38`です。共通serveはheaders受信後にhandlerを呼び、`request.body`を読む需要に応じて本文を取得します。Goの独立clientがprefixだけを送り、JS handlerからの別経路ACKを待って残りとEOFを送る試験により、EOF前の受信を確認しました。

| 対象 | 実装 |
| --- | --- |
| serve.go・serve_body.go | 本文全量bufferを廃止。HWM 0のReadableStream、最大32 KiB・同時1件のnative read、bytesとEOF/errorの順序、prefix上限、owner上のPromise/Uint8Array生成 |
| 入力と応答 | Response producerによる入力読取りを応答送信まで許可。論理cancelと物理Read中断を分け、応答flush後に未読入力を回収。終了後のreadや遅延結果を拒否/破棄 |
| HTTP/1・Expect | Goのauto-drainを避け、未読入力時は接続を再利用しない。明示100、Goと同じExpect token判定、最終headersのflush通知後に応答pump開始。cancel済みreadは先行中断し、局所deadline由来のcontext取消しを応答失敗と分離 |
| 寿命とupgrade | 同期server.closeは最大5秒graceful後にforce、loop/runtimeのforceは独立。listenerとactive handler/readを回収。hijack後のHTTP入力cleanupを避け、socket引渡し待機中の終了も接続を閉じる |
| docs・例 | 英文の内蔵API docs、UPSTREAM、現行ガイドを更新。有限binary受信を集計するreceive-stream.jsを追加 |

global serve・`noe:http.serve`・`node:http`・`http`は同じ関数値です。有限本文のBody methods、移譲/clone/one-shot、応答streamingとWebSocketを共通実装で維持しました。GET/HEADはJS本文なし、既知の空POSTはnull、長さ不明で実際には空なら空streamです。受信Request.signalや新しいJS option/exportは追加していません。

Serve.MaxBodyBytesは0なら32 MiB、正ならprefixで正常EOF、負なら空本文という従来結果を保ちます。追加byteで超過判定せず、413にせず、受信Content-Lengthも書き換えません。fetch側の応答上限とは別の設定です。

## API比較と関連検証

変更前sourceから作った一時CLIと最終sourceの一時CLIで、main・Node形式Worker・孫Worker・Web Workerのinventoryが完全一致しました。globalsは119/119/119/129、各modeに26 Node module・41 topics/100 detailsです。既存export/alias・型同一性・process.versionも一致しています。CLIのserve docsと実providerのrun_jsで参照した同じ詳細本文も一致しました。

関連テストは298 case成功、既存process helperのみskip。重点E2Eは72 case成功・skipなしでした。その後のreview修正と総合確認には追加caseも含まれます。

- Content-Length/chunkedとも、本文を1 byteも送る前のhandler起動と、残り/EOF前のprefix読取りを独立client/barrierで確認。
- 実際のserveJS・Go Promise bridgeを通し、初期microtaskを進めたowner checkpointで、需要なし0 native call/0 read、1 pull1 read、32 KiB bufferを確認。Go goroutineの実行待ちを偽の0にしない同期計数を併用。
- binary、短いread、bytes+EOF/error、prefix上限、Body methods、二重消費、Request移譲・明示clone、malformed bodyのread reject、通常EOF後のkeep-aliveを確認。
- 未読/部分読取り/cap/cancel/handler errorの有限応答を、producerが開いたまま受信。Goの実際のBody.Read開始をbarrierで確認し、Expectの中断後も遅延応答後半まで配送。
- Expectの単一・反復comma・他token/大文字混在×direct/HWM0/HWM1/cancelを確認。HWM 0の入力sourceと、利用者source自身の先読みを区別。
- 同期closeの実際の5秒後force、graceful中のruntime force、root error・Worker terminate・CLI SIGINT/timeoutのpending read回収、接続終了とport再bind、既存WebSocketを確認。
- main/各Worker、CLI、fixture agent/run_jsの受信・保存・close・最終回答を確認。

最終CLIの追加smokeでは、GET/HEADのwire本文をJSへ公開しないこと、既知/不明の空POSTの形を確認しました。既定上限についてはContent-Lengthを32 MiB+1と宣言し、32 MiBだけ送って最後の1 byteを留保したまま、32 MiBの集計結果とHTTP 200を受信しました。元のContent-Lengthも保たれ、EOFや超過検出の追加byteを待っていません。

## 独立コードレビュー

[レビュー記録](stdlib-step5-receive-review.md)に採否と根拠を保存しました。新規の独立レビュアー`/root/stdlib_step5_code_review`が限定packet・計画・製品/テスト/docs/例14ファイルをread-only確認しました。初回P2はExpectの完全一致によるGo受理形式の取りこぼし1件、任意P3はJS需要の計数試験1件でした。

Expect token判定とraw HTTP caseを修正し、計数試験の同期も再修正して、再レビューでP0〜P3すべて0件になりました。総合raceで見つかった終了確認helperの誤判定も、変更helperだけの限定再確認でP0〜P3すべて0件です。各回の開始・終了で14/14のmanifest一致を確認しています。

最終製品・テスト・docs/例14ファイルのmanifest SHA256は`9e76911a840f90e8aaee200d90be7fc92f5abb8654aea024e4efd3f73b01ca1a`。終了確認helperの修正後、製品binaryは不変のまま、最終unit/race/E2E/vetと該当する6対象のruntime test compileを確認し直しました。

## 最終の総合テスト

環境はGo 1.27.2、Linux amd64。表は復旧・終了テスト修正後の最終結果です。Go一時領域は短いdisk path`/var/tmp/n5-baec`を使いました。

| 確認 | 結果 |
| --- | --- |
| 全体unit | `CGO_ENABLED=0 go test -json ./... -count=1 -timeout=180s`成功。1123 case、23 package成功・2 packageテストなし。wall 41.983秒 |
| internal race | `CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race -json ./internal/... -count=1 -timeout=300s`成功。1122 case、20 package成功・1 packageテストなし。wall 61.081秒 |
| 全CLI/fixture E2E | prepared MCP bundleを指定し、`CGO_ENABLED=0 go test -json -tags=e2e ./tests -count=1 -timeout=10m`成功。203 case・skipなし。wall 36.237秒 |
| vet | `CGO_ENABLED=0 go vet -tags=e2e ./...`成功 |
| CLI cross-build | Linux/Darwin/Windows × amd64/arm64、CGO無効の6件成功 |
| test cross-compile | 同じ6対象でstdlib/runtime/apidocs/E2Eの4 package、24件成功 |
| 変更境界 | go.mod/go.sum、固定engine v0.27.0、初期instructions、rootの配布binaryは不変。ステップ6・7は未実装 |

通常unitの既存skipは4つの実provider spike/diagnostic opt-inとprocess helper、raceの既存skipはprocess helperのみです。新しいskipや既存テストの期待値緩和はしていません。他OS/archはcompileのみで、実機確認とは扱いません。

## 総合確認中の失敗と回復

初回のcrossとraceは、`/tmp`のtmpfs容量不足でtest binaryのlinkに失敗しました。今回作成した大きな成果物をdisk上の`/home/agent/.cache/noe-stdlib-step5-baecbtxg/`へ移し、従来のartifact pathはsymlinkで維持しました。他の成果物や利用者ファイルは削除していません。

最初に選んだdisk上のGOTMPDIRは長すぎました。Go 1.27のtesting.TempDirはGOTMPDIRも使うため、既存Unix socket試験が107 byteのpath上限に当たりました。短い`/var/tmp/n5-baec`へ変更して解消し、nativeioの製品コード/テストやskipを変更していません。

同じraceで、新規Worker終了テストの「最初のReadでbytesが返ったら未終了」という誤判定を確認しました。buffer済みHTTP応答の到着は接続終了と両立するため、既存2秒deadline内のdrain→EOF/transport閉鎖へ修正しました。timeout失敗・port再bindは保持し、独立した限定再確認後に最終suiteを実行しました。これは製品不具合の修正ではありません。

追加CLI smokeの再bind確認では、Python側もGo listenerと同じSO_REUSEADDRを使い、終了済み接続のTIME_WAITをlistener残存と誤認しないようにしました。各失敗のlogと復旧前の結果をartifactへ保持しています。

## 実プロバイダの基本フロー

OpenCode Go / `deepseek-v4.1-flash`、effort autoで1課題を実行し、初回で成功しました。課題文はdocs参照を指示していません。NOE serverが独立Go clientからのbinary prefixを読み、別経路でACKを送り、clientが残りとEOFを解放する流れです。client結果を検証・report.jsonへ保存し、serverをcloseして英文の最終回答を返しました。

| 観点 | 結果 |
| --- | --- |
| provider/tool | provider4要求、tool3回、すべてexit 0、CLI exit 0。上限は6要求・115秒 |
| docsの自発参照 | docsを含むtool2回。catalog/HTTP概要とserve/fetch詳細の計4 lookupを自発実行 |
| 受信 | `[0,255,17,34,128,0,255]`、7 bytes、sum 689、HTTP 200。handlerが最初の3 bytesを読んでからACKし、clientはACK後に残り4 bytesを送った。prefixBeforeEOF=true |
| 保存・終了 | report.jsonが観測結果と一致、task completed・最終回答あり。受信listenerとfixtureの停止を親が確認 |
| 時間 | 21.598秒。再試や追加provider呼出しはなし |

実行したtool codeを親が確認し、ACKを本文EOF後へ遅らせていないことを確認しました。局所テストではheadersだけでdispatchすることも確認していますが、実provider課題は基本の受信フローに絞っています。製品コードはその後不変で、終了テストだけの修正を理由に実providerを再実行していません。認証情報は保存していません。

## 残る境界と引継ぎ

ステップ5は完了です。一般的な全二重通信は保証しません。特にNOE fetchは最終headersが来ると未完了uploadを止める契約のままです。Expectの応答pump gateは利用者sourceのstart/先行pullまで制御せず、既に始まったreadはbytes/EOF/cancelで解放する必要があります。HWM 0でstartでは入力を読まないproducerと`Response(request.body)`の保証範囲を英文docsへ記載しています。

背圧はNOE bridge内の1 read/32 KiBであり、clone/tee、source自身、Go HTTP/TCP/OSのbuffer全体の上限ではありません。入力cancelは応答producerを自動停止せず、物理Readの中断を応答終了まで遅らせる場合があります。

次はステップ6のAPI登録・資源管理境界の整理です。HTTP exchangeの入力/応答/接続の所有権と、graceful/force終了の区別を引き継ぎます。ステップ6・7、commit・push・release、root配布binary更新は行っていません。生ログ・一時CLI・比較・scriptsと実接続証跡は`/tmp/noe-stdlib-step5-20261019-baecbtxg/`、大きなbuild成果物は前記disk pathにあります。
