# NOE版stdlib：ステップ5・要求受信のストリーミング計画

2026-10-19。[段階案](../future-candidates.md)のステップ5を具体化した計画です。基点はv1.11.0公開後の`41e40d46d304a2f996e58459cd861d7d6a376d38`。利用者の番号確認により対象をステップ5とし、その後の実装依頼に基づき、S5-A〜Dの実装・関連テスト・独立コードレビューと再レビュー・総合テスト・実プロバイダの基本フローまで完了しました。[実装・検証記録](stdlib-step5-receive-implementation.md)と[構造化結果](stdlib-step5-receive-results.json)を参照してください。以下は実装前にレビューした方針と受入条件です。

## 目的と範囲

`serve`が本文EOFを待たずにhandlerへWeb Requestを渡し、handlerが`request.body`から到着したbytesを順次読めるようにします。本文を先に全量収集せず、JSの読取り需要に合わせてGoのHTTP request bodyを読みます。切断・読取り失敗・実行終了でpending IOを回収できることも到達条件です。

`noe:http.serve`、global serve、`node:http`・`http`のserveは同じ関数値・共通実装のままです。既存のhandler形式、有限本文のtext/json等、応答streaming、WebSocket upgrade、Allow・Go設定、main・各Worker・agent/run_jsの経路を保ちます。Web型や公開export・JS option・依存は増やしません。

全二重HTTPの一般保証、Node request/response互換API、TLS listener、HTTP/2専用API、trailer公開、本文の自動退避・再送、新しいtimeout設定は対象外です。ステップ4のfetchはheaders受領時に未完了uploadを止める契約を維持します。API登録の境界整理・公開Go package化はステップ6・7です。

## 調査で確認した出発点

- `internal/stdlib/serve.go`はGo handler冒頭で`io.ReadAll(io.LimitReader(r.Body, limit))`し、bytesをexchangeへ保存してからLoop.Postします。現行MaxBodyBytesは0なら32 MiB、正ならそのprefixで正常EOF、負なら現在のLimitReaderでは空本文になる挙動です。超過時に413を返すAPIではありません。
- JS側はGET/HEAD以外の非空bytesだけをRequestへ渡します。受信Requestはglobal Request・Bodyを共有し、text/json/bytes等は全量消費する便利method、bodyは一度だけ読むReadableStreamです。Request移譲・cloneとbodyUsedはステップ4で整備済みです。
- 応答はexchange.answer/chunks/goneを通じてGo handlerへ渡しています。Response本文はhandlerがResponseを返した後にJS側でpumpされるため、`new Response(request.body)`等はhandlerのreturn時に入力を閉じると壊れます。
- Loop.Postは閉じたloopではcallbackを捨てます。JS値はownerだけで操作し、Go側のIO回収をcallbackの実行やPromiseの完了に依存させられません。
- 固定環境Go 1.27.2のnet/httpはHTTP/1で応答前に未読本文をdrainし、handler終了後のBody.Closeでもdrainする場合があります。CloseはReadと内部lockを共有するため、context取消しとBody.Closeだけでblocked Readが必ず解放されるとは扱えません。ResponseController.EnableFullDuplexは前者を抑止しますが、後者の回収も別に必要です。
- `Expect: 100-continue`には別の順序制約があります。最初の応答flushがbodyRemainsでpending Readと同じlockを取り、最初のRead前の最終WriteHeaderは入力を閉じ得ます。通常要求と同じ「flush後にだけ入力を中断」の規則では成立しないため、下記の専用の順序を使います。
- 現行server.closeは最大5秒Shutdownしますが、timeout後の強制Closeがなく、同じcloseOnceがruntime hookの強制Closeも抑止し得ます。streamingで待機中の要求を増やすため、今回その到達経路を局所的に直します。

根拠はserve.go、fetch.goのBody/Request、streams.go、loop.go、worker.go、runtimeのHTTP・終了テスト、および使用中toolchainのnet/http/server.go・transfer.go・responsecontroller.goです。上流更新や外部の最新仕様への追随は前提にしません。

