# NOE版stdlib：ステップ4・要求送信のストリーミング計画

2026-10-19。[段階案](../future-candidates.md)のステップ4を具体化した計画です。基点はステップ3をコミットした`0de9351072ceaaadae7063a30ab52448e93b1d58`。その後の実装依頼に基づき、4スライスの実装・関連テスト・独立コードレビューと再レビュー・総合テスト・実プロバイダ基本フローまで完了しました。[実装・検証記録](stdlib-step4-upload-implementation.md)と[構造化結果](stdlib-step4-upload-results.json)を参照してください。以下は実装前にレビューした方針と受入条件です。

## 目的と範囲

`fetch(url, { method: "POST", body: readableStream })`で、producerが閉じる前に本文の先頭が送信先へ届くようにします。本文を全量収集せず、送信側の読取り需要に合わせてJSの`Uint8Array`チャンクをGoのHTTP clientへ渡します。取消し・通信失敗・runtime終了で送信を止め、待機中の読取りや書込みを回収できることも到達条件です。

`noe:http.fetch`とglobal fetchは現在と同じ関数値を使い、共通実装を改善します。URL/stringとRequest入力、既存の有限本文、応答streaming、Fetch.Allow/Client/MaxBodyBytes、main・各Worker・agent/run_jsの利用経路を保ちます。公開export・設定field・依存は追加しません。

受信側の`serve`は本文を読み終えてからhandlerを呼ぶ現在の方式を維持します。受信streamingはステップ5です。今回、全二重HTTP通信の保証、Node request/response互換API、TLS socket、HTTP/2専用API、トレーラー、ファイルへの自動退避、ストリームの自動再送は追加しません。API登録や公開Go package化はステップ6・7へ残します。

## 調査で確認した出発点

- `internal/stdlib/fetch.go`のJS fetchは、streamを`request._consume()`で全量連結してからhost.sendへ渡しています。AbortSignalの送信取消しの登録も、その収集後です。
- `Request`からの構築では`from._bytes`だけを継承し、stream本文を落とす経路があります。さらにfetchの既定引数`init = {}`により、Requestをそのまま使う`init === undefined`分岐に通常到達しません。streamを渡す標準的な2経路を成立させるため、この部分を同時に修正します。
- Go側は本文を`strings.Reader`として渡しており、長さとGetBodyが設定されます。Go 1.27.2のnet/httpは、GetBodyがない非空・長さ不明の本文について307/308を自動追従せず、リダイレクトResponseを返します。301/302/303はGET/HEADへ切り替えて本文を落とします。
- NOEはLoop付きのFetchを使います。LoopなしのGo個別導入は同期通信・応答全量取得であり、同じowner上のJS producerを待つ非同期uploadへそのまま切り替えると停止します。
- Loop.Postは閉じたloopではcallbackを捨てます。JS値の操作はownerだけで行い、Go側のpipeや待機の解放はcallbackの実行を前提にしてはいけません。
- 現在のReadableStreamはreader.cancelでpending readを閉じますが、利用者のsource.cancelが返すPromiseは完了しない場合があります。releaseLockはpending readがあると失敗します。teeされた未消費の分岐には別途キューが溜まり得ます。

確認元は`fetch.go`、`streams.go`、`loop.go`、`worker.go`、`internal/runtime/runtime.go`、既存stdlib/runtimeのHTTP・寿命テスト、および使用中のGo toolchainのnet/http/client.go・request.goです。上流の変更や最新版の採用を前提にしません。

## 公開する本文・送信契約

