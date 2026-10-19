# 内蔵docsとagent初期説明の整備計画

2026-10-19。レビュー指摘を反映し、実装・検証へ進めます。結果は別の実装記録へ保存します。

## 目的と合意した方向

実行バイナリと同じソース・依存版に対応するAPI説明をバイナリへ含め、人間は`noe docs`、agentは`run_js`から必要な説明を取得できるようにします。ネット接続や外部instructionファイルの配置を前提にしません。

初期instructionsには、NOEとは何か、自分がNOEに管理されていること、`run_js`が毎回新しいNOEプロセスで動くことを短く記載します。現在の組込み説明は18,777 bytesで、bytes/4の概算では約4,700 tokensです。この大きさだけを理由に削減目標は設けず、参照先を整備した部分から詳細の移動を判断します。

本文は英文Markdown、topic名・種別・概要・本文の対応はGoのcatalogで保持する案です。CLIとJSは同じcatalogと本文を使います。利用ガイドと開発記録は既存の日本語資料に置きます。

## 参照インターフェース案

```sh
noe docs
noe docs stdlib
noe docs node:fs
noe docs node:child_process execFile
noe docs globals fetch
noe docs noe:sockets connect
noe docs noe:run_js
```

```js
import { docs } from "noe:docs";
console.log(docs());
console.log(docs("node:fs"));
console.log(docs("noe:sockets", "connect"));
```

- CLIは`noe docs [topic [member]]`。引数なしは概要付き一覧、topic指定は概要・利用可能なmember一覧、member指定は詳細を返します。`--help`はコマンド構文の説明です。
- JSは同期関数`docs(topic?, member?) -> string`。メモリ内の組込み本文を返し、Promise・外部I/O・keepaliveを必要としません。mainとWorkerの双方に登録します。
- `fs`と`node:fs`のような実在するstdlib別名は同じ本文へ解決します。正規表記は`node:`付きに揃えます。`noe:fs-handle`・`noe:sockets`・`noe:signals`は現在の公開名を使います。
- `noe:run_js`は参照topic名で、種別をagent toolと明記します。同名のimport可能moduleや追加のmodel toolは作りません。
- CLIの引数誤り・未登録topic/memberはstderrへ説明して終了コード2。JSの型違反はTypeError、未登録項目は`ERR_DOC_NOT_FOUND`を持つErrorとします。未収録memberを「ランタイムが未対応」とは説明しません。
- 本文はMarkdownをそのまま返します。CLIのみ末尾改行を補う場合を除き、CLIとJSで同じ内容にします。長い全マニュアルを引数なしで出力しません。

## stdlibの扱い

**stdlibも内蔵docsの対象です。** 基準は採用版go-quickjs v0.27.0の実装と、NOEの`stdlib.Config`、既存テストです。Node.jsの最新リファレンスをそのままNOEの仕様として扱いません。依存更新時には、この版情報・対応一覧・差分も更新します。

最初の実装で、対応moduleすべての一覧と概要を揃えます。各概要には提供機能、公開exportの一覧、主な制限、関連APIへの案内を含めます。個別memberの詳細があるものは、その一覧から分かるようにします。namespaceの予約と実装済みmoduleは区別します。例えば`node:`が予約されていても、任意のNode moduleを利用できるわけではありません。

現在のmodule一覧は次の26件です。実装時には`internal/modulemap/builtins.go`だけでなく、採用版の実登録とNOE設定を照合します。

```text
assert          async_hooks      buffer           child_process
crypto          dns              dns/promises     events
fs              fs/promises      http             os
path            path/posix       path/win32       process
querystring     stream/web       string_decoder   timers
timers/promises url              util             vm
worker_threads  zlib
```

global APIは別の`globals` topicにまとめ、NOEで有効なglobalの分類・一覧を用意します。ECMAScript組込み言語機能の全リファレンスは初回範囲に含めません。

初回の個別詳細は、実際のスクリプト作成と誤用防止に関係する次の範囲を優先します。