## 公開する受信契約

| 観点 | ステップ5の契約 |
| --- | --- |
| dispatch | headers受信後、本文EOFより前にhandlerを呼ぶ。JS readerがない間、NOE bridgeはr.Body.Readを開始しない |
| body | GET/HEADは現在どおりJS本文なし。それ以外はGoが本文なしと判定できる要求はnull、本文あり/長さ不明はReadableStream。長さ不明で実際は0 bytesなら空streamがEOFになる。chunked・Content-Lengthの両方を扱う |
| bytesと背圧 | Uint8Arrayを渡す。固定上限のread buffer（初期案32 KiB）で1回ずつ読み、JS pull1回につきnative read1件だけ進める。sourceはHWM 0で先読みしない。bridgeは1 read/1 chunk以内で、本文全量や無制限queueを持たない |
| 保証の範囲 | net/http・TCP・OSのbufferや利用者がclone/teeして消費しない分岐のqueueは別層。wire chunkとJS chunkの一致は保証しない。text/json等は利用者の要求による全量収集であり、incremental利用にはbody readerを使う |
| 上限 | Serve.MaxBodyBytesの0＝32 MiB、正＝prefixだけ返して正常EOF、負＝空本文という現行結果を維持。追加の1 byteを読む超過検出や413化はしない。上限到達時はそれ以上native readしない。Content-Length headerは受信値のままで可視prefix長へ書き換えない |
| one-shot | text/json/arrayBuffer/bytes/blob/formData、reader、Request移譲と明示cloneは既存Bodyの規則を共有する。二重消費・使用済みcloneの拒否とbinary bytesを維持。暗黙tee・Response用の別Request型を作らない |
| malformed body | handler dispatch後の切断・不正chunk framing・短いContent-Length等はbody読取りのrejectとなる。handlerが捕捉すればResponseを選べるが、接続が壊れていれば配送は保証できない。未捕捉は既存onError/500経路へ渡す。dispatch前のGo HTTP parse失敗は従来のHTTP処理のまま |
| signal | 新しい受信Request.signalの保証は追加しない。入力のread rejectとruntime/connection cleanupを今回の取消し契約にする |

GET/HEADに実際のwire本文がある場合もhandler前には読まず、JSへ公開しない本文として後述の未読処理を適用します。既知の空本文とchunked空streamの形の差は、英文docsと回帰テストで明示します。32 MiBは送信fetch側の応答上限とは別の設定です。

### 応答と入力の生存期間

入力はhandlerがResponseを返した時点では閉じません。Responseのsourceがrequest.bodyを読んで応答を作る経路を許し、要求と応答の両方向が同じexchangeに属するものとして扱います。これはResponseによる入力消費を壊さないための仕組みで、NOE fetchを含む全二重通信の一般保証ではありません。

- 本文をすべて読む通常経路はEOFまで読んでからResponseを返せます。既存text/json等の結果は変えません。
- 未読・一部読取り・上限到達・明示cancel後でも、有限ResponseやonErrorのResponseをproducerのEOFを待たずに送れます。HTTP/1では入力が残る応答について接続再利用を諦め、必要なConnection: closeとEnableFullDuplexを内部で適用します。応答送信前のdrainを避けます。
- Responseの送信が終わるまで、request.bodyを使う応答producerによるreadを許します。送信完了・失敗時には残る入力を終了し、その後のreadは閉鎖済み入力のエラーになります。Responseを返した後に別timerで入力を読み続けられる保証はしません。
- reader.cancel/body.cancelはJS streamとそのpending readを速やかに閉じ、以後のread開始を止めます。既にGoのReadが待っている場合は、応答を壊さないためその物理的中断を応答送信完了まで遅らせられます。cancel PromiseはGoのRead終了を待たずにsettleします。遅れたread結果は破棄し、閉じたstreamを再openしません。
- 応答が正常に送信できる間、入力だけの取消しを接続全体の取消しへ直結させません。SetReadDeadlineによるRead失敗はGoの接続contextも終了させるため、通常要求では入力放棄のdeadlineを応答flush後に設定します。未読bodyのClose/drainにもそのdeadlineを適用してEOF待ちを残しません。Expect要求で応答前に中断が必要な場合は、次節の局所的なcontext取消しの分離を使います。
- 応答送信完了時の読取り中断・Close・待機goroutine回収は、Go handlerが戻る前に完結させます。Go handler終了後にr.Body/ResponseWriterを使いません。通常EOFで接続を再利用できる経路は維持します。入力放棄経路では再利用より有限応答の配送と確実な回収を優先します。