| 観点 | ステップ4の契約 |
| --- | --- |
| 対象 | NOEの通常構成、すなわちLoop付きFetchでReadableStream本文を逐次送信する |
| チャンク | Uint8Arrayを受け付け、byteOffset/byteLengthを保って送る。空チャンクは無送信で続行。文字列等のチャンクはTypeErrorの原因として送信を失敗させる。本文全体がstring/ArrayBuffer/view/Blob/FormData等の場合は既存encodeBodyを維持する |
| 大きさ | upload本文全体への新しい上限を設けない。MaxBodyBytesは応答読取りの設定であり、upload制限へ転用しない |
| 背圧 | 次のreader.readは、前チャンクをGoの本文readerへ渡し終えた後に行う。bridgeは本文全量・無制限キューを持たず、最大1チャンクを処理中にする。大きい1チャンクのサイズ自体はproducer次第 |
| 保証の範囲 | JS source自身のqueue、ReadableStreamのhighWaterMark、clone/teeの未消費分岐、Go Transport・OS・相手のbufferは別の層。引渡し完了は相手アプリによる消費の通知ではない |
| framing | streamは長さ不明の本文として扱い、GetBodyを作らない。Content-Length/Transfer-EncodingはGoのRequest fieldとTransportへ任せ、JS headerから長さを推測・採用しない。標準HTTP/1送信では通常chunkedとなるが、JSに手動chunkedやduplex optionは要求しない |
| 有限本文 | 従来のbytes/string等は既知長・GetBody付きの経路を維持し、streamへ変換しない。空本文・GET/HEADの本文禁止・既存content-type生成も維持する |
| Loopなし | Go個別導入の同期モードでは、従来の有限stream全量収集を維持する。NOEのstreaming保証と区別してGo comment・由来記録へ明記する。公開設定の追加は不要 |

### Request入力と本文の所有権

`fetch(url, {body: stream})`、`fetch(request)`、`fetch(request, {headers: ...})`、`new Request(request)`からの送信で、選ばれたstreamが失われず一度だけ消費されるようにします。送信開始時に本文を使用済みにし、readerを1つだけ取得します。既に使用済み・lockedの有効本文は、通信を始める前に拒否します。本文を明示的に置き換えた場合は、新本文を対象にし、旧本文を読みません。

Request由来のstreamは暗黙にteeしません。`new Request(source)`は未使用のstream本文の所有権を新Requestへ移し、sourceは再消費不可とします。`fetch(source, init)`も同じ所有権の移動規則を使います。`fetch(source)`はsource自身の本文を使用済みにします。URL・method・headers・signalの継承とoverride、および既存のnull/undefined本文選択は維持します。本文なしのRequestに送信しただけで架空の本文を作りません。

明示的な`request.clone()`は既存のteeを使う別操作として維持します。未使用streamの両分岐を別々に送れることと、使用済み本文のclone拒否を確認します。uploadのためにResponseの消費や汎用Streamsの仕様を全面改修しません。bytes本文についてもRequestとしての二重送信・使用済み判定を整えますが、既存の再利用可能な外部Uint8Array値自体を使用済みにする意味にはしません。

### リダイレクトと早期応答

- bytes/string等の既知長本文は従来どおりGoのGetBodyによる再送を利用できます。stream本文は一回限りとし、307/308で再送が必要なら追従せず、そのResponseを返します。全量収集・暗黙のclone・途中からの再送はしません。
- 301/302/303による本文なしへの遷移はGo clientへ任せます。元のuploadを止めても、追従先の要求・Responseを取り消さないようにします。Allowは実際に追従する各要求で従来どおり適用し、CheckRedirectとcustom clientを保持します。
- fetchは応答headersが得られた後にResponseを返します。最終Responseが得られた時点でproducerがまだ開いている場合は、未完了uploadを終了してreader.cancelを試みます。401/413等の早期応答でproducerのEOFを待ち続けません。この終了だけを理由に受信Responseのcontextを取り消してはならず、応答本文を読めることを保証します。
- Transportによる要求Body.Close、リダイレクトによる本文の放棄もupload終了の通知にします。CloseはDo終了後に別goroutineから呼ばれる場合があるため、重複終了と競合を扱います。producerの正常EOFと意図した早期終了を、producer失敗と区別します。

