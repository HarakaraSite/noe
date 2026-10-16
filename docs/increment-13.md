# NOE インクリメント13 OpenRouter agent loop計画

[開発記録](README.md) · [スパイク](../spikes/agent-loop/README.md)

状態：S1〜S7の実装・テスト・個別レビュー、実OpenRouter検証、最終総合テスト、通常・批判的全体レビューが完了。必須指摘はすべて修正・再確認済み。Darwin/Windowsのnative実行は未確認で、6targetクロスビルドの成功と区別する。各段階の変更・検証・レビュー結果は[実装記録](increment-13-implementation.md)へ記載する。この文書を今回の要件・設計・受入条件の正本とする。

## 目的と合意済みの方針

定型の`.js`から`agent.run("資料を要約して")`を呼び、モデル判断、必要なJS実行、修正、親子agentの処理を経て最終回答を返せるようにする。実行中に人間の承認・追加質問・再開入力を待つ状態は設けない。

- 初版の実プロバイダはOpenRouter。実接続の確認モデルは`deepseek/deepseek-v4.1-flash`。別モデルへの自動フォールバックは設定しない。
- agent loopと作業の構成はJS、認証情報の管理・プロバイダ通信・子プロセスの管理はGoが担当する。
- モデルへ公開するtoolは`run_js`一つ。生成JSから子agentを呼び、子もJSを実行できる。
- 実行をまたぐ会話の保存・再開は作らない。そのrunの履歴と実行中の親子関係・キャンセル管理は必要。
- キャッシュ用キーと会話セッションを区別する。OpenRouterへ必要な履歴を毎回送る。
- 任意の回数・トークン・時間上限を利用者が指定できるようにする。未指定時に独自の回数・委譲深度制限を加えない。
- NOEは実行ユーザーのOS権限で動く。認証をGoへ集約しても、同じOSユーザーのファイル読取り権限を隔離することにはならない。

ユーザーは計画レビュー後、各スライスの実装・テスト・コードレビューと、最後の総合テスト・全体レビューを指示した。本体のローカル変更と検証、許可済みOpenRouterキー・指定モデルでの実接続を対象とする。release・公開は別作業とする。

## スパイクで確認した事実と未確認事項

2026-10-16の[OpenRouter実接続記録](../spikes/agent-loop/results/openrouter/results.json)で、単体集計、子JS失敗後の修正、親と子2体の並行処理、認証付きローカルサービス中継を確認した。モデル呼出し20回、JS実行14回、キャッシュ読取り9,344 tokensを観測した。これはその試行の結果で、実装後の合格や将来の応答一致を示さない。

スパイクのGoホストは外側から既存NOEを起動する別実行ファイルであり、製品の`noe:agent`モジュール、通常の`run`・`eval`・REPLへの組込み、配布先OSでのプロセス管理は未確認。模擬Web検索は実検索サービスではない。

## 初版の範囲

対象は`noe:agent`、OpenRouterモデル通信、ファイル配置によるAPIキー設定、`run_js`、親子agent、正常終了・失敗・中断の管理、使用例、検証、ガイド更新とする。通常の`noe run`・`noe eval`とREPLから利用する。

複数の実プロバイダ、ChatGPTプランのOAuth、実Web検索サービス、永続会話・再開、厳密なJSON形式の保証は次段階とする。スパイクの模擬検索やfixture用の指示は本体APIに含めない。Workersからのagent利用は初版の受入対象に含めず、未対応ならimport時に明確な診断を返す。通常Workerの既存APIは維持する。

## 公開JavaScript API

```js
import { createAgent } from "noe:agent";

const agent = createAgent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  name: "summary",
  system: "日本語で簡潔に回答する",
  // maxTokens: 4096,
});

const result = await agent.run("report.txtを読んで要約して", {
  cacheKey: "summary-v1",
  // maxSteps: 6,
});
console.log(result);
```

