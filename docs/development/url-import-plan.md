# HTTP/HTTPS URL importの設計案

状態：利用者から、複数スライスへの計画化・再レビュー、各スライスの実装/テスト/コードレビュー、最後の総合テストとコミットまでの実行指示を受領。初回の設計レビュー指摘2件とMIME合意を反映済み。本改訂は通常/批判的再レビューで承認済み。[スライス計画](url-import-slices.md)に従いS1〜S4の実装/テスト/コードレビュー、公開ESM利用、総合検証を完了。詳細・最終確認とコミット範囲は[実装記録](url-import-implementation.md)を参照。本書は利用者との対話で合意した範囲を正本としてまとめる。採否・残件とレビュー原文へのリンクは[レビュー結果](url-import-review.md)を参照。

本書の「合意」は利用者が選択した利用方針、「実装仕様」は実装側が詰めた技術判断を表す。以下の新APIは実装計画であり、実装・検証完了前に現行リリースAPIとして扱わない。初回レビューの原文は当時の記録として維持する。

## 1. 目的と成功基準

公開されているESMライブラリを、事前の手動ダウンロードなしにNOEから利用できるようにする。自作・管理下のJSも同じ経路で扱う。

次の利用経路で、入口だけでなく依存するESMグラフを読み、結果を利用できることを成功基準にする。

```js
import { chunk } from "https://esm.sh/lodash-es@4.17.21";
const lib = await import("https://esm.sh/lodash-es@4.17.21");
```

```json
{
  "imports": {
    "lodash": "https://esm.sh/lodash-es@4.17.21"
  }
}
```

```js
import { chunk } from "lodash";
console.log(chunk([1, 2, 3], 2));
```

計画時点では上記CDN URLは未検証の利用形だった。その後S4で直接importとURL alias/dynamic importからchunkを使い、実接続・結果利用が成功した（[実装記録](url-import-implementation.md)）。任意のnpm packageが動くことは成功基準にしない。

## 2. 利用者との合意

| 項目 | 合意した方針 |
| --- | --- |
| 主な用途 | 公開されているESMライブラリも使いたい |
| URL scheme | HTTPとHTTPSの両方 |
| 初回の経路 | 通常のimport、dynamic import、import mapの完全一致bare名のURL target |
| 実行入口 | URLをCLI runやWorker constructorの入口にする対応は初回に含めない |
| キャッシュ | ダウンロードキャッシュは最初は持たない。runtime間・プロセス間のソース再利用、永続キャッシュを導入しない |
| 更新方針 | キャッシュを持たないため、更新・reload・失効管理は初回対象外 |
| 依存固定 | ハッシュ固定・lockfileは初回に含めない |
| URLの拡張子 | .js・.mjsを必須にしない。拡張子なし・その他の拡張子も許可 |
| Content-Type | JavaScript MIMEに加えてtext/plainも許可。本文はJavaScriptのESMとして扱う |
| CLI | URL importを既定で有効にする |
| Goへの組込み | Goアプリが明示的にURL対応loaderを選んだ場合に有効にする。既存FileLoaderの権限は増やさない |

「キャッシュなし」は通常のESM module registryをなくす意味ではない。同じruntimeで同じmodule IDをimportした場合は同じnamespaceを使い、評価は一度とする。既存のruntime-local source保持はこの意味の一貫性を保つために使う。別のrun_js・Worker・実行runtimeでは再取得する。この整理は対話で説明済みで、本書でも明示する。

Content-TypeはURL拡張子と別の規則として扱う。レビュー後の追加合意により、JavaScript MIMEとtext/plainを許可する（§7.2）。

## 3. 初回の対象と対象外

対象：

- ローカル入口のrun/eval、agent task/run_js、ローカル入口のmodule WorkerからのURL依存import。
- static import、export ... from / export * from、dynamic importで同じ解決規則。
- HTTP/HTTPSの絶対URL、リモートmodule内部の相対URLと依存グラフ。
- 既存import mapのURL targetと、同じ対応表のWorker・親子agentへの継承。
- source/moduleの同一性、取消し、エラー診断、能力の通知、公開Go API・利用文書の整合。

対象外：