正常EOFと意図した早期終了では、pipe writerを正常closeしてTransport側のBody.ReadへEOFを伝えます。未完了のPipeReaderまでNOEの早期応答cleanupで閉じると、ErrClosedPipeを受けたHTTP/1 Transportが接続を閉じ、受信Responseも壊すため、この経路ではreaderのcloseをTransportのBody.Closeへ委ねます。停止に伴うwriteのErrClosedPipeや遅延read結果を、producer失敗としてrequest取消しへ昇格させません。実際のproducer/通信失敗・abort・runtime終了では、request context取消しとpipe両端のcloseでIOを解放します。

これは要求を送りながら任意に応答を読み続ける全二重通信の契約ではありません。サーバーが先に応答する用途ではuploadが打ち切られ得ることを英文docsへ明記します。通常の受信完了後の応答では、全bytesの到着と正常EOFを確認します。

### 中断・失敗・終了

| 契機 | 必須の振る舞い |
| --- | --- |
| pre-aborted signal・無効本文 | 送信開始前にrejectし、bridge・readerを残さない |
| upload中のAbortSignal | source.read待機・pipe.write待機・通信を止める。fetch未完了時は従来のsignal.reasonによるrejectを維持する |
| producerのread失敗・不正チャンク | Go本文readerへ失敗を伝え、通信contextを取り消す。headers受領前はTypeError("fetch failed", {cause})。受領との競合後は既に返したResponseを再rejectせず、残る本文読取りを失敗させる |
| 無効URL・Allow拒否・dial失敗・timeout | native側を必ず終了し、ownerが生存していればreaderをcancelして解放する。既定30秒のclient timeoutにはproducer待機中も含める |
| upload正常EOF | pipe writerを正常closeし、Response用contextは維持する。応答の読取り・cancel・既存32 MiB上限まで従来どおり処理する |
| ResponseのEOF・cancel・読取り失敗 | request contextと残るuploadの所有資源を解放する。headersが返っただけでAbortSignalの接続を外してはならない |
| timeout/SIGINT/root error/runtime Close/Worker terminate | loop contextからGo側のpipe・待機を終了する。閉じたruntimeへJS callbackを実行せず、Go側の回収にJS処理完了を要求しない |

終了は冪等にし、fetch Promise・write Promise・pending readを二重settleしません。owner生存時はreader.cancelの結果のrejectを処理し、pending readがsettleした後にreleaseLockします。source.cancelが返すPromiseが未完了でも、GoのIO・fetchの失敗/早期応答・runtime終了を待たせません。終了済みuploadへ遅れて届いたread結果は送信せず破棄します。利用者自身が保持するtimerや外部資源までNOEが自動停止する保証はしません。

## 内部構成と資源の所有

`fetch.go`の共通経路を拡張し、Go側のupload資源は必要なら`internal/stdlib/fetch_upload.go`へ分けます。runtime・nativeioへHTTP専用bridgeを増設しません。

1. owner上でRequestのmetadata、本文種別と使用状態を確定し、stream用readerを取得する。内部host protocolで送信開始・chunk書込み・EOF/失敗・終了通知を扱う。Loopの有無は内部flagでJSへ渡し、公開JS APIは増やさない。
2. stream経路では`io.Pipe`等のGo reader/writerを作り、Goのhttp.Request.Bodyへ接続する。送信開始を最初のreader.readより前に行い、最初のchunkを待つ間にもGoの通信・timeout・取消しが有効になるようにする。
3. JS pumpはreader.read→Uint8Array検証→native write完了のawaitを直列に繰り返す。native callbackはowner上でbytesをGoへコピーし、pipe.Writeはgoroutineで行う。JS bufferをGoのIO中に参照せず、バイナリをstringへ変換しない。
4. read/transport側のClose・context終了・producer EOF/失敗・早期Responseの競合をGoの同期機構で一つのupload状態へ集約する。uploadだけを止める操作とrequest全体を取り消す操作を分け、意図した本文放棄でResponseを壊さない。
5. Goのcleanupは終了理由を分ける。正常EOF・意図したupload停止はwriterの正常closeでblocked ReadへEOFを返し、pending Writeを解放する。readerはTransport所有のままBody.Closeで閉じる。実失敗・request全体の取消しは両端を閉じる。Go-onlyの終了通知とwrite goroutineの終了はJS callbackに依存させず、必要なJS通知だけLoop.Postする。Postが捨てられてもGo側の終了は確定する。owner終了後のJS値アクセス、JSを待つClose、goroutine内でのPromise操作を禁止する。
6. Loop.Begin/DoneとPostのkeepaliveを対応付ける。network待機・chunk書込み・owner通知中に実行が早く終わらず、全終了経路でpendingを残さない。AbortSignalのlistenerは、要求と応答が終わった時点で取り外し、終了済み操作を保持し続けないようにする。