- 組込みモジュールとして提供する。旧`noe`グローバルや`noe.*` APIは復活させない。
- `createAgent()`の`provider`は省略時`openrouter`。初版ではそれ以外を設定エラーにする。ルートagentの`model`は明示指定を必須とし、モデル選択を固定の最新版名へ置き換えない。
- `name`と追加の`system`は任意。`system`は自律処理とtool利用の共通指示へ追加する。
- `run(instruction, options)`は`Promise<string>`。成功時はモデルの最終回答を返し、標準出力へ自動表示しない。継続不能な失敗はrejectする。JSONやMarkdownの除去・修正を暗黙に行わない。
- `instruction`は空でない文字列。`cacheKey`は任意の文字列。v1.2.0公開後の追加要件として、未指定時は子agentを含むrunごとの`runId`を自動利用し、同じrun内のモデル要求では固定する。明示指定は維持し、空文字は`session_id`省略。`maxSteps`は正整数または0（独自上限なし）、省略時0。`maxTokens`も正整数または0（プロバイダの既定）、省略時0。設定の型の誤りは通信前に診断する。
- `maxSteps`は各run自身のモデル呼出し回数で、tool実行回数や子runの累計ではない。`maxTokens`は各モデル要求の出力上限で、run全体の累計ではない。時間上限は既存CLIの`run/eval --timeout`で指定し、その計測範囲と終了コードを維持する。REPLに新しい時間上限オプションは追加せず、既存のCtrl+C中断へ接続する。
- 同じagentオブジェクトを再利用しても、各`run()`は別の履歴を作る。独立した複数runの`Promise.all`を許す。
- REPLでは`await import("noe:agent")`を使い、既存のawait入力形式に従う。importと`createAgent()`だけではキー読込み・待受・モデル通信を開始しない。

### 生成JSと子agent

`run_js({code})`はESモジュールのコード文字列を受け取る。生成JSでは通常の標準ライブラリをimportでき、`createAgent`を事前に束縛する。モデルにはその利用方法を伝える。子agentの作成は別toolにせず、次のようなJSで構成する。

```js
const results = await Promise.all([
  createAgent({ name: "left" }).run("a.jsonを集計して", { cacheKey: "left-v1" }),
  createAgent({ name: "right" }).run("b.jsonを集計して", { cacheKey: "right-v1" }),
]);
console.log(JSON.stringify(results));
```

子の省略された接続設定（provider・model・maxTokens）は、その生成JSを起動した親runの設定を継承する。名前・追加指示・履歴は別にする。明示した子の設定は、初版で対応する範囲で上書きできる。キーの実値はJS設定に含めない。

各JS実行は新しいNOEプロセス。変数は次のJS実行と共有しない。cwdは親runの作業ディレクトリを引き継ぎ、相対ファイル操作はそこを基準にする。生成ソースは専用の一時領域へ置く。ローカル相対importは既存のソースファイル基準という契約を維持し、作業ディレクトリのモジュールは生成ソースから解決できる相対パスを案内する。 WindowsでOSのTEMPとcwdのvolumeが異なる場合は、相対パスを成立させるためcwd内の専用一時領域を使う。この場合cwdへの一時書込みが必要で、書込みできない環境では既存のTEMP設定をcwdと同じvolumeの書込み可能な場所へ指定する。

結果は`{stdout, stderr, exitCode, reason}`。`reason`は`completed`・`failed`・`timeout`・`cancelled`。モデルにstdoutとstderrの両方を渡し、JSの非ゼロ終了を隠さない。

## Go側の構成とランタイムへの接続

### パッケージと責務

| 配置案 | 責務 |
| --- | --- |
| `internal/agent/` | 実行中のbroker、親子run、プロセス起動・回収・中断 |
| `internal/agent/provider/` | providerの要求・応答契約、OpenRouterアダプター、認証ファイル読込み |
| `internal/agent/js/` | 埋込みagent loopとbrokerクライアント |
| `internal/runtime/` | 組込みモジュール登録、現在の評価contextと終了処理の接続 |
| `internal/cli/` | 子を起動するNOE実行ファイルの指定、内部接続情報の受渡し |

`agent`パッケージから`runtime`や`cli`へ逆依存させない。子の起動設定・実行関数を上位から渡し、`runtime`の単体テスト実行ファイルを誤ってNOEとして再起動しない。製品CLIでは実行中のNOEの絶対パスを渡す。

### brokerと組込みモジュール

