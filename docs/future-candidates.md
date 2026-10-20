# NOE 次の機能候補

2026-10-19、v1.7.1後のソース・テスト・利用ガイド・実装記録と照合して整理しました。
`<完了>`は合意した範囲を実装済み、`<一部完了>`は実装済み部分と残る候補がある状態、`<未完了>`は検討・未着手を表します。未完了は実装の決定や優先順位を意味しません。

現在の仕様は各利用ガイドを参照してください。実装前の案・旧API・当時の判断は[検討履歴](archive/future-candidates-history.md)へ保存しました。履歴にある未実装記述を現在の残件とは扱いません。

## 検討を続ける候補

### <完了> Go側での再委譲の制限 — 2026-10-21

利用者の追加依頼で、モデルへの指示に加えてGo側の起動受付で再委譲を制限した。起動時の第三引数`options.allowDelegation`はrootでは省略時true、子では省略時false。親が明示的にtrueを指定して起動した子だけが次の階層を起動でき、許可は次の子へ自動継承しない。`spawn_agent`・`run_agent`・hookに共通の`Host.spawn`で起動元を検査する。拒否は当該要求だけをrejectし、group停止や起動枠の消費はしない。[API](api.md#設定)。local実装・関連テスト済み。リリース・配置は未実施。

現在の`executionLimits.agentStarts`による累計起動数制限とは別の制御とする。任意の最大階層・同時起動数の設定は追加していない。子を親タスクに所有させる寿命も維持し、親のqueue task間での長期再利用へは拡張しない。

発端は`noe-events-20261020-224654-b7d639559fb5e94d.jsonl`の確認。親が直接16体、子が孫を1体起動していた。孫`modulemap-review`は、子`inspect-cli-docs`の担当範囲の一部である`internal/modulemap`の仕様照合・不具合候補調査を行ったが、子への依頼文には再委譲の明示許可がなかった。

### <一部完了> タスクループの完了待ちと観察 — 2026-10-20

利用者の当面の運用方針として、親NOEを`--events`付きで起動する。親・子・孫・同じinstanceのqueue taskは共通のJSONLへ記録し、必要なrunを`runId`で選んで読む。通常の状態確認には`observe_agent({agentId})`、thinking等の詳細確認には`observe_agent({logPath, runId, includeThinking: true, cursor?})`を併用する。保存先の自動生成は、起動時のcwdの`.noe/events/*.jsonl`に集約する方針。保存は引き続き`--events`で選択し、常時保存へ変更する方針ではない。[イベント記録](events.md)・[観察API](observe.md)。

実行途中の詳細ログをJSONL保存なしで読めるメモリ保持・live観察の拡張は、当面保留する。完了結果については2026-10-21の追加合意で、`wait_task`・live `observe_agent`の`includeAnswer:false`と、保持済み原文の範囲取得`read_task_result`をlocal実装した。原文の保持・寿命は維持し、追加モデルによる要約は行わない。`wait_task({agentId, commandId})`は実装済み。queueの受付・FIFO・人間向けCLIを維持し、特定タスクの保持済み結果を完了通知で待つ。初期タスクのIDも`spawn_agent`から返し、通常のJS・run_js・hookから利用できる。待機専用イベントは追加しない。[API](api.md)。

### <未完了> JSでのhook登録と、実行前評価・親agentへの問い合わせ — 2026-10-20

利用者の検討メモ。今は記録のみとし、実装・API名・優先順位は決定しない。ユーザーのJSで`hook("after_toolcall", handler)`のように処理を登録したり、`run_js`実行前にコードを評価したり、親agentが子の実行へ介入できる使い方を検討する。評価・承認を全実行に必須化する依頼ではない。

現在はconfigの`hooks: ["./hooks.js"]`とESM exportの`beforeTool`・`afterTool`等で登録する。`beforeTool`は実行予定のコードを受け取り、非同期の評価が終わるまで実行を待たせ、引数を置換できる。hook内から評価用agentを呼ぶ構成も可能で、評価用agentには`hooks: []`を指定して同じhookの継承を解除できる。例外を投げた場合は対象task全体が失敗する。[現行hook仕様](hooks.md)。

残る検討点は、同じJS内でのcallback登録と別runtimeへの受け渡し・外側の変数の扱い、task全体を失敗させずに今回のtool実行を見送って理由をモデルへ返す契約、子の実行前に親モデルへ判断を問い合わせて回答まで待つ仕組み。実行許可・引数変更・実行見送り・親への問い合わせをどう表現するか、待機中の取消しと親子の所有関係も含めて整理する。現状でも親が子へhookを指定・継承できるが、親モデル自身への同期的な問い合わせとは区別する。

### <未完了> agentのアプリ内runtime実行 — 2026-10-20

利用者の今後の課題メモ。外部Goアプリへagentを組み込む際、別のNOE実行ファイルを配備せず、アプリ内のgo-quickjs runtimeで実行できる方式を検討する。今は記録のみとし、実装時期・API・優先順位は決定しない。

現在の公開`agent` packageは、プロバイダー接続・認証・agent管理をGoホストで行い、taskのJSループと`run_js`を別のNOEプロセスで実行する。このため、ツールを使わないtaskでもNOE実行ファイルが必要。汎用`stdlib`だけの組み込みには、この実行ファイル依存はない。[現行のagent組み込み仕様](go-agent-embedding.md)。

候補は、内部の実行部分を差し替え可能にし、既存の別プロセス方式を維持しながらアプリ内方式も選べる構成。既存JS・プロバイダー処理の再利用を想定するが、難所はtask・`run_js`ごとのruntimeと状態の分離、親子agentの所有関係、取消し、タイマー・通信・goroutine等の終了回収。現在プロセス終了で回収している資源を個別に扱う必要があり、取消しに応答しないGo処理をOSプロセスと同じように強制終了することはできない。小規模な修正ではなく中規模以上という暫定的な見立てで、実装前に生存期間と終了契約を整理する。

### <一部完了> NOE版stdlibの実装 — 2026-10-19

利用者が進めたい方向として、「go-quickjsエンジン＋NOEのライブラリ群＋それらを組み立てるNOE実行コマンド」という構成を目指す。NOE版stdlibとagent拡張を、NOE自身も利用するライブラリ群として育てる。将来は他のGoプロジェクトがimportし、go-quickjsのruntimeに汎用APIとagent機能を選択して組み込める構成にする。汎用Go入口を7.1、agent拡張の接続を7.2で実装・検証済み。v2公開は常用確認後に行う。

現行はgo-quickjsをJavaScriptエンジンに使い、Goの公開`forge.harakara.site/littleisland/noe/v2/stdlib`から内部composerを通じてJSの汎用APIとNOE独自APIを登録する構成へ切り替えた。NOE独自機能もGoとJSで実装され、`internal/nativeio`・`internal/agent`などの内部packageと`internal/runtime`の登録・実行制御に分かれている。外部Go向け入口は同じrepository/root moduleで実装し、別repository・別Go moduleへ分離しない。

実装の土台は、固定版go-quickjsのstdlibを取り込み、NOE側で拡張・保守するforkとする。既存のWeb型、Streams、HTTP・WebSocket連携、loopとテストを活用し、上流への変更や新機能の全面追随を前提にしない。エンジンは引き続きgo-quickjsを利用する。ステップ1でstdlibと`internal/wsproto`を取り込み、非公開の`internal/hostaccess`への依存は公開評価APIへ適合させた。全機能の自前実装や既存機能の一括移動は決定していない。

具体案として、通常のHTTP用途と要求・応答のストリーミングを`noe:http`へ集約し、NOEの推奨入口と今後の開発先にする。既存の`fetch`・`serve`・`node:http`は従来の利用向けに維持する。forkしたstdlibから既存APIを提供する構成も可能で、上流版とNOE版を別々に導入することを前提にしない。Web型・Streamsの共有、WebSocket連携、初版の対応範囲と終了動作を実装計画で決める。HTTP整備に合わせてAPI登録・通信・資源管理とCLI・agent固有の実行制御の境界を整理し、NOE版stdlibの一部として扱える構成にする。

公開名は実装の出自と分けて考える。Web標準の型・共通機能は標準名を維持し、NOEが設計するAPIは`noe:`を正式な入口にする。`node:fs`は当面維持し、NOEとして仕様を整える段階で`noe:fs`を正式名、`node:fs`を同じ実装を参照する互換エイリアスにする案を持つ。将来は`noe:fs-handle`を`noe:fs`へ統合することも検討し、ハンドルの表現・読み書き・closeの契約と旧入口の互換性を整理する。エイリアスなら振る舞いも共通になり、互換動作に差が必要な部分だけ別途扱う。一律の改名は行わず、具体的な範囲・時期は未決定。

汎用stdlibとagent拡張は選択して利用できる境界を設ける。現在のagentは子NOEプロセスの起動・認証・観察・終了管理と接続しているため、外部アプリへ提供する際は実行方法や生存期間をどう接続するかも整理する。公開ライブラリ化を一度に完了することはHTTP整備の前提にせず、段階的に進める。現在は次のステップ1〜6に加え、7.1汎用公開と7.2agent接続も実装・検証完了。v2公開と常用の安定性確認は後続。

利用者の追加方針（2026-10-19）：まずv1.13.0を常用し、見つかった小さなバグを修正して問題なさそうと判断できてからv2.0.0にする。公開済みv1.13.0はステップ6までの実装とステップ7の計画を含む。その後の追加依頼で7.1と7.2の実装・テスト・コードレビュー・総合テストを完了した。v2公開や常用確認完了は今回の実装完了と区別する。

利用者と共有した長期的な節目は、他のGoアプリにも組み込めるNOEを目指すv2.0.0。途中は1.xで完成した単位ごとにリリースできる進め方とし、最終形を一度に実装しない。各段階の具体的なversion・時期は未決定。

段階案（2026-10-19）：

| 段階 | 変更内容 | その段階の完了の目安 |
| --- | --- | --- |
| 1：stdlibの取り込み（完了） | 固定版のソース・テスト・ライセンスと由来を取り込み、非公開packageへの依存を整理した。取り込み時点ではNOE本体は従来版を維持した | 単独/race/総合テスト・6対象cross-compileが成功、独立した通常コードレビュー完了 |
| 2：NOEの利用先の切替（完了） | stdlibの導入、loop、Workerを取り込んだ版へ揃えた。機能追加・API名変更は含まない | 4モードのAPI比較、関連/全体/race/E2E、6対象のビルドと統合test compileが成功。独立した通常コードレビュー完了 |
| 3：`noe:http`の入口（完了） | 既存と同一の`fetch`・`serve`を公開し、英文の内蔵docsと利用例を整備した | 各Worker/agent、旧API比較、総合テスト・6対象compile・実providerのHTTP往復が成功、独立コードレビューと再レビュー完了 |
| 4：要求送信のストリーミング（完了） | `ReadableStream`を全量収集せず送信し、Request所有権・背圧・取消し・早期応答を整えた | EOF前到達、各Worker/agent、全体/race/E2E・6対象compile・実provider再試が成功。独立コードレビューと再レビュー完了 |
| 5：要求受信のストリーミング（完了） | headers後にhandlerへ渡し、需要に応じて本文を逐次読取り。早期応答・Expect・終了回収を整えた | EOF前dispatch/受信、各Worker/agent、全体/race/E2E・6対象compile・実provider基本フローが成功。独立コードレビューと再レビュー完了 |
| 6：NOE独自APIの導入方法の整理（完了） | 内部の組立packageでstdlib/nativeio/docs・設定複製・共有出力・Workerを接続し、NOE自身も切替 | CLI/agent非依存のGo例、API比較、全体/race/E2E・6対象compile・実provider限定再試が成功。独立コードレビューと再レビュー完了 |
| 7：外部向けGoライブラリの提供（7.1・7.2実装/検証完了） | 正規/v2 module・公開stdlib/agent package・設定・外部consumerを整備。NOE自身も共通bridgeを利用し、明示NOE processとlifetimeを接続 | 別Go moduleから汎用APIとagentを選択して組み込める。v2公開・常用の安定性判断は別途 |

最初の作業単位は第1・第2段階までとし、「NOEが管理するstdlibで従来どおり動く」を一つの到達点にする。切替では既存テスト、HTTP・WebSocket・Worker・終了処理、配布対象のビルドとdocsの由来・版情報を確認する。その後、第3～第5段階をHTTP整備のまとまりとして進める。既存APIの契約とNOE側の拡張を整理し、Web型・loop・HTTP・WebSocketの共通実装を活用する。全二重通信や詳細なtimeout設定は、基本的な要求ストリーミングとは分けて仕様を決める。

ステップ1は[固定版stdlibの取り込み計画](development/stdlib-step1-import-plan.md)に沿って3スライスを実装し、[独立した通常の計画/コードレビュー](development/stdlib-step1-import-review.md)を完了した。いずれもP2以上の指摘は0件。[実装・検証記録](development/stdlib-step1-import-implementation.md)と[構造化結果](development/stdlib-step1-import-results.json)を保存している。取り込んだ41ファイルのうち30件が原本と一致し、11件の適合変更とローカル追加2件に限定した。NOE本体の切替は含めず、上流stdlibを使うCLIの依存境界と既存runtime・配布バイナリの維持も確認した。

ステップ2は[runtime切替計画](development/stdlib-step2-runtime-plan.md)の4スライスを実装し、[独立した通常の計画/コードレビュー](development/stdlib-step2-runtime-review.md)を完了した。いずれもP2以上の指摘は0件。productionと統合helperをローカル版へ揃え、内蔵英文docsと現行ガイドの出自説明を更新した。変更前後の4モードAPI inventoryは完全一致し、取り込み版を使うCLI・Worker・agent・`run_js`の総合検証も成功した。追加のOpenCode Go実接続でも、agent起動・`run_js`によるJSON集計と保存・最終回答の基本フローを確認した。[実装・検証記録](development/stdlib-step2-runtime-implementation.md)と[構造化結果](development/stdlib-step2-runtime-results.json)を保存している。

ステップ3は[noe:http入口計画](development/stdlib-step3-http-plan.md)の3スライスを実装し、[独立した通常の計画/コードレビューと再レビュー](development/stdlib-step3-http-review.md)を完了した。コードレビューのdocsに関するP2を解消し、最終P0〜P3は0件。既存と同一のfetch/serveを公開し、Web型・WebSocket・loop・終了処理を共有する。設定・4モード・旧API比較、英文docsと例、関連/総合テスト、6対象のbuild/compileが成功した。OpenCode Go実接続の1課題でもdocsを自発参照し、新入口でのHTTP往復・保存・close・最終回答を確認した。[実装・検証記録](development/stdlib-step3-http-implementation.md)と[構造化結果](development/stdlib-step3-http-results.json)を保存している。要求送信・受信のstreamingはステップ4・5へ残す。

ステップ4は[要求送信streaming計画](development/stdlib-step4-upload-plan.md)の4スライスを実装し、[独立した通常の計画/コードレビューと再レビュー](development/stdlib-step4-upload-review.md)を完了した。コードレビューで見つかったpre-abort時の本文lockのP2を解消し、最終未解消P0〜P3は0件。EOF前到達、Request所有権・背圧・取消し・redirect・早期応答、英文docsと各実行経路、全体/race/E2E・6対象compileが成功した。OpenCode Go実接続も課題の並行手順を明確化した再試で送信・保存・最終回答を確認し、docsを自発参照した。[実装・検証記録](development/stdlib-step4-upload-implementation.md)と[構造化結果](development/stdlib-step4-upload-results.json)に初回失敗も保存している。serveの受信buffering改善はステップ5へ残す。

ステップ5は[要求受信streaming計画](development/stdlib-step5-receive-plan.md)の4スライスを実装し、[独立した通常の計画/コードレビューと再レビュー](development/stdlib-step5-receive-review.md)を完了。Expect tokenのP2と需要計数のP3を解消し、終了テストの総合確認後の修正も限定再確認済みで、最終未解消P0〜P3は0件。EOF前dispatch/受信、背圧・早期応答・取消し/終了、英文docs/例と各実行経路、全体/race/E2E・6対象compileが成功した。OpenCode Go実接続1課題でもdocsを自発参照し、受信・保存・close・最終回答を初回で確認した。[実装・検証記録](development/stdlib-step5-receive-implementation.md)と[構造化結果](development/stdlib-step5-receive-results.json)へ環境上の失敗と復旧も保存している。API登録・資源管理境界は次のステップ6で整理する。

ステップ6は[API導入と所有関係の整理計画](development/stdlib-step6-composition-plan.md)の4スライスを実装し、[独立した通常の計画/コードレビューと限定再レビュー](development/stdlib-step6-composition-review.md)を完了。計画のProcess前提不足とコードのwriter動的比較のP2を解消し、最終未解消P0/P1/P2は0件。CLI/agent非依存の内部組立package・Worker/DNSと資源所有・NOE切替・英文Go例を整え、API/CLI比較、全体unit/race/E2E/vet・6対象build/compileが成功した。実プロバイダの同じ基本課題は初回のNode API仮定で未完了後、noe:http指定の限定再試でdocs自発参照・HTTP往復・保存・終了・最終回答を確認した。[実装・検証記録](development/stdlib-step6-composition-implementation.md)と[構造化結果](development/stdlib-step6-composition-results.json)に両試行を保存。公開Go package化はステップ7へ残す。

ステップ7は[外部Goライブラリ提供計画](development/stdlib-step7-library-plan.md)を作成し、[独立した通常の計画レビュー](development/stdlib-step7-library-review.md)を完了。初回のP0〜P3は0件。7.1をmodule移行・公開facade・外部consumer/artifact・NOE切替/英文ガイド・総合確認の5スライスに分割し、7.2のagent接続は汎用公開の成立後に別計画とする。その後の追加依頼で7.1の5スライスを実装し、独立コードレビューのP2以上は0件、全体unit/race/E2E/vet・外部consumer/local artifact・6対象compile・実プロバイダ基本フローが成功。[実装・検証記録](development/stdlib-step7-library-implementation.md)と[構造化結果](development/stdlib-step7-library-results.json)へ保存。7.2は[agent外部接続計画](development/stdlib-step72-agent-plan.md)を6スライスで作成し、[独立した通常レビューと限定再レビュー](development/stdlib-step72-agent-review.md)を完了。公開設定にregistryで無視されるProfileを含めたP2を解消し、最終P0〜P3は0件。利用者の追加依頼により7.2の6スライスも実装・検証完了。独立コードレビューでlive consumerの完了判定に関するP2を修正し、限定再レビューでP0〜P3は0件。全体unit/race/E2E/vet・外部consumer/local artifact・6対象compile・実provider基本フローが成功。[実装記録](development/stdlib-step72-agent-implementation.md)と[構造化結果](development/stdlib-step72-agent-results.json)に保存した。v2公開・常用の安定性判断は未実施。

`noe:fs`への正式名変更や`noe:fs-handle`の統合は、HTTP整備・ライブラリ公開とは独立した後続作業とする。汎用stdlibの外部利用が成立してからagent拡張の接続を段階的に整え、全APIの改名や統合をv2.0.0の必須条件にはしない。

### <完了> x/term依存の整理 — 2026-10-19

REPL削除後に残ったobserve表示の端末幅取得を、既存の`x/sys`を使う小さなOS別helperへ置換しました。Linux/macOSは`IoctlGetWinsize`、Windowsは`GetConsoleScreenBufferInfo`の可視window幅を使います。`COLUMNS`→端末幅→80桁の選択を維持し、NOEの`go.mod`・`go.sum`と6配布対象のCLIビルド依存から`x/term`を除去しました。go-quickjs自身の`go.mod`にある間接的なmodule graph上の記載（v0.40.0）は残ります。

CLI単体テスト、観察とCLI入口の重点E2E、`go vet ./internal/cli`が成功しました。LinuxのPTYで実際の端末幅、環境変数の優先、0幅のfallbackを検証し、通常ファイル・bufferでもfallbackを確認しています。6配布対象のCGO不要ビルドとCLIテストのcross-compileも成功しました。他環境は実機確認ではありません。自己レビューではfdの非所有、Windowsの可視windowとbuffer幅の区別、失敗時のfallback、他依存versionの維持を確認しました。ビルド記録は`/tmp/noe-term-removal-20261019-mqhozkrm/checks.json`にあります。

### <未完了> mdcli取り込みコードの整理（保留） — 2026-10-19

当初は未使用の色付け・syntax highlight関連の整理を候補にしました。その後の相談で、既にソースを取り込み済みで上流追随は必須でなく、将来`ANSI:true`で利用できる機能を残す利益もあるため、現状維持・整理保留としました。Markdown整形、日本語の表示幅、折返し・表表示も維持します。

`x/sys`とWebP用の`x/image`も維持する方針です。

### <完了> モデル要求のretry追加 — 2026-10-20

組込みproviderのagentモデル要求に、最大3試行・2回目まで2秒・3回目まで4秒の固定retryを追加した。通信障害・429・5xx・不完全なstreamを再試行し、認証失敗・残高不足・入力不正・取消しは再試行しない。待機中も既存の取消しに従う。

同じ入力をprovider内部で再送し、完了済みtool・hookは再実行しない。失敗した応答は会話履歴へ追加せず、成功した試行の完全な応答だけを採用する。表示・JSONLへ既に流れたthinking等は残し、重複を許容する。利用者の追加方針に従い、回数・待機時間の設定項目やretry専用の観察イベントは追加していない。`maxSteps`・`providerCalls`は従来の論理要求単位で、内部再送を別要求として数えない。decision・画像生成・利用者が注入した独自providerにはこのretryを追加しない。[providerガイド](providers.md#モデル要求の再試行)。

### <完了> 内蔵docsとagent初期説明の整備 — 2026-10-19

実行バイナリと同じ版のAPI説明を内蔵し、`noe docs`とJSの`noe:docs`から必要な説明を参照できます。[計画](development/builtin-docs-plan.md)に沿って英文Markdown本文とGo catalogを共通の正本にし、stdlib全26対応moduleの一覧・概要、よく使うAPIの詳細とNode.jsとの差分を収録しました（[実装・検証記録](development/builtin-docs-implementation.md)）。実装テスト・コードレビュー後、実プロバイダへdocsへの誘導を含まない2課題を与え、両方で自発参照と課題完了を確認しました。

初期instructionsへNOEとは何か、agentがNOEに管理され、`run_js`が毎回新しいNOEプロセスで実行されること、docsの調べ方を加えました。system I/Oの詳細は参照先の整備後に内蔵docsへ移しました。約4,700 tokensという変更前の概算だけを理由に削減目標は設けていません。

2026-10-20、利用者の確認により「辞書参照APIの必要性 — 2026-10-18」をこの完了項目へ統合しました。目的は、agentがNOEのAPI仕様を辞書のように必要な部分だけ参照できることです。`noe docs [topic [member]]`と`noe:docs`で実現済みです。任意の辞書ファイルを検索する汎用APIは、この項目の残件には含めません。[使い方](reference.md)・[当時の検討メモ](archive/future-candidates-history.md#未完了-辞書参照apiの必要性--2026-10-18)を参照してください。

<a id="一部完了-汎用の低レベルapi拡張と優先順位--2026-10-18"></a>

### <一部完了> 汎用の低レベルAPI拡張と優先順位 — 2026-10-18

目的は、MCP、LSP、常駐ワーカー、監視・対話スクリプトなどをJSで組み立てるための汎用機能です。NOEの拡張として検討し、stdlibの変更や上流採用を前提にしません。外部ESM・シェル・OSコマンド・ライブラリのNOE向け改修も選択肢です。既存APIとJS・CLIで足りる処理をGo必須とは扱いません。

| グループ | 現在の状態 | 残る検討 |
| --- | --- | --- |
| 1：プロセスと標準I/O | **完了**。`noe:process`の子プロセスハンドルとstdin/stdout/stderr、`noe:io`のホスト標準I/Oストリーム | [公開契約](process-io.md)・[実装記録](development/process-io-implementation.md)を参照 |
| 2：具体的な用途に合わせて追加 | **完了**。`noe:fs-handle`の位置指定I/O、`noe:sockets`のTCP・Unix接続/待受、`noe:signals`のOSシグナル購読 | [公開契約](low-level-io.md)・[実装と総合検証](development/low-level-group2-implementation.md)。stat/truncate/sync等の追加操作は後続候補 |
| 3：後続候補 | **未完了**。ファイル変更通知、端末操作 | ポーリングやCLIと比較。TTY判定・サイズ・raw mode・PTYは別々に評価 |

`execFile`は既に非同期、ファイルの逐次I/Oも既存stdlibで利用可能です。第1グループの追加価値は実行中の入出力・終了観察・停止をハンドルで扱えることです。Ctrl+Cの取消しは既にあり、JSへのOSシグナル配送は別候補です。元QuickJSのfd番号・`dup`・readiness callbackの再現は要件にしません。

### <一部完了> MCPクライアントの利用整備 — 2026-10-18

**JSで外部MCPツールを呼ぶ経路は検証済みです。** 公式SDKのClientをESMへbundleし、`noe:process`を使う独自JS stdio transportを組み合わせた[スパイク](../scripts/spikes/process-io-mcp/README.md)があります。main・Worker・fixture agentの`run_js`で、初期化、tool一覧、並行呼出し、要求と応答の対応付け、大きなUTF-8応答、異常終了と子の回収を確認しました（[import mapの検証記録](development/local-import-map-implementation.md)）。

Node/npm/esbuildはbundleの事前作成に使い、実行時はNOEがJSを動かします。専用のGo製MCP APIは追加していません。通常利用向けの配布・設定・接続先管理をどう整えるかは未決定です。スパイクがあることと、製品として組込みMCPクライアントを提供していることは区別します。

### <未完了> MCPとデーモンのexample作成 — 2026-10-19

利用者の作業メモとして、次の実行例を`examples/`へ追加する。

- MCP：既存のMCPクライアントスパイクを参考に、利用例と起動手順を整える。クライアント/サーバーの役割・通信方式の詳細は作成時に決める。
- デーモン：`noe:sockets`の待受・要求処理と`noe:signals`による終了を使う常駐処理の例を作る。独立したNOEプロセスとして外部から起動・停止する手順も示す。

### <未完了> NOEへのMCPサーバー機能の組込み — 2026-10-17

**優先度は低い。** NOEで実行するJSがtoolを公開し、別アプリ・実行環境のagentから利用できるようにする候補です。同じVM内の別アプリでも別マシンでもよく、自作の集計・レポート、Exa検索ライブラリ、NOE agentへの仕事の委譲などを想定します。

NOE内の親子agentは既存の起動・観察・操作APIで連携でき、この用途にMCPサーバーは必須ではありません。外部へ公開したい具体的な処理が出てから必要性を再検討します。

サーバーSDKの採用、通信方式、起動・公開方法は未決定です。スパイクの`server.js`はクライアント検証用fixtureであり、製品のMCPサーバー機能ではありません。クライアントSDKの検証済み範囲を、そのままサーバーSDKの互換性確認とは扱いません。可能ならJSで実装します。

### <完了> REPLの削除 — 2026-10-19

日常利用をファイル編集と`noe run`、短い確認を`noe eval`へ揃え、REPLを削除しました。CLI、入力UI、履歴、補完、await変換、Sessionと入力間の回復専用処理・テスト・現行資料を撤去しました。

共有の実行・取消し・I/O回収、Worker・agent/run_js、docsと旧JSONLの閲覧は維持しています。[計画](development/repl-removal-plan.md)に基づき、実装・テスト・独立コードレビュー、その後の全E2E・race・vet・6対象ビルド・実CLI総合テストを完了しました。[実装・検証記録](development/repl-removal-implementation.md)を参照してください。

`noe repl`は未対応コマンドとして終了コード2を返します。既存履歴ファイルは削除しません。v1.8.0のタグ・配布物は変更せず、Zedの開発環境整備は次の別候補です。

### <未完了> ZedでのNOEスクリプト開発環境の整備 — 2026-10-17

SSH先のJSを編集し、NOE APIの補完・実行・観察・追加指示まで行える環境を整える候補です。NOE用`.d.ts`、`jsconfig.json`、リモート側の`.zed/tasks.json`と利用ガイドは未整備です。

型定義は本家QuickJSの`std`・`os`、go-quickjs stdlib、Node.jsを混同せず、採用版の実際の契約に合わせます。実行コードをTypeScriptにする必要はありません。まず他のエディターでも再利用できる設定・型を用意し、Zed専用拡張は必要になってから検討します。

利用者はリモートターミナルとtmuxを使う方針です。ファイルに書いた指示は現在もシェル経由で送れます。専用の`--file`・stdin入力指定はありません。

```sh
noe steer RUN_ID "$(cat instruction.md)"
noe queue AGENT_ID "$(cat next-task.md)"
```

### <一部完了> go-quickjs stdlibとNode.jsのAPI差異の説明整備 — 2026-10-17

現在の組込み[モデル向けinstructions](../internal/agent/instructions.md)は、NOEの実行環境と版が一致する内蔵docsの調べ方を説明します。stdlib全26 moduleの概要・export一覧と、優先APIの詳細を英文で収録しました。[参照方法](reference.md)と[実装・検証記録](development/builtin-docs-implementation.md)を参照してください。

`execFile`のPromise、fsのWeb Streams、Node形式Workerのtype option、spawnSyncの起動失敗時statusなどの差異も該当topicに説明しました。全APIの個別詳細や全差異の列挙は完了条件にせず、実際の誤用や利用用途を基に残る説明を追加します。

### <未完了> go-quickjsへの上流PR案：同期コマンドの失敗情報保持 — 2026-10-17

原型のgo-quickjs v0.27.0の`stdlib/exec.go`では、`execFileSync`は失敗時に捕捉したstdout・stderrをErrorへ付けずに返します。非同期`execFile`は`code`・`stdout`・`stderr`をErrorへ付けます。NOE管理のstdlibでは、利用者の実装依頼に基づき`execFileSync`・`execSync`にも同じ項目を付ける変更とテスト・docsを追加しました。正常時の文字列戻り値と既存メッセージは維持します。[APIの説明](../internal/apidocs/content/stdlib-child_process.md)。

上流PR候補としては、この同期経路の改善を提案する余地が残ります。上流側の修正・PR作成・提出は未実施で、このメモは提出許可ではありません。NOE側の変更はv1.15.1でリリースし、常用バイナリへの配置と配布物の検証も完了しました。

### <一部完了> Web検索と検索APIの認証ラッパー — 2026-10-17

**ExaのJSライブラリと利用案内は実装済みです。** [examples/exa/](../examples/exa/README.md)の`exa.js`は`search(query, options)`を公開し、検索結果のJSONと失敗情報を返します。既定の`~/.config/noe/credentials/exa-api-key`または明示した`apiKeyFile`から、JS側でキーを読みます。このサンプルの既定パスは固定で、NOEホストのXDG設定解決と同じではありません。

既知URLは`run_js`の`fetch`で取得でき、agentは配置したライブラリやimport map・instructionsを通じてExaを利用できます。緊急に組込みWeb検索を作る必要はないという方針を維持します。

検索APIの認証をGoホストへ隠す組込みラッパーは未実装です。現在のExa JSサンプルと区別し、専用API・provider・配置方法の採用は必要性が出てから検討します。

### <未完了> Worker内からの`noe:agent`利用

Workerのloaderは`noe:agent`を受け付けません。通常JSからのagent起動は既に非同期で、並行起動や親子の委譲のためにWorker対応を足す必要はありません。必要ならWorkerからmainへメッセージを送り、mainでagentを呼ぶ構成を取れます。

重い計算・解析の途中から直接agentを呼ぶ用途で中継が実際の不便になった時に検討します。module登録だけでなく、broker・認証への接続、Worker終了時の取消しと所有関係も決めます。`noe:process`・`noe:io`のWorker対応や、MCPクライアントのWorker検証とは別の残件です。

### <未完了> 呼び出し元の終了後も続く独立agent実行

`keepAlive:true`は同じNOE内の待受であり、呼出し元の終了後も続く独立実行ではありません。現在の子agentは親runに所有され、親終了・取消しで回収します。

独立したNOEプロセスが自分の認証・broker・loop・回収を持ち、起動元は終了できる構成を候補にします。端末から離れて続けるだけなら、既存のOSのバックグラウンド実行・tmuxも利用できます。組込みAPIの必要性、ログ・結果・停止方法を検討し、会話の永続化・daemon・再起動後の復旧まで一括で必須にはしません。

### <未完了> Anthropic系SDK・Messages形式の追加 — 2026-10-17

OpenAI SDKは追加済みですが、Anthropic系SDK・Messages形式のadapterは未実装です。OpenCode Goの初期対応は利用者がChat Completions・Responsesに限定したため、Messagesはその未完了扱いにせず別候補にします。

追加時もprovider/modelとカタログは共用し、形式変換と認証はGoの通信adapterで扱う方針です。SDK選択と実装範囲は未決定です。

### <未完了> HTTP URLからのimportとimport mapの拡張

ローカルの完全一致import mapは実装済みです。HTTP/HTTPS URLの直接import、永続ダウンロードcache、prefix mapping、scopes、npmのパッケージ探索は提供していません。必要性は個別に判断し、ローカルmapの完了条件へ追加しません。

ESMでもgo-quickjsでの動作保証にはならないため、自作JSと動作確認済みライブラリを再利用する方針です。対応済みの範囲は[import mapガイド](import-map.md)を参照してください。

### <未完了> 観察イベントのSQLite保存

JSONLを検索しやすくする外部保存先の候補です。採用は未決定です。現在のリアルタイム表示・親向け観察APIはSQLiteなしで利用できます。

## 実装済みとして扱う項目

### <完了> 親agentによる子agentの監督・追加指示

現在の入口は`noe:agent`の関数です。旧`agent.run()`・`createAgent`や、`spawn_agent({wait:true})`という検討案を現行APIとして使いません。

| 操作 | 現行の契約 |
| --- | --- |
| `run_agent(config, instruction, options?)` | instance停止まで待ち、最終snapshotを返す |
| `spawn_agent(config, instruction?, options?)` | 開始後に`{agentId, commandId?}`を返す。`options.keepAlive:true`ならtask後も待受 |
| `observe_agent({agentId})` | live snapshotを即時取得。完了結果も含む。liveに`wait`・cursorは指定しない |
| `observe_agent({logPath, runId, cursor?, ...})` | 保存JSONLから状態・差分・結果を取得。こちらは`wait`も指定可能 |
| `steer_agent({runId, instruction, ...})` | 実行中taskへ追加指示を受領。次のモデル判断前に反映 |
| `interrupt_agent({runId})` / `interrupt_agent({agentId})` | task単位の取消し、またはinstanceと予約の停止 |
| `queue_agent({agentId, instruction, ...})` | 待受instanceへ次taskを予約 |
| `wait_task({agentId, commandId})` | 初期・queue taskの個別結果を待つ。完了済み結果の再取得も可能 |

人間用の`noe agents`・`steer`・`interrupt`・`queue`も実装済みです。子は一回の`run_js`終了後も親runに所有され、後続toolから観察・操作できます。親の完了・取消しで回収されます。[API](api.md)・[起動ガイド](agents.md)・[control実装記録](development/agent-control-implementation.md)。

同じinstanceのqueueは**前taskの最終回答を含む、確定済み・trim後の会話を継続**します。queue指示はそのtask開始時に保護されたuser messageとして追加し、run IDと実行runtimeは新しくします。新しいagent instanceへの履歴共有や、プロセス終了後の再開は意味しません。[queue会話継続の記録](development/queue-continuation-implementation.md)。

<a id="user-instructions"></a>

### <完了> ユーザー共通インストラクションの既定読込み — 2026-10-18

組込みinstruction → 共通設定の`instructions/*.md` → workspaceの`.noe/instructions/*.md` → 明示した`system`の順に追記します。共通側は`XDG_CONFIG_HOME`、空・未設定なら`~/.config`を使います。各ディレクトリ内はファイル名順で、同名でも両方を読みます。

各instanceの最初のtaskで本文を固定し、同じinstanceのqueueでは再読込みしません。hook moduleのtaskごとの再読込みとは異なります。workspaceはNOE起動時のcwdで、祖先・Git root・`AGENTS.md`は自動探索しません。

任意の複数ファイルもJSで読み、本文を結合して`system`へ渡せます。固定本文は切り捨て対象外です。詳細は[instructionsガイド](instructions.md)。

### <完了> 簡易的な実行コンテキスト管理と予算通知

各モデル要求直前に使用量／予算と切り捨て方針を通知し、予算へ達したら古い完了blockを削除します。切り捨て後は再推定した情報と履歴を送信します。固定指示・元task・適用済みsteer・開始したqueue指示を保持し、最新の完全blockも保護します。

既定の`retainRatio`は0.1ですが、最新blockの保護で保持量が10%を超える場合も許容します。モデルによる要約・保存専用turn・必須checkpointは追加せず、必要な情報のファイル保存は任意です。`noe:decision`の単発要求は切り捨てません。

windowと応答予約が既知なら、固定入力と応答予約を差し引いた残りの約90%を実行履歴予算とします。未知値がある場合の計算は[hook・contextガイド](hooks.md)を参照してください。window不明かつ明示予算なしでは総入力目安1,000,000 tokensを使い、モデルcapacityの保証とは扱いません。window不明でも計算済み予算を表示します。

同じinstanceのqueueでは会話とtrim状態を継続します。モデル・人間・JSONL/live snapshotへの通知、hookのcontext APIも実装済みです。[実装記録](development/context-trimming-implementation.md)・[観察ガイド](observe.md)。

### <完了> hookと処理の連鎖の追跡・管理

`beforeModelRequest`・`afterModelResponse`・`beforeTool`・`afterTool`と、hook内からのagent操作を実装済みです。agent・hook・queueを共通のgroup／原因情報で追跡し、任意の`executionLimits`を所有下で共有します。既定で新しい回数上限を課す仕組みではありません。[hookガイド](hooks.md)・[実装記録](development/execution-hooks-implementation.md)。

### <完了> agentの実行状況の観察

人間用は`--observe`とGoの`noe observe`、親用は`observe_agent`です。JSONLには時刻・ID、モデル要求とtoolの開始／終了、thinking・summary、発言・最終回答、実行JS全文、取得できる未処理例外の詳細を記録できます。traceだけでは取得できなかった情報を`--events`で保存できます。

人間向け表示は英語を標準とし、連続するthinkingをまとめ、Markdownを整形します。`run_js`は先頭コメントと成功失敗を表示し、そのコマンド出力やJS全文を通常表示へ流しません。生のJSONLとは表示を分けます。[イベントガイド](events.md)・[観察ガイド](observe.md)。

<a id="外部jsによるリアルタイム表示--2026-10-17"></a>

#### <完了> 外部JS案から親用・人間用の観察口へ — 2026-10-17

外部の観察JSそのものは同梱していません。目的だった逐次表示と親の観察はGo実装で提供済みです。`run`・`eval`で入口を使えます。

```sh
./noe run --observe --events=agent.jsonl examples/agent/try-agent.js '任意の指示'
./noe run --events=- examples/agent/try-agent.js '任意の指示' | ./noe observe
```

`--observe`だけではファイルを作りません。`--events=path`、またはcwdへ自動命名する単独の`--events`で記録します。通常JSの出力はJSONLでは出力イベントとして区別し、NOE自体の診断はstderrへ出します。SQLite保存は別候補です。

### <完了> 意思決定モデルの呼び出し

`noe:decision`の`decide`からOpenRouterのDecisions APIを呼べます。Jev 1.14・Perplexity Decider V1.2・Clefと、親agentの`run_js`からの呼出しを確認済みです。通常の生成・agent loopとは別経路で、認証と要求管理はホストに寄せています。[利用ガイド](decision.md)・[実接続記録](development/decision-openrouter-validation.md)。

#### <一部完了> 応用例：Jevによる起動設定プリセットの選択 — 2026-10-17

[agent-route-model.js](../examples/agent/agent-route-model.js)は実装済みです。`list_models`の候補と説明をJevに渡し、返却キーからprovider/modelを選んで親agentを起動します。専用ルーターモデルや本体の自動ルーティング機能は不要です。

ローカルfixtureで一連の経路を確認済みです。現在の例は`effort:"auto"`固定で、effortを含むプリセット選択は残る応用候補です。このサンプル自体の有料実接続は未確認であり、`decide`そのものの実接続済み範囲とは区別します。[モデルカタログ](model-catalog.md)。

<a id="完了-agentのvision対応--2026-10-17"></a>

### <完了> agentのVision対応 — 2026-10-17

`noe:image`の`read_image`と独立した`generate_image`を実装済みです。ローカル画像を読み、固定したbytesをモデルへ添付します。queue履歴・block単位のtrim・hookの添付選択・画像イベントにも対応します。通常JSからの直接生成も可能です。

生成はOpenRouterとOpenAI APIキー経路に対応します。ChatGPTプラン／OpenCode Goでの画像生成、画像編集・mask・複数枚生成・入力URLは今回の対応範囲外です。モデルの認識精度とNOEの入力経路は区別します。[画像ガイド](agent-images.md)・[実接続記録](development/agent-images-integration.md)。

### <完了> モデルの推論エフォートと設定継承

ルート起動ではprovider・model・effortを必須とし、`auto`も明示できます。子では省略項目を親から継承し、明示指定で上書きします。queueごとのeffort上書きはありません。reasoningの履歴保持も対応済みです。[APIリファレンス](api.md)。

### <完了> OpenCode Go APIの組込み — 2026-10-17

合意したChat Completions・Responsesの範囲を実装済みです。モデルごとの経路、reasoningとtool履歴、認証、取消し、組込みデフォルト＋外部JSON上書きに対応します。`User-Agent: noe/<version>`と、同じinstanceのqueue task間でも固定した`x-opencode-session`にagentIdを送り、親子は別のセッションIDを使います。taskごとのrunIdは別途発行します。[provider設定](providers.md)・[実装記録](development/provider-config-validation.md)。

通常起動の実接続と親子・tool往復・取消しのfixture検証を記録しています。利用枠超過時の実応答は未確認です。Messagesは別の将来候補です。

### <完了> OpenAI SDKとローカル互換接続の設定経路 — 2026-10-17

公式OpenAI SDK、`openai-chatgpt`・`openai-api`のResponses、互換接続のChat Completions、外部JSのChatGPT認証ヘルパーを実装済みです。外部JSONでbaseURL等を上書きし、認証なしの互換接続を設定する経路もあります。個々のローカルLLMでの動作確認は別途必要です。[OpenAIガイド](openai.md)・[実装記録](development/openai-implementation.md)。

### <完了> ローカルESM・import map

ローカルの完全一致bare名をESMへ対応付け、main・Worker・agent生成JSで同じmapを利用できます。ライブラリやmapの配置場所は任意で、自動探索はしません。`~/.config/noe/lib/`も利用者側の配置例であり、予約パスではありません。[利用ガイド](import-map.md)・[検証記録](development/local-import-map-implementation.md)。

### <履歴・完了> 自作REPLの必要性とqjsの再利用

v1.8.0時点では固定版go-quickjsのqjs由来の入力・行編集ソースを取り込み、評価先にNOEのruntimeを使っていました。当時の利用説明は[タグ付きv1.8.0のREPLガイド](https://forge.harakara.site/littleisland/noe/src/tag/v1.8.0/docs/repl.md)、統合経緯は[archive記録](archive/qjs-repl-integration.md)に残しています。2026-10-19の削除計画でこの決定は置き換えられ、現行のREPL利用・保守対象ではありません。[削除計画](development/repl-removal-plan.md)。

### <完了> 実行JS・未処理例外の記録、run_jsの捕捉出力上限

JS全文と利用可能な例外詳細をイベントへ保持します。`run_js`のstdout・stderrはそれぞれ先頭32KiB＋末尾32KiBの計64KiBまで保持し、省略量を示します。JSONLにも省略後の値を記録し、省略部分を自動で完全保存する仕様ではありません。全文が必要な処理はJS側でファイルへ保存できます。[イベントガイド](events.md)・[runner実装](../internal/agent/runner/output.go)。

実行JSは引き続きNOE利用者のOS権限で動きます。cwdはworkspaceへのアクセス制限ではありません。隔離が必要ならVM等で動かすという利用方針で、今回NOE独自のworkspace権限制御は追加していません。

### <完了> internal/agentの責務整理

provider、runner、観察、instanceとtask、JS側の公開API／loop／履歴／hookなどへ責務を整理しました。[構成ガイド](development/agent-architecture.md)・[実装記録](development/agent-refactor-implementation.md)を参照してください。今後の変更場所も構成ガイドを入口にします。

## モデル・接続先の選択方針

普段はサブスクのOpenCode Goを優先し、OpenRouterは様々なモデルの試用と意思決定モデルに使います。よく使う順はDeepSeek V4.1 Flash、MiMo-V2.6-Flash、GPT 6 Luna、GLM 5.3で、effortは用途で変えます。OpenAIではSol・LunaのResponsesを使い、ChatGPTサインインをAPIキーより優先します。

カタログは組込みJSON＋外部JSON上書きと`list_models`を実装済みです。通常生成・意思決定モデルのwindow等も記録しており、実行時の予算計算に使います。画像生成モデルのwindowは未設定です。値の根拠・未知値・OpenCode GoへOpenRouter値を採用した経緯は[モデルカタログ](model-catalog.md)を参照してください。

MiMo・GLMの組込み候補、優先度項目、providerをまたいだ自動選択は未実装です。カタログは許可リストではなく、直接JSへprovider/modelを書いたり、外部JSONへ候補を追加したりできます。これらの選択方針を自動適用する本体機能を新しく決定したものではありません。

## 維持する製品方針

外部コマンド、ファイル・HTTPのストリーミング、暗号・ハッシュは既にstdlibから利用できます。v1.0.0時点の未対応候補とは区別します。TypeScriptは後回し、npm・パッケージ管理とNode.js/Denoの完全互換は対象外です。元QuickJSの`std`・`os`の提供を前提にしません。

[開発記録へ戻る](README.md) · [検討履歴](archive/future-candidates-history.md) · [現在のAPI](api.md)


2026-10-19の追加指示で、7.1/7.2を含むCLIを **v1.14.0** として公開した。v1系での常用・小さな不具合修正を続け、v2.0.0の公開時期はその後に判断する。詳細は[リリース記録](releases/release-checkpoints.md)を参照。
