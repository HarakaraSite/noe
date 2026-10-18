# 汎用プロセスAPIと標準I/Oストリームの実装計画

状態：実装完了。2026-10-18。利用者の指示に従い、S1〜S5の実装・テスト・コードレビュー、指摘対応、最後の総合テストを完了した。[実装記録](process-io-implementation.md)に結果・証拠・OS別の未確認範囲を記録した。

[低レベルAPIの候補と優先順位](../future-candidates.md#一部完了-汎用の低レベルapi拡張と優先順位--2026-10-18)の第1グループを扱う。初回の通常・批判的レビューと、利用者の「stdlibは今回は範囲外」という指定を反映した改訂案。通常・批判的の再レビューを完了し、結果を末尾へ記録した。

## 目的と成功条件

NOEのJSから、起動した子プロセスへ継続的に書き込み、その出力を終了前から読み、終了を待つ・停止する操作を行えるようにする。NOE自身の標準入力もEOF前から非同期に読み、標準出力・標準エラーへバイナリを逐次書き込めるようにする。

Go側は汎用のプロセス管理とバイト列のI/Oを提供する。MCP、LSP、JSONLの解釈、要求IDの管理、接続の再試行などはJS側に置く。MCP専用のGo APIは作らず、go-quickjs本体の変更・fork・上流への提案を前提にしない。

**今回はstdlibの変更・API拡張・exportの置換を行わない。** `process.stdin/stdout/stderr`へのStreamプロパティ追加、`execFile`等のバックエンド置換、`ReadableStream.prototype.pipeTo`の修正・上書きは対象外とする。既存Web StreamsとWorkerの公開hookを利用し、NOE側の新モジュールとホスト実装で成立させる。

利用者が確認できる成功条件は次のとおり。

- 一つの常駐子プロセスへ複数の要求を送り、その都度応答を受け取れる。
- stdoutとstderrを実行中から別々に読み、stdinを閉じてEOFを伝えられる。
- 子のstdoutをファイルや別の子のstdinへWeb Streamsで接続できる。
- パイプ入力を閉じなくても、NOEのJSがJSONLの1行を受け取り応答できる。
- 正常終了・非ゼロ終了・起動失敗を識別でき、明示停止・Ctrl+C・実行期限・Worker終了で所有リソースを回収できる。
- JS製stdio transportを通してMCPの初期化・tool取得・複数回の実行を行える。MCP以外の常駐ワーカーでも同じAPIを使える。

## 現行経路と追加価値

| 現行箇所 | 確認した動作 | 今回の接続点 |
| --- | --- | --- |
| `internal/runtime/stdlib.go` | 固定版go-quickjsのstdlibをinstallし、ホストのstdin・stdout・stderr、cwd・envを渡す | 同じホスト設定でNOE拡張をinstallする |
| go-quickjs v0.27.0 `stdlib/exec.go` | `execFile()`は既に非同期だが、出力は終了後にまとめて返す | 実行中のpipeと操作ハンドルを新設する |
| 同 `stdlib/process.go` | `process.stdin.read()`は`io.ReadAll`、stdout・stderrは文字列の同期write | 既存APIを残し、別モジュールで逐次のバイトストリームを提供する |
| `internal/runtime/eventloop.go` | stdlibのLoopとQuickJSのジョブを駆動する | 新しい非同期処理の待機と結果配送を接続する |
| `internal/runtime/session.go` | REPLのstdinはソース入力専用。各入力は現在の非同期処理をdrainする | stdinを横取りせず、既存の待機・中断規則と統合する |
| `internal/cli/cli.go` | 観察時はホストのstdout・stderrをイベント用writerで包む | 新APIも同じwriterへ出力し、観察経路を維持する |
| `internal/agent/process_posix.go`・`process_windows.go` | agent用子プロセスのprocess group・Job管理 | 必要な所有・停止処理を汎用部へ抽出して共用する |

ファイルの逐次I/O、HTTP、WebSocket、タイマー、Workerは既存である。今回それらを作り直さない。外部ESM、シェル・OSコマンド、ライブラリのNOE向け改修も引き続き利用できる。既存のJS＋shell＋FIFO方式は実現可能性の根拠であり、本APIの内部実装には匿名パイプを使う。

## 今回の範囲

- `noe:process`による子プロセスの起動、入出力、終了観察、停止、close。
- `noe:io`によるホスト標準入出力のWeb Streams。
- stdlibを変更しない、NOE所有の取消し対応`pipe()`ヘルパー。
- `run`・`eval`・`repl`、agent生成JS、Workerへの組込みと、既存の実行取消しへの接続。
- 利用例、API資料、モデル向けinstructions、変更経路のテスト、最後の総合確認。

OSシグナルのJSへの公開、汎用FileHandle、TCP/Unixソケット、ファイル監視、TTY・raw mode・PTYは後続グループで扱う。任意PIDを指定する管理API、daemon化、親終了後の独立実行、URL import・npm解決、Node.js全体の互換層、MCPクライアント/サーバーの製品組込み、リリース作業は本計画に含めない。外部JS SDKは検証・応用例として利用できる。後述の内部監督プロセスは公開daemonや新CLI機能ではなく、NOEプロセスAPIの所有・回収を実装する部品とする。

## 公開API案

### `noe:process`

```js
import { spawn } from "noe:process";

const child = await spawn("worker-program", ["--jsonl"], {
  cwd: ".",
  env: { WORKER_MODE: "jsonl" },
  stdin: "pipe",
  stdout: "pipe",
  stderr: "pipe",
});
```

`spawn(command, args = [], options = {})`は、起動に成功し、入出力が使える時点でハンドルを返すPromiseとする。コマンドは引数配列で直接起動する。シェルを使う場合は`spawn("sh", ["-c", script])`のように明示する。

| 項目 | 契約案 |
| --- | --- |
| `cwd` | 省略時はそのruntimeのcwd。相対指定も同じcwdから解決する |
| `env` | 呼出し元runtimeの`process.env`を起動時に読み取り、指定キーを上書き。`null`の値はその子の環境から除く。Workerで指定された環境も反映し、rootの環境を固定して捕捉しない。ホスト全体の環境と既存execFileの挙動は変更しない |
| `stdin`・`stdout`・`stderr` | 各`"pipe"`・`"inherit"`・`"ignore"`。既定はすべて`"pipe"`。inheritはそのruntimeに渡された標準I/Oを使い、Goのグローバル標準I/Oへ直接迂回しない |
| `child.pid` | 要求した対象コマンドのPID。内部監督プロセスのPIDではない。操作対象を指定するためにJSからPIDを送り返す必要はない |
| `child.stdin` | `WritableStream<Uint8Array>`。pipe以外では`null`。closeで子にEOF、abortで書込み側を閉じる |
| `child.stdout`・`child.stderr` | 独立した`ReadableStream<Uint8Array>`。pipe以外では`null`。cancelは対応する読取り側を閉じる |
| `child.exited` | `Promise<{ code: number | null, signal: string | null }>`。対象コマンドの終了を表し、非ゼロ終了も値として返す。起動失敗はspawnをrejectし、待機・監督機構自体の失敗はexitedをrejectする。内部helperの終了コードを対象コマンドの結果へ代用しない |
| `child.kill(signal = "SIGTERM")` | 所有するprocess group/Jobへ停止を要求するPromise。終了待ちとは分ける。POSIXでは指定シグナルを送る。WindowsではSIGTERM・SIGKILLともJobの強制終了となり、POSIXのシグナル配送とは意味が異なることを明記する |
| `child.close()` | 冪等な回収操作。残る所有プロセスを強制停止し、所有pipeを閉じ、対象コマンドと内部helperの回収を待つ。明示closeでは未読出力を捨ててよい。終了済みのハンドルも回収できる |

シグナル終了をWindowsで架空のPOSIX signal値へ変換しない。環境ごとに利用可能な終了情報を返す。process group/Jobの所有範囲と、そこから独立したプロセスまで管理するAPIではないことを資料に記述する。

ハンドルは生成したruntimeが所有する。生存中の子と回収中の内部helperは実行ループを維持する。`exited`の解決とstdout/stderrのEOFは別であり、子の終了直前の出力も読めるようにする。大きな出力を受け取る場合は両方を並行して読むか、inherit/ignoreを選ぶ。Go側で全文をため込んだり、既存execFileの出力上限をこのAPIへ持ち込んだりしない。

子のstdinをcloseしても、子の終了を自動的に仮定しない。正常終了を待つ猶予やTERMからKILLへの切替はJSで記述できる。MCP専用の停止手順を汎用APIに埋め込まない。

### `noe:io`

```js
import { stdin, stdout, stderr, pipe } from "noe:io";

await pipe(stdin, stdout, { preventClose: true });
```

- `stdin`は`ReadableStream<Uint8Array>`、`stdout`・`stderr`は`WritableStream<Uint8Array>`。
- importやストリーム作成だけではstdinを読まない。最初のread/pullから非同期読取りを始め、EOFを待たずに到着したバイトを返す。
- stdout・stderrへのwriteも非同期で、書込み完了でPromiseを解決する。文字列の変換やJSONLの行分割はJSで行う。
- ホスト標準I/Oは借用品。ストリームのclose/abort/cancelやruntime終了によって、呼出し元のstdin・stdout・stderrそのものを閉じない。取消し可能な読取りアダプターと、JS側のストリームの寿命を分ける。
- 既存の`process.stdin.read()`・`process.stdout.write()`・`console`はstdlibのまま利用する。新しいStreamプロパティやaliasは追加しない。同じstdinを新旧APIで並行消費した場合の読取り順を保証するAPIは追加しない。内部で勝手に複数の読取りを開始しない。
- 同じ出力先を使う既存console・新ストリーム・WorkerのGo側writeを共通のwriter経由で直列化する。`--events`・`--observe`では既存のイベント用writerを通る。通常実行のバイナリ透過と、観察モードの文字列イベント表示を混同しない。

`pipe(source, destination, { signal, preventClose = false } = {})`は、NOE所有のJSヘルパーとする。EOFまで転送して必要なら宛先をcloseし、完了を待つPromiseを返す。エラー・abortではreader.cancelとwriter.abortを開始し、待機中のread/writeにも取消しを伝える。abortはその理由でrejectする。`preventClose`は正常EOF時だけ宛先を開いたままにする。子のkillは行わず、必要なら呼出し元がfinallyでchild.closeを行う。

固定版stdlibの`pipeTo(..., {signal})`は、データ待ち中のabortで取消しが進まないことを実NOEで確認した。この計画ではstdlibを修正せず、取消しが必要な接続に`noe:io`のpipeを使う。通常の`.pipeTo()`もそのまま利用できる。新helperは標準pipeTo全体の再実装を目的とせず、上記オプションと新native I/Oの取消しが接続することをS1で確認する。

子の出力をホストへ流す例では、ホストのストリームを後続処理でも使えるようにする。

```js
import { spawn } from "noe:process";
import { stdout, stderr, pipe } from "noe:io";

const child = await spawn("some-command", [], { stdin: "ignore" });
try {
  const [result] = await Promise.all([
    child.exited,
    pipe(child.stdout, stdout, { preventClose: true }),
    pipe(child.stderr, stderr, { preventClose: true }),
  ]);
  console.log("exit code:", result.code);
} finally {
  await child.close();
}
```

## ランタイムとの統合

### 非同期処理と所有権

GoのOS操作・待機と、QuickJSの値の操作を分ける。非同期結果はruntimeのownerへ配送し、別goroutineからJSのPromiseや値を操作しない。`Runtime.StartAsyncWork`または既存LoopのBegin/Post/Doneを使い、生存する子と実行中のread/writeを追跡する。moduleのimportや未使用のstdinは実行ループを維持しない。

子用pipeはNOEが所有する`os.Pipe`等で作り、起動後に親が不要な端を閉じる。inheritの転送にも所有pipeとpumpを使い、共通writerや借用readerをcmd.Std*へ直接渡してCmd.Waitがcopy処理まで待つ構成を避ける。対象コマンドのWait、出力EOF、pump完了を分離し、Waitで未読stdoutを先に閉じない。close/取消し時は、停止・pipeのclose・Go待機の完了を先に成立させ、JSへのPromise配送を回収の完了条件にしない。詰まったwriteの正常完了を待ってからkillする順序にしない。

ホスト標準I/Oの取消しは読取り・書込みとも最初のスライスで実証する。任意の`io.Reader`・`io.Writer`を一般に中断可能とは仮定せず、CLIの実stdin/stdout/stderr、メモリI/O、テスト用pipeについて、借用元を閉じずに取消せるアダプターを決める。必要ならruntime内部のI/O供給インターフェースを分け、既存の一括readを使う経路と共存させる。OSのファイル記述子を複製するだけで必ず待機を中断できるとは仮定しない。

共通writerはNOEのホスト境界で一度作り、その同じwriterをstdlib Installの設定、新Stream、Workerへ渡す。stdlib内部のWorker用lockだけに依存して、新APIが元writerを並行操作する構成にはしない。これはNOE側のI/O設定の接続であり、stdlibの関数やexportの変更ではない。

### 実行形態

| 実行形態 | 方針 |
| --- | --- |
| run / eval | ホストstdinをデータ入力として使う。実行期限・Ctrl+C・未処理例外・process.exit時に所有する子とI/Oを回収する |
| agentのrun_js | 通常のNOE子プロセス内で同じAPIを使う。認証brokerを経由せず利用できる。POSIXの内部監督プロセス/Windows Jobによって、外側RunnerからNOEを強制終了されても所有する子を回収する |
| repl | 標準入力はREPLソースが所有し、noe:ioのstdinは既存のprocess.stdinと同様に空入力。子の通信はその子のpipeを使う。現在の「入力内の非同期処理をdrainしてから次のpromptへ戻る」契約を維持し、通信・closeまでを一つの入力で行える例を用意する。生存する子を次のpromptへ持ち越してバックグラウンド駆動するREPL再設計は別の検討とする |
| Worker | stdlibのWorkers.Installed hook等でruntimeごとに同じ拡張を登録する。stdinは既存Workerと同じ空入力、stdout・stderrはホストの出力先を継承。子とpipeはそのWorkerが所有し、terminate時に回収する |

REPLのエラー・中断判定に新しいリソースも含める。現在のactive stdlib workがある場合のsession終了規則を迂回して、新リソースだけが残る状態を作らない。Workerのmodule登録・非同期配送は親runtimeのJS値を共有せず実装する。

### OS別の停止処理とrun_jsからの回収

**初回レビューのP1：新childを別process groupに置くだけでは、外側Runnerがrun_jsのNOE groupをSIGKILLした際にchildが残る。内側NOEのOnCloseは実行されない。** 既存agent runnerのscopeを単に抽出して適用するだけでは、この問題を解消できない。

POSIXでは、各spawnに対してNOEバイナリの内部実行モードを使うGo製の監督プロセスを置く案を採用する。QuickJS・agent loop・provider接続は監督プロセスで起動しない。

```text
呼出し元NOE ── 所有者通知pipe・制御pipe ── 監督プロセス
                                              └ 対象コマンドのprocess group
```

- 監督プロセスと対象コマンドのgroupを、呼出し元NOEのgroupから分ける。監督プロセスは対象groupの外に置き、対象groupへのSIGKILL後も対象コマンドをWaitして実際の終了値を報告できるようにする。
- 所有者通知pipeの書込み端は呼出し元NOEだけが保持する。監督側は読取り端を監視し、EOFで対象groupを強制停止・回収して自身も終了する。対象コマンドやその子へこの通知pipeを継承させない。
- 呼出し元の通常close/Worker terminateは通知端のcloseまたは明示制御を行う。run_jsのNOEが外側RunnerのSIGKILLで終了した場合も、OSが通知端を閉じるため同じ回収へ到達する。JSのfinallyや内側NOEのOnCloseの実行を前提にしない。
- 起動中も監督側が所有者の終了を記録する。所有者終了後に対象を起動し続けず、起動との競合では取得したgroupを直ちに回収する。親へのspawn成功通知は対象の起動成功後に行う。
- 対象コマンドの実stdin/stdout/stderrは匿名pipeで接続し、対象のPID・起動結果・終了値・停止応答は別の制御pipeで報告する。監督用メッセージを利用者のstdout/stderrへ混ぜない。envを含む起動設定は内部pipeで渡し、CLI引数やログへ出力しない。
- 対象コマンドが終了したら、監督側は残る所有groupを回収して終了する。既にpipeへ出力された末尾データは呼出し元が読める。監督プロセスの終了待ちと対象コマンドのexitedは別に管理する。

追加の監督プロセスを置くことで、spawnの起動時間・メモリ・プロセス数にコストが生じる。S1で必要なpipe本数、起動中取消し、所有者の強制終了、繰返し起動後の残存と実測コストを確認し、S2へ進む前に内部プロトコルと回収方式を固定する。実測で方式変更が必要になった場合も、外側RunnerのSIGKILL後に子が残る旧案へ戻さない。

Windowsでは既存のJob管理を基に、対象を起動時から所有Jobへ所属させる。外側RunnerのJobと新APIのJobの包含関係を確認し、run_jsの取消しが内側の対象へ届くことを確認する。POSIXのhelperとWindowsのJobの違いは内部に置き、公開APIの対象PID・終了値・closeの意味を揃える。

必要なprocess group/Job操作だけを`internal/processscope/`等へ共通化する。既存agent runnerの環境整形・認証・結果形式はagent側の責務に残し、停止契約も維持する。agent packageから新APIへ呼び返す循環依存や、provider/brokerへの新しい依存を作らない。配布対象のビルドを維持し、native確認とクロスビルドのみのOSは区別して記録する。

## 既存NOE機能への影響

| 機能 | 今回扱う接続・改善 | 今回変更しないこと |
| --- | --- | --- |
| run_js / interrupt_agent / 実行期限 | 新spawnの子・孫まで停止が届く所有関係を実証する。内部監督と既存Runnerのgroup停止を接続する | モデルtool名、run_js結果、steer・queue・interruptの公開契約 |
| agent runner | 汎用化できるscope操作を共用し、新APIの子がrun_js取消し後に残る問題を解消する | provider認証・環境整形・agent状態管理を汎用process APIへ移さない |
| events / observe | 新I/Oも同じホストwriterへ通し、既存のJSONLや表示経路を維持する | run_js出力の自動live化や、バイナリを復元できる新イベント形式は追加しない。通常実行・eventsファイルのmirrorはバイト出力、イベントtextとobserveは文字列表現という既存の区別を記述する |
| REPL / Worker | NOE拡張の登録、リソース追跡、取消しを接続する。新spawnのenvはそのWorkerの設定を使う | REPLのバックグラウンド駆動、stdlibのWorker/execFileのenv挙動の変更 |
| noe:decision / noe:auth | 新I/Oを利用する場合も共通writerを使う | provider/API・認証契約の変更 |

stdlibの既知の課題や改善案は上流側の別検討として残す。今回は`process.std*`の拡張、`execFile`の停止改善・失敗情報保持、`pipeTo({signal})`の修正を完了扱いにしない。

## 実装スライス

各スライスで実装・対応するテスト・コードレビューを行い、採用した指摘を修正してから次へ進む。下のファイル名は責務の配置案で、既存のモジュール構成に合わせて調整する。

| Slice | 作業と主な配置 | 受入条件 |
| --- | --- | --- |
| **S1 共通I/Oと所有者終了の実証** | `internal/hostio/`のread/write・取消し・closeとStreams接続、noe:ioのpipe helperを試作。標準I/Oの借用取消し、POSIX内部監督/Windows Job、Worker hookとAsyncWorkを確認する | EOF前のread、詰まったwriteとreadの取消し、借用元を閉じないこと、import時の非読取りを確認する。所有者NOEをgroupごとSIGKILLしても別groupの対象・孫・監督が残らない。起動中の取消しと実測コストを記録し、S2へ進む前にI/Oアダプターと内部監督方式を固定する |
| **S2 汎用プロセス操作** | `internal/process/`等に起動・匿名pipe・Wait・kill・closeと内部監督モード、`internal/processscope/`に共用部。`noe:process`のJSラッパーとnative登録 | 一つの子と複数回双方向通信できる。stderr分離・バイナリ・cwd/env・終了値を取得できる。inheritのpump待ちと対象終了を分離し、起動失敗・EOF・非ゼロ終了・停止・詰まったI/Oを扱える。既存agentの回収も維持する |
| **S3 ホスト標準I/O** | `noe:io`公開、`runtime.Options`とCLIの入力アダプター、既存writerとの共用 | run/evalでstdinを開いたままJSONLに応答できる。バイナリを透過する。close/cancelでホストの標準I/Oを閉じない。既存process.stdin.readと観察出力を維持する |
| **S4 全実行形態と応用例** | `internal/runtime/`のmain/Worker/REPL登録・リソース追跡、ローカル常駐ワーカー・pipeline・JS製stdio serverのexamples。公式MCP SDKのtransportを新APIへ適合してローカルで確認する | run/eval、REPL同一入力、Worker、agent生成JSで使える。生成JS→spawn対象→孫の経路を作り、run interrupt・timeout・Ctrl+Cで全終了を確認する。Worker terminateと新pipeのsignal取消しも確認する。汎用ワーカーとMCPの両方で複数回通信できる |
| **S5 文書・総合確認・全体レビュー** | README両言語、API資料、REPLガイド、examples案内、instructions、検討メモの状態を更新。`docs/development/process-io-implementation.md`へ実装・検証・指摘対応を記録 | 第1グループの公開契約と実動が一致する。未対応・OS差・外部SDKの適合箇所が明確。総合検証と全体コードレビューを完了する |

## 検証の方針

通常の検証はローカルプロセスと実NOEバイナリを使う。実プロバイダ・有料APIは不要。Goの小さなhelper processを使い、NodeやPythonがないとテストできない構成にしない。MCP SDKを用いる統合確認は通常テストから分け、SDKの版・準備方法・結果を記録する。SDKは製品バイナリや全テストの必須依存にしない。

重要な実経路を次のように確認する。

- **常駐ワーカー**：子の起動確認後、1件目の要求と応答、2件目の要求と応答、EOFと終了を順に確認する。3件の並行要求でも応答をJS側で対応付けられることを確認する。
- **逐次出力**：子の終了をgateで待たせ、stdout・stderrを終了前に取得できることを確認する。
- **pipeline・バイナリ**：JSまたは子から別の子/ファイルへバイト列を流し、変換前後のデータと終了を確認する。既存32 MiBキャプチャ上限による切捨てがないことも新APIの経路で確認する。noe:ioのpipeについて、read待ち・write待ち中のsignal取消しとpreventCloseを確認する。stdlibのpipeToは修正対象にしない。
- **ホスト標準I/O**：入力を開いたまま1行送り、EOF前の応答を確認する。入力待ち・出力のwrite待ちでの実行期限・取消し、標準出力の再利用も確認する。
- **失敗と停止**：存在しないコマンド、非ゼロ終了、EOFで正常終了、EOFを無視する子、詰まったread/writeの明示closeを実際に起こし、診断と終了を確認する。シェルが子を起動する場合の所有範囲も確認する。
- **統合**：run/eval、既存REPLの同一入力・中断、Worker固有envと終了、fixture providerからのrun_jsで操作する。run_jsの外側groupを強制終了し、対象・孫・監督が残らずRunnerの出力収集も完了することを確認する。inheritとignoreの両方で回収を確認する。`--events`・`--observe`時も既存の出力writerを通ることを確認する。

可能な箇所はready通知・pipe・gateで順序を制御し、固定sleepの経過だけで成功判定しない。未確認の外部仕様や無関係な境界値の網羅を目標にしない。

スライス中は変更packageと対応するE2Eに絞る。最後に`go test ./...`、`go test -tags=e2e ./...`、`go vet -tags=e2e ./...`を実施する。並行処理・所有権を変更したpackageと関連E2Eはrace detectorでも確認する。OS別コードを変更するため、配布対象のクロスビルドを一度確認する。必要なチェックが通った後の反復は、新しい変更・失敗・未解決事項がある場合に限る。

## 計画段階で残した確認事項

以下は着手時の確認事項。実施結果は[実装記録](process-io-implementation.md)を参照。

- S1でホスト標準I/Oの取消しアダプターとruntime内部のI/O供給契約、POSIXの内部監督方式・コストを実証して固定する。任意のio.Reader/io.Writerの中断保証や、未実証の親終了回収を成功と扱わない。
- Workerは既存Installed hookとruntime固有のAsyncWorkで実現する方針だが、終了時の回収まで実証してから完了とする。
- REPLのバックグラウンド駆動と、後続のpromptから同じ常駐子を操作する拡張は今回とは別の設計課題として明示する。
- module名・公開APIは上記を具体案とし、詳細計画レビューで確認する。上流採用・stdlibの改善・外部import機構の完成を着手条件にしない。

## レビューの反映

初回の通常・批判的レビューで共通したP1は、別process groupの子がrun_js取消しから外れる所有関係である。POSIXの内部監督/Windows Jobを明記し、S1の親強制終了とS4の実agent取消しの実証へ接続した。これは設計による対応案であり、実装・動作確認済みとは扱わない。

利用者の範囲指定に従い、stdlibのAPI拡張・置換・pipeTo修正は除外した。標準I/Oはnoe:ioのみで公開し、signal付きの接続はNOE所有のpipe helperで扱う。Worker環境、writerの共用、inheritの終了とpumpの分離を具体化した。

2026-10-18、変更箇所と既存P1の解消方針に限定して、通常・批判的の再レビューを実施した。両レビューとも追加の必須修正はなく、S1の実証へ進める計画と評価した。

既存P1は、監督プロセスを所有者・対象の両groupから分離し、所有者専有pipeのEOFから対象を停止・回収する設計により、対応経路が成立した。stdlibを変更しない境界、独立したpipe helper、Worker環境、共通writer、inheritのWaitとpumpの分離も確認した。実装・native動作の成功を保証する評価ではなく、起動中取消し、所有者強制終了後の回収、借用I/O取消し、Windows Job包含、追加コストはS1、実run_js取消しはS4で確認する。

[開発記録](../README.md) · [検討メモ](../future-candidates.md)