スパイクのlocalhost HTTP中継を内部transportとして使う。brokerを最初の`run()`時に遅延起動し、rootのQuickJS所有goroutineはSDK通信や子プロセスの終了を同期的に待たない。HTTP handler内でQuickJSの値へアクセスしない。

内部RPCにはGo側の専用HTTPクライアントを使い、固定の全要求timeoutを設けず、runtime・現在の評価operation・該当runのcontextで停止させる。公開Fetchの動作は変更しない。現行の既定Fetchは固定依存で30秒の`http.Client.Timeout`を持つため、子agent全体を待つ内部RPCに流用すると、利用者が上限を指定していなくても処理が30秒で切れてしまう。

内部RPCはJSへPromiseを返す。固定依存の`stdlib/fs.go`と同様に、所有goroutineで`NewPromise`と`loop.Begin()`、別goroutineでHTTP処理、`loop.Post()`で所有goroutineへ戻ってresolve/reject、`loop.Done()`で追跡終了という接続を使う。取消しやpostの不受理でもGo側の追跡を回収し、閉鎖後のJS値を触らない。`rt.Go/AsyncWork`だけに依存せず、内部RPCもstdlibの`Pending()`へ参加させ、awaitと補完時の新規I/O判定から漏らさない。

モジュールは`installLoader()`へ追加し、相対ローカルimportとstdlibの解決を維持する。内部クライアントの接続情報はランタイム専用の設定として供給し、`process.env`のスナップショットが先に作られていることへ依存しない。Go側の同期的な接続初期化口は接続メタデータだけを返し、SDKの実通信を行わない。

子NOEには一時的なbroker接続情報と実行scopeを明示的な環境で渡す。子は既存brokerへ接続し、別brokerを暗黙に起動しない。接続先が終了済みの場合は失敗として返し、新しいroot処理へ切り替えない。普通のJSからこの環境変数を設定する必要はない。

brokerはloopbackの空きポートで待ち受け、プロバイダキーとは別のランダムな一時トークンと実行scopeで要求を対応付ける。親を待つhandlerが子のhandler受付やモデル要求を塞ぐ共通ロックを保持しない。

### lifetimeと終了

brokerの寿命と各評価・runの寿命を分ける。brokerはランタイムのcloseで閉じ、親contextの終了時には待受終了を待たずに進行中の通信・子プロセスを取り消す。

- `Execute(ctx)`：全体のcontextをbrokerの実行scopeへ接続する。`--timeout`、SIGINT、実行失敗、`process.exit()`、正常終了の全経路で回収する。
- REPL：セッションにbrokerを保持できるが、評価ごとに現在の`Eval(ctx)`へ結び付くscopeを作る。キャッシュされたJSモジュールに前回の評価contextを固定しない。import済み・未使用のbrokerは未完了のI/Oとして扱わず、次のプロンプトを妨げない。
- 補完：`Complete()`の`inputCtx`にも現在のoperationを接続する。Proxy trap等がagent処理を開始した場合は、内部RPCの`Begin/Done`と既存の新規I/O判定を通じて取り消し・セッション終了を接続する。
- agentの成功・失敗：該当runを完了し、そのrunが所有する未完了の子処理を取り消す。別の独立runは取り消さない。
- 生成JSの終了：`finally`に到達しなかった子runもGo側で終了扱いにして、実行記録と通信を回収する。
- 中断中のREPL：Fetch等が残る場合にセッションを終了する現在の契約を維持する。回復可能なエラーの後は、新しい評価contextでagentを再利用できる。

`Runtime.OnClose`はQuickJSの閉鎖後に呼ばれる。そこではGo側のcontext取消し・待受停止・プロセス回収のみを行い、PromiseのsettleやJS値へのアクセスは行わない。

既存のCLI終了コードとタイムアウト原因の判定を維持する。agent固有のエラーでCLIの`--timeout`由来の124を失わない。キャンセルの後に新しい子プロセスやモデル要求を開始しない。

### OS別のプロセス管理

スパイクのLinux専用`syscall`実装を共通ファイルへそのまま移さない。POSIXとWindowsの実装をbuild tagで分け、配布契約のLinux・Darwin・Windows各amd64/arm64のビルドを維持する。POSIXはプロセスグループ、WindowsはJob Object等で、ホスト管理下の子とその通常の子プロセスの終了を接続する。