- noe run https://...、URLを入口とするWorker起動、URLからimport map文書自体を取得する機能。
- npm/package.json/node_modules探索、CommonJS loader、TypeScript/JSX変換、ブラウザ/Node/Denoの全面互換。
- prefix mapping、scopes、URL/path keyのremap、integrity、alias連鎖などimport map全体の拡張。
- ダウンロードキャッシュ、条件付きGET、オフライン動作、lockfile、依存の自動更新。
- agentごとの新しい権限昇格API、親agentによるHost Profileの変更、ブラウザorigin単位の隔離。
- 外部コマンドで起動する任意の子プログラムへのloader設定注入。

URLの末尾が.tsであっても、本文をJavaScriptとして解析するだけで、TSの型構文は変換しない。ローカルファイルの.js/.mjs制限は維持する。

## 4. 現在の実装と変更する境界

現在のコードを根拠とし、旧計画の廃止済みREPL・子CLI Runner構造を実装前提にしない。

| 現在の場所 | 現状と変更案 |
| --- | --- |
| internal/modulemap/snapshot.go | 完全一致bare mapとローカル解決。HTTP/HTTPS targetとURL referrerを扱う純粋な解決へ拡張 |
| internal/modulemap/path.go・virtual.go | OS path・file URL・rooted filesystemの規則。remote referrerをpathとして扱わないよう分岐する |
| internal/modulemap/loader.go | runtime-local source保持と読取り。ファイル読取りとHTTP取得を分離し、URL IDをfilepath処理へ渡さない |
| stdlib/loader.go | FileLoaderは選択されたFSだけを使う契約。互換維持し、別の明示的URL対応factoryを追加 |
| internal/jsexec/execution_loader.go | 同期loader接続、Origin、virtual entry、Worker入口準備。HTTP contextと入口制限の整合を確認 |
| internal/jsexec/profile_options.go | FetchOptionsとProfile能力。fetchとURL importの有効化を暗黙に連動させない |
| internal/noeprofile/profile.go | CLI既定ProfileでURL対応loaderを選ぶ |
| internal/agentexec/executor.go・agent.HostConfig.Runtime | 同一HostのProfileを各実行へ適用。新しいruntimeごとに独立したloaderを作る |
| internal/agent/facts.md・internal/apidocs/content/noe-run-js.md | 利用可否と制限の説明を実設定と一致させる。全APIカタログを有効能力の証拠にしない |

## 5. 解決・module IDの提案

以下は合意を満たすための実装仕様であり、利用者に細かなURL規則の選択を求めない。

### 5.1 解決の順序

1. 登録された組込みmoduleの既存の優先順位を維持する。mapからshadowしない。
2. 完全一致bare mapがあれば、正規化済みのローカルpathまたはHTTP/HTTPS URL targetへ解決する。
3. HTTP/HTTPS絶対URLは、referrerがローカルでもremoteでもURLとして扱う。
4. remote referrerからの ./、../、/、// はURL参照として解決する。たとえば /dep.js は配信元originのpathであり、OS rootではない。
5. 明示的なfile URLはreferrerがローカルでもremoteでも既存のローカル解決へ渡す。実際の読取り権限は選択FSに従い、FSなしなら読めない。ローカルreferrerからのファイルpathは現行規則を維持する。
6. 未登録bare名はエラー。配信元の隣接ファイル探索やnpm探索へfallbackしない。その他schemeは未対応と診断する。

remote内のbare importは同じHostの完全一致mapを使える。CDNが返すESM中にbare dependencyが残る場合、必要なaliasか、依存をURL化したCDN出力を利用する。自動package探索は追加しない。

### 5.2 URLとID

- ローカルmodule IDは既存の字句上の絶対path。remote module IDは正規化した絶対HTTP/HTTPS URL。
- Go net/urlの階層URI解決規則を使い、scheme/hostを小文字化、既定port（http:80/https:443）を除去、空pathを/へ統一し、literal dot segmentを解決する。OSのfilepath.Clean/Joinへ流さない。pathの大小文字とescapeの意味を変えず、queryを削除・並べ替えない。全ブラウザのWHATWG URL互換やIDNA等価表記の全面統合は保証しない。
- queryが異なるURLは別moduleとして扱う。
- 非空fragmentはmodule IDの一部として保持する。HTTP要求のrequest targetへfragmentは送らない。同じ本文を取得してもfragment違いは別module評価。空fragmentはfragmentなしと同一IDにする。
- redirectは最終到達URLを、依存の相対解決基準とmodule IDに使う。元の要求URL→最終IDの対応をruntime内に保持し、元URL・最終URL・alias経由で同じmoduleに到達すれば同じnamespaceへ統合する。未知のredirect URLは最終IDを知るための通信が必要だが、既に保持した最終IDのsourceは置換しない。
- redirect Locationがfragmentを明示しない場合は直前URLのfragmentを引き継ぐ。Locationが#（空を含む）を明示した場合はその値に置換する。Go clientのredirect hookでこの規則と各hopのAllowを適用する。
- 取得済みのcanonical IDのsourceをruntime内で保持する。新しいreferrer/aliasからのloader再呼出しで再取得・再評価しない。
- 取得失敗は成功sourceとして保持しない。構文・評価失敗後の再importはengineのmodule semanticsに従い、独自hot reloadを設けない。

