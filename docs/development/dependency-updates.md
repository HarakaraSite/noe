# 依存ライブラリの互換性確認と追随 — 2026-10-17

v1.3.1を基準に候補を個別確認し、全候補の組合せも検証した。その結果を受け、go-intlを先に適用して関連テストを確認し、残りを保守更新として適用した。NOEの実装コードや公開APIの変更は不要だった。

## 更新対象

| module | 更新前 | 更新後 | 判断材料 |
| --- | --- | --- | --- |
| `github.com/go-quickjs/go-intl` | v0.6.0 | v0.6.1 | 正規化・照合の高速化、Localeの参照共有修正 |
| `github.com/OpenRouterTeam/go-sdk` | v0.9.59 | v0.9.61 | 主にBatch型と説明文の変更。NOEが使う経路の互換性を確認 |
| `github.com/tidwall/gjson` | v1.19.0 | v1.20.0 | 整数オーバーフロー、UTF-8、Pathの修正 |
| `github.com/tidwall/pretty` | v1.2.1 | v1.2.2 | エスケープを含むJSONのin-place圧縮を修正 |
| `github.com/tidwall/match` | v1.1.1 | v1.2.0 | 既存の照合は互換。新しい大文字小文字非区別APIはNOE未使用 |
| `github.com/spyzhov/ajson` | v0.8.0 | v0.9.6 | Cloneの親参照などを修正。既存のcursor/data抽出を確認 |
| `golang.org/x/sys` | v0.48.0 | v0.49.0 | OS定義・システムコール更新。NOEの配布対象でビルド成功 |
| `golang.org/x/term` | v0.40.0 | v0.47.0 | x/sys v0.49.0と組み合わせて確認。PTY・端末復元は互換 |

go-quickjs v0.27.0、OpenAI SDK v3.79.0、websocket v1.8.15、sjson v1.2.5は維持した。qjsとmdcliの取り込み済みソースも更新不要だった。Goの要求バージョンは1.27.2のまま。

