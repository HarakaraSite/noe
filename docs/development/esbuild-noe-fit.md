# esbuildのNOE適合性調査

[開発ガイド](README.md) · [以前のTypeScriptの議論](typescript-discussion-20261024.md)

> 調査後の更新: 以下で既存の別件として挙げたimport.meta.url未設定は、後続の[対応](import-meta-url-implementation.md)で解消した。未設定という記述と対照実験は調査時の状態を示す。現在の契約は[利用ガイド](../import-meta.md)を参照。TypeScript対応自体は引き続き未実装。

## 結論

**上流esbuildの公開Go APIを依存として使い、NOE側に薄い変換adapterを置く方針を推奨する。現時点でforkを始めるだけの必須要件は見つからなかった。**

用途は「読み込んだTypeScriptモジュールをJavaScriptへ変換し、解決・実行は既存のNOE/QuickJSへ渡す」こと。
公開 `api.Transform` はこの用途に合う。Node.js・外部esbuildコマンド・CGOは不要で、NOEのimport map、URL import、`node:`/`noe:`を置き換えずに使える。

ただし、**依存を追加するだけでTS対応が完成するわけではない**。入口・import・Workerの変換経路、設定、診断の接続はNOE側の作業になる。
esbuild自身で一番注意すべき制約は、同期のTransformに途中キャンセルAPIがないこと。
型検査や完全なtsc互換も提供しないが、これは型消去実行器としての役割の違いであり、forkして直ちに解消できる不足ではない。

本調査は実装計画の採用・TypeScript機能の実装ではない。NOE本体・rootのgo.mod/go.sumには変更を加えていない。

## 対象と実験条件

- esbuild: **0.28.2**、commit `f6058f8364fe7ab91ca57a83e02577ed74c9cae4`。公式ソースを `_refs/esbuild/` に取得済み。
- NOE: 調査時のworkspaceソース、go-quickjs **v0.29.0**。
- Go **1.27.2**、Linux amd64。
- 変換実験: `api.Transform` の `LoaderTS` / `FormatESModule` / `Target: ESNext` / `Sourcefile` / `LogLevelSilent`。minify/bundleは使わない。
- 実行実験: NOEの公開 `stdlib.Execute`、現在の標準loader、必要な箇所だけ実験用loader wrapperを使用。URLはローカルHTTP fixtureであり、外部CDN依存ではない。
- 独立したソース調査も行い、公開APIでできることと内部改造が必要なことを照合した。

