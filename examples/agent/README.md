# agentの起動・委譲・制御

[実行例一覧へ戻る](../README.md) · [認証・起動・観察の手順](../../docs/agents.md)

コマンドはリポジトリのルートで、ビルド済みの`./noe`を使って実行します。

| スクリプト | 内容 | provider |
| --- | --- | --- |
| [try-agent.js](try-agent.js) | 指示文またはファイルから任意タスクを実行 | 既定OpenRouter、起動引数で変更可 |
| [agent-summary.js](agent-summary.js) | ファイルを読んで3点に要約 | OpenRouter |
| [agent-parallel.js](agent-parallel.js) | 子agentを二つ起動して結果を集約 | OpenRouter |
| [agent-control.js](agent-control.js) | keepAliveで待受、別terminalから操作 | OpenRouter |
| [agent-hooks.js](agent-hooks.js) | 外部hook、context予算、共有上限 | OpenCode Go |
| [agent-openai.js](agent-openai.js) | Sol/LunaのResponses呼び出し | OpenAI ChatGPT |
| [agent-opencode-go.js](agent-opencode-go.js) | Chat / Responsesモデルを指定 | OpenCode Go |
| [agent-route-model.js](agent-route-model.js) | Jevがcatalogから選んだモデルを起動 | OpenRouter＋選択先 |

## 起動例とprovider

`./noe run --observe --events=agent.jsonl examples/agent/agent-hooks.js '任意のタスク'`は
OpenCode Goの資格情報を使い、[外部hook](hooks/context.js)を実行します。
例の予算と共有上限は選択例であり、NOEの既定値ではありません。
`contextPolicy`は既定で自動切り捨て、`null`なら通知のみです。例では既定の保持比率0.1を明示しています。
hookのAPI・継承・停止契約は[hookガイド](../../docs/hooks.md)を参照してください。

OpenAIは`./noe run --observe examples/agent/agent-openai.js gpt-6-luna`、または`gpt-6.1-sol`で実行します。先に[ChatGPT認証ヘルパー](../../docs/openai.md)で登録するか、既存のcredential recordを用意します。

`./noe run --observe examples/agent/agent-route-model.js openrouter 'README.mdを3点に要約して'`は、カタログ取得・Jevによるモデル選択・親agentの起動を組み合わせた例です。候補の説明は[外部JSONで上書き](../../docs/model-catalog.md)できます。

OpenCode Goは`NOE_OPENCODE_GO_API_KEY_FILE=/path/to/key ./noe run examples/agent/agent-opencode-go.js`で実行できます。第二のモデル例は`examples/agent/agent-opencode-go.js gpt-6-luna`です。[組込みデフォルトと外部上書き](../../docs/providers.md)も参照してください。

OpenRouterの例（`try-agent.js`・`agent-summary.js`・`agent-parallel.js`・`agent-control.js`）にはOpenRouter keyが必要です。`agent-openai.js`はChatGPT認証、`agent-opencode-go.js`と`agent-hooks.js`はOpenCode Go keyを使います。`agent-route-model.js`はOpenRouterのJevと選択先providerの認証が必要です。キーの値はscriptへ直接記述せず、credential fileを使います。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/agent/agent-summary.js README.md
```

`agent-summary.js`は既定で`README.md`を読み、簡潔な日本語の要約を標準出力へ表示します。別のファイルは引数で指定できます。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/agent/agent-parallel.js
```

並行例の既定入力は`examples/agent/data/left.json`と`examples/agent/data/right.json`です。必要なら二つのpathを引数で置き換えられます。モデル出力の文章や形式は変わる場合があります。`run_agent()`はinstance snapshotを返し、例は`snapshot.lastResult.answer`を表示します。並行例は`spawn_agent()`で子IDを受け取り、`observe_agent({agentId})`で終了までの状態を確認します。

ルートagentには`provider`・`model`・`effort`を明示指定します。子agentはprovider・model・effort・`maxTokens`・`hooks`・`contextBudget`・`contextPolicy`を省略時に起動元runから継承します。その他のconfig項目は継承しません。共有上限はrootで設定します。`NOE_OPENROUTER_API_KEY_FILE`を省略した場合の既定path、関数APIの設定・戻り値・エラー、keepAliveとqueue、任意の実行timeout、traceは[APIリファレンス](../../docs/api.md)を参照してください。agentと生成JavaScriptはNOEと同じOS権限で動きます。

任意のタスクを試す場合は、`examples/agent/try-agent.js`へ指示を渡します。次の例はREADMEを読み、要約ファイルを作らせます。

```sh
NOE_AGENT_TRACE=1 NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/agent/try-agent.js "README.mdを読み、要点をSUMMARY.mdへ保存してください。" \
  >answer.txt 2>agent-trace.log
```

`tail -f agent-trace.log`で実行状態を確認できます。traceは状態・使用量などのメタデータで、生成JS本文やtoolの出力を含みません。標準エラーには通常の診断も入りえます。

UTF-8ファイルの指示を渡し、モデルを指定することもできます。次は[agentサーバー作成の指示](../agent-server/build-task.md)をChatGPT経由のSolへ渡します。

```sh
./noe run --observe --events=agent-server-build.jsonl examples/agent/try-agent.js \
  --provider openai-chatgpt --model gpt-6.1-sol --effort high \
  --task-file examples/agent-server/build-task.md
```

`--task-file`の相対パスは起動時の作業ディレクトリが基準です。省略時は従来どおり引数を指示文として使い、provider・model・effortの既定値はOpenRouter・DeepSeek V4.1 Flash・maxです。

## Live agent control

この例は最初のtaskを45秒間実行し、その後も`keepAlive`でidleになります。OpenRouter keyを設定して一つ目のterminalで起動します。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run --observe examples/agent/agent-control.js
```

二つ目のterminalからactive instanceのIDを調べます。

```sh
./noe agents
```

最初の45秒以内に`RUN ID`を指定してsteerすると、実行中のtimerは続き、指示はその後のモデル判断で適用されます。taskがidleになった後はsteerを受け付けないため、別taskは`queue`で追加します。

```sh
./noe steer <run-id> '回答にこの文を含めて: control message received'
./noe queue <agent-id> 'README.mdの目的を一文で説明してください'
```

queueで始まるtaskは新しいrun IDを持ちますが、同じagentが前taskで作った会話と最終回答を引き継ぎます。予約した指示はtask開始時にprotected user messageとして追加され、trim済みの古い履歴は戻りません。system instructionはagentの最初のtaskで読み込み、その後のqueueでは再読込みしません。各taskのruntime・hook module・`maxSteps`・task interrupt scopeは新しくなり、trim countはagent内で続きます。

`./noe interrupt --run <run-id>`はそのtaskだけを止め、instanceをidleにします。実行を完全に終了するには`agentId`を指定します。

```sh
./noe interrupt --agent <agent-id>
```

操作CLIは受付結果を返し、task完了や回収を待ちません。`--observe`のinstance/task表示やJSONLから実際の進行を確認できます。別の利用例・API引数は[APIリファレンス](../../docs/api.md#実行中taskへの指示と停止)と[観察ガイド](../../docs/observe.md)を参照してください。