pipeの具体的な型・内部method名は実装時に決めます。上の順序、非同期のowner境界、1チャンク制御、upload単独終了とrequest取消しの分離は受入契約です。既存loop・Streamsの変更はこの契約の成立に必要な局所変更に限り、汎用的な作り直しは含めません。

## 実装スライス

### S4-A：送信bridgeとRequest所有権を一緒に成立させる

- 開始時のHEAD・依存・Go環境・API/doc ID集合を記録する。rootの配布binaryはv1.10.0のため基準実行には使わず、基点sourceから一時CLIをビルドする。
- fetch.goと内部upload helperへLoop付きstream送信を実装する。Request入力・override・使用済み/locked判定・明示cloneを整え、Loopなしの有限bufferingを残す。
- 最初にGoの受信fixtureとchannelの順序barrierを用意する。producerがprefixを出して待機し、相手がprefixを読んだ通知で残りとEOFを許可する。全量収集版なら完了できないテストにし、受信側serveの改善に依存しない。
- fake RoundTripper/Go readerで引渡しを止め、NOEが次のchunkを際限なくreadしないことを検証する。sourceのhighWaterMarkを0にし、Streams側の先読みとbridgeの読取りを区別する。
- bytes/string/typed view/空本文・stream binary/空chunk・Request経由・clone・二重送信・GET/HEAD拒否と、Loopなしの有限streamの既存結果を確認する。

完了条件：通常uploadで全量収集がなくなり、prefix到着がEOFより先になる。本文の所有権が1つに定まり、GoがJS値をIO goroutineへ持ち出さず、bridge内部に無制限キューを持たない。

### S4-B：取消し・リダイレクト・終了の競合を閉じる

- 取消し・producer失敗・Go IO失敗・正常EOF・早期Responseを上の状態規則へ接続し、全cleanupを冪等にする。正常EOFと意図したupload停止で応答contextを取り消さない。
- pending source.read、blocked pipe.Write、接続待機でのabort、producer例外/不正chunk、短いcustom client timeout、source.cancelが未完了/失敗するcaseをbarrierで再現する。初回chunk前のabortも含める。
- 401/413等の早期Responseを返すGo fixtureで、producerを閉じなくてもResponse本文が読め、uploadのGo資源が終了することを確認する。HTTP/1 server側ではResponseController.EnableFullDuplexと明示flushを使い、net/httpによる未読要求のauto-drainで試験自体がEOF待ちになるのを避ける。これはfixtureの設定であり、NOEの全二重APIを追加する意味ではない。
- 早期応答の本文は前半をflushし、後半はupload停止を確認するbarrierの後に送る。read buffer内の短い応答だけで成功扱いにせず、writerの正常EOFとreaderのTransport所有を保つことで、遅延した後半まで読めることをHTTP/1で確認する。Transport.CloseとDo完了が競合しても結果を上書きしない。
- streamの307/308非追従と301/302/303の本文放棄、既知長本文の307/308再送、Allowの初回/redirect拒否とcustom CheckRedirect/Client保持を確認する。自動再送用の全量bufferを導入しない。
- runtimeのroot error・timeout、CLI SIGINT、Worker terminateで、upload開始・待機を確認してから終了させる。Go側done通知・receiverの終了等で回収を確認し、goroutine総数や固定sleepで判定しない。