Go net/httpには、本文を読まず応答も書かない間にすべてのpeer切断を即時検出する保証はありません。検出のための無断先読みはせず、実際のRead/Write失敗・r.Context終了・server/runtime終了を終了契機にします。応答を無期限に生成し続ける利用者の処理を入力cancelだけで自動停止する契約にはしません。

### Expect: 100-continueの順序

HTTP/1のExpect要求も、通常の本文消費・早期応答・`Response(request.body)`を扱います。Goが受理した100-continue要求について、次の順序を内部protocolとして実装します。417で一律拒否したり、全量bufferingへ戻したりしません。利用者sourceの先読みと、serve自身の応答pumpによる需要は区別します。

1. Go handlerは本文を読まず、最終Responseより先に明示的な100 Continueを送る。Goの自動continueをこの時点で完了させ、後から最終WriteHeaderしただけで入力が閉じる経路を避ける。これは本文の先読みではなく、JS read開始まではbridgeのRead件数を0に保つ。100は要求受理を示す暫定応答であり、後のonError/500等を妨げない。
2. Responseがstreamなら、Go側の最終headersのwrite/flush完了をJS pumpへ通知してから、そのpumpのreadを始める。このgateが止めるのはserve自身の需要であり、利用者が作ったstreamのstartや既定HWM 1による先行pullまでは止めない。`Response(request.body)`は入力がHWM 0のため、この順序で初回flushより先にnative readを起こさず扱える。同じEOF前進行を保証する独自producerはHWM 0を使い、startでは入力を読まず、pumpから要求されたpullで読むものとする。通知待機もexchange/loop終了で解放し、Postの実行をGo cleanupの条件にしない。
3. すでにhandlerが開始したreadをcancelして有限Responseへ切り替えるときは、入力放棄を先に記録し、初回flushより前にread deadlineで物理Readを解除し、readのdoneを確認してbody lockを解放する。入力を閉じた後に新しいreadは始めない。この経路は接続再利用しない。
4. この局所的なdeadlineに起因するr.Contextの取消しは、応答の中止へ昇格させない。入力放棄フラグをdeadlineより先に確定し、その後の応答はloop/runtime終了と実際のWrite/Flush失敗で停止する。単にr.Context.Doneをselectし続けて応答を捨てる実装にしない。真の切断を隠して書込み失敗を成功扱いにもせず、応答が送れたかは実際のwrite/flush結果で決める。
5. handlerの独立したread、またはResponse source自身のstart/先行pullが既に入力readを開始した場合、初回flushに先立ってそのreadへのbytes到着・EOF・明示cancelのいずれかが必要になる。pump gateだけでそれらのreadを防ぐとは扱わない。Expect clientは100後に本文を送る通常経路で動作できるが、そのproducerが最終headersを待って入力bytesを送るような任意の全二重進行は保証しない。独自sourceが自ら全量を待ってからenqueueするなら、その待機をbridge bufferingと混同しない。入力を先読みしない2のproducerと`Response(request.body)`、未読・読了済み・cancel済み要求への有限応答を、EOF待ちにしない受入対象とする。

英文docsでは100 Continueを先に返す方針、pump gateで制御できる需要と利用者sourceの先読み、独立したpending readの違いと上の保証範囲を説明します。特定Go版のprivate fieldやunsafe操作には依存せず、公開ResponseWriter/ResponseControllerとbridgeの状態・pump順序で扱います。

