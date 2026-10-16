# NOE agent loopスパイク

既存NOEを子プロセスとして使い、JavaScriptのagent loop、Goでのモデル通信・認証、親子agentの実行が成立するかを確認する。製品本体への組込み前の独立したGo moduleであり、公開APIは未確定。

対象環境はLinux amd64、Go 1.27.2、Python 3。通常ビルドはCGO不要。race検査にはCツールチェーンを使う。

## 構成

```text
Goホスト（agent-loop）
 ├─ APIキー読込み・OpenAI Go SDK・OpenRouter SSE
 ├─ ローカルHTTPの呼出し窓口・認証付きサービスfixture
 └─ 既存NOEで定型JSを実行
      └─ 親agent.run()
           └─ run_js → 別のNOEプロセス
                ├─ 子agent left.run() → run_js → 別のNOE
                └─ 子agent right.run() → run_js → 別のNOE
```

GoホストはHTTP要求を並行処理する。親の`run_js`要求を待つ間も、子からのモデル・JS・サービス要求を処理する。JSの実行とPromise処理は、それぞれの既存NOEランタイムが担当する。

モデルに公開するtoolは`run_js({code})`のみ。各実行は新しいプロセスで、`stdout`・`stderr`・`exitCode`・`reason`を返す。`reason`は`completed`・`failed`・`cancelled`・`timeout`。JS変数は引き継がず、ファイルは指定した作業ディレクトリを共有する。

生成JSには`createAgent`と`webSearch`を事前にimportする。子agentも同じGoホスト・プロバイダ・モデルを利用し、各runは独立した履歴を持つ。agentとOSプロセスは一対一ではなく、同じ生成JS内で複数の子agentを`Promise.all`で動かせる。

## ビルド

リポジトリのルートから、既存NOEをビルドする。

```sh
CGO_ENABLED=0 go build -o /tmp/noe-agent-loop-noe ./cmd/noe
```

以下は`spikes/agent-loop/`から実行する。

```sh
CGO_ENABLED=0 go build -o /tmp/noe-agent-loop-spike .
CGO_ENABLED=0 go test ./... -count=1 -timeout 90s
CGO_ENABLED=1 go test -race ./... -count=1 -timeout 90s
go vet ./...
```

テストは現在の製品ソースからNOEを一時ビルドし、実際の子プロセスを起動する。外部モデルAPIには接続しない。race検査の対象はGoホストで、子NOEは通常ビルド。

## 実行

模擬モデルで試す。

```sh
/tmp/noe-agent-loop-spike --noe /tmp/noe-agent-loop-noe \
  --timeout 20s --max-steps 6 demo single

/tmp/noe-agent-loop-spike --noe /tmp/noe-agent-loop-noe \
  --timeout 20s --max-steps 6 demo delegate
```

`mock`は名前付きの検証シナリオに固定応答を返す。任意の指示を理解するモデルではない。

実モデルに接続する。

```sh
/tmp/noe-agent-loop-spike --noe /tmp/noe-agent-loop-noe \
  --provider openrouter --model deepseek/deepseek-v4.1-flash \
  --max-tokens 4096 --max-steps 6 --timeout 120s \
  --trace /tmp/noe-agent-live.jsonl demo delegate
```

Goホストは、既定で`~/.config/hjh/credentials/openrouter-api-key`の前後の空白を除いた内容をAPIキーとして読む。`--key-file`で変更できる。ファイルは読み取るだけで更新しない。子プロセスへAPIキーを渡さず、既知のプロバイダキー環境変数も引き継がない。子に渡すホスト呼出し用の一時トークンはプロバイダの認証情報とは別物。

NOEのファイル・ネットワーク・コマンド実行権限は既存のまま。これは認証処理の集約であり、同じOSユーザーで動くJSから認証ファイル自体を読めなくする隔離機構ではない。

`demo`は`single`・`recovery`・`delegate`・`service`から選ぶ。自分の定型スクリプトは`run script.js [args...]`で実行できる。相対importは通常のNOEと同じくソースファイル基準で、作業ディレクトリは`--cwd`で指定する。例えば、このディレクトリに次のJSを置いて利用できる。

```js
import { createAgent } from "./lib/agent.js";

const agent = createAgent({ name: "summary" });
console.log(await agent.run(
  `${process.argv[2]}を読んで要約して`,
  { cacheKey: "summary-v1" },
));
```