redirect先のURL IDが循環importと組み合わさる場合も、固定版go-quickjs v0.29.0のloader→module registryの順序をS2の小さなfixtureで確認する。namespace統合を未検証のまま保証済みとはしない。

### 5.3 import map

- keyの制約、組込み名の予約、重複key・型不正の起動時診断は維持する。
- targetの種類にHTTP/HTTPS絶対URLを追加する。相対targetは従来どおりローカルmap文書のディレクトリ基準であり、暗黙にremote化しない。
- 起動時にはURLの形式とschemeだけを検証し、未使用targetへのHTTP接続はしない。取得・MIME・構文の失敗は実際のimport時に報告する。
- map snapshotが固定するのは対応表であって、リモートの本文や依存グラフではない。実行中に配信内容が変われば別runtimeでは変化しうる。

## 6. Goへの組込みAPI

現行のstdlib.Profile.NewLoader、LoaderFactory、FileLoaderの契約を壊さない。能力metadataを安全に運ぶため、初回の仮案を具体化し、新しいProfile.Loaderへ構造化providerを設定する。NewLoaderは既存の関数型のまま残し、Loaderとの同時指定は設定エラーにする。

追加する型の契約：

```go
type ModuleLoaderProvider interface {
    NewLoader(context.Context, Origin) (quickjs.ModuleLoader, error)
    URLImports() URLImportSupport
}

type URLImportSupport string
// "enabled" / "disabled" / "unknown"。
// 有効は接続先ごとの許可やライブラリ互換性まで保証しない。
```

stdlib.ModuleLoader(LoaderOptions)はimmutableな標準providerを返す。Imports/HTTP option値をcopyし、clientとcallbackは借用する。Profile.Loaderもsnapshot中に借用providerを保持する。独自providerはホスト所有であり、返すmetadataの正確性はホスト責任。

### 6.1 URL import無効：既存コード

```go
profile := stdlib.Profile{
    Environment: stdlib.Environment{Cwd: cwd},
    Modules: stdlib.Modules{
        FS: &stdlib.FileSystemOptions{ReadOnly: true},
    },
    NewLoader: stdlib.FileLoader(nil),
}
```

FileLoaderはHTTP取得を行わず、HTTP/HTTPS指定は診断する。Modules.Fetchを有効にしてもFileLoaderの意味は変わらない。

### 6.2 URL import有効：追加API

```go
// 現在のソースの追加API。既存の配布版へ機能が遡って追加されるわけではない。
profile := stdlib.Profile{
    Environment: stdlib.Environment{Cwd: cwd},
    Modules: stdlib.Modules{
        FS: &stdlib.FileSystemOptions{ReadOnly: true},
    },
    Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{
        Imports: map[string]string{
            "app/math": "./lib/math.mjs",
            "lodash":   "https://esm.sh/lodash-es@4.17.21",
        },
        HTTP: &stdlib.ModuleHTTPOptions{
            Client: &http.Client{Timeout: 30 * time.Second},
        },
    }),
}
```

optionの責務：

- Imports：既存と同じ完全一致map。省略しても絶対URLの直接importは可能。
- HTTP：nilならURL import無効、非nilならHTTP/HTTPS取得を有効にする。
- HTTP.Client：借用するGo HTTP client。呼出し元のclientを変更・closeしない。
- HTTP.Allow：取得先を制限したいホスト用の func(*http.Request) error。初回要求と各redirect要求に適用する。
- HTTP.MaxBodyBytes：取得上限。既存Fetchと同じ0=32MiB、負数=上限なし。圧縮が展開された、実際にソースとして読む本文へ適用する。
- Client未指定時は既存Fetchと同じ30秒timeout。ホスト提供clientのTimeoutを勝手に書き換えず、実行contextによる取消しは常に接続する。
- Importsとoption値をfactory作成時にcopyし、runtimeごとにsource保持を新しくする。client/callbackは借用する。