### 終了の分類と所有権

| 契機 | 必須動作 |
| --- | --- |
| 入力正常EOF | streamを正常closeし、入力用の資源・keepaliveを解放。応答は継続できる |
| prefix上限 | 正常EOFとしてJSへ渡す。それ以上readせず、未読wire bytesは応答完了後の放棄経路へ |
| 明示入力cancel | JS readを閉じ、新しいreadを拒む。遅延結果を破棄。応答は維持し、Goのblocked Readは遅くとも応答終了時に解放。Expectでは初回flush前に解除し、局所deadline由来のcontext取消しを応答中止と分離 |
| Read失敗・検出済み切断 | pending readをowner上でreject。Go側exchangeを終了できる状態へし、利用者が選んだ応答は接続が生存する範囲で扱う |
| handler throw・不正Response | 既存onError/500。入力EOFを待たず有限応答を処理し、その後入力を回収 |
| 応答送信失敗 | 入力と応答の残る待機を止める。reply/chunk待機も解放し、Go goroutineを回収 |
| server.close | 同期・冪等を保ち、新規受付停止→最大5秒graceful Shutdown→未完了なら強制Close。待機入力/応答も回収し、timeout後にlistener/hook/loop pendingを残さない。handler内closeが自身の完了を待ち得る注意は維持 |
| Loop.Close・runtime Close・timeout・SIGINT・root error・Worker terminate | JSの完了を待たずGo側の接続・blocked Read・exchange待機を停止。runtime Closeの強制経路は並行中のgraceful closeに阻まれず、閉じたownerへJS操作しない |
| WebSocket upgrade | 入力本文を事前にread/closeせず既存takeOverへ渡す。upgrade後は既存socket側が接続を所有し、HTTP入力cleanupのdeadline/closeがhijack済み接続を壊さない |

exchange・入力reader・listenerの状態遷移は冪等にします。EOF/cancel/失敗/応答完了/終了が競合してもread Promiseは二重settleしません。正常EOF・意図した入力放棄と実失敗を区別します。server/runtimeの終了ではGo-onlyの接続切断が優先され、JS通知の実行やsource.cancel Promiseは回収条件にしません。

## 内部構成

`internal/stdlib/serve.go`の共通経路を拡張します。入力状態・IO bridgeは必要なら`serve_body.go`へ分離し、runtime/nativeioへHTTP専用経路を作りません。loopやStreamsの作り直しはしません。

1. Go handlerはmetadataとr.Bodyを持つ入力状態を作り、本文を読む前にexchangeをLoop.Postする。Go-onlyのexchange終了通知とrequest/loop contextを最初から接続し、dispatch前の取消しやPost破棄でも終了できるようにする。
2. owner上で内部raw.read/cancel等を受信ReadableStream（HWM 0）へ接続し、global Requestへ渡す。JS値をGoのIO goroutineに持ち出さない。readはbounded bufferと残量から長さを決め、Go goroutineでr.Body.Readを1件だけ実行する。
3. n > 0とerrが同時に返った場合はbytesを先に渡し、次のreadでEOF/失敗を通知する。短いreadや0 byteを勝手なEOFとしない。Go bytes→JS Uint8Arrayの作成・Promise settleはownerだけ。遅延通知は終了世代/状態を確かめて捨てる。
4. 応答送信は既存answer/chunksを使い、入力をResponse producerが消費する場合も同じloopで進むようにする。Go-onlyの入力停止/応答停止を分け、明示cancelだけで応答を切らない。通常は必要なflushを終えてから入力放棄のdeadline・Body.Closeを行う。Expectは明示100、初回最終headersのflush通知とpump開始、cancel済みreadの先行解除を前節の順序で行う。
5. HTTP/1未読入力の応答前drain・応答後drain・blocked Readの停止を別々に扱う。固定Go実装に対してEnableFullDuplex、接続再利用判断、通常の応答後deadline/CloseとExpect例外を関連テストで成立させる。局所的な入力停止と実失敗を記録してr.Context終了を分類し、write/flush失敗・loop終了を取りこぼさない。raw connectionの全面hijackやHTTP自前実装には切り替えない。
6. listenerのgraceful停止とforce停止を別に冪等化し、Shutdown timeout後もCloseを実行できるようにする。loop終了ではownerの進行を待たないforce経路を用意し、各exchangeのGo doneを回収する。入力read/応答pushのBegin/Done・Post分とserver keepaliveの対応を全終了経路で保つ。

