# 第2グループ低レベルAPIの実装計画

状態：S0〜S6完了。2026-10-19。計画確定前スパイク、本体実装、slice別テスト・レビュー、総合受入と通常/批判的レビュー・修正再確認を完了。未解決のP2以上はない。[本体実装・検証記録](low-level-group2-implementation.md)・[計画確定前スパイク](low-level-group2-spike.md)・[計画レビュー](low-level-group2-review.md)を参照。

[候補と優先順位](../future-candidates.md#一部完了-汎用の低レベルapi拡張と優先順位--2026-10-18)の第2グループを扱う。第1グループの[プロセス・標準I/O](../process-io.md)は実装済み。計画確定時には実動作のスパイク、複数sliceの計画、レビューと修正、計画・証拠の保存を行った。その後、この計画の実行依頼に従って各sliceの本体実装・テスト・コードレビューと最後の総合テストを実施した。

## 目的と採否の方針

通常ファイルを開いたまま行う位置指定の読み書き、TCP/Unixドメインソケットの双方向通信、OSシグナルのJSへの配送を汎用のNOE拡張として提供する。MCP等の専用プロトコル、特定のagent、daemonをGoに作る計画ではない。

既存stdlib・JS・shell/CLIで実現できることを基準にし、追加APIが減らす再open・全面書換え・プロセス中継・ライフサイクル管理を評価する。file、connect、listen、signalは独立に採否判断する。統合した一つの巨大な低レベルAPIや、全OS APIの再現を必須にしない。

stdlibの変更・fork・export置換・prototype上書きは行わない。外部ESM、shell・OSコマンド、NOE向けのJSライブラリ改修は引き続き使える。実行権限は現在のOSユーザー権限のままで、workspace sandboxや新たな許可フローは追加しない。

## 計画確定前のスパイク

独立したopt-inコードを`spikes/low-level-group2/{file,net,signals}/`へ置く。実験moduleを本体へ登録せず、`lowlevel2_spike` build tagで通常build/testと分ける。再現コマンド、失敗も含む結果、Goだけ/JS/runtime/実NOEの確認範囲、OS別の未確認事項を記録する。

| 対象 | 確定前に確認する問い | 成功条件・比較対象 |
| --- | --- | --- |
| file | 同じhandleで位置指定I/Oを行う価値と、openだけではkeepaliveしない所有モデル | 既存の範囲read streamと反復readを比較。writeAtで対象外bytes不変、短いreadとEOF、close/終了時回収 |
| TCP/Unix | EOFと送信終了を片方向だけに伝えられるか。既存Streamを利用できるか | client送信close→server EOF→server応答→client受信。accept/read取消し。listener単独のkeepaliveとclose後終了 |
| signal | process-wideのOS受信をruntime単位の購読へ配送できるか | 複数購読・片方解除・最後の解除、SIGINTのCLI取消しとの共存、購読だけのkeepalive、終了後のGo回収 |

通常close時と強制終了時のUnix socket pathname残留も比較する。強制終了でJS finallyが動くことや、pathnameが必ず自動削除されることを前提にしない。

[スパイク記録](low-level-group2-spike.md)と[再現コード](../../spikes/low-level-group2/README.md)に実行結果を保存した。fileの位置指定I/O、duplexの片方向終了、実Workerを含むsignal配送、timerなしのkeepaliveを確認したため、以下の契約で本実装へ進む案を採用する。これは埋込みQuickJSによる実証で、本体の公開module/CLI/REPLでの確認はS5/S6へ明示的に残す。実プロバイダは今回使わなかった。

## 公開APIと寿命の契約

以下を実装時の契約とする。既存APIの意味は変えず、新しい`noe:` moduleとして公開する。スパイクの実験APIと以下の製品APIは同一ではない。

### ファイルハンドル：`noe:fs-handle`

```js
import {open} from "noe:fs-handle";
const file = await open("./index.bin", {mode: "r+"});
try {
  const {data, bytesRead, eof} = await file.readAt(32, 16);
  await file.writeAt(32, data);
} finally {
  await file.close();
}
```

| 操作 | 契約 |
| --- | --- |
| `open(path, {mode="r"}={})` | Promiseでhandleを返す。空/NUL入りpathは入力エラー。相対pathはそのruntimeのcwd基準。`r`は既存fileの読取り、`r+`は既存fileの読書き。どちらも作成・truncate・appendしない |
| `readAt(position, length)` | Promiseで`{data:Uint8Array, bytesRead, eof}`を返す。位置はbyte単位。部分bytesとEOFを同時に保持し、EOFだけではrejectしない。長さ0は空bytesで成功し、EOFの探索は行わない |
| `writeAt(position, bytes)` | 入力はUint8Array。呼出し時にbytesをGoへcopyし、成功時に全長の書込み数を返す。fileを縮めない。部分失敗はrejectし、errorに`bytesWritten`を保持する。完了済みの書込みをrollbackしない |
| `close()` | Promise。冪等。新規I/Oを拒否し、所有fdのcloseを一度実施する。開始済みI/Oは成功または閉鎖に伴う失敗となり、Go処理の終結を追跡する |

position/lengthは非負のsafe integerでGoのoffset/長さへ変換可能な値とし、加算overflowを拒否する。readのEOF以外の部分失敗は、errorの`data`・`bytesRead`に既取得分を保持してrejectする。OSエラーの`code`は取得できるものを保持し、入出力不正・閉鎖と区別する。

まず通常fileの位置指定I/Oを実装する。一般のデバイス・FIFO・NFS等のblockするOS file I/Oに、即時のtimeout/close完了を保証するAPIとはしない。取消し済みのopenが遅れて成功した場合もfdを捨てずcloseする。Go処理が残る間はresourceの回収完了と表示しない。

`stat`・`truncate`・`sync`は今回の中核用途に必須ではないため後続候補に分ける。既存fileのmetadata取得や新規作成はstdlibも使える。durabilityやcrash consistency、共有seek位置、fd公開、lock、Node FileHandle全面互換は今回の契約に含めない。sandbox/FS.Rootも新設せず、既存のOSユーザー権限に従う。

通常fileは開いているだけでは実行を維持しない。未完了I/Oは追跡し、runtime終了時に所有handleをcloseする。REPLでは同じruntimeに残したhandleを後続入力でも使えるが、待受socketや購読を別入力から動かすREPL再設計は行わない。

### TCP・Unixドメインソケット：`noe:sockets`

`connect(options)`と`listen(options)`はPromise。optionsは`{network:"tcp",host,port,signal?}`または`{network:"unix",path,signal?}`とする。host/pathを明示し、TCP portはconnectで1〜65535、listenで0〜65535（0はOS割当て）。DNSとIPv4/IPv6はGo netの解決に従う。空/NUL入りpath・不正port・既にabortされたsignalは開始前にrejectする。Unix相対pathはruntimeのcwd基準。独自URL scheme、TLS、UDP、abstract namespace、アプリの接続retryは今回追加しない。

open待機中の`signal`はそのdial/listenを取消し、遅れて得られたresourceも回収する。成功後は接続/listenerの所有へ移り、この開始用signalを購読解除する。以後はhandleのcloseまたはruntime取消しで停止する。

| handle | 契約 |
| --- | --- |
| connection | `readable:ReadableStream<Uint8Array>`、`writable:WritableStream<Uint8Array>`、`close():Promise<void>`。接続中/受理済みとも生成runtimeが所有 |
| listener | bound addressのsnapshot `address`、`accept():Promise<connection>`、`close():Promise<void>`。port 0ではaddressから割当てportを取得。TCPはhost/port、Unixはpathを返す |
| listener close | 新規acceptを拒否し、pending acceptは閉鎖errorでreject。既に受理済みのconnectionは閉じず、各connectionの所有者がcloseする |

listenerは必要なときにacceptし、JSから要求されていない接続をGoで無制限に先読みしない。複数のpending acceptはそれぞれ一つの接続を受け取る。read streamはdemand-driven、write streamは完了をawaitしてbackpressureを保つ。stdoutの64KiB捕捉制限はsocketに適用しない。

**方向別の終了を固定する。** 既存`nativeio.endpoint`をduplexへそのまま流用しない。

| 操作・状態 | read方向 | write方向 |
| --- | --- | --- |
| peerからEOF | EOFまでのbytesを返して終了 | 維持 |
| `reader.cancel()` | pending readを解除し終端化 | 維持 |
| `writer.close()` | 維持 | pending writeを正常に完了してFINを送る |
| `writer.abort(reason)` | 維持 | pending writeを中断して終端化。再利用しない |
| `connection.close()` / runtime回収 | pending I/Oを解除して閉じる | 同左 |

write abortではwrite deadline等を使って詰まったwriteを先に解除し、その後CloseWriteする。read側のdeadlineを同時に変更しない。Streamのreason/例外とGo OS終了を一度だけ配送する。両方向のEOF/終了を観測しても明示connection.closeの責務は残し、利用例はfinallyでcloseする。

listenerと接続はopen中に実行を維持し、closeしてGo側のI/Oが終結した時点でそのrefを解除する。listener closeだけで受理済み接続のrefは消さない。module importだけではkeepaliveしない。回収はJSのPromise配送・finalizerを待たない。

Unix socketのbind前に既存pathを削除しない。通常close時は、作成したsocket pathnameのidentityを記録・照合して自分のpathだけを後片付けする。Goの自動unlinkをそのまま任せず、置換された通常file/別socketを削除しないテストを入れる。強制終了後はpathnameが残り得る。再起動時はアプリ所有directoryで状態を確認して必要ならJS/CLIで片付ける。自動stale判定・任意path削除・このためだけの監督プロセスは追加しない。

### OSシグナル：`noe:signals`

```js
import {subscribe} from "noe:signals";
const signals = subscribe("SIGHUP");
try {
  for await (const name of signals) {
    await reloadSettings();
    break;
  }
} finally {
  await signals.close();
}
```

`subscribe(name)`は、要求したsignalだけを登録してからsubscriptionを同期的に返す。初期対象はLinux/DarwinのSIGHUP・SIGINT・SIGTERM・SIGUSR1・SIGUSR2、WindowsのSIGINT。未対応名や捕捉不能なSIGKILL/SIGSTOPは`ERR_UNSUPPORTED_SIGNAL`でrejectではなく同期throwする。OS別対応表を公開資料に記載する。

subscriptionはAsyncIteratorで、`next()`はPromiseで`{value: signalName, done:false}`、`close()`後は`{done:true}`を返す。`return()`はcloseへ接続し、for-awaitのbreakでも解除する。`close()`はPromiseで冪等。複数のpending nextはFIFOで配送し、close時は未解決nextをdoneで解決する。閉鎖後のGo通知はJSへ配送しない。close前に既に完了したnextを取り消すAPIではない。

通知はOSとGoのcoalescing/dropを含み、送信回数と受信回数が一致するexactly-once保証はない。待受nextがないときは、そのsubscriptionの同名signalを一つのpending通知へまとめ、無制限にGo/ownerジョブを積まない。受信後のasync処理はfor-awaitを書く通常のJS/Promiseの一部であり、例外は既存の未処理例外・rejection経路に従う。Go専用のhandler Promise監視は新設しない。

OS受信はprocess-wide、購読とJS配送はruntimeごと。main/Workerに固定の上下関係を作らず、同じprocessで複数購読できる。WorkerのexitはそのWorkerを終了し、NOEプロセス全体のexitではない。WorkerだけのSIGTERM購読でもプロセスの既定終了は抑止されるため、そのことを例と資料に明記する。

SIGINTは現在の通常CLI/REPLの取消しを維持する。JS購読はSIGINTを消費せず、非同期cleanupの完走を保証しない。SIGTERMは購読中のOS既定終了を抑止するだけで、Goが暗黙にprocess.exitを呼ばない。終了したいJSは所有resourceとsubscriptionをcloseしてloopを終える。mainとWorkerの終了協調は通常のメッセージ等で組む。

最後の同名NOE購読の解除で、brokerが所有するsignal.Notify登録だけをsignal.Stopで解除する。signal.Resetで他のGo登録を消さない。OSの既定動作へ戻るのは、他の受信登録もない場合である。unsubscribeと新規subscribeが競合しても、新世代の購読を旧世代のStopで消さない。

購読単独で待受を維持し、明示close/runtime終了/Worker terminateで解除しowner参照を外す。runtime終了ではGoだけでStop/配送停止/AsyncWork終了を行い、JSのhandlerやPromise配送を待たない。スパイクのcallback型APIと固定SIGINT/SIGTERM登録は製品へコピーせず、このsignal選択とIterator契約をS4で実装・検証する。

## ランタイムへの接続と互換性

GoはOS I/Oと所有resourceを扱い、QuickJS値・Promise・callbackの操作はruntime ownerへ配送する。OS操作中にJS値や入力bufferを共有せず、必要なbytesをGo側へ固定する。既存のAsyncWork/Loop、所有resource registryとclose順序を利用するが、既存pipe用endpointの全体closeをソケットのhalf-closeへ流用しない。

module importだけではkeepaliveしない。file open、listener、signal購読のkeepalive条件を分ける。runtime終了時はGoだけでunsubscribe/closeを開始し、JSの応答やhandler完了を回収の前提にしない。

| 実行形態 | 接続方針 |
| --- | --- |
| run/eval | 同じcwdとOS権限で利用。通常終了、失敗、timeout、Ctrl+Cと所有resourceの回収を接続 |
| agent run_js | 通常のNOE子プロセス内で使う。broker/認証は不要。親interrupt/timeout/強制停止の後も処理が動き続けないことを確認 |
| Worker | runtimeごとにmodule登録・resource所有・owner配送。terminateでそのWorkerのhandle/購読のみ回収し、mainと別Workerを壊さない |
| REPL | 現行の入力内drain方式を維持。待受・通信・解除まで同一入力で行う。未完了resourceを伴うエラー/中断では既存session回収規則へ接続 |

新moduleは通常の生bytes通信で、observabilityのJSONLとは別である。`noe:io`へ明示的に出力したときだけ既存のイベントwriterを通る。既存agent・serve/fetch・child_process・process/stdout・noe:process/noe:io・import mapの挙動を変えない。

## 実装スライス

S0は今回実施する計画確定前スパイク。S1以降は本体実装時の作業単位で、各sliceを**実装→対象を絞ったテスト→コードレビュー→指摘修正・再確認**の順で完了させる。P2以上の未解決指摘があれば次の依存sliceへ進めない。並列化は所有ファイル・resource境界を切り分けられる場合のみ行う。

| Slice | 実装範囲 | テストとコードレビューの焦点 |
| --- | --- | --- |
| S0 計画確定前の実証 | file/net/signalsの独立prototype、比較と結果記録 | 実動作と主張の一致、fixtureの補助待機で寿命管理を隠さないこと、未確認範囲 |
| S1 ファイルハンドル | `noe:fs-handle`の中核操作、所有・close、通常runtimeからの垂直接続 | 非対象bytes不変、EOF/部分I/O、open失敗、close競合、owner配送、既存range streamとの差 |
| S2 TCP・Unix接続 | `noe:sockets.connect`、duplex Streams、送信終了と全体close | FIN後の応答、read/write並行、blocked I/O取消し、接続失敗、resource回収 |
| S3 TCP・Unix待受 | listener/accept、接続所有、待受keepalive、Unix pathname | 複数接続、accept close、listenerと接続の独立close、通常/強制終了後のpath状態 |
| S4 シグナル配送 | process-wide broker、runtime購読、解除とkeepalive | 複数runtime/Worker、部分解除と最後の解除、SIGINT既存取消し、SIGTERM既定動作、close競合 |
| S5 全実行形態と資料 | main/Worker/REPL/run_jsへの統合、例、API資料・instructions更新 | 既存APIへの回帰、timeout/interrupt/終了、REPL同一入力、実NOEでの往復と回収 |
| S6 総合テスト・全体レビュー | 機能を組み合わせた実行と全体文書の整合、OS差の確認 | 下記の総合受入条件、通常/批判的コードレビューとP2以上解消 |

想定する変更箇所は以下。既存`host.go`をさらに大きな実装の置場にせず、module登録と所有関係の接続に留める。

| 箇所 | 責務 |
| --- | --- |
| `internal/nativeio/file.go`・`net.go`・各JS adapter | 新moduleの入力snapshot、Go handle、ownerでの値/Promise変換。既存の片方向endpointとは別型でduplex状態を持つ |
| `internal/signals/` | OS別の名前対応とprocess-wide broker、購読世代、GoだけのStop。QuickJS値を持ち込まない |
| `internal/nativeio/signals.go`・JS adapter | runtime所有のsubscription/Iterator、ref、pending nextとclose時の解決 |
| `internal/nativeio/host.go` | module登録、既存resource registryとactive/refの共通接続。資源登録とkeepaliveを分離 |
| `internal/runtime/{stdlib.go,eventloop.go,session.go}` | main/Worker登録、既存drainとREPLの中断・回収への接続 |
| `tests/`、`examples/`、`docs/`、組込みinstructions | 実CLI/fixture agent/REPLの受入と利用例、公開契約・限界の説明 |

S1は短いread/非EOF部分エラー・部分writeのfake backendを使い、ディスク枯渇を起こさず返却情報を検査する。S2/S3はread/write/acceptが開始した合図から取消しを行い、固定sleepだけで「block中」と判定しない。S4はmainとWorkerで異なるsignal名を購読するケース、同名複数購読、一方close/terminate、別Go NotifyContextと最後の解除、新旧購読世代の競合を含める。S5ではWindows/Darwinを含むビルドを先送りせず、利用できないsignal名を明示errorにする。

各sliceのレビューは所有・部分失敗・停止のsource-to-impactを確認し、テスト件数を達成目標にしない。S0コードは計画レビュー時にも主張と再現性を確認する。

## 最後の総合テスト

単体のAPI呼出し成功だけでなく、NOEの実行と停止を観測する。実装した新APIを公開moduleから使い、内部helperを直接呼ぶだけの確認で代用しない。

1. ファイルの索引から位置指定で情報を読み、TCP/Unix serverへ要求し、結果を対象位置へ更新する。対象外bytesと送受信bytesを照合する。
2. 同じJS処理をTCPとUnix socketで動かし、反復・並行要求と送信EOF後の最終応答を確認する。
3. mainとWorkerが同じsignalを購読し、片方の解除/terminate後も他方が受信できる。最後の解除後の既定動作を隔離したhelperプロセスで観測する。
4. 補助timerのないlistener/購読が待機を維持し、close/解除後は実行が終了する。開いただけの通常fileは終了を妨げない。
5. blocked accept/read/write、購読待機中にtimeout/interrupt/Ctrl+C/Worker terminateを行い、停止・残るfile descriptor/接続/pathを確認する。OSの通常ファイルI/Oの取消し限界は別に記録する。
6. REPL同一入力、JSONL/observe、既存process/io、HTTP、agent queue/steer/interrupt、import mapに回帰がないことを確認する。

ローカルfixture agentを使えばrun_jsの本体経路を費用なしで確認できる。実モデルがAPI説明を使って構成できるかが未解決の場合のみ、許可済みの実providerを小さなタスクで利用し、モデルの誤用とAPI不具合を分けて記録する。実providerの成功をOS回収・raceテストの代用にしない。

標準の`go test ./...`、`go test -tags=e2e ./...`、`go vet -tags=e2e ./...`に加え、新しいGo共有状態とnative I/O、signal broker、既存runtime/processの対象race検査を行う。Linux/Darwin/Windowsのamd64/arm64ビルドを確認し、native実行したOSとcross buildだけのOSを区別する。対応しないOS機能を疑似成功にせず、公開資料とerrorへ明記する。

## 計画レビューと完了条件

計画作成後に通常・批判的レビューを行う。通常レビューは目的・実装可能性・スライス・受入条件、批判的レビューは誤った実証成功・所有/取消し/配送の衝突を重点にする。P2以上の指摘は根拠と採否を記録し、計画の具体的な契約・テスト・範囲で解消して再レビューする。未解決指摘を単に後回しと書いて完了解釈しない。

再レビューは変更箇所と既存指摘の確認を基本とし、ユーザーの明示によりP2以上がなくなるまで必要な回数を行う。初回各30分以内・再レビュー各15分以内、10分間新しい証拠/中間結論がなければ確認済み範囲と暫定所見を受け取り、主担当が具体的な不足を判断する。

計画確定前のスパイク・検証結果・未確認範囲と、両レビューの最終結果を記録済み。計画確定時の保存対象は計画・レビュー・スパイクと再現コードで、本体sliceの実装完了とは区別した。計画の確定時には利用者の指定でcommit/pushした。本体実装の完了状態と検証範囲は[実装記録](low-level-group2-implementation.md)に記載し、今回の実装作業で公開・releaseは行わない。