HTTP取得とローカルFSは独立した能力である。URL対応loaderを選んでもFSが未選択ならローカルファイル読取りは許可しない。URL importが有効でもJSのfetch()は自動で有効にせず、逆も同じ。

### 6.3 agent Hostへの接続：既存API

```go
// ctx、models、profileは設定済み。
host, err := agent.NewHost(ctx, agent.HostConfig{
    Models:  models,
    Runtime: &profile,
})
if err != nil {
    return err
}
defer host.Close()
```

このHost配下のtask/run_js・親子agentはHostの実行Profileを使う。親agentのspawn引数や指示文でURL importを有効化するAPIは追加しない。Workerも親Profileのloader設定を受け継ぐが、runtime/source/module状態は共有しない。

### 6.4 URL入口を意図せず有効にしない

prepareWorkerは現在loaderを使って入口sourceを読むため、汎用loaderにHTTPを足すだけでWorker URL入口が偶然通る可能性がある。標準module loaderでは、入口の解決先をローカルpath/file URLに限定する。HTTP/HTTPSの直接指定に加え、URL targetを持つalias経由もURL入口として扱い、入口用のHTTP取得より前に拒否する。入口取得後のresolved ID検査だけでは接続を防げないため、入口の純粋な解決と読取りの間で判定できる構成にする。

現在のglobal Workerはbare文字列もloaderへ渡し、ローカルtargetのaliasで入口を読める経路があるため、そこは維持する。node:worker_threads.Workerは文字列に別のpath形式検査を持つので、その入力形式を新たに拡張しない。入口のconstructorごとの違いを今回統一しない。

この制限は入口の一回の読取りに適用する。同じWorkerの依存moduleはURL import可能であり、Origin.Workerだけを見てruntime内のHTTP全体を禁止してはならない。eval Workerはコード文字列の入口を維持する。ホスト独自のcustom LoaderFactoryが内部で行うI/Oまで返却後に検知・抑止できるとは保証せず、custom loaderは従来どおりホスト実装の責任とする。

CLI runの入口、Source.Name/Origin.Entry、EntryIDもローカル/virtual sourceの現行契約を保ち、URL依存のIDを入口正規化のfilepath処理へ流さない。

## 7. HTTP取得と配信形式

### 7.1 取得処理

- Go側でGETし、2xxの本文をJavaScriptのESMとして扱う。拡張子によるsource変換は行わない。
- 取得エラー、非成功status、MIME不一致、構文・評価エラーを区別して、specifier・referrerと失敗理由を既存診断へ渡す。
- HTTP clientのCheckRedirect等の判断を尊重し、許可callbackと組み合わせる際はclientをcopyして適用する。各redirect先でもHTTP/HTTPS schemeとAllowを確認する。
- clientがredirectを追わず3xxを返した場合、HTML等をmoduleとして黙って評価せず、未解決のHTTP応答として診断する。
- 実行contextを要求・本文読取りへ接続し、完了・エラー・取消しのいずれでもloaderが所有するresponse bodyをcloseする。借用client/transportのcloseはしない。
- 認証設定・API keyをNOEのprovider/authから自動転送しない。ホストのclientによるCookieJar/transport等はホストの選択とする。
- NOE独自の自動retry・fallback・ダウンロード保存・条件付きGETは入れない。ホスト提供client/transportが行うキャッシュ等はNOEの保証対象外。

### 7.2 Content-Type：JavaScript MIMEとtext/plainを許可（合意済み）

通常/批判的レビュー時点では未決だったが、その後、利用者が「JavaScriptのContent-Typeに加え、text/plainも許す」案を選択した。