具体的な型・内部method・buffer長は実装時に決めます。JS owner境界・需要読取り・bounded buffer・入力と応答の終了分離・応答後の回収順序は受入条件です。Go HTTPの局所処理ではこれを満たせず接続処理の全面変更が必要なら、実装を広げず根拠と代案を利用者へ戻します。

## 実装スライス

### S5-A：受信bridgeとBody契約

- HEAD・依存・toolchainとAPI/doc IDのbaselineを記録。一時CLIをビルドし、配布済みroot binaryを上書きしない。
- 本文の先読みを外してmetadata先行dispatchし、read/cancel付きの入力状態とHWM 0のstreamを実装。既存Body/Requestへ接続し、各種消費・使用済み・移譲・明示cloneを維持する。
- Goの独立clientを使い、headersを送って本文の初回bytes前にhandler起動を確認。その後prefixを送り、JS handlerがprefixを読んだ独立通知でclientの残り/EOFを解放する。NOE fetch単独の成功を受信streamingの証拠にしない。
- 計数付きGo readerとchannel barrierで、JS readがない間は0 read、1 pullにつき1 bounded read、未消費時の先読みなしを確認。TCP送信が直ちにblockすることを背圧の判定にしない。
- 既知長/chunked/空/GET/HEAD、binary・短いread・n+EOF/n+error、text/json/bytes等、二重消費・Request移譲・cloneを確認。上限は小さいcustom値でちょうど/超過/負、default32 MiBと設定未変更も確認する。

完了条件：handler起動・prefix読取りがEOFより先で、NOE所有のIO/queueがbounded。有限本文と一度だけ読む契約が共通実装で保たれる。

### S5-B：早期応答・取消し・終了回収

- 入力/応答の状態とGo-only終了を接続し、HTTP/1の未読応答をEOF待ちにしない。Response(request.body)と入力を読むResponse producerを壊さず、応答終了後に残る入力を止める。
- 独立HTTP/1 client（必要ならraw TCP）がproducerを開いたまま、未読・部分読取り・上限・body cancel・handler例外/onErrorのResponseのheadersと全bytesを受け取れることをbarrierで確認。既知長とchunked両方、短い応答と複数flushの遅延応答を含め、drain/ReadDeadlineの誤適用を検出する。
- pending read中のreader.cancelはJS側で速やかに終わり、late resultを捨て、応答を送れることを確認。応答完了後にはread goroutineがdoneとなり、Go handlerが戻った後にBody/ResponseWriterへ触れないことも確認する。
- 独立HTTP/1 Expect clientで、明示100を受けてもJS需要まではRead件数0、初回readをResponse producerまで遅らせた`Response(request.body)`/HWM 0・startでreadしないpull型producer、pending readをcancel後に返す有限Responseを確認。最後のcaseはclientが100後も本文を送らず待機し、初回flush前にread done→最終headers/全応答bytesという順序を観測する。遅延する応答後半も受信し、局所deadlineによるr.Context取消しで応答を捨てないこと、実際のpeer切断/loop終了では回収することを両方確認する。
- 既定HWM 1で1入力read→1enqueueするResponse sourceも別caseにし、先行pullのReadをbarrierで観測する。clientが100後にprefixを送ってそのreadを解放し、最終headers/応答prefixが要求EOFより先に届く通常利用を確認する。このcaseを「すべての利用者sourceの先読みをpump gateで防げた」という根拠にはしない。start/先行pullで未完了readを保持する場合の制約はdocs検証へ含める。
- malformed/truncated body、読取り中のpeer切断、応答Write失敗、応答を待つ間のloop/runtime終了、root error・timeout・CLI SIGINT・Worker terminateを、実際に待機を確認してから発火する。Read失敗がJSへ届く経路とowner破棄後にGo-onlyで回収する経路を分けて確認。
- server.closeの通常終了・5秒Shutdown上限後のforce Close・並行するruntime Closeを検証。同期closeの既存注意を保ち、終了後のport再bindとexchange/read doneで回収を確認する。テストのためだけの公開JS optionは足さない。
- WebSocket handshake・upgrade後の双方向通信/close、通常EOF後のkeep-alive再利用を回帰確認。入力放棄経路は接続再利用を要求せず、次接続で正常要求を処理できることを確認する。