完了条件：producerやネットワークが待機した状態から中断でき、Responseの寿命を壊さず、Loop終了後もGo側にblocked IOやupload所有資源が残らない。

### S4-C：英文docs・例・全実行経路を揃える

- `internal/apidocs/content/noe-http.md`、`stdlib-globals.md`のfetch/Request関連、関連ガイドとHTTP例READMEを更新する。streamのUint8Array契約・所有権・背圧の範囲・redirect制約・早期応答・取消しを英文API docsに記載する。応答32 MiBのprefix/正常EOFと受信側bufferingの説明は維持する。
- `fetch.go`のGo commentと`internal/stdlib/UPSTREAM.md`にNOE独自の変更とLoopなしの相違を英文で記録する。固定した原型・licenseは変更しない。過去のステップ3計画・検証記録は当時の結果として残す。
- `examples/http/upload-stream.js`に有限のUint8Array producerを使う例を追加する。NOE serveを使う例なら「handlerはEOF後」と説明し、producer終了前の受信を示す証拠にはしない。portや外部サービスへの固定依存を避ける。
- main・Node形式Worker・孫Worker・Web Workerで、新入口からstream uploadが届く代表caseを共通fixtureで確認する。新export/optionはないため、global/moduleの同一性・26 Node module・41 topics/100 detailsのID集合を維持する。
- 新sourceの一時CLIを使うE2Eとfixture agent/run_jsでupload・結果保存・最終回答まで確認する。CLI docsとrun_js内のdocsで現在の説明が一致することも確認する。初期instructionsは増やさない。

完了条件：CLI・各Worker・agentが同じ送信実装を使い、英文docsの新仕様と実際の送信/取消し/redirectが一致する。serveの受信改善を済ませたように説明しない。

### S4-D：独立コードレビュー・総合テスト・引継ぎ

- 関連テスト後に独立した通常コードレビューを行う。upload状態と競合、owner境界、終了回収、Request互換性、docs・テストの根拠を確認し、P2以上を解消する。その後、全体/race/E2E/vetと配布対象のcompileを行う。
- 実プロバイダの総合確認は、実装依頼後に基本フロー1課題だけ行う。既存のOpenCode Go / deepseek-v4.1-flashを使い、Go製loopback受信fixtureへnoe:httpで有限streamをPOSTして結果を保存し最終回答する。課題にdocs参照は指示せず、tool code・送信結果とdocs参照の有無を別々に記録する。細かな失敗経路はローカルテストで検証する。
- 実接続の上限は6 provider要求・115秒を基本とし、一時workspaceへprompt/tool/結果を保存する。認証情報は保存しない。設定が使えない場合は未実行と報告し、fixture成功を実接続成功と扱わない。現在の計画作成では呼び出さない。
- `stdlib-step4-upload-implementation.md`と構造化結果へsource範囲、API/doc ID比較、コマンド・review・skip・実接続・cross-compile範囲を記録する。段階案のステップ4だけ完了にし、ステップ5へ受信buffering・全二重の非保証・共通の終了規則を引き継ぐ。

完了条件：計画の送信契約を実利用と観測可能な順序で検証でき、未解消P2以上が0。ステップ5の実装は混ぜず、commit・push・releaseは別の明示依頼を待つ。

## 検証と実行順序