- 公開ESM CDN等のJavaScript MIMEに加え、GitHub raw等のtext/plain配信も対象にする。
- どちらも本文をJavaScriptのESMとして解析・実行する。text/plainであることを理由にJavaScript以外の構文を変換しない。
- URL拡張子は不問。逆に、拡張子が.jsでもHTML/JSON等を返すURLを受け入れる理由にはしない。すべてのContent-Typeを無条件に受理する合意ではない。
- mime.ParseMediaTypeでContent-Typeを解析し、大小文字を区別せず、パラメータ付きのtext/plain; charset=utf-8等を扱う。
- JavaScript MIME集合はHTMLのJavaScript MIME essenceの集合（application/ecmascript、application/javascript、application/x-ecmascript、application/x-javascript、text/ecmascript、text/javascript、text/javascript1.0〜1.5、text/jscript、text/livescript、text/x-ecmascript、text/x-javascript）。これとtext/plainのみ受理する。
- Content-Type欠落・不正・その他のMIMEは診断する。拡張子による救済や本文sniffingは行わない。
- module sourceはUTF-8として読み、先頭のUTF-8 BOMを除去する。charset宣言による別encodingへの変換は行わず、不正なUTF-8は読込みエラー。TypeScript・JSON modules・Wasmへの自動切替はしない。

レビュー原文にあるMIME未決の記述は、レビュー時点の記録として保持する。現在の合意は本節と§2を正本とする。

## 8. 同期loaderと取消し

現在のquickjs.ModuleLoaderは同期コールバック。JSのfetch()を呼んで解決する設計にはしない。

runtime owner上のloaderからGo HTTP clientで取得し、source/IDを返す。同期取得中は同じruntimeのJS job/timerが進まない。別runtime/Workerは独立して進められる。

そのため、同じruntimeのJS setTimeoutやAbortControllerだけに取消しを頼らず、Go側の実行context、runtime.Cancel、Host/taskの取消しから通信を止める。要求だけでなく本文読取り中も取消せることを確認する。JS timer非進行は利用文書へ明記する。

非同期module loadingのためのengine改修は初回の前提にしない。ただし、実行contextの取消しがowner上の同期取得中に届かない実装経路であれば成立しないので、そこはfocused fixtureの確認対象とする。

## 9. agentへ利用可否を伝える

目標の責務分担：

- Goホスト/CLI：実際の能力を設定する。
- NOE：有効・無効・不明という実際の設定情報をagentへ伝える。
- 親agent/子agent：与えられた能力の範囲で、URL importをタスクへ使うか判断する。
- 利用者の指示文：使うライブラリ・禁止方針等のタスク判断を指定する。能力の有効化そのものには使わない。

通知例は「HTTP/HTTPS ESM import利用可。runtime間のダウンロードキャッシュなし。TypeScript変換・npm探索なし」。allow callbackがある場合、利用可は「全URLが許可される」ことを意味しない。

現在のfacts.mdは汎用契約、noe:docsは全APIカタログであり、選択Profileの能力一覧ではない。URL import対応時に一律「利用可能」と書いて組込み側にも流すことはしない。

**metadataの接続仕様**：Profile.Loaderの構造化providerからURLImports()を読み、HostがsnapshotしたProfileからcore.Optionsの実行能力factsへ接続する。Profile.NewLoaderのみの旧関数型はFileLoaderを含め推測せずunknown、両方nilならdisabled。標準ModuleLoaderはHTTP nilならdisabled、非nilならenabled。独自providerはenabled/disabled/unknownを明示でき、未知の値はunknownとして扱う。既存FileLoaderが実際にはHTTPを拒否する契約は変わらず、metadataのない関数の正体を推測しないという通知上の制約だけである。

公開helper（stdlib.URLImportCapability等）で同じ判定を一か所にまとめ、agent HostとCLIはその結果を使う。CLIはHTTP有効の標準providerを選ぶので自動的にenabledとなる。関数名・試験通信・docsから判定せず、手動でloaderと別のbooleanを二重管理させない。

通知のためだけにimport map全文や秘密のURL/HTTP header/client設定をモデルへ送らない。親子agentには同じHost能力の事実を伝える。毎回利用者が可否を指示文へ追記する運用を成立条件にしない。

## 10. 権限と互換性

- URLのコードは実行先runtimeへホストが与えた能力で動く。originごとの権限隔離やCORSによるsandboxではない。
- CLIでは起動ユーザーのOS権限を使える既存の実行モデルを維持する。信頼する配信元を使う必要があり、URL importをコードの安全性保証と説明しない。
- 同じURLでも配信内容は変わりうる。URL中のバージョン表記だけで内容の固定を保証しない。
- プロセス全体の依存graphをsnapshot化しないため、別runtimeだけでなく初回取得の時点が異なる依存同士でも配信更新の影響を受けうる。
- remote moduleから明示的にfile URLや組込みAPIへアクセスすることをoriginだけで新たに禁止する仕組みは作らない。実行ProfileのFS等の設定に従う。
- 既存のローカルpath/file URL、組込み優先順位、in-memory入口、virtual cwd、診断、Workerの未対応agent moduleなどを維持する。

