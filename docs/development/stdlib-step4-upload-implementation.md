# NOE版stdlib：ステップ4・要求送信streamingの実装と検証記録

2026-10-19。[計画](stdlib-step4-upload-plan.md)に沿ってS4-A〜Dの実装・関連テスト・独立コードレビューと再レビュー、その後の総合テスト・実プロバイダの基本フローまで完了しました。[構造化結果](stdlib-step4-upload-results.json)にコマンド・hash・skip・実接続の課題とtool codeを保存しています。

## 結果と変更範囲

基点は`0de9351072ceaaadae7063a30ab52448e93b1d58`です。`noe:http.fetch`と同じglobal fetchで、ReadableStream本文を全量収集せず送れます。Go製受信fixtureがprefixを読んだ通知を待ってproducerが残りを出す試験により、producer終了前の到達を確認しました。serveの受信bufferingは変更していません。

| 対象 | 実装 |
| --- | --- |
| fetch.go・fetch_upload.go | JS ownerでbytesをcopyし、Go pipeへ最大1chunkずつ引渡し。write完了後に次を読む。streamは長さ不明・GetBodyなし。有限本文の既知長/再送経路とnil Loopのbufferingは維持 |
| RequestとStreams | Request入力とoverrideで本文を落とさず、明示cloneだけtee。移譲はHWM 0のproxyで元streamをlock。read/cancel済み状態を記録し、reader解放で本文が再使用可能になるのを防止 |
| 中断とResponse | 正常EOF/早期応答はwriterを正常close。実失敗/abort/runtime終了はcontext取消しとpipe両端close。早期Responseの後半を読め、source.cancelが未完了でもGo IOを回収 |
| timeoutとsignal | Client.Timeoutをupload contextにも接続し、初回chunk待機のBody.Readを解除。Signal listenerはResponseのEOF/cancel/errorまで接続し、終了後に解除。pre-abortは本文移譲前に判定 |
| docs・例 | 英文API docsとUPSTREAMを更新し、有限binary upload例を追加。URLと任意outputを受け取りraw responseを保存。ガイドは受信bufferingと全二重の非保証を説明 |

streamの307/308は再送せずResponseを返し、301/302/303の本文なしへの遷移はGo clientへ任せます。bytes/string等のGetBody、Allow・custom Client/CheckRedirect、応答上限32 MiBのprefix/正常EOF、WebSocket・既存serveは維持しています。新しいexport・設定・依存・初期instructionsは追加していません。

## API比較と関連検証

変更前後sourceから別々の一時CLIをビルドし、同じinventoryをmain・Node形式Worker・孫Worker・Web Workerで実行しました。既存global・26 Node moduleのexport/alias・型の同一性・docs ID集合・process.versionは全modeで一致しました。各modeは41 topics・100 detailsです。新旧fetchの関数同一性は実行テストでも確認しています。

関連テストではprefix→ack→remainder/EOF、HWM 0とfake Transportによる背圧、offset付きUint8Array・空chunk、不正chunk/producer失敗、Requestのdirect/override/移譲/clone/used/locked、307/308と本文なしredirect、read/write待機abort、初回chunk待機timeout、pre-abort、Signalの寿命、Loop/runtime Closeを確認しました。

HTTP/1の早期Response試験はserver側EnableFullDuplexとflushでauto-drainを避け、upload停止をEOFで確認してから応答本文後半を送ります。短い応答が既にread bufferに入っていたため成功する試験にはしていません。これはfixture設定であり、NOEの全二重APIではありません。

runtimeの4mode・fixture agent/run_js・成果物保存/最終回答、root error・Worker terminate、CLIの開始済みuploadに対するSIGINT/timeoutも成功しました。最初の取消しfixtureは未読本文がある状態でContext.Doneだけを待ち、切断を観測できませんでした。受信側が本文を読み続けて接続終了を観測する形へ修正し、成功しています。

初回chunkが来ない場合、Go TransportがBody.Readの終了を待ち、Client.Timeout後もDoが戻らない経路を試験で確認しました。upload contextにもclientの期限を接続することで、DoのreturnやJS callbackを待たずpipeを解除できるようにしました。Go個別導入のnil Loopは従来の同期有限bufferingを維持します。

## 独立コードレビュー

`/root/stdlib_step4_code_review`が限定packet・指定source・実装/test/docs差分をread-onlyで確認しました。初回はP0:0、P1:0、P2:1、P3:0です。P2は「abort済みsignalをRequest入力へ指定すると、拒否前に本文を移譲してlockが残る」の1件でした。

本文移譲前の有効signal確認と3経路の回帰テストへ修正し、再レビューでP0〜P3はすべて0件です。[レビュー記録](stdlib-step4-upload-review.md)に採否と根拠を保存しています。