| 契約 | 主な検証 |
| --- | --- |
| EOF前に送信先へ届く | Go httptest receiverがprefixを読んでproducerの残りを解放。NOE serveは使わず、chunk境界の一致は要求しない |
| bridgeの背圧 | fake Transportのread barrierとHWM 0のsource。NOE所有のread/writeが直列で、write停止中に次々readしない |
| 所有権とbytes保持 | direct/Request/override/transfer/clone、locked/used、byteOffset・0/255等。clone両分岐を消費し、teeのqueueをbridgeの保証と混同しない |
| 中断と資源回収 | 初回read前/read待機/write待機/timeout/producer失敗、source.cancel未完了、Transport.Close競合、root/CLI/Worker終了。Go done通知とloop pendingの終息を確認 |
| HTTP方針 | 307/308一回限りstream、既知長再送、301/302/303本文なし、早期応答と遅延する応答本文後半、Allow/CheckRedirect/custom client。HTTP/1 fixtureのauto-drainを回避し、正常停止がErrClosedPipeによる接続破壊へ変わらないことを確認。wireのContent-Lengthが不明なstreamを既知長と誤認しない |
| 回帰 | 既存Fetch/Serve/Streams/WebSocket/lifetime/runtimeの関連テスト。32 MiB応答上限の正常EOF、response body abort、受信bufferingと既存有限本文を維持 |
| 到達とdocs | 4モード・新旧関数同一性・CLI/standalone docs・fixture agent/run_js、既存41/100 IDと26 module。実providerは基本1課題を別記録 |

S4-A/Bのテストは`internal/stdlib/fetch_upload_test.go`等へ集め、runtimeとtestsの既存HTTPテストへ必要な到達/終了caseを加えます。失敗経路はGoのbarrier・deadlineを使って有限時間で終わらせます。性能ベンチマーク・RSSの数値目標・一定時間のsleepを正しさの代替にしません。

関連確認の基本コマンド：

```sh
CGO_ENABLED=0 go test ./internal/stdlib/... ./internal/runtime ./internal/apidocs -count=1 -timeout=180s
CGO_ENABLED=0 go test -tags=e2e ./tests -run 'Test.*(HTTP|Fetch|Serve|Stdlib|Docs|Worker|Interrupt|Timeout)' -count=1 -timeout=5m
```

コードレビュー後の総合確認：

```sh
CGO_ENABLED=0 go test ./... -count=1 -timeout=180s
CGO_ENABLED=1 go test -race ./internal/... -count=1 -timeout=300s
CGO_ENABLED=0 go test -tags=e2e ./tests -count=1 -timeout=10m
CGO_ENABLED=0 go vet -tags=e2e ./...
```

Linux/Darwin/Windows × amd64/arm64でCGO無効のCLIと、変更したstdlib/runtime/E2E等のtest packageを一時出力先へcross-compileします。native実行はLinux amd64、他対象はcompileのみと明記します。既存helper/OS/opt-in skipは理由を記録し、今回の都合によるskipや既存期待値緩和はしません。準備済みMCP bundleはあれば再利用し、新規取得を前提にしません。差分が増えた後は必要な関連確認をやり直し、同じ総合テストを理由なく反復しません。

## 今回の成果物・停止条件・計画レビュー

今回の変更はこの計画、レビュー記録、開発indexと段階案への導線に限定します。source/API docs/exampleの変更、実装テスト、外部書込み・費用発生、commit・push・releaseは行いません。`_refs/`と利用者の`agent-server-build.jsonl`は参照・変更しません。

実装時の局所的な型・ファイル分割・テストfixtureはこの契約を満たす範囲で決めます。上流engineの変更、汎用loop/Streamsの大規模な変更、受信側streaming、既存の有限本文/Responseの公開契約変更が必要と判明した場合は、根拠と代案を整理して利用者へ戻します。上のRequest所有権修正とstream固有のredirect/早期応答制約は計画内です。

独立した通常レビューでは、段階案との整合、Request入力と所有権、Go HTTPとredirect/早期応答、owner境界・背圧・終了競合、nil Loopの扱い、docsと検証の実現性を評価します。初回30分、新しい根拠・中間結論が10分ない場合は中断。再レビューは変更箇所と既存指摘の解消を15分以内で確認します。利用者の明示依頼に従い、P0/P1/P2の未解消指摘が0になるまで修正・再レビューを反復し、P3は任意改善として区別します。