Linuxでは親子agentと中断を実行検証する。Darwin/Windowsのnative実行確認がない場合は、クロスビルド結果と区別して未確認として記録する。未確認をnative成功と報告しない。ここでrelease手順や配布対象を変更しない。

## 認証・OpenRouterアダプター

認証ファイルは`$XDG_CONFIG_HOME/noe/credentials/openrouter-api-key`、XDG未指定時は`~/.config/noe/credentials/openrouter-api-key`を標準位置とする。`NOE_OPENROUTER_API_KEY_FILE`でパスを指定できるようにする。今回の実接続検証では、この設定に許可済みの`~/.config/hjh/credentials/openrouter-api-key`を指定する。移動・複製・上書きはしない。

最初にモデル通信が必要になった時にGo側でファイルを読み、前後の空白を除く。未設定・空・読取り失敗は診断して終了し、登録プロンプトは開かない。APIキーの実値をtool定義・生成ソース・broker応答・traceへ載せない。子を起動するときはプロバイダのキー環境変数を渡さない。これはOSのファイルアクセス制御を追加する要件ではない。

OpenRouterアダプターは専用の公式Go SDK`github.com/OpenRouterTeam/go-sdk`を第一候補とし、Chat CompletionsのSSEを扱う。候補バージョンは`v0.9.59`に固定する。スパイクで使用したSDKは`github.com/openai/openai-go/v3 v3.78.0`であり、計画作成時点では専用SDKを使った実行は未確認だった。その後S1のfixture検証で採用を確定し、S7で製品CLIから実OpenRouter接続も成功した。結果は実装記録へ記載する。

`cacheKey`を`session_id`へ変換し、使用量・キャッシュ読取り量・費用は返された値を記録する。キーの指定だけでキャッシュヒットを保証しない。

### SDKの選定根拠と最初に確認する事項

| 候補 | 確認済みの事実 | 今回の位置付け |
| --- | --- | --- |
| OpenRouter公式Go SDK `github.com/OpenRouterTeam/go-sdk v0.9.59` | ChatRequestにSessionID・Provider、assistant/streamにReasoningDetails、SSE応答型、独自HTTPクライアント・retry設定がある | OpenRouterを最初の対象とする方針に合い、専用パラメーターをSDKで表現できるため第一候補 |
| OpenAI公式Go SDK `github.com/openai/openai-go/v3 v3.78.0` | OpenRouter互換endpointでスパイクの4シナリオに成功。専用項目はJSON overrideやraw応答を使って引き継いだ | 比較の基準とする既存実績。スパイクの成功だけを理由に本体SDKとして固定しない |

2026-10-16に専用SDKの公式repositoryと、取得した`v0.9.59`のsource（commit `d43957a08d1b0bce13b4a640ab729fae279204b0`）を静的に確認した。SDKはbetaとして公開されており、可変のlatest指定ではなくバージョンを固定する。Go要件はSDKのgo.modで1.25.10、NOEの1.27.2で満たせる。今回の調査では製品のgo.modを変更していない。

専用SDKの既定HTTPクライアントは60秒timeoutを持ち、Chat.Sendには既定のbackoff retryがある。本体ではプロバイダ通信にも固定の全要求timeoutを持たない独自HTTPクライアントを渡し、run/evaluationのcontextで止め、retryは明示的に無効化する。SDKのoperation timeoutは未指定とし、`WithTimeout(0)`で無制限を表現しない（このAPIはcontext.WithTimeoutを作るため即時期限になる）。内部broker RPCの専用クライアントと、プロバイダ向けクライアントは役割を分ける。

S1ではローカルHTTP/SSE fixtureで、tool callのIDと引数断片、reasoning_detailsの往復、session_id、使用量、エラー分類、取消し、既定timeout/retryの除去を確認する。SDKが必要な情報を失う場合は、その具体的な制約と対処を記録して選定を見直す。検証前にSDK依存の型をJSへ出したり、両SDKを本体へ同時導入したりしない。agent loopは引き続きNOEのJS側に置く。