## 11. 実装時の進め方と必要な確認

今回の追加指示により、[4スライスの計画](url-import-slices.md)を再レビューした後、以下を実装・テスト・コードレビューする。依存順はS1→S2→S3→S4。各スライスの対象テストとコードレビューを終えてから次へ進み、修正は元担当、再レビューは元レビュアーへ戻す。

### 段階A：設計の未決事項とengine境界を確定

本改訂でContent-Type判定、能力metadata経路、redirect/fragmentと正規化規則を具体化した。S1/S2でsource ID、循環、取消しを実装と同じ小さなfixtureで確認する。外部CDNの全面互換調査へ広げない。

### 段階B：resolver・HTTP loader・組込みAPI

HTTP/HTTPS targetとURL referrerを解決し、runtime-local source保持とGo HTTP取得を接続する。FileLoader互換、HTTP無効時、選択FS、redirect先Allow、未使用mapの非接続を確認する。

### 段階C：CLI・Worker・agent・説明

CLI既定設定、ローカル入口WorkerのURL依存、親子agentのrun_js、能力通知を接続する。公開Go consumerの利用例とdocsを更新し、利用者の実経路を通す。

確認は次の製品動作に対応させる。

| 必要な動作 | 対応する確認 |
| --- | --- |
| 公開ESMをURL/aliasから使う | ローカルHTTP fixtureの入口→相対依存→結果利用。static/re-export/dynamicを共通経路で確認 |
| CDNの拡張子なしURLを使う | 拡張子なし・別拡張子、query、origin絶対pathを含むfixtureでURL解決を確認 |
| テキスト配信のESMも使う | JavaScript MIMEとtext/plain（パラメータ付きを含む）のfixtureからexportを利用できることを確認 |
| 同じ依存を複数経路から使う | URL/alias/redirect先のnamespaceと副作用一回、循環を確認 |
| キャッシュなしで独立実行する | fixtureの本文を変更し、別run_js/runtimeでは新しい本文を取得。同一runtimeは保持 |
| Goホストが権限を選ぶ | FileLoaderのみではHTTP拒否、URL loaderでは許可、FS/Fetchの独立、Allowとredirectを確認 |
| taskを取り消せる | ヘッダー待ち・本文待ちのHTTP fixtureを取消し、実行が戻ることを確認 |
| Worker入口は従来のまま | ローカル入口内のURL import成功、直接URL/URL target aliasの入口指定はHTTP接続前に未対応診断。global Workerの既存local target alias経路は維持 |
| agentが能力を判断できる | URL有効/無効/不明の通知をfixture agentで確認。親子run_jsで同じ設定を使用 |
| 実際の公開ライブラリ利用 | 選定した公開ESMをNOEで一件、実際にimportしてexportを使う。通常テストの外部ネット依存にはしない |

実provider・有料モデルによる検証は製品動作の成立に不要。実装中は変更範囲に絞り、全体検査が必要なら実装・レビュー修正後の候補に対して親がまとめる。テスト件数や反復回数を目標にしない。

## 12. 残る判断と完了条件

ユーザー判断として残る利用方針は現時点ではない。レビュー後、JavaScript MIMEとtext/plainを許可する方針も合意済み。下記は実装側が詰める技術事項であり、利用者の追加判断待ちとはしない。

本改訂でAPI・MIME・URL ID規則を実装仕様として固定した。実装中に成立しない具体的な根拠が得られた場合だけ、影響する設計を修正し必要な範囲で再レビューする。同期HTTP中の取消しとmodule同一性はS2/S3の実行証拠を残す。

追加依頼の完了条件は、計画の通常/批判的再レビュー、4スライスそれぞれの実装・対象テスト・独立コードレビュー、最後の総合テスト、選定公開ESMの実利用、文書更新とコミットである。[実装記録](url-import-implementation.md)へ証拠とコミット範囲を残す。公開ESM一件の成立を任意のnpm互換保証とはしない。