| 対象 | 初回に詳細を用意する範囲 |
| --- | --- |
| `node:fs`・`node:fs/promises` | readFile/writeFile/appendFile、同期版、createReadStream/createWriteStream。その他のexportはmodule概要から辿れる一覧に含める |
| `node:child_process` | execFile/execFileSync、exec、spawnSync。NOEのプロセスハンドルを使う場合への案内 |
| `node:worker_threads` | Worker、parentPort、workerData、メッセージ交換と終了、NOE側loader制約 |
| `node:process`・`node:path` | cwd/env/argv/exit、join/resolve/dirname/basename/extname、posix/win32の選択 |
| `node:stream/web`・globals | ReadableStream/WritableStreamとreader/writer、fetch/Request/Response、serve、WebSocket、AbortController/AbortSignal、timer |

特に次の差分・使い分けを各topicの該当箇所に含めます。

- `execFile`はPromiseを返すため、Nodeのcallback形式を前提に`promisify`しない。
- fsのread/write streamはWeb Streamsとして扱う。Node streamのイベント形式を前提にしない。
- 全体読み書き・逐次streamは`node:fs`、既存ファイルの位置指定I/Oは`noe:fs-handle`。どちらかを上位互換とは説明しない。
- HTTP・WebSocketと、TCP/Unixのバイト列を扱う`noe:sockets`を区別する。上流Configの`Sockets`はWebSockets設定である。
- main/Worker/agent toolから使う際の差分、プロセス終了・取消しとresourceの寿命を説明する。

上流の`docs/stdlib.md`は設定・設計の説明で、全memberのリファレンスではありません。本文は採用版sourceとtestsを根拠に作成し、実行例で契約を確かめます。上流の文章を取り込む場合は出典と必要な著作権・license表示を保持します。実行時の外部サイト参照は不要にします。

## NOE固有の説明

`noe`概要、`noe:docs`、公開の`noe:agent`、`noe:decision`・`noe:image`・`noe:auth`、`noe:process`・`noe:io`・`noe:fs-handle`・`noe:sockets`・`noe:signals`を収録します。公開exportと利用者が使う返却objectの操作を説明し、private moduleは公開一覧へ含めません。`noe:agent/api`と`noe:agent/hooks`はprivateのため除外し、hook登録・handler・渡される`api.agents/context/execution`は利用者向けの`noe:agent`の説明へ含めます。

各詳細には構文、引数と既定値、返却値、エラー、最小例を置きます。該当するAPIでは取消し・close・所有関係・対応OS・Worker制限も記載します。memberだけ参照しても重要な制約が分かる本文にします。

`noe:run_js`には入力schema、先頭コメント等のtool契約、stdout/stderr・exitCode・reason、出力省略、毎回独立する実行環境、import解決とresource寿命を記載します。toolとmoduleの違いを明示します。

## 配置と実装方針

- `internal/apidocs/`にGo catalogと検索処理、`internal/apidocs/content/`に英文Markdownを置き、`go:embed`で取り込みます。catalogに安定topic ID、kind、概要、memberと本文の対応を持たせます。JSON内に本文を埋め込む形式は採りません。
- catalogは初期化後不変とし、検索のためのprovider・認証・workspace指示の読込みを行いません。NOE版はCLIの既存Version、stdlib版は依存版と整合させ、版番号の別管理を増やしません。
- CLIの`docs`分岐はruntime生成・agent設定・観察の初期化より前に置きます。バイナリ単体を任意のcwdへ移しても参照できます。
- JS adapterはruntimeに登録し、WorkerのInstalled hookでも登録します。native I/O hostに文書機能の責務を加えません。Workerで利用できないAPIの説明も参照できます。
- 同じAPIの詳細を日本語ガイドと内蔵本文に手動で二重管理しません。ガイドは導入・利用手順・背景を扱い、契約の詳細には`noe docs`の参照コマンドを案内します。
- JSON出力、全文検索、多言語本文、Webサイト生成、型定義生成は今回の必須範囲に含めません。後から追加できるcatalog構造に留めます。

## 初期instructions

冒頭に次の趣旨を数行で加えます。英文は実装時に現在の名称・役割に合わせて整えます。