固定SDKのEventStreamはscanner.Scan()がfalseになると読取りエラーをErr()へ移さず戻る経路を持ち、正常終端`[DONE]`の受信状態も外部へ公開しない。そのため、正常なfinish chunkと`EventStream.Err()==nil`だけを成功の根拠にしない。専用HTTPクライアントのbody観測等で、読取りエラーと正常なSSE終端をSDK外でも保持する。終端はSSEイベントとして識別し、本文内の単純な`[DONE]`文字列検索では判定しない。

成功判定にはSDKのエラー・finish reasonに加え、正常終端の観測、読取りエラーの有無、判定時点のcontext状態を用いる。正常finish chunk後の異常切断、終端のないEOF、body読取り中のcontext取消しもrejectする。正常終端後の通常のEOFやアダプター自身の正常な回収と、通信途中の異常を区別する。取得したstream/bodyの所有者はproviderアダプターで、正常・失敗・取消しの全経路で閉じる。SDKの`[DONE]`受信で自動的にbodyが閉じると仮定しない。

provider境界には、モデル・履歴・tool定義・cacheKey・出力上限を含む要求と、assistant応答・終了理由・使用量を含む応答を置く。SDKの型をJSへ公開しない。OpenRouterのtool call ID、分割された関数引数、reasoningの必要な拡張情報を欠落させず次の要求へ引き継ぐ。将来のproviderはこの境界で変換するが、初版で一般化のための複数アダプターを作らない。

## エラーと出力

- JS実行失敗・不正なtool引数：tool結果としてモデルへ返し、次の判断で修正可能にする。
- 認証失敗・利用枠不足・レート制限・通信失敗・SSEの異常終了：分類したエラーでrunをrejectする。初版ではSDKを含め暗黙の自動再試行をしない。
- トークン上限による打切り・拒否・空の未完了応答：成功した最終回答として返さない。
- モデルが追加質問を最終回答として返す場合：入力待ちには入らず文字列としてrunを終了する。意味上の完遂をランタイムが保証するわけではない。
- 診断に認証ヘッダーやキーを含めない。モデル回答やtool出力の一般的な内容を自動で削除・加工する仕様は追加しない。

利用者に返す値は最終回答の文字列のままとする。検証用のtraceは明示指定時だけ出し、run ID・親子関係・終了状態・所要時間・使用量などのメタデータを記録する。全要求や認証情報は記録しない。traceの指定方法はCLI/ランタイム設定に集約し、JSの戻り値の型を変更しない。

## 実装スライス

各スライスを、差分・動作・検証結果を確認できる単位にする。対応する受入条件の一部を通しただけで、条件全体を合格扱いにしない。正常系と、そのスライスが新たに所有する処理の取消し・回収を同じスライスで実装する。終了処理を最後へまとめて先送りしない。

| ID | 成果 | 依存 | 主に確認する受入条件 |
| --- | --- | --- | --- |
| S1 | SDKの適合確認、OpenRouterアダプター、ファイル認証 | なし | A6・A9のprovider境界、A10の依存互換性 |
| S2 | 組込みモジュール、内部RPC、最終回答を返す最小run | S1 | A1、A5の履歴分離、A7の内部通信、A8のoperation接続 |
| S3 | 子NOEの起動・結果回収・OS別終了管理 | S2 | A5のプロセス分離、A7の子回収、A9の子環境、A10のOS別ビルド |
| S4 | run_jsを使う単体agent loopと自律修正 | S1〜S3 | A2・A3、A6のtool往復・上限・不完全応答 |
| S5 | 子agentへの設定継承、並列実行、親による統合 | S4 | A4、A5の継承、A7の親子・独立run |
| S6 | REPL再利用・回復・中断・補完との統合 | S2・S5 | A1のREPL、A7・A8のセッション統合 |
| S7 | 利用例とガイド、実OpenRouterでの受入、全体確認 | S1〜S6 | A1〜A10の最終確認と証拠記録 |

### S1：SDK・OpenRouterアダプター・認証

