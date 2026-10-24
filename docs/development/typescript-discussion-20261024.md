# TypeScript対応の議論とexamples評価 — 2026-10-24

[開発ガイド](README.md) · [TSの既存検討メモ](../future-candidates.md#typescript) · [改善提案](noe-improvement-proposals-20261024.md#3-typescript対応を段階的に検討する)

## 位置付け

セッション終了にあたり、利用者とのTypeScript（TS）対応の議論、メリット・デメリット、Go向け変換器の選択、examplesのJSごとの評価を保存する。

**検討記録であり、TS実装・依存追加・公開API変更・リリース時期を承認したものではない。** TS実行、`run_ts`、変換器の組み込みは今回実装していない。実装・性能・互換性の検証結果として扱わない。

議論の元は[改善提案3](noe-improvement-proposals-20261024.md#3-typescript対応を段階的に検討する)。提案1の文書整合は別途実施した。後半のagent-server UI改善も別作業であり、TS化ではない。

## 要旨

- **Goの公開APIから直接呼び、外部プロセス・CGOを増やさずTS→JS変換を行うなら、esbuildが事実上の第一候補。** ただし「他の実装が原理的に存在しない」という断定ではない。
- **短いJSだからTSの負担が大きい、という一般化は不適切だった。** 型注釈は任意で、既存APIの宣言と推論で済む例は多い。短い例の多くは「追加効果も負担も小さい」。
- 行数よりも、共有関数、ファイル間通信、工程間のデータ、可変な共有状態などの**契約の境界**があるかが重要。
- 主な恩恵は、保存して育てるスクリプト・共通ライブラリ・複数工程の保守性。全JSのTS化や、短い処理への過剰な型注釈は必要ない。
- esbuildは型検査をしない。型による不具合検出はエディター・外部`tsc`との併用が前提であり、実行時の入力検証とも別。

## メリット

1. **検査したファイルをそのまま実行できる。** 外部`tsc --noEmit`で検査した`.ts`をNOEで実行できれば、利用者が別途JS生成物を作成・管理する手間を減らせる。
2. **複雑な契約を本文の近くに書きやすい。** agent設定、task結果、hook引数等を型・union・genericsで記述できる。大きなJSDocより読み書きしやすい場面がある。
3. **継続開発・リファクタリングに向く。** 共通関数の引数・戻り値の変更を、利用側までcheckerで確認しやすい。
4. **共同作業・agentへの委譲の境界を共有しやすい。** 担当間の入力・戻り値をコード上の契約として渡し、結合時に型の不一致を検出しやすい。
5. **元のTS位置から診断できる。** ソースマップとruntime診断を接続すれば、変換後JSでなく元のファイルを手掛かりに修正できる。ただしこれは実装が必要な目標であり、現状の提供機能ではない。
6. **実行と型検査を分離できる。** NOEは変換・実行、エディターや外部`tsc`は型検査を担当し、毎回の型検査や`tsc`同梱を必須にしない構成を取れる。

現在でもNOEの型宣言を用いたJS＋JSDoc＋外部`tsc`で補完・検査ができる。上記の型支援をすべて「TS実行対応で初めて得られる効果」とは扱わない。TS固有の追加価値は、型構文の書きやすさと、型を含むソースを直接実行する利用経路にある。

## デメリット・注意点

### 利用者側

- **TSを実行できても型の正しさは保証されない。** esbuildは型エラーのあるコードも変換しうる。「NOEで動いた＝型も正しい」ではない。
- **短い処理へ必要以上の型を書くと負担が増える。** ただし注釈は必須ではない。推論可能な変数まで型を書く運用を前提にしない。
- **既存JSで足りる用途では追加効果が小さい。** API呼出しと表示だけの処理は、既存宣言とJSDocでも十分なことが多い。
- **型は実データの検証ではない。** JSON／HTTP／JSONLやモデル回答を`as SomeType`で扱っても、入力の妥当性は確認できない。
- **型で保証できないものがある。** 指示文内の要求、回答品質、整数性・有限性、ファイル操作の成功、資源の回収順序、処理の収束等は別の検査が必要。

### esbuildを組み込む実装・保守側

| 項目 | 負担・制約 |
| --- | --- |
| 依存・配布サイズ | コンパイラ部分を含めるため依存とバイナリサイズが増える。Go APIなら外部esbuildコマンドやNode.jsは不要だが、コストがなくなるわけではない |
| 変換の実行コスト | TSのparse・変換に時間とメモリが必要。短い処理では相対的に目立つ可能性があるが、NOEでの実測値はない |
| loaderとの統合 | 入口だけでなくimport先のTS、ESM/import map、module ID、source cacheとの整合が必要 |
| 診断 | 変換時と実行時の二段階のエラー、ソースマップ、元ファイルの位置との対応が必要 |
| 設定契約 | `tsconfig.json`を読むか、どの設定を反映するか、target等の仕様を決めて維持する必要がある |
| 対応経路 | 対応すると決めたmain・eval・Worker・agent等の経路で、既存JS・取消し・資源所有を維持する必要がある |
| 変換器への追随 | TS構文・esbuildの変換挙動・固定engineの組合せを維持する必要がある |

esbuildは`tsc`の完全な代替ではない。ファイル単位の変換が基本で、型情報が必要な処理には制約がある。`emitDecoratorMetadata`や通常の`.d.ts`生成を提供せず、`tsconfig.json`の全設定を同じように扱うものでもない。[公式の制約](https://esbuild.github.io/content-types/#typescript-caveats)。

変換targetを設定しても、不足するWeb／Node APIが追加されたり、固定engineの既知不具合が修正されたりはしない。TS対応をnpm探索・パッケージ管理・Node.js／Deno完全互換へ拡張しない。

## Go向け変換器について確認したこと

想定する構成は **esbuildのGo APIでTS→JS変換し、既存のgo-quickjsで実行する** もの。

| 候補 | この議論での位置付け |
| --- | --- |
| esbuild | 公開Go APIの`api.Transform`でメモリ上のソースを変換できる。Goへ直接組み込みやすく、今回の要件では基本候補 |
| 公式TypeScriptのGo実装 | Go製でも、外部アプリ用の公開組込みAPIがあるとは限らない。今回確認した本家`tsc/`は`cmd`・`internal`中心で、通常の公開Go APIとしてesbuildと同列には扱えなかった |
| SWC・Oxc | TS変換器としては候補だがRust製。Goへ導入するならFFI・外部プロセス等を含む別の構成になる |
| esbuildのfork・自前変換 | 理論的な選択肢ではあるが、本家に不足が確認されていない段階で保守負担を持つ理由にはしない |

2026-10-24に公式資料・公開ソースを確認した。esbuildの`pkg/api/api.go`には`TransformOptions`・`TransformResult`・`Transform`の公開入口があり、Microsoftの本家`tsc/`およびtypescript-go側は内部package中心だった。公開Go APIの将来計画と、現在そのまま利用できる入口を区別する。「今後も公開されない」とは判断していない。

参照：
[esbuild Transform](https://esbuild.github.io/api/#transform) ·
[esbuild公開Go API](https://github.com/evanw/esbuild/blob/main/pkg/api/api.go) ·
[TypeScriptのtsc配下](https://github.com/microsoft/TypeScript/tree/main/tsc) ·
[Go APIの議論](https://github.com/microsoft/typescript-go/discussions/481) ·
[SWC](https://swc.rs/) ·
[Oxc](https://oxc.rs/docs/guide/usage/transformer)

NOEでの変換追加時間・メモリ・バイナリサイズは未測定。公式の大規模bundleベンチマークを、短いNOEスクリプトの実測値として使わない。

## 実装する場合の方向と未決定事項

- 既存ESM構成を保つなら、まずバンドルせず、NOEのloaderが読む各TSモジュールを変換する構成が自然、という見解。実装契約としては未確定。
- ファイル実行の最小範囲を決め、import先・ESM/import map・module IDも対象にする。入口だけの変換成功を完了としない。
- 元TS位置への診断対応を扱う。
- main・eval・Worker・agentのどこまでを同時対応するか、変換中の取消し、既存の所有・回収との接続を決める。
- `tsconfig.json`、target、構文の対応範囲を明示する。未確認の不安だけで不要な機能制限やfallbackを追加しない。
- 型検査は外部`tsc`・エディターへ任せる案を維持する。毎回の型検査・`tsc`同梱・新しい`noe check`を前提にしない。
- **`run_js`→`run_ts`の標準切替は別判断。** tool・hook・履歴・イベント・既存呼出しへの影響がある。TSファイル実行の導入だけでも価値があり、先にtool名を変える必要はない。
- 具体的なversion・時期、変換器の採用版、対応経路、公開契約は未決定。過去メモのv2.5／v3.0は候補であり予定ではない。

## examplesのJSごとの評価

### 対象と評価方法

利用者の依頼に基づき、`examples/`の手書き`.js` **51件すべて**を読んで評価した。未コミット・未追跡のagent-server変更も含む。対象漏れ・重複がないことをファイル一覧と照合した。

**評価時点は`1bad24a`をHEADとする当時のworking tree。agent-serverの変更は後に`10f36dc`へコミットされた内容に相当する。** その後のHistory等幅化・Tasks展開・New agent追加より前の評価であり、UI改善後の再評価ではない。表のリンクは現在のファイルを参照するが、評価の時点は区別する。

- **効果**：必要な型情報を補い、エディター／外部checkerを併用した場合の保守上の価値。
- **負担**：実際に有用な型を補い、維持する手間。全変数への注釈や、共通のengine組込み費用は含めない。
- 型支援の多くはJS＋JSDocでも得られるため、TS固有の効果と過大評価しない。
- 高・中・低はソースを読んだ定性評価であり、移行工数の実測やコンパイル成否の検証ではない。

| 評価 | 低 | 中 | 高 |
| --- | ---: | ---: | ---: |
| 型付けによる効果 | 26 | 17 | 8 |
| 型を補い維持する負担 | 35 | 15 | 1 |

以下のpathは`examples/`からの相対。

### basics・import map

| JS | 効果 | 負担 | 評価・具体的なメリットと限界 |
| --- | --- | --- | --- |
| [`basics/hello.js`](../../examples/basics/hello.js) | 低 | 低 | 出力のみ。型注釈は不要で、TSの追加効果もほぼない。 |
| [`basics/sleep-demo.js`](../../examples/basics/sleep-demo.js) | 低 | 低 | stepやsleepは推論できる。注釈を足す必要はほぼない。 |
| [`import-map/main.js`](../../examples/import-map/main.js) | 低 | 低 | 呼出し一つで型はmath側から利用するだけ。checkerのalias設定は実行時mapへ合わせる必要がある。 |

### HTTP

| JS | 効果 | 負担 | 評価・具体的なメリットと限界 |
| --- | --- | --- | --- |
| [`http/download-file.js`](../../examples/http/download-file.js) | 低 | 低 | Responseとbyte列は既存宣言から推論できる。引数・HTTP statusの検査は実行時のまま。 |
| [`http/fetch-json.js`](../../examples/http/fetch-json.js) | 低 | 低 | JSONをそのまま保存し構造を使わない。具体的な応答型を作る意味が薄い。 |
| [`http/http-json.js`](../../examples/http/http-json.js) | 中 | 低 | RequestInit一つでbody/headersの後付けを表せる。任意JSON本文を細かく型付けする必要はない。 |
| [`http/http-server.js`](../../examples/http/http-server.js) | 低 | 低 | Request/Responseは文脈推論でき、echo本文は透過処理。独自型はほぼ不要。 |
| [`http/receive-stream.js`](../../examples/http/receive-stream.js) | 中 | 低 | byte streamと受信結果の形を明示できる。Uint8Array/number配列等の少数の型で済む。 |
| [`http/roundtrip.js`](../../examples/http/roundtrip.js) | 低 | 低 | 固定JSONの往復だけ。入力を型assertionしても実データの検証にはならない。 |
| [`http/upload-stream.js`](../../examples/http/upload-stream.js) | 中 | 低 | streamのchunk型と任意serverの扱いを明確にできる。byteOffsetや送信結果の正しさは実行時確認。 |

### JSON・売上レポート

| JS | 効果 | 負担 | 評価・具体的なメリットと限界 |
| --- | --- | --- | --- |
| [`json/collect-json.js`](../../examples/json/collect-json.js) | 低 | 低 | readdirのstring[]、lstatのFileInfo、集計オブジェクトは推論可能。主な入力契約は既存の有限数値配列チェックにある。 負担・限界：型だけでは数値の有限性を保証できず、型注釈を増やしてもファイル集計の追加保証は小さい。 |
| [`json/lib/transform.js`](../../examples/json/lib/transform.js) | 中 | 低 | 短くても公開関数なので、入力プロパティを保持してprocessedを上書きする契約を呼出側へ伝える価値がある。 負担・限界：単純なT & {processed:true}では既存processedと衝突し得る。JS＋JSDocのジェネリックでも同等の支援が可能。 |
| [`json/process-json.js`](../../examples/json/process-json.js) | 低 | 低 | filesのname/count/totalとreportは生成箇所から推論でき、移動処理も同じfilesを直接使うため追加の共有型は必須でない。 負担・限界：型はrenameの成功、部分完了、出力先との衝突を保証しない。行数の多さほど型注釈の利益は増えない。 |
| [`json/summarize-stdin.js`](../../examples/json/summarize-stdin.js) | 低 | 低 | stdinのstring\|nullは既存宣言で分かり、数値配列検査後のcount・total・averageは局所的に推論できる。 負担・限界：有限数値や空配列時のaverage:nullは実装上の検査・分岐が担い、TS化そのものの追加効果は小さい。 |
| [`json/transform-json.js`](../../examples/json/transform-json.js) | 低 | 低 | readFile→JSON.parse→transform→writeFileという接続だけで、独自にプロパティを参照しない。transformの契約を利用すれば足りる。 負担・限界：JSON.parseのanyをそのまま渡すと型付きtransformの保証も弱まる。objectというassertionは入力検証にならない。 |
| [`sales-report/lib/sales.js`](../../examples/sales-report/lib/sales.js) | 高 | 中 | Orderから商品別集計、SalesSummaryからformatReportへの共有契約があり、quantity/unitPriceの数値性や集計フィールドの変更を確認できる。 負担・限界：引数と空のMapには明示型が有用で、その維持が必要。Order型だけではJSON注文を検証できず、statusの許容値も実契約に合わせる必要がある。 |
| [`sales-report/sales-report.js`](../../examples/sales-report/sales-report.js) | 中 | 中 | 読み込んだ注文をbatches.flat→summarizeSales→formatReportへ渡す境界で、共有Order・集計契約を利用できる。 負担・限界：JSON.parseのanyを残すと型付きライブラリへの接続保証が抜ける。Order[]というassertionだけでは入力検証にならない。 |

### agent

| JS | 効果 | 負担 | 評価・具体的なメリットと限界 |
| --- | --- | --- | --- |
| [`agent/agent-control.js`](../../examples/agent/agent-control.js) | 低 | 低 | run_agentの設定とsnapshotのJSON出力だけで、独自のデータ契約はない。設定キーの支援は既存宣言から得られる。 負担・限界：型を追加してもkeepAliveや45秒待機・外部steerの実行上の意味は検査できず、TS化による追加効果は小さい。 |
| [`agent/agent-hooks.js`](../../examples/agent/agent-hooks.js) | 低 | 低 | contextBudget/contextPolicy/executionLimitsの設定形状とlastResultのnull可否を既存AgentConfig/AgentSnapshotから確認できる。 負担・限界：hookはパス文字列で指定するため、このファイルのTS化だけではhook exportやcallback契約との接続を検査できない。 |
| [`agent/agent-openai.js`](../../examples/agent/agent-openai.js) | 低 | 低 | argv由来の文字列とrun_agent結果を扱う直線的な処理で、lastResult確認も既存宣言による支援で足りる。 負担・限界：TSの追加注釈で得られるものは少なく、model名の存在やagentが計算を正しく行ったかは検査できない。 |
| [`agent/agent-opencode-go.js`](../../examples/agent/agent-opencode-go.js) | 低 | 低 | model/instructionからrun_agentを呼び、結果状態を確認して表示するだけで、独自型を必要とする境界がない。 負担・限界：providerとmodelの対応や生成回答の正しさは静的型の対象外で、JS＋既存宣言との差は小さい。 |
| [`agent/agent-parallel.js`](../../examples/agent/agent-parallel.js) | 低 | 低 | 親JS自身は二つのpathを指示文へ埋め込み、answerを表示するだけ。子の集計構造をこのJSでは操作していない。 負担・限界：left/right/totalはプロンプト内の要求であり、親をTS化しても子が読むJSONや回答JSONの形状は保証されない。 |
| [`agent/agent-route-model.js`](../../examples/agent/agent-route-model.js) | 中 | 中 | list_modelsのModelInfoをchoicesへ保持し、decisionのchoiceから選んでrun_agentへ渡すデータ接続を明示できる。 負担・限界：DecisionResponseのanswersは汎用JSONで、動的なm0等のキー集合との対応は自動推論されない。choiceの絞り込みと存在確認が必要で、assertionでは代替できない。 |
| [`agent/agent-summary.js`](../../examples/agent/agent-summary.js) | 低 | 低 | fileを指示文へ埋め込んでrun_agentのanswerを表示するだけで、独自の解析結果や共有関数はない。 負担・限界：ファイルの存在や要約が3点になっているかは型では検査できず、TS化による保守上の追加効果は小さい。 |
| [`agent/hooks/context.js`](../../examples/agent/hooks/context.js) | 中 | 中 | 短くても外部から呼ばれるbeforeModelRequestのapi契約があり、usedPercentのnull可否、execution.counters、setRuntimeInfoの入力を明示する価値がある。 負担・限界：現行の公開型宣言にはhook callback用API型がなく、利用部分の契約を補い維持する必要がある。TSだけで呼出しphaseの適法性まで保証するわけではない。 |
| [`agent/task-loop-jev/solution.js`](../../examples/agent/task-loop-jev/solution.js) | 低 | 低 | export関数の入力境界とnumberの戻り値は明示できるが、処理の中心である正の整数判定は静的なnumber型では表せない。 負担・限界：number[]に限定すると、異種要素を無視し非配列を拒否する実際の契約を狭める。意図的な小数・Infinity加算の誤りも型だけでは検出できない。 |
| [`agent/task-loop-jev/test.js`](../../examples/agent/task-loop-jev/test.js) | 中 | 低 | casesの各行がname/input/expectedの位置契約を持ち、resultsにはexpected/actualの有無が異なる行を追加するため、少数の型で意図を明確にできる。 負担・限界：単純な配列推論ではtuple位置が失われる一方、過度に厳しい入力型は文字列・null・非配列を試す検査を妨げる。 |
| [`agent/task-loop-jev/workflow.js`](../../examples/agent/task-loop-jev/workflow.js) | 高 | 中 | completed/review/submit/testがsnapshot・結果・判定・子プロセス終了情報を受け渡し、reportへ後付けする項目も多い。関数境界の型が工程間の保守に有用。 負担・限界：report/stepsの可変形状、汎用record、DecisionResponseの絞り込みを補う必要がある。型を付けてもレビュー内容の妥当性やループの収束は保証できない。 |
| [`agent/try-agent.js`](../../examples/agent/try-agent.js) | 中 | 中 | CLIからconfigを動的更新してrun_agentへ渡すため、許可キーとAgentConfig/Effortの契約を明示する価値がある。指示文・ファイル選択の処理は推論で足りる。 負担・限界：arg.slice(2)は許可キーへ自動で絞られず、effortも未検証の文字列なので、型指定だけでなくキー分岐と値の絞り込みが必要になる。 |

### agent-server

| JS | 効果 | 負担 | 評価・具体的なメリットと限界 |
| --- | --- | --- | --- |
| [`agent-server/app.js`](../../examples/agent-server/app.js) | 高 | 高 | tasks・activeを共有するfinish/refresh/dispatchとsteer更新に、Task・状態・nullableな結果の契約が有用。HTTPへ返す構造の変更も追跡しやすい。 負担・限界：Task/Steer/Progress、null初期化、lockedの総称型、HTTPErrorの追加プロパティを整える負担がある。JSONLの正しさや状態遷移の順序は型だけでは保証できない。 |
| [`agent-server/server.js`](../../examples/agent-server/server.js) | 中 | 中 | CLIの文字列をport数値・Effortへ変換しstartAgentWrapperへ渡す境界を明示でき、options[key]による異種プロパティ更新を整理できる。 負担・限界：現在はportへ文字列を一旦代入するため、単なる注釈追加では済まず生引数と確定設定の分離が必要。範囲・effortの実行時検証は残る。 |
| [`agent-server/worker.js`](../../examples/agent-server/worker.js) | 中 | 低 | 17行でもwrapperから受けるstdinのoptions/readyURLと、登録時のurl/agentIdはファイル間の共有契約であり、変更追従に型が役立つ。 負担・限界：JSON.parse結果へのassertionだけではbootstrapを検証できない。fetchと起動結果の型支援自体はJSでも得られる。 |
| [`agent-server/wrapper.js`](../../examples/agent-server/wrapper.js) | 高 | 中 | bootstrap送信とworker登録の往復契約、ready/exitedの競合、captureのバイトstreamとwriterを明示でき、プロセス管理とHTTP転送の変更を追跡しやすい。 負担・限界：nullableなchild/worker/status、Promiseのresolver、streamのnull可否に補足が必要。登録JSONやstderrから得るeventsPathの妥当性は型だけでは確認できない。 |

### process・daemon・低レベルI/O

| JS | 効果 | 負担 | 評価・具体的なメリットと限界 |
| --- | --- | --- | --- |
| [`daemon/client.js`](../../examples/daemon/client.js) | 低 | 低 | 主にJSONLの透過送受信。共通応答型は使えるが、受信検証は別。 |
| [`daemon/server.js`](../../examples/daemon/server.js) | 高 | 中 | ping/echo/sumの要求・応答型と接続/非同期task集合が有用。入力絞り込みと状態型の維持が必要。 |
| [`low-level/index-roundtrip.js`](../../examples/low-level/index-roundtrip.js) | 中 | 中 | TCP/Unixの識別unionとaddress分岐を明確にできる。argvのnetworkの絞り込みを整える手間がある。 |
| [`low-level/wait-signal.js`](../../examples/low-level/wait-signal.js) | 低 | 低 | subscribeと非同期反復はAPI型から推論できる。signal待機・配送の正しさは型の対象外。 |
| [`process/process-pipeline.js`](../../examples/process/process-pipeline.js) | 低 | 低 | 子プロセスとpipeはAPI型から推論できる。文字列中のevalコードは親の型検査対象外。 |
| [`process/process-worker.js`](../../examples/process/process-worker.js) | 低 | 低 | 固定requestの送信と生の応答表示が中心。追加注釈はほぼ不要で応答JSONの保証も増えない。 |
| [`process/stdio-jsonl.js`](../../examples/process/stdio-jsonl.js) | 中 | 低 | reply引数とid/valueの要求応答契約を少数の型で表現できる。JSON入力の実行時検証は別。 |

### MCP・Exa・decision

| JS | 効果 | 負担 | 評価・具体的なメリットと限界 |
| --- | --- | --- | --- |
| [`decision/decision-classify.js`](../../examples/decision/decision-classify.js) | 低 | 低 | decideの引数は既存DecisionOptionsで文脈型付けでき、応答は表示するだけ。独自の型契約がほぼない。 負担・限界：questionsは汎用JSONRecordで、teamの選択肢と応答の対応までは既存宣言で保証されない。 |
| [`exa/exa.js`](../../examples/exa/exa.js) | 高 | 中 | 公開searchのoptionsとsearchErrorの追加プロパティを型で共有すると、呼出側の補完と変更追従が改善する。現JSDocのobject指定は粗い。 負担・限界：Exaパラメータの透過転送を保ちながら既知項目を記述する必要がある。応答型のassertionではHTTP JSONを検証できない。 |
| [`mcp/client.js`](../../examples/mcp/client.js) | 高 | 中 | 短くても共有withMCPのcallbackへClient型を渡し、operationの戻り型をジェネリックで保持する効果が大きい。 負担・限界：実行用import mapのmcp/sdkは生成JS bundleを指すため、checkerには公式SDK宣言への対応付けが別途必要。JS＋JSDocでも同じ契約を表現できる。 |
| [`mcp/exa.js`](../../examples/mcp/exa.js) | 中 | 中 | 型付きwithMCP経由でlistTools/callToolの呼出契約を利用でき、動的headersと一覧・検索結果の扱いを明確にできる。 負担・限界：list結果とcall結果でisErrorの有無が異なるため絞り込みが要る。汎用SDK型だけではweb_search_exa固有の引数schemaを保証できない。 |
| [`mcp/local.js`](../../examples/mcp/local.js) | 中 | 中 | HTTP handler、JSON-RPC返信、SDKのtool応答contentの接続に型を付けると、fixture内のprotocol形状とcontent種別の取り違えを確認しやすい。 負担・限界：request.jsonはunknownで、受信値の扱いとcontentの種別絞り込みが必要。型だけでは初期化順序や分割SSE送信の正しさは保証できない。 |
| [`mcp/sdk-entry.js`](../../examples/mcp/sdk-entry.js) | 低 | 低 | 公式SDKの2つのexportを再公開するだけで、独自のデータ処理・契約がない。型情報は元SDKが持つ。 負担・限界：TSへ変えても実行用bundleに型宣言は自動付与されず、mcp/sdkのchecker側解決という課題は残る。 |

### ログ解析・画像

| JS | 効果 | 負担 | 評価・具体的なメリットと限界 |
| --- | --- | --- | --- |
| [`image/generate.js`](../../examples/image/generate.js) | 低 | 低 | 設定と結果は既存API型から推論できる。provider/modelの実際の対応は型で保証できない。 |
| [`image/read.js`](../../examples/image/read.js) | 低 | 低 | agent結果は既存型、画像読取りは指示文内。TS化してもプロンプトや画像内容は検査できない。 |
| [`log-analysis/analyze.js`](../../examples/log-analysis/analyze.js) | 高 | 中 | analyzeJSONLとtoMarkdown間のMetric・要求行・Report契約、number\|nullとrateStatusを共有でき、集計と表示の変更不整合を検出しやすい。 負担・限界：pairedの絞り込みにはnullable項目間の関係を表す工夫が要る。JSONLイベントの型を宣言しても実データの欠落・不正値は検証されない。 |
| [`log-analysis/compare.js`](../../examples/log-analysis/compare.js) | 中 | 低 | analyzeJSONLの戻り値からreportsを推論してtoMarkdownへ接続でき、共有Report契約の変更をCLI側でも確認できる。 負担・限界：formatやbelowの正当性はCLIの実行時チェックが必要。型だけでは閾値0..1やファイルの存在を保証できない。 |
| [`log-analysis/test.js`](../../examples/log-analysis/test.js) | 中 | 低 | Reportのプロパティ参照とtoolCasesのevent・expected・labelの位置関係をcheckerで確認でき、公開契約変更への追従を助ける。 負担・限界：equalはJSON比較なので値や集計意味の正しさは実行時検証のまま。actualとexpectedへ過剰な型制約を付ける必要はない。 |

### 補足：mjsと生成物

- 手書きの[`import-map/lib/math.mjs`](../../examples/import-map/lib/math.mjs)は **効果：中／負担：低**。`sum(values: number[]): number`という公開関数の契約を少量の記述で表せる。上記51件の集計には含めない。
- `examples/mcp/mcp-sdk.bundle.mjs`は生成物であり、TSへ移行する元ソースではないため個別評価対象外。元SDKの型宣言とcheckerの解決設定は別に扱う。

### 特に効果が見込めるまとまり

- 共有ライブラリ：`sales-report/lib/sales.js`、`log-analysis/analyze.js`、`exa/exa.js`、`mcp/client.js`。
- 複数工程の接続：`agent/task-loop-jev/workflow.js`。
- 共有状態・通信契約：`agent-server/`、`daemon/server.js`。

十数行の`mcp/client.js`でもcallbackと戻り値をgenericsで表す価値がある。同様に`agent-server/worker.js`は短くてもbootstrap・登録というファイル間契約を持つ。逆に、長めでも局所的な値を生成してそのまま使う`json/process-json.js`は推論で済む部分が多い。

**結論は「短いからTSに向かない」ではなく、「独自の契約がない処理は効果も負担も小さい」。このexamples群にはTS対応の用途があるが、全JSの書き換えや型注釈の強制は必要ない。**

## 確認範囲・再開時の注意

- examplesのソース、必要なREADME、NOEの既存型宣言を選択的に照合した。agent群・データ／SDK群は分担して確認し、対象一覧と評価件数を集約した。
- 変換器の選択では公式資料と公開ソースを参照した。外部リポジトリのmainや将来計画は変更されうるため、採用時には必要な公開契約を再確認する。
- **TS変換器の接続実験、TSへの書き換え、`tsc`での移行検証、性能・配布サイズの測定は行っていない。**
- 同セッションで行った文書整合やagent-serverのHTTP／Firefoxテストは、それぞれの変更の検証であり、TS対応の成立を示すものではない。
- 再開する場合は、この記録と[既存検討メモ](../future-candidates.md#typescript)を読んだ上で、まず実際の利用経路と最小対応範囲を決める。今回の保存を実装承認として扱わない。