レビューした製品・テスト・API docs/例の15ファイルは総合テスト後も一致しました。manifest SHA256は`f7a8a037e95abde58ffaca63ac13f4c4c70e9fe3370afb2ff35602102dc2db92`です。計画・レビュー記録・index・段階案の状態更新は別の管理文書変更として扱います。

## レビュー後の総合テスト

環境はGo 1.27.2、Linux amd64です。

| 確認 | 結果 |
| --- | --- |
| 全体の単体テスト | `CGO_ENABLED=0 go test -json ./... -count=1 -timeout=180s`成功。23package成功、2packageはテストなし。wall 42.677秒 |
| internal全体race | `CGO_ENABLED=1 go test -race -json ./internal/... -count=1 -timeout=300s`成功。20package成功、1packageはテストなし。wall 97.978秒 |
| full CLI/fixture agent E2E | `CGO_ENABLED=0 go test -json -tags=e2e ./tests -count=1 -timeout=10m`成功。199 test case成功、skipなし。wall 31.145秒 |
| E2Eを含むvet | `CGO_ENABLED=0 go vet -tags=e2e ./...`成功 |
| 6対象CLIビルド | Linux/Darwin/Windows × amd64/arm64、CGO無効で6件成功 |
| test cross-compile | 同じ6対象でstdlib/runtime/apidocs/E2Eの4packageを個別compile、24件成功 |
| 変更境界 | 既存stdlib45ファイルを基点と比較し、変更はfetch.go・streams.go・UPSTREAM.md。go.mod/go.sum・engine依存・root配布binaryは不変 |

productionはローカルstdlibを使用し、上流stdlibのimportはありません。全E2Eには既存のprepared MCP SDK 1.32.1 bundleを渡しました。通常suiteの既存skipは子process helperと実provider spikeのopt-in caseだけで、新規skipや既存期待値の緩和はありません。native実行はLinux amd64、他対象はcompileのみです。

## 実プロバイダの基本フロー

OpenCode Go / `deepseek-v4.1-flash`、effort autoで同じ基本課題を2回試し、再試で完了しました。Go製loopback fixtureへbinary streamを送り、受信prefixのackを待って残りとEOFを出し、HTTP 200・bytes・prefixBeforeEOFを検証してreport.jsonへ保存し、英文の最終回答を返す課題です。どちらも課題文にdocs参照は指示していません。

初回はモデルがPOST完了を待ってからACKを取得するコードを書き、producerが進まず30秒でtimeoutしました。その後の診断で送信・保存には成功しましたが、provider6要求の上限に達し最終回答まで完了しませんでした。これは送信/ACKのモデル側の制御順序の誤りであり、製品コードは変更していません。失敗した初回のtool・結果も保存しています。

再試では「POSTを開始してその応答を待たずにACKを並行取得する」と課題文の順序を明確にしました。新しいfixtureで初回prefixを受信した後にACKを返し、producerがそのACK後に残りを出すことを確認しました。モデルは確認と成果物保存で2回uploadしています。harnessに誤って置いた「HTTP要求は厳密に1回」という条件は、1課題という受入条件に合わせて除き、実行済みのtool・受信・成果物・最終回答を再評価しました。追加のprovider呼出しはしていません。後続uploadのACKを独立した増分到達証拠とはせず、fresh fixtureの最初のuploadを証拠にしています。

| 再試の観点 | 結果 |
| --- | --- |
| provider/tool | provider4要求、tool3回、tool全件exit 0、CLI exit 0 |
| docsの自発参照 | noe:http概要を1回参照。実際のcodeとstdoutを構造化結果へ保存 |
| 通信の確認 | 受信bytesは`[0,255,17,34,128,0,255]`。最初のACK時にPOSTは未完了で、producerはACK後に残りとEOFを出した。保存reportも一致 |
| 完了と回収 | task completed・英文最終回答あり、親がfixtureを停止してlistener解放を確認 |
| 所要時間 | 16.549秒。各試行の上限はprovider6要求・CLI115秒。初回は59.614秒、計provider10要求 |

一時workspaceと既存認証を使い、API key・認証headerは保存していません。初回失敗、harnessの訂正、再試の成功、通常ローカル試験の成功は別々に記録しています。

## ステップ5への引継ぎ

要求送信streamingは完了しました。serveの受信は引き続き既存上限までbufferingし、受信streamingはステップ5です。早期Responseで未完了uploadを止めるため、全二重通信の保証はありません。背圧の保証はNOE bridgeの1chunkで、source/tee/Go Transport/OSのqueue全体の上限ではありません。nil LoopのGo個別導入は同期bufferingを維持しています。

生ログ・一時CLI・inventory・manifest・scripts・build出力・実接続の証跡は`/tmp/noe-stdlib-step4-20261019-9z60a2oi/`です。commit・push・releaseとroot配布binaryの更新は行っていません。