- **変更範囲**：`internal/agent/provider/`、対応テスト、SDKを確定した後の`go.mod/go.sum`。
- **実装**：小さなprovider要求・応答契約、専用SDKのChat Completions SSE、設定ファイルの解決と読込み、session_id、使用量、エラー分類。カスタムHTTPクライアントとretry無効化、SDK外でのSSE終端・read error観測、stream/bodyのclose所有を含む。
- **完了条件**：ローカルfixtureが受け取る認証ヘッダーとsession_idが正しく、分割されたtool引数・reasoning・使用量を往復できる。正常完了と、正常finish chunk後のread error・終端欠落・body待機中のcontext取消しを区別し、後者はrejectする。正常・失敗・取消しの全経路でstream/bodyを閉じ、既定の再試行をしない。SDK候補の採用可否と固定バージョンを記録する。
- **確認範囲**：Goのprovider境界を検証する。JS公開API、子プロセス、実モデルの動作はまだ完了扱いにしない。新SDKの実接続成功を既存スパイクから推定しない。

### S2：組込みモジュールと内部RPC

- **変更範囲**：`internal/agent/`のbroker基盤・埋込みJS、`internal/runtime/`のmodule/operation/Loop接続、CLIからの実行設定、対応テスト。
- **実装**：`noe:agent`、遅延初期化、Go専用クライアントのPromise接続、最小runの履歴と最終回答、実行メタデータ記録の基盤。Execute/Eval/Completeの現在のoperationを取得する接続口をこの段階で用意する。
- **完了条件**：通常run/evalから、テスト注入したproviderの最終回答を受け取れる。import/createAgentのみでは待受・キー読取りが起きない。同じagentの再利用と独立runで履歴が混ざらない。30秒超の内部RPC完了と、明示したcontext取消しによる停止を確認する。
- **確認範囲**：テスト用providerは内部注入に留める。tool実行はS4、REPLの連続操作全体はS6で完成させるが、作ったbroker・要求・Promiseの回収はS2で確認する。

### S3：子NOE実行基盤

- **変更範囲**：`internal/agent/`のrunnerとOS別ファイル、CLIの子実行設定、対応テスト。
- **実装**：実行中NOEの絶対パス、専用一時ソース、cwd、標準出力・標準エラー・終了コード・終了理由、子への接続scopeの受渡し、プロバイダキー環境変数の除外、プロセス回収。POSIX/Windowsの終了管理をこの段階で分離する。
- **完了条件**：モデルを使わずにrunnerへコードを渡し、成功・例外・process.exit・時間切れ・中断を観測できる。別プロセスで変数が分離され、cwdとimport基準が設計どおりで、取消し後にホスト管理下の子PIDが残らない。6配布targetがビルドできる。
- **確認範囲**：Linuxのnative結果と他OSの未確認部分を分ける。OS別終了管理をS7まで未実装のままにしない。

### S4：単体agent loopとrun_js

- **変更範囲**：埋込みagent loop、brokerのtool実行経路、プロンプト、対応テスト、最小の利用例。
- **実装**：モデルへrun_js一つを渡し、runnerの結果をcall IDと対応付けて履歴へ追加する。tool引数の誤り・非ゼロ終了からの修正、任意のmaxSteps/maxTokens、不完全応答の診断を接続する。
- **完了条件**：ローカル模擬APIを使う通常CLIで、ファイル集計と「JS失敗→修正→再実行→最終回答」が完了する。複数tool callの結果・reasoningを次の要求へ保持し、上限到達・認証/通信失敗を成功扱いにしない。
- **確認範囲**：子agentを呼ばない単体の縦の流れを完成させる。一般の自律処理を初めて利用できる到達点とする。

### S5：親子agentと並行処理

- **変更範囲**：子用createAgentの設定継承、brokerのrun/scope管理、対応テスト、親子集計例。
- **実装**：親の接続設定を継承し、子ごとに履歴を分ける。Promise.allによる子の並行実行、親による結果統合、親終了・生成JS異常終了に伴う子処理の取消しと記録回収を接続する。
- **完了条件**：2つの子モデル要求が両方到着するまで応答しないfixtureでも完了し、各子のJS結果30/70を親が100へ統合できる。親終了は配下へ伝わり、一つのrunの失敗・終了は独立runへ波及しない。
- **確認範囲**：根拠なく委譲深度や回数の制限を加えない。継承された設定と明示上書きを実際の要求で確認する。