完了条件：未読producerやblocked Readに関わらず有限応答を配送でき、検出済み切断・server/runtime終了後にGo資源を残さない。入力cleanupが正常な応答やupgrade済み接続を壊さない。

### S5-C：英文docs・有限例・各実行経路

- 内蔵`noe-http.md`、`stdlib-globals.md`のserve/Request・該当ガイドとHTTP例READMEを英文で更新。受信stream、上限prefix/負値、需要読取り、空本文、one-shot、読取り失敗、cancelと応答の寿命、Expectの暫定100/応答pump順序・HWM 0のproducer保証・利用者sourceのstart/先行pullと独立pending readの制約、close、全二重非保証を実装と揃える。初期agent instructionsは増やさない。
- `serve.go`等のGo commentと`UPSTREAM.md`へ変更とGo HTTPとの接続方針を英文で記録。古い計画/実装記録は当時の事実として保持する。
- `examples/http/receive-stream.js`へ有限のuploadをreaderで順次処理して集計結果を返す例を追加。portはOS割当、外部接続不要。例単体の完走と、独立clientを使うEOF前の観測試験を区別する。send/receive双方streamでもfetch側の早期応答打切り契約を変えない。
- main・Node形式Worker・孫Worker・Web Workerへ独立clientから送信し、handlerのprefix観測を確認。CLI E2Eとfixture agent/run_jsで受信処理・結果保存・close・最終回答の基本フローを確認。
- 4modeのglobal/module inventoryと旧入口の関数同一性、26 Node module、41 topics/100 detailsのID集合、CLI docs/run_js内docsの内容を比較。公開export追加を期待値更新で隠さない。

完了条件：各経路が同じ受信実装を使い、英文docsと実装・有限例が一致する。clone/teeやtransportのbufferをbridgeの背圧と誤って説明しない。

### S5-D：独立コードレビュー・総合テスト・引継ぎ

- 関連テスト後、独立した通常コードレビューと必要な再レビューでP2以上を0にする。owner境界・状態競合・未読bodyとnet/httpのdrain・応答とupgradeの所有権・shutdown・docs/テストの実現性を重点確認。その後、全体/race/E2E/vet・6配布対象compileを行う。
- 実装依頼後の実provider確認は既存OpenCode Go / deepseek-v4.1-flashで基本1課題。NOE serverを立て、独立Go fixture clientから有限の分割本文を送り、handlerで逐次集計・結果保存・close・最終回答する。課題文はdocs参照を指示せず、モデルの自発参照と課題達成を別々に記録する。clientはhandlerからの別経路ACKでEOFを解放し、同じ接続の早期Responseを待つ課題にはしない。
- 基本上限6 provider要求・115秒、一時workspaceへprompt/tool/結果を保存、認証情報は保存しない。設定不在や通信障害は未実行/失敗を正直に記録し、fixtureを実provider成功へ読み替えない。今回の計画作成では呼び出さない。
- `stdlib-step5-receive-implementation.md`と構造化結果へsource範囲・コマンド・review・API/doc比較・skip・cross-compile・実接続を記録。ステップ5だけを完了にし、入力/応答/接続の所有権とshutdownをステップ6へ引き継ぐ。commit・push・releaseは別の明示依頼を待つ。