以下のesbuildの行番号は上記commitのもの。
[上流ソース](https://github.com/evanw/esbuild/tree/f6058f8364fe7ab91ca57a83e02577ed74c9cae4)。

## 1. 現在のesbuildが適合する点

| 観点 | 評価 |
| --- | --- |
| Goへの組込み | `pkg/api`の公開関数にソース文字列を渡し、JS・map・Errors/Warningsを受け取れる。外部プロセス不要 |
| NOEのモジュール解決との共存 | Transformはbundleしない。依存の解決・取得をNOEに残せる |
| ESM | ESM出力を指定できる。実験ではimport specifier、top-level await、dynamic importを維持 |
| 組込みmodule | 変換後のコードから`node:path`・`noe:docs`を正常利用できた |
| TSの変換範囲 | 型注釈・import typeの消去に加え、enum/namespace等のJS生成もできた |
| 診断 | 元TSのファイル名・行・列・該当行・Notesを構造化して返す。stderrへ直接表示せずNOEに渡せる |
| source map | inline/external mapを生成でき、既存のgo-quickjs公開APIで元TSのstackへ戻せた |
| ビルド互換 | NOEの既存依存と併用し、CGO無効のCLIリンクが成功。rootの依存変更はしていない |
| ライセンス | MIT。配布時にはライセンス・著作権表示を維持する |

根拠: esbuild `pkg/api/api.go:194-223,422-490`、
`pkg/api/api_impl.go:1711-1831`、`LICENSE.md`。
Transformは内部でMockFSを使い、入力文字列を単一ファイルとして処理する。
Build APIのようにNode/npmの解決器をNOEに持ち込む必要はない。

### 実行して確認したこと

| 実験 | 結果・意味 |
| --- | --- |
| TS entryからlocal JSのstatic/dynamic import、TLA、node:path、noe:docs | default exportが`{answer:42,path:"a/b",docs:true}`となった |
| `import type`先が実在しない | type importが消え、実行時の取得は不要だった |
| enum / namespace | 変換・実行後の値が`7`となった |
| 型に合わない代入 | 変換エラーなしで文字列を返した。型検査はしていない |
| URLの`value.ts`をtext/plain配信 | 現loaderの後段wrapperで変換すると実行成功、値は`17` |
| ローカル`./local.ts` import | 現NOEの拡張子検査で拒否。esbuildより前の問題 |
| application/typescript配信 | 現NOEのMIME検査で拒否。esbuildより前の問題 |
| CommonJS系TSの`import = require` | esbuildはwarning付きでJS生成したが、NOEでは`require is not defined` |
| inline source map | managed NOEでは生成JSの3行目。直接QuickJSでmap有効化すると元TSの7行目へ戻った |

「現NOEで拒否した」項目は既存のJS専用契約であり、今回の変更による退行ではない。
URL実験のwrapperは可能性の確認であり、TSが標準loaderで既に利用できるという意味ではない。

## 2. 先に改善・設計すべきなのはNOE側の接続

### A. entryと依存を同じ変換契約へ接続する

現在は次の位置でJS専用の処理がある。

- CLIは`.js/.mjs`以外のentryを拒否する: `internal/cli/cli.go:82-86`。
- local module/import map targetも`.js/.mjs`限定: `internal/modulemap/snapshot.go:243-254`。virtual filesystemにも同じ検査がある。
- 取得ソースとcanonical module IDを返すloader: `internal/modulemap/loader.go:70-148`。
- entryのインメモリソースはloaderより前に返す: `internal/jsexec/execution_loader.go:144-155`。
- managed entryの実行: `internal/jsexec/runtime.go:153-173`。
- Workerは別VM/別loaderを持つ: `internal/stdlib/worker_runtime.go:67`、`internal/jsexec/execution_loader.go:183-203`。
- agentのrun_jsはvirtual ESM entry: `internal/agentexec/executor.go:88`。

そのため、CLIで最初のファイルだけ変換する実装では、TS依存・dynamic import・Worker・Go embeddingを一貫して扱えない。
**canonical IDが決まったソースをJSへ変換し、IDそのものは維持する共通の境界**を設けるのがよい。
変換済みentryをsource cacheへ入れること、import依存と二重変換しないことも同じ境界で扱う。

ファイルなら拡張子でTS/TSXを選べるが、eval/run_js/Worker evalの文字列には拡張子がない。
これらにTSを提供するか、提供するならどう明示するかは別途仕様化する。
既存JSをすべてTSとして解釈する必要はない。

外部Goホストへの公開境界は、esbuildのオプション型を直接露出するより、
NOE側の小さな変換設定・インターフェースにしておく方が責務を分けやすい。
esbuild adapterを別packageにすれば、利用しない組込みホストがコンパイラ依存を選ばない構成も取りやすい。
これはesbuildのforkではなくNOE側のpackage設計でできる。

### B. URLでは最終IDとMIMEを見て入力言語を決める

現HTTP readerはJavaScript MIMEとtext/plainを受理するが、TypeScript MIMEは受理しない
（`stdlib/loader_http.go:84-91,139-151`）。

TS用MIMEを追加しても、拡張子なしURLの入力言語はURL名だけでは判別できない。
現`RemoteReader`はソースと最終IDしか返さないため、MIMEが分かるreader内で変換するか、
取得結果に言語/メディア種別を渡す設計が必要になる。

- loader選択は元specifierの文字列だけではなく、redirect後のURL・URLのpath・MIMEを考慮する。
- canonical URL/query/aliasの既存identityは保つ。`.ts`を仮の`.mjs` IDへ変更しない。
- 現在のHTTP Allow・body上限・取消し・filesystem境界はそのまま使う。esbuildへ取得を任せる必要はない。
- source mapも同様で、通常はメモリ内のinline mapか登録済みmapで足りる。mapのために任意の追加HTTP/OSファイル読込みを増やす必要はない。

### C. 解決規則とtsconfigを混同しない

Transformは`tsconfig.json`を探索せず、`TsconfigRaw`を受け取るだけ。
そのraw設定でも`extends`は無視する
（esbuild `internal/resolver/resolver.go:320-365,1253-1263`）。

`paths/baseUrl`等でimport specifierをNOE向けに書き換えるAPIでもない。
たとえば「sourceにはdep.tsがあるがimportは./dep.js」というtsc向けプロジェクトが、
追加の解決規則なしでNOEから直接動くわけではない。
明示的な`.ts` import、拡張子補完、`.js`から`.ts`への対応のどれを採用するかはNOE loaderの判断であり、forkの理由ではない。

tsconfigを読む場合は、反映する項目・探索範囲・extends・優先順位を明示し、
Go embeddingでは選択されたfilesystemを通して読む。外部URL向けに無条件のtsconfig探索を加える必要はない。

### D. 型消去にも実行時の意味を変える設定がある

実験で次の違いを確認した。

1. **未使用のvalue import**は既定で削除された。
   `verbatimModuleSyntax:true`では残った。`import type`は消える。
   NOEで「書いたvalue importの副作用を保つ」契約にするなら、この設定をadapterの既定候補にできる。
   単にminify/tree shakingを無効にするだけでは、TS import elisionを止められない。
2. class field初期化は、既定のdefine semanticsではsetterを呼ばず、
   `useDefineForClassFields:false`ではsetterを呼んだ。
   `TsconfigRaw`の扱いは実行結果にも影響する。
3. `tsconfig.target`はTransformのJS出力`Target`と同じではない。
   class-field semanticsなどに関係するが、出力構文のtargetは公開`Target/Supported`で別に決める。
4. ESM保持には出力formatだけでなくtargetも関係する。
   古いtargetではdynamic importがrequire系へloweringされたり、import.metaが置換されたりする。
   QuickJS専用の`Engine`指定はないが、公開`Supported`でfeatureごとの調整は可能。

実験のESNextは基本機能を通した設定であり、全esbuild出力とQuickJSの互換性認定ではない。
JS/TS混在、必要な構文、ESM/importの実利用経路に対応するcompatibility設定をNOE側で持つのがよい。

### E. runtime診断にはsource mapの接続が必要

esbuildはparse/printするため、minifyしなくても行・列が変わる。「元の行位置を保った型の空白置換」ではない。

現NOEのmanaged VMは`quickjs.New()`を使い、source mapを有効にしていない
（`internal/jsexec/runtime.go:58`、Workerも`internal/stdlib/worker_runtime.go:67`）。
一方、実際に依存するgo-quickjs v0.29.0には公開
`quickjs.WithSourceMaps(nil)`があり、inline mapを読み込める。
同じesbuild出力を使った実験で、stackが生成JSの3行目から元TSの7行目へ戻った。

したがって、**esbuildやQuickJSのforkより先に、既存のmap生成とVMの公開オプションを接続するべき**。
Workerにも設定を伝え、structured diagnosticsのpositionと表示stackのどちらをどの単位で返すかを確認する。
esbuildのLocationは行が1始まり、列/長さが0始まりのUTF-8 byte単位なので、
QuickJS側の位置やsource mapのUTF-16列と無条件に同一視しない
（esbuild `pkg/api/api.go:194-223`）。

source mapにはinline/externalの選択があり、TransformでLinked mapは使えない。
生成mapをメモリ内で保持する構成、inline mapで渡す構成のいずれも公開APIで可能。

### 既存の別件: import.meta.url

#### 何を表すか

`import.meta`はES Moduleごとのhost metadataのobject。
その`url`プロパティには、Node.js・ブラウザ・Deno等では**今実行しているmodule自身のURL**が入る。

- ローカルファイルなら、たとえば`file:///workspace/lib/main.mjs`。
- HTTPで読み込んだmoduleなら、たとえば`https://example.com/lib/main.js`。
- `process.cwd()`のような作業ディレクトリや、最初に実行したentryの場所ではない。importされたmoduleも自分自身のURLを持つ。

代表的な用途は、**module自身を基準にした相対URLの解決**。

```js
// そのmoduleと同じ場所にあるdata.jsonのURLを作る。
const dataURL = new URL("./data.json", import.meta.url);
```

この式はURLを計算するだけで、ファイルやHTTPを読み込む処理ではない。
設定ファイル・Wasm・画像等の付属assetを探すライブラリで使われる。
Node.jsではfile URLからパスへ変換してmodule-relativeなファイル操作に使う例もある。

#### 現NOEではどうなるか

esbuildはESM/ESNextで`import.meta.url`をそのまま残した。
しかし現在のNOEは`import.meta`のobjectにhost metadataを設定しておらず、`url`が存在しない。

変換しない普通のCLI JavaScriptで確認した:

```sh
./noe eval 'console.log(JSON.stringify({urlType:typeof import.meta.url,keys:Object.keys(import.meta)}));'
# {"urlType":"undefined","keys":[]}
```

つまり、`import.meta`という構文が読めないのではなく、**objectはあるが`url`が未設定**という状態。
`import.meta.url`を読むだけならundefinedだが、
それを基準に`new URL("./data.json", import.meta.url)`等を行うコードは解決できず、エラーになる。

すべての外部ライブラリに影響するわけではなく、
このmetadataを使わないコードは通常どおり動く。
今回の時間変換ライブラリやTSの基本実行の成立を否定するものではない。

ECMAScriptは`import.meta`の具体的なプロパティをhost側へ委ねている。
したがって「JSの構文・言語仕様そのものの未実装」というより、
Node.js・Web等を想定したライブラリに対する**host互換性の不足**として扱うのが正確。
今回のTS変換による退行ではなく、以前からある別件である。

#### どこで対応するか

実際に依存するgo-quickjs v0.29.0には既に`Runtime.OnImportMeta`があり、
moduleが初めて`import.meta`を参照したときにhostがobjectを埋められる
（`extend.go:452-475`）。
NOEはそのcallbackを登録していない。

対応するなら、esbuildを改造して文字列へ置換するより、
**NOEのmodule identityを使って実行時に設定する**のが自然。

| module種別 | 設定する値の方向 |
| --- | --- |
| ローカルファイル | canonicalなファイルパスを正しいfile URLへ変換。空白・非ASCII・Windows path等をURLとしてエンコードする |
| HTTP/HTTPS module | redirect/alias処理後に実行するcanonicalなmodule URLを使う |
| eval・agentのrun_js等のvirtual entry | 実在ファイルを装うかどうかを含め、NOEとしてのURL契約を先に決める |
| Worker | 親とは別VMなので、同じ設定をWorker側にも適用する |

entryだけの固定値では、importされたmoduleの相対URLが誤った場所を指す。
現在のloaderが持つmoduleごとの解決済みIDを使う必要がある。
virtual entryの相対importは現在cwd基準であり、
新しいmetadataのURL解決との関係も利用者に分かるよう仕様化する。

`import.meta.url`を提供しても、npm解決・CommonJS対応・任意のasset読込みが自動で可能になるわけではない。
既存のfilesystem/HTTP権限も変わらない。
実装する際は各module種別の実利用経路を確認するが、今回の調査ではruntime変更は行っていない。

この改善に、esbuildのforkもgo-quickjsのforkも必要ない。

## 3. esbuild自身の制約と改善候補

### 途中キャンセルがない: 採用前に認識すべき最重要点

`api.Transform(input, options)`は同期関数で、context・Cancel・deadlineを受け取らない。
Transform側の内部OptionsにもCancelFlagをセットしていない
（`pkg/api/api.go:422-490`、`pkg/api/api_impl.go:1738-1784`）。

NOEのruntime ownerで変換する場合、その間はJS timer/jobが進まず、
contextが取消し済みになってもTransform自体が終わるまで戻れない。
前後のctx確認は必要だが、「変換中の停止」を実現するものではない。
別goroutineに投げてcallerだけ復帰しても、CPU/メモリを使う変換処理は残るため、
所有作業の停止を満たした説明にはできない。

公開`BuildContext.Cancel`は存在するが別のBuild APIであり、
parser内の細粒度キャンセルを保証する代替ではない。
cancel flagの確認はscan/compileの節目にあり、TS parserはflagを参照していない
（`internal/bundler/bundler.go:1492-1518,2182,3054`）。

**変換中も迅速に停止することが製品要件なら、context/Cancelに加えparser・printer等の協調停止を上流提案する価値がある。**
上流で提供されず必須となれば、ここはforkの具体的理由になり得る。
ただし今回、NOE全処理のhard deadline保証を確認したわけではなく、
新たな入力サイズ制限や外部プロセス化を自動で必須にする結論ではない。

### 任意の最適化候補: 現時点ではfork理由にしない

- **Transformの繰返し呼出し向けcache**: 毎回logger/cacheを作る。NOEのruntime-localな変換結果cacheは公開APIの外で持てる。
  まず既存のcanonical ID/source snapshotと設定に紐付ける。runtimeをまたぐ共有cacheや内部AST再利用は、必要な性能問題が確認されてから考える。
- **parser専用の薄い公開入口**: 現Transformは内部でbundler/linker等も使う。AST/parser/cacheへの直接公開APIはなく、Goのinternal制約もある。
  TS専用の最小依存surfaceが必要なら上流API拡張/forkの対象だが、現在その必要性は未確認。
- **元の行位置を保つ型消去**: 現Transformとは別の出力方式。enum/namespace/decorators等を含めると単純な型の削除では済まない。
  source mapで目的を満たせるなら独自実装の理由は薄い。
- **型検査・emitDecoratorMetadata・declaration emit**: 単なる隠れたオプションではない。
  program-wideの型意味解析等が必要で、esbuild forkの小改造で得られるものではない。

## 4. 配布サイズの実測

NOEの同じCLI mainを2つの実験用commandへコピーし、一方にだけ
「環境変数が指定されたとき公開api.Transformを呼ぶ」到達可能な関数を追加した。
CLI本体を変更せず、同じGo module graph・CGO無効・`-trimpath -ldflags="-s -w"`で比較した。

| 比較 | bytes | MiB |
| --- | ---: | ---: |
| NOEのCLI entryのみ | 50,073,760 | 47.75 |
| 同じentry + Transform参照 | 54,050,976 | 51.55 |
| 増分 | **3,977,216** | **3.79（約7.94%）** |

追加したTransform呼出し自体も実行できることを確認した。
これは調査時の依存・Linux amd64でのリンク増分であり、
将来のadapter完成版や他OS/architectureの配布サイズ保証ではない。

「公開APIが大きそうだから先にforkして切り出す」判断をするほどの根拠は現状ない。
サイズ予算がこの増分を許容しないと明確になった場合に、切出しの削減量・保守コストを比較する。

## 5. 取り込み方の判断

| 選択肢 | 現時点の判断 |
| --- | --- |
| 上流pkg/api + NOE adapter | **推奨**。変換器は上流に任せ、loader/設定/diagnosticsはNOEで制御 |
| 上流ソースをvendor | オフライン配布等の理由があれば可能。変換機能の不足を解決するものではなく、forkとは別 |
| 最初からfork | 必須の独自機能が確認されていないため非推奨。TS構文追随・修正・内部API変更の保守を引き受ける |
| 内部parserを直接使う独自変換器 | 公開APIではない。必要なら上流公開化/独自module化が必要で、通常のTransform wrapperより保守範囲が大きい |

進めるなら、まず公開API adapterとNOEの変換境界を設計し、
entry/import/Worker/embedding・diagnostics・設定の製品契約を揃える。
必要な改善を**NOE側の統合**と**esbuild側に追加する機能**に分け、
途中キャンセル・サイズ等に実際の未達要件があれば、その段階で上流提案または小さなforkを検討する。

## 確認範囲と再現資料

確認したものは上の代表的な実行経路、source map、公開API/内部ソース、CGO無効リンクサイズ。
全TypeScript仕様・TSX/decoratorの実行・WorkerのTS統合実装・並列race/stress・巨大入力の時間/peak memory・他OS配布は未検証。
これらを未確認だけを理由にすべて採用前の必須gateにするものではない。

詳細な実験ソースとログはGit追跡外にある。

- `_refs/esbuild-noe-probe/main.go`、`go.mod`、`go.sum`
- `_refs/esbuild-noe-probe/results.json`: 変換結果・実行値・診断
- `_refs/esbuild-noe-probe/cmd/base/`、`cmd/esbuild/`、`size.json`: サイズ比較
- `_refs/esbuild-noe-probe/import-meta-control.json`: 非変換JSの対照実験
- `_refs/esbuild-noe-api-assessment.md`: 独立したソース調査メモ

再現コマンド:

```sh
cd _refs/esbuild-noe-probe
go run . > results.json
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o noe-base ./cmd/base
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o noe-esbuild ./cmd/esbuild
```

probeはcheckoutへのlocal replaceを使う調査用moduleであり、公開consumerの依存設定ではない。
検証済みの結果と主要な結論は本書に残し、Git追跡外のログがなくても採用判断の根拠が分かるようにした。