### S6：REPL・補完・評価context

- **変更範囲**：`internal/runtime/session.go`・`completion.go`・lifetime接続、CLI/PTYを含む対応テスト。
- **実装**：キャッシュ済みmoduleと再利用agentを、その都度のEval/Complete operationへ接続する。正常終了、回復可能なエラー、Ctrl+C、補完trapが開始した内部RPCの回収を既存契約へ揃える。
- **完了条件**：複数評価のagent再利用が前回contextに依存しない。importだけで次のプロンプトへ戻る。通信中の中断と補完による新規RPCをPending追跡で検出し、必要な取消し・セッション終了が行われる。回復可能なエラー後の再実行も通る。
- **確認範囲**：既存REPLの終了・回復契約に対する差分を確認する。新しい対話的な承認・質問フローは追加しない。

### S7：実接続・利用ガイド・最終受入

- **変更範囲**：`examples/`、README両言語、`docs/api.md`・`docs/repl.md`、本記録と実装後の検証記録。
- **実装**：認証パス設定、定型JS、親子agent、任意上限、出力・失敗時の扱いをガイドへ反映する。traceの利用手順と観測記録を揃える。
- **完了条件**：許可済みOpenRouterキーと指定DeepSeekモデルでA2/A3/A4を再確認する。通常/E2E/race/vetと配布先クロスビルドを完了し、A1〜A10の証拠と残るnative未確認範囲を記録する。
- **確認範囲**：実モデルの試行回数や文章の一致を求めず、計算結果・tool実行・親子関係で判断する。release・公開は別作業とする。

### 各スライスの進め方と完了記録

順序はS1→S2→S3→S4→S5→S6→S7。各スライスについて、変更ファイル、動作の到達点、対応する受入条件、検証コマンドと結果、コードレビュー・指摘の採否・未確認事項を[実装記録](increment-13-implementation.md)へ追記する。

各段階では変更した責務に必要なテスト・確認を行う。並行処理やlifetimeを変更した段階ではrace、OS別実装を変更した段階ではクロスビルドを確認し、全体の必須コマンドはS7でまとめて実行する。同じ全体検証を変更のない状態で反復しない。完了確認では通常の要件整合と、具体的な失敗・回収経路の両方を見る。

予定変更先は`internal/agent/`、`internal/runtime/{runtime,stdlib,session,eventloop,completion}.go`周辺、`internal/cli/`の設定接続、`go.mod/go.sum`、対応テスト、`examples/`、README両言語・`docs/api.md`・`docs/repl.md`・本記録。既存スパイクとその観測記録は履歴として保持する。

## 受入条件と証拠

| ID | 条件 | 確認方法 |
| --- | --- | --- |
| A1 | 通常のrun/evalとREPLからimportし、`run()`の結果を受け取れる | 組込みモジュール・CLI・REPLテスト。importのみで終了でき、無認証でも通常JSは動く |
| A2 | 指定ファイルを実際に読み、計算結果に基づく回答を返す | ローカル模擬APIと実OpenRouterでcount 4・total 100・average 25を確認 |
| A3 | 失敗した子JSを修正して別プロセスで再実行できる | 非ゼロ終了の記録後に成功があり、最終値が正しいことを確認 |
| A4 | 親と子2体、各子のJS実行、結果統合が成立する | 2つの子モデル要求が両方到着するまで応答を保留するローカル検証。実接続は親子traceと30+70=100を確認 |
| A5 | 履歴とJS変数が実行間で漏れず、子の接続設定が正しく継承される | 同じagentの再利用・独立run並行・子のmodel/system/cwdと履歴を確認 |
| A6 | 認証・通信失敗、上限到達、不完全なSSEを成功扱いしない | ローカルHTTP/SSE fixtureで診断と終了、引数断片・reasoning・複数tool callの引継ぎを確認。正常finish chunk後のread error・終端欠落・body読取り中の取消しでもrejectし、全経路でstream/bodyを閉じる |
| A7 | 長い内部RPCが固定30秒で切れず、root中断・時間切れ・process.exit、親run終了、生成JS異常終了で子を回収する | 上限未指定で30秒超待機する内部RPCの完了を1ケース確認。別途、待機中のモデル要求と子PIDを観測して明示したtimeout・中断で終了することを確認。独立runは継続し、CLI終了コードも維持 |
| A8 | REPL再利用・エラー回復・中断・補完と整合する | 複数評価で同じagentを利用し、前回contextを参照しない。補完trapによる内部RPCを含む新規I/OがPendingへ参加し、CompleteのinputCtxと既存の終了契約で回収されることを確認 |
| A9 | 認証情報の受渡し方針と設定優先順位が守られる | ダミーキーでGo側のAuthorizationを観測し、子環境・生成ソース・traceへ入らないこと、明示パスと標準位置を確認 |
| A10 | 既存機能・通常CGO不要ビルド・配布先ビルドを維持する | 既存の通常/E2E/race/vetと6配布targetのビルド。native実行未確認は明記 |