上流の差分：[go-intl](https://github.com/go-quickjs/go-intl/releases/tag/v0.6.1)、[OpenRouter SDK](https://github.com/OpenRouterTeam/go-sdk/compare/v0.9.59...v0.9.61)、[gjson](https://github.com/tidwall/gjson/compare/v1.19.0...v1.20.0)、[pretty](https://github.com/tidwall/pretty/compare/v1.2.1...v1.2.2)、[match](https://github.com/tidwall/match/compare/v1.1.1...v1.2.0)、[ajson](https://github.com/spyzhov/ajson/compare/v0.8.0...v0.9.6)、[x/sys](https://github.com/golang/sys/compare/v0.48.0...v0.49.0)、[x/term](https://github.com/golang/term/compare/v0.40.0...v0.47.0)。

## 互換性と性能の確認

基準のソースはv1.3.1タグの`b94b275ed6e382d80c20ec6c6a74646ca81ea5f3`。一時コピーと候補別modfileを使い、実行依存の解決結果を記録した。各候補は単独で比較し、x/termだけは要求されるx/sysとセットにした。8候補すべての関連テストが成功した。

- providerのfixtureでChat・Responses・Decisions、reasoningの保持、認証、ヘッダ、取消しを確認した。
- SDKの通常応答では、大きい整数`9007199254740993`と未知フィールドのraw JSONを保持した。
- gjsonでは、範囲外整数の変換が折返しから飽和へ変わった。SDKの型付きデコードにも反映する。無効なUTF-8のJSON表記は変わるが、デコード後の文字列は同じだった。効果がなくなる非推奨のglobal変数は、NOEと利用SDKで参照していない。
- prettyの修正対象はJSON圧縮であり、Markdown表示用のmdcliとは別。比較したエスケープ付き3入力中、旧版の2入力で標準JSON圧縮と一致せず、新版はすべて一致した。
- ajsonはClone後の子の親参照が修正された。SDK形式の`$.cursor`・`$.data`抽出結果は維持した。
- x/termの`Terminal.ReadLine`修正は、NOEが使うqjs由来の入力処理とは別。NOEで使うraw mode・復元・サイズ取得と、PTYによる編集・中断・EOFを確認した。

go-intlの性能は、Linux amd64 / Intel Core i9-13900HX / Go 1.27.2で、同じNOEコードと用意した文字列を使い、基準版・候補版の順を入れ替えて5回測定した。初期化とwarmupは計測区間から外した。中央値による結果は次のとおり。

| 処理 | 速度比（更新後／更新前） |
| --- | --- |
| ASCIIのNFD正規化 | 約1.04倍、測定のばらつきと重なる範囲 |
| ラテン文字のNFD正規化 | 約2.34倍 |
| 日本語のNFD正規化 | 約1.29倍 |
| 英語の照合 | 約2.14倍 |
| 日本語の照合 | 約1.19倍 |

これらは指定した文字列処理の測定で、一般のagentタスク全体の速度を示すものではない。

## 組合せと本体への適用

全候補の組合せでは、unit・E2E・vet・総合race、Linux/macOS/Windows × amd64/arm64の6ビルドとLinux版の起動確認を実施した。初回の並列raceでは、300ms以内の通信開始を前提にした`TestCLIAgentTimeoutPreservesExit124`だけが失敗した。終了コード124は正しく、データ競合の報告はなかった。基準版・候補版の各5回の再確認と、他のビルドと並行させない総合raceは成功した。更新固有の再現はなく、負荷による時間切れと考えられるが、初回と同じ失敗を基準版で再現したわけではない。

本体への適用では、go-intl単独でruntimeの関連テストを再確認した。その後、残り7候補を適用して`go mod tidy`し、解決した全モジュールとバージョンが事前検証の組合せと完全一致することを確認した。

| 適用後の確認 | 結果 |
| --- | --- |
| `CGO_ENABLED=0 go test ./internal/runtime -run 'TestStdlib\|TestException\|TestSession' -count=1`（go-intl単独） | 成功 |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1` | 全体unit・E2E成功 |
| `CGO_ENABLED=0 go vet -tags=e2e ./...` | 成功 |
| `CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o noe ./cmd/noe` | 成功。手元の`./noe`を更新 |
| バイナリの依存情報 | 対象8モジュールの更新後バージョンを確認 |
| version・算術・組込みmodule・Intl・認証ヘルパーhelpの5起動確認 | exit 0、stderr空で成功 |

raceと6対象ビルドは、同じ実装ソース・同じ依存構成で行った上記の事前検証結果を利用する。ネイティブ実行はLinux amd64のみ。実プロバイダへの要求や認証ファイルの更新は行っていない。

個別確認・性能測定・組合せ検証のログと機械可読結果：

```text
/home/agent/.cache/noe-qjs-repl/noe-dependency-compat-dci6fgko/
```

本体への適用と適用後の検証記録：

```text
/home/agent/.cache/noe-qjs-repl/noe-dependency-update-cyv6hdvn/
```

## go-quickjs v0.28.0への更新 — 2026-10-21

engineをv0.27.0からv0.28.0へ更新する。Go 1.27.2、go-intl v0.6.1、provider
SDKと他の依存は維持する。JITのビルドタグとruntimeオプションは追加しない。
NOE管理stdlibはv0.27.0原型のままで、取り込み直しや原型の由来情報の変更は行わない。
公開CLI/Go/JS APIの変更とNOE実装の互換性修正は不要だった。

上流の公開production Go 14ファイル、go.mod、stdlib production Goは両版で同一。
内部のBigInt/object最適化とprimitive deleteの仕様適合修正について、既存テストと
NOE経由のsmokeで確認する。NOE用途の性能改善は測定・主張していない。
consumerのv0.27.0要求は維持し、MVSがv0.28.0を選ぶことを確認する。

計画の通常・批判的レビュー、コードレビューと検証結果は
[取り込み計画・実施記録](go-quickjs-v0.28.0-plan.md)に記録する。
関連テスト、BigInt/object/Worker smoke、stdlib/agent両方の外部consumerがPASS。
通常・批判的コードレビュー後の全体unit/E2E・race・長時間Go Client・vet・6対象compile・
最終CLI smokeもPASS。native実行確認はLinux amd64のみで、公開・配置は行っていない。

初回の総合raceで、既存の外部コマンドowner-deathテストがプロセス消失時のESRCHを
失敗扱いにしていた。旧engineでも自然再現することを確認し、停止後pollだけを
同repositoryの既存判定に揃えた。修正後の反復・制御試験と通常/批判的再レビューで、
生存・権限・停止前の異常を引き続き検出することを確認してから総合テストを再実行した。
最終変更は依存・説明/記録と、この既存テストの最小修正のみ。
初回の失敗も含む作業ログ・結果は`/var/tmp/noe-q280-XrXP1S`に保存している。

## 上流不具合の報告と修正：ES module内のブロック関数宣言 — 2026-10-24

**現状（2026-10-26）：正式v0.29.0で解消し、NOEへ取り込み済み。** [影響調査・取り込み記録](go-quickjs-v0.29.0-impact.md#6-取り込み実施--2026-10-26)を参照。以下はv0.28.0を維持して上流修正を待っていた当時の調査記録。2026-10-24に独立再現を添えて[上流issue #4](https://github.com/go-quickjs/go-quickjs/issues/4)を英語で投稿した。
発端は`noe-events-20261024-033909-c8ef146fa483f3ef.jsonl`のHTTP stream課題で、
定義済みの`worker`が`ReferenceError: worker is not defined`になったこと。

### 再現と影響

次のES moduleは本来`7`を出力するが、NOE v2.1.1/v2.3.0と正式go-quickjs
v0.28.0単体の`EvalModule`でReferenceErrorになる。

```js
try {
  async function worker() { return 7; }
  console.log(await worker());
} catch (error) {
  console.log(String(error));
}
```

- module本体配下の`try`・`catch`・`finally`・`if`・`for`・`switch`等のブロックで発生。
  通常の`function`でも再現し、asyncや並列処理に限らない。
- 同名の外側の関数があると、ReferenceErrorではなく外側の関数を呼ぶこともある。
  期待値7に対して外側の関数の値99を返すケースを確認した。
- script実行、module最上位の関数宣言、関数本体内のブロック、`const`へ代入する
  関数式は成功。Node.jsでは対応するscript/moduleの38ケースが成功した。
- 当該箇所を`const worker = async () => { ... };`とすることで回避できる。

### 原因と追随時の確認

上流v0.28.0の`internal/compiler/module.go:91`が`moduleBindingsDone`を設定する。
`internal/compiler/stmt.go:91`の`hoistBlockDeclarations`はこのフラグだけで関数宣言を
スキップし、module最上位以外のブロック内宣言まで生成しなくなる。
`compileStatement`も関数宣言は生成済みとして処理を省くため、bindingが存在しない。
NOEのwrapperや今回のdata/診断変更を介さずに再現しており、上流compilerの既存不具合。
[仕様のBlockDeclarationInstantiation](https://tc39.es/ecma262/multipage/ecmascript-language-statements-and-declarations.html#sec-blockdeclarationinstantiation)
ではブロック内の関数bindingを作成・初期化する。

修正版を取り込む際は、最小再現と外側の同名関数を誤って呼ぶケースの解消を確認する。
初回調査では上流最新branch・issue/PRの確認と提出は行わなかった。その後の投稿準備で、当時のmainでも独立再現することを確認した。詳細は[独立再現と上流報告](#2件の独立再現と上流報告--2026-10-24)を参照。
engine・module cache・依存・NOE製品コードは変更していない。

調査用consumer、再現コード、ケース別結果、集約結果は
`/var/tmp/noe-worker-scope-investigation-9dsr772r`（`summary.json`、`repro.mjs`、
`quickjs-results.jsonl`、`shadow-results.jsonl`、`node-matched-results.json`）に保存。
consumerにNOE依存やlocal replaceはなく、`go mod verify`も成功した。

## 上流の不具合：colon付きmodule specifierのnamed re-export — 2026-10-24

**現状（2026-10-26）：正式v0.29.0で解消し、NOEのtask moduleも直接named re-exportへ戻した。** [影響調査・取り込み記録](go-quickjs-v0.29.0-impact.md#6-取り込み実施--2026-10-26)を参照。以下は初回報告・回避時点の記録。正式go-quickjs v0.28.0の再export解決処理に原因がある。上記のブロック関数宣言の不具合とは別件として追随する。2026-10-24に独立再現を添えて[上流issue #5](https://github.com/go-quickjs/go-quickjs/issues/5)を英語で投稿した。依存更新・上流PRの提出は行っていない。

### 発生と回避

agentのJS責務分離（S5）で、次のnamed re-exportを導入すると、実executor経由でtaskの起動が失敗した。

```js
export { executeToolWithHooks } from "noe:agent/tool-pipeline";
```

診断は `TypeError: cannot resolve "noe": stdlib: no module loader for "noe" from "noe:agent/task"`。NOEの `noe:agent/...` というmodule名に含まれるcolonで表面化したが、specifierを誤分割する処理はNOEのloaderではなく上流engine側にある。

既存のimport済みbindingをlocal exportする形へ変更し、同じ関数identityを維持して回避した。engineや依存は変更していない。

```js
import { executeToolWithHooks } from "noe:agent/tool-pipeline";
export { executeToolWithHooks };
```

### 原因と追随時の確認

上流v0.28.0の `internal/compiler/module.go` は、再exportの隠しbinding名を `"*re*" + moduleSpecifier + ":" + importedName` として生成する。`internal/vm/moduleresolve.go:49` の `indirectSource` はprefixを除いた文字列を `strings.Cut(rest, ":")` で最初のcolonから分割するため、`noe:agent/tool-pipeline` のmodule部分を `noe` と誤認する。標準の再export構文に対するengine実装の不具合であり、NOE独自の構文制限として扱わない。

初回調査ではNOE経由の失敗ログと固定版上流ソースを照合した。その後の投稿準備で、NOEを含まない独立したGo consumerによる再現と、当時のmainでも同じ結果になることを確認した。詳細は[独立再現と上流報告](#2件の独立再現と上流報告--2026-10-24)を参照。

回避後はagentbridge／agentexecの関連テストとruntimeのhook・tool出力・画像・context・queue等の関連fixtureが成功し、task経由のexportとpipeline直接importの関数identityも確認済み。詳細は[S5の対応記録](responsibility-boundaries-refactor-implementation.md#s5-task-loopとtool処理の境界)を参照。

証拠ログは `/var/tmp/noe-boundaries-KVDans/` の `s5-executor.log`（初回失敗）、`s5-executor-local-export.log`・`s5-runtime-local-export.log`（回避後成功）。修正版の取り込み時は、元のnamed re-exportがcolon付きspecifierを正しく解決し、同じbindingへ到達することを確認する。

## 2件の独立再現と上流報告 — 2026-10-24

利用者の明示指示に従い、`HarakaraSite`として次の2件を英語で投稿した。各issueには、そのまま実行できるGoコード、依存取得・実行手順、期待値と実測結果、原因候補、回避方法を記載した。投稿後にauthor・title・bodyを取得し、準備した内容との一致を確認した。

| issue | 独立再現の結果 |
| --- | --- |
| [#4: Block-scoped function declarations are skipped in ES modules, causing ReferenceError or incorrect shadowing](https://github.com/go-quickjs/go-quickjs/issues/4) | moduleのブロック内宣言はReferenceError、外側の同名関数があると期待値7に対して99。scriptとfunction式による回避は7。宣言2ケースはNode.jsのmoduleでも7 |
| [#5: Named re-exports misparse module specifiers containing a colon](https://github.com/go-quickjs/go-quickjs/issues/5) | `demo:dep`からの再exportが`demo`へ誤解決。colonなしの対照とimport＋local exportの回避はいずれも7 |

検証環境はGo 1.27.2 / Linux amd64、JIT build tagなし。両方とも正式v0.28.0と、確認時のmain `92bd61dd7575e61f378ae65a5bbdfeca159459c1`（`v0.28.1-0.20261024003228-92bd61dd7575`）で同一結果。独立consumerは上流の公開Go APIだけを使い、NOE・NOE管理stdlib・local replaceを含まない。再export用loaderはGoコード内の文字列を返すだけの実装。両版で`go mod verify`成功。NOEのgo.mod・go.sumは変更していない。

再現コード、版別modfile、英語本文、実測結果と投稿確認は `/var/tmp/go-quickjs-issues-MkRuD9/` に保存：`block/main.go`、`reexport/main.go`、`go.mod`・`main.mod`、`block-issue.txt`・`reexport-issue.txt`、`results.json`、`verified-issues.json`。issue本文にも再現コードを含めたため、このVMのファイルなしで再現できる。