実行中の承認・追加入力・再開待ちはない。モデル通信の失敗や不完全な応答はエラーで終了する。JSの非ゼロ終了と不正なtool引数はtool結果としてモデルへ戻し、修正できるようにする。SDKの自動再試行は無効にしている。

`--timeout`はホスト全体、`--max-steps`は各agentのモデル呼出し回数、`--max-tokens`は各モデル要求の出力上限。いずれも未指定時は独自の上限を設けない。`agent.run()`の`maxSteps`でそのrunの回数を指定することもできる。ルートCLIの終了コードは成功0、失敗1、引数不正2、時間切れ124、SIGINT中断130。

## 認証・キャッシュ・状態

接続には`openai-go/v3 v3.78.0`を使用し、接続先をOpenRouterの`https://openrouter.ai/api/v1/`へ変更する。モデルはホストで指定したものを全agentに使い、別モデルへのフォールバックは指定しない。

`cacheKey`はOpenRouterの`session_id`へ渡す。これは接続先の選択を安定させるヒントで、会話保存・復元用ではない。必要な全履歴を毎回送信し、`reasoning_details`などの応答拡張も保持する。ストリームのtool引数とreasoning断片を組み立て、使用量・キャッシュ読取り量・報告された費用を記録する。

永続的な会話セッションは作らない。ホストは実行中のrun・親子関係・キャンセル・プロセスを一時的に管理する。親runの終了やホストの中断は子のモデル通信とホスト管理下のプロセスへ伝える。Linuxのプロセスグループを使って終了させる。

ChatGPTプランのOAuth登録・更新と、複数の実プロバイダへの対応はこの実装には含めていない。今回の実接続は、利用許可されたOpenRouterキーとDeepSeek V4.1 Flashを使う。

## 検証と記録

次のコマンドは4シナリオを順番に起動し、結果を確認して、標準出力・標準エラー・イベント記録・集計を新しいディレクトリへ保存する。`openrouter`を指定すると実APIを利用する。

```sh
python3 verify.py --host /tmp/noe-agent-loop-spike \
  --noe /tmp/noe-agent-loop-noe --provider mock \
  --output /tmp/noe-agent-mock-results

python3 verify.py --host /tmp/noe-agent-loop-spike \
  --noe /tmp/noe-agent-loop-noe --provider openrouter \
  --output /tmp/noe-agent-live-results
```

| シナリオ | 確認する結果 |
| --- | --- |
| single | 実ファイル`[10,20,30,40]`をJSで読み、count 4・total 100・average 25を返す |
| recovery | 存在しないファイルの読取りで子プロセスが失敗した後、別のJS実行で正しい集計を返す |
| delegate | 親と子2体が実行され、子それぞれがJSを実行し、30と70を親が100へ統合する。子の実行期間も重なる |
| service | 生成JSからGo経由で認証付きローカルfixtureを呼び、fixtureと明記された応答を受け取る |

最終回答は文字列であり、モデルはJSON指定に対してMarkdownや説明文を加えることがある。検証スクリプトは回答内のJSONオブジェクトを確認し、未加工の回答も保存する。厳密なJSON出力を保証するAPIではない。

`go test`では、二つの子のモデル要求が両方到着するまで応答を待つ検証により、ホストが入れ子の要求を並行処理できることを確認する。そのほか、履歴の分離、実行間のJS変数の分離、認証失敗、出力打切り、回数上限、時間切れ、中断後の子NOE・通信終了、SDKストリームの断片結合と途中切断を確認する。

イベント記録には実行識別子、親子関係、生成コードのハッシュ、終了理由、使用量を保存する。要求本文・生成コード本文・認証ヘッダーは保存しない。再現記録にはベースcommit、ソースと実行ファイルのSHA-256も含む。

今回の記録は[模擬モデル](results/mock/results.json)と[OpenRouter実接続](results/openrouter/results.json)を参照。これは当該実行の観測結果であり、将来のモデル応答の一致を保証しない。

## 参照

- [OpenRouterのOpenAI SDK接続](https://openrouter.ai/docs/guides/community/openai-sdk)
- [tool calling](https://openrouter.ai/docs/guides/features/tool-calling)
- [reasoning情報の引継ぎ](https://openrouter.ai/docs/guides/best-practices/reasoning-tokens)
- [session_idとキャッシュ利用量](https://openrouter.ai/docs/guides/best-practices/prompt-caching)
- [DeepSeek V4.1 Flash](https://openrouter.ai/deepseek/deepseek-v4.1-flash)