実モデルの文章の完全一致やキャッシュヒット自体は合格条件にしない。ローカル検証は外部APIを使わず、実接続は明示した手順・キー設定でのみ行う。観測記録には対象ソース、コマンド、モデル、出力、終了状態、親子関係、使用量を残す。本体実装で再検証し、スパイクの結果を転用して合格扱いにしない。

実装後の必須コマンド：

```sh
CGO_ENABLED=0 go build -o noe ./cmd/noe
go test ./... -count=1
go test -tags=e2e ./... -count=1
CGO_ENABLED=1 go test -race ./internal/... -count=1
go vet -tags=e2e ./...
```

加えて`.forgejo/release-profile.yml`の6targetを`CGO_ENABLED=0`でクロスビルドする。ビルド・テストとrelease・公開は別の作業とする。

## 計画レビュー

通常レビューは要件・API・範囲・段階・受入条件の整合と実装可能性、批判的レビューは遅延初期化、所有goroutine、broker再入、親子キャンセル、REPLの評価context、OS別管理の具体的な破綻経路を確認する。

各初回15分以内、新しい証拠・中間結論が10分ない場合は中断。再レビューは一回、変更箇所と既存findingの解消に限定して各15分以内。指摘の採否・計画修正・最終報告はdefaultが担当する。結果と採否は`increment-13-review.md`へ記録する。

通常レビューの任意指摘N1（上限の単位）、批判的レビューの必須指摘C1（内部Fetchの固定30秒期限）を採用して修正し、両reviewerが変更箇所を再確認した。残存する必須指摘はなく、計画として実装着手可能と判断する。[レビュー記録](increment-13-review.md)に根拠・採否・再確認結果を記録した。これは本体実装の完了や実行検証の成功を示さない。

その後のユーザー指摘を受け、SDK候補を専用OpenRouter SDKへ見直し、従来4段階の実装順を7スライスに詳細化した。N1/C1の対処と公開API・受入条件は維持している。このSDK選定・スライス改訂はdefaultが公式sourceと既存要件に照らして確認したもので、過去のreviewerの合格を新しいSDKの適合確認や改訂差分の第三者レビューに転用しない。

ユーザーの追加依頼により改訂差分の通常・批判的再レビューを実施した。通常は指摘なし。批判的指摘C2（SDKが読取りエラー・正常終端を十分に公開しない）を採用し、SDK外での観測、成功判定、close所有、S1/A6の確認条件を追記した。両reviewerが変更箇所を再確認し、残存する必須指摘なし・S1から実装着手可能と判定した。専用SDKの実際の適合と本体依存の確定は、S1のfixture検証で行う。

## 公式参照

- [OpenRouterのOpenAI SDK接続](https://openrouter.ai/docs/guides/community/openai-sdk)
- [OpenRouter公式Go SDK](https://github.com/OpenRouterTeam/go-sdk)
- [候補バージョンv0.9.59のsource](https://github.com/OpenRouterTeam/go-sdk/tree/v0.9.59)
- [tool calling](https://openrouter.ai/docs/guides/features/tool-calling)
- [reasoning情報の引継ぎ](https://openrouter.ai/docs/guides/best-practices/reasoning-tokens)
- [session_idとキャッシュ利用量](https://openrouter.ai/docs/guides/best-practices/prompt-caching)