> You are an agent managed by NOE. Your run_js tool executes JavaScript in a fresh NOE process.
>
> NOE (No Ordinary Engine) is a lightweight JavaScript runtime and agent execution environment written in Go. It uses go-quickjs and provides supported Web and Node-style APIs, plus NOE-specific APIs for controlling agents and performing system I/O. NOE manages agent lifecycles, model requests, tool execution, and owned resources. Model inference is performed by the selected provider.

これにdocsの参照方法、利用可能な機能の短い一覧を加えます。`run_js`の入力・出力規則、実行の独立性など、最初の呼出し前から必要な規則は残します。API詳細の移動は、対応する内蔵本文と参照例ができてから行います。

親子agent・queue間のinstruction snapshotの既存契約を維持します。初期説明の前後のbytesと概算tokensを記録し、実際にdocsを調べて目的のAPIを使えることをfixture agentで確認します。

## 実装の順序と完了条件

| 段階 | 作業 | 完了条件 |
| --- | --- | --- |
| S1 | 採用版・公開module/export/global・NOE設定の棚卸し | source根拠と収録範囲が揃い、alias・未対応・詳細未収録を区別できる |
| S2 | catalog・Markdown・embedと検索 | topic/member単位で本文を取得でき、一覧が決定的。本文欠落とID重複を検出できる |
| S3 | CLIとJS adapter | 同一本文をCLI/main/Workerから取得でき、バイナリ単体で動作する |
| S4 | NOEとstdlib本文 | 上記の一覧・概要・初回詳細が揃い、例と重要な差分が現実の契約に合う |
| S5 | 初期instructions・利用ガイドの整理 | NOEの自己説明と参照経路が伝わり、詳細を移した後もfixture agentが利用できる |
| S6 | 統合確認と記録 | 以下の受入条件を確認し、実装・検証記録と候補の状態を更新する |

受入確認は、内容の正しさと実際の参照経路を中心に行います。

1. `noe docs`、module概要、member詳細、alias、未登録項目がCLIとJSで想定どおり動く。catalog一覧は実登録と照合し、予約されているだけのmoduleを対応済みと表示しない。
2. CLIの参照は無効なagent設定や認証のない環境でも成功し、外部ファイル・ネット接続なしで動く。`noe:docs`はmain/Workerで使え、import・呼出しだけで終了を妨げない。
3. `run_js`からdocsを取得し、その後のtool呼出しで説明どおりのAPIを使う一連の流れをローカルfixture providerで確認する。実providerへの有料要求は不要。
4. 誤用しやすい契約の実行例を代表として確認する。説明文全体を固定するsnapshotや単なるテスト件数の目標は設けない。
5. `go test ./...`、`go test -tags=e2e ./... -count=1`、`go vet -tags=e2e ./...`を実行する。初期instructions変更の影響を受ける親子・queue・context関連の既存テストも含める。対応6環境のCGO不要buildを確認する。
6. 実装・テストとコードレビュー後、実プロバイダへ通常の課題を与える総合確認を行う。課題本文にはdocsへの参照指示を含めず、組込みinstructionsから自発的に参照するか、参照した契約に従って課題を完了するかをtool記録と成果物で確認する。利用者はこの実接続を明示的に許可済み。参照しなかった場合も実測結果として記録し、成功と扱うために課題へ誘導を足さない。

REPL削除、Zed設定、MCP・デーモンのexampleは別の作業メモとして維持します。この計画にはそれらの実装、依存更新、release・公開を含めません。

## 根拠となる現行source・資料

- [CLI](../../internal/cli/cli.go)、[stdlib設定とWorker](../../internal/runtime/stdlib.go)、[stdlib module一覧](../../internal/modulemap/builtins.go)、[依存版](../../go.mod)
- [組込みinstructions](../../internal/agent/instructions.md)、[agentの構成](agent-architecture.md)、[instructionガイド](../instructions.md)
- [プロセス・標準I/O](../process-io.md)、[位置指定I/O・socket・signal](../low-level-io.md)、[APIガイド](../api.md)

本計画は会話で合意した内蔵説明・`noe docs`・NOE自己説明の方向を入力とし、JS APIの厳密な形と初回収録範囲を具体化したものです。実装結果や実施済みの検証記録ではありません。