完了条件：順序を観測できる受信テスト・総合確認が成功し、P0/P1/P2未解消0。ステップ6・7の実装を混ぜない。

## 検証と実行順序

| 契約 | 主な根拠 |
| --- | --- |
| EOF前dispatch/受信 | 独立Go/raw HTTP clientと別経路ACK。headers→handler→prefix→JS read通知→残り/EOFという順序を記録 |
| 背圧とbounded IO | 計数reader/Go barrierとHWM 0。readなし時0、pull単位のread/bytes上限と最大1件を確認。RSS・goroutine総数・wire chunk一致は判定にしない |
| 上限/Body | 小さいcapで境界・超過prefixの正常EOF、default/負値、空・binary・Body methods/移譲/clone/used |
| 入力/応答の分離 | 未読/途中/cap/cancel/error、開いたproducerへの有限応答、遅延応答後半とflush、Response(request.body)、応答完了後のread done。Expectの初回read前の最終headersとpending read cancel後の初回flushも別々に確認 |
| 終了 | 読取り/応答待機に入ったことをbarrierで確認後、切断/失敗/close/loop/runtime/CLI/Worker終了。done通知・port再bindとloop pendingの終息 |
| 回帰 | fetch upload/応答streaming・早期応答・上限、WebSocket handshakeとupgrade後通信、HTTP有限本文・通常keep-alive、旧入口/4mode/CLI/agent/docs |

新しい関連単体テストは`internal/stdlib/serve_body_test.go`等、到達/終了確認は既存runtime HTTPテストとtestsへ置きます。固定sleepを同期や回収の証拠にせず、channel/barrierと有限deadlineを使います。server.closeの実際の5秒上限は必要な代表caseで確認し、件数を目的に似た長時間試験を増やしません。

関連確認：

```sh
CGO_ENABLED=0 go test ./internal/stdlib/... ./internal/runtime ./internal/apidocs -count=1 -timeout=180s
CGO_ENABLED=0 go test -tags=e2e ./tests -run 'Test.*(HTTP|Fetch|Serve|Stdlib|Docs|Worker|Interrupt|Timeout)' -count=1 -timeout=5m
```

独立コードレビュー後の総合確認：

```sh
CGO_ENABLED=0 go test ./... -count=1 -timeout=180s
CGO_ENABLED=1 go test -race ./internal/... -count=1 -timeout=300s
CGO_ENABLED=0 go test -tags=e2e ./tests -count=1 -timeout=10m
CGO_ENABLED=0 go vet -tags=e2e ./...
```

Linux/Darwin/Windows × amd64/arm64のCGO無効CLIと、変更したstdlib/runtime/apidocs/E2E等のtest packageを一時出力先へcross-compileします。native実行はLinux amd64、他はcompileのみと明記。既存helper/OS/opt-in skipは理由を記録し、今回の都合によるskipや既存期待値の緩和をしません。MCP bundleは必要なら準備済みを再利用し、新規取得を前提にしません。新たな修正・失敗がなければ同じ総合確認を重ねません。

## 今回の成果物・停止条件・計画レビュー

今回の変更はこの計画、レビュー記録、開発indexと段階案への導線のみです。source/API docs/example、テストやprovider呼出し、依存、commit・push・releaseは対象外。`_refs/`と利用者の`agent-server-build.jsonl`は参照・変更しません。

独立した通常レビューでは、段階案との整合、実装の局所性、受信の需要駆動と上限、Request/Responseの共有、HTTP/1のdrainとdeadline、入力cancel/応答/upgradeの生存期間、Go-only終了、shutdownと各実行経路・検証の実現性を確認します。初回30分、新しい根拠・中間結論が10分なければ中断。再レビューは変更箇所と既存指摘の解消を15分以内で確認。利用者の明示依頼に従い、P0/P1/P2の未解消0になるまで修正・再レビューを反復し、P3は任意改善と区別します。
