# OpenAI SDKの実装・検証記録

## 目的と範囲

利用者の明示要件は、公式OpenAI Go SDKを追加し、APIキーとSign in with ChatGPTの両経路を提供すること。OpenAIではChatGPTサインインを優先し、Sol・LunaはResponsesを基本とする。ローカルOpenAI互換サーバーにもSDKを使える構造にする。Anthropic SDKは今回の対象外。

通常の利用経路は、JSの`run_agent`/`spawn_agent`→Go broker→固定済みprovider設定→SDK別adapter→認証→HTTP/SSE→既存agent loopである。生成JS・子agentも同じrootのprovider接続と資格情報を利用する。

認証登録は外部の`~/.config/noe/auth-chatgpt.js`からブラウザOAuthを行い、`noe:auth`のGo側がIDトークンを検証して保存する。配布元はリポジトリの`scripts/auth-chatgpt.js`。期限近くのトークン更新はGoが同一recordのfile lock内でread/refresh/writeする。

## 実装

- OpenAI Go SDK v3.79.0。既存OpenRouter SDK v0.9.59は維持。
- provider設定へ`sdk`・`auth`・`oauthFile`・`oauthFileEnv`を追加。省略時は既存のOpenRouter SDK/APIキー形式。
- `openai-chatgpt`・`openai-api`を組込み、両方ともResponsesと`gpt-6.1-sol`・`gpt-6-luna`を持つ。
- Chat/ResponsesのJSON変換、SSE記録・観察、native reasoning/tool履歴をSDK間で共用。
- SDKの暗黙の再試行を無効にし、明示されたNOEの接続先・資格情報を利用。
- credential recordはhjhと同形式。token値は観察イベントやヘルパーの出力へ載せない。
- 外部JSの初回登録・再ログイン・headless callbackを提供。IDトークンの署名検証はGo。

## 確認した外部契約と参照実装

- [OpenAI Go SDK](https://developers.openai.com/api/reference/go)
- [ChatGPT登録・サインイン](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)
- [ChatGPTのモデルと推論](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)
- [トークン更新](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions#refreshing-tokens)
- ローカルhjhの`internal/credentials/{storage,oauth,oidc}.go`、`internal/provider/protocol/{encode,stream,generate}.go`。参照のみでソースは変更していない。利用者の追加許可に従うcredential recordの同期は別途実施。

## ローカル検証

- APIキーと認証なしのChat接続、Responsesのtool往復・暗号化reasoning再送・summary観察・取消し・stream中のquota。
- 初回OAuth登録と再ログイン、PKCE、別ターミナルからのcallback受渡し、IDトークン署名・nonce検証、tokenを表示しない保存。
- 別credential sourceによる同一recordの同時更新を直列化し、置換されたrefresh tokenを保存。
- CLIからChatGPT経路の親子agentを起動し、認証・effort継承とrunごとのcache keyを確認。APIキー経路も同じCLIで確認。

## 実接続で見つかった差分

最初の2モデルの呼出しは、HTTP要求・認証・`response.completed`まで成功したが、agentは空回答として失敗した。追加の1回の診断で、実際のfunction callは`response.output_item.done`にあり、最後の`response.completed.output`は空であることを確認した。hjhのevent assemblyも参照し、完成済みitemsを集約する処理へ修正。同じ形式をローカル回帰テストへ追加した。

診断用の`spikes/openai/provider_live_test.go`は`NOE_OPENAI_LIVE_DIR`を明示したときだけ実APIへ接続し、認証ヘッダを保存せずSSEと正規化結果を指定directoryへ記録する。通常の`go test`では実接続しない。

## 最終の実接続結果

2026-10-17、利用者の許可を受け、既存hjh credential recordをNOEへコピーした。期限切れaccess tokenをGoホストで更新し、更新済みrecordをhjhにも同期した。OpenCode Go・OpenAI APIキーもNOEのcredentialsへコピーし、各ファイルのmode 0600を確認。ヘルパーは`~/.config/noe/auth-chatgpt.js`へ配置済み。

| モデル | effort | 結果 | model要求 / run_js | 所要時間 |
| --- | --- | --- | --- | --- |
| gpt-6-luna | auto | 437 | 2 / 1 | 4549ms |
| gpt-6.1-sol | auto | 437 | 2 / 1 | 5114ms |

両方とも通常の`noe run`・公開`run_agent`から実行し、exit 0、stderr空、最終状態completed。タスクはrun_jsで19×23を1回計算し、数値だけを返す内容。修正版の4 model要求に加え、修正前2要求と診断1要求の合計7要求を送信した。数値のAPI費用は返されていない。今回の単純なタスクではthinking summaryイベントは返らず、summary・暗号化reasoningの保持はローカルfixtureで確認した。

[機械可読の結果](openai-chatgpt-results.json)にrun IDとusageを記録した。raw artifactsは`/home/agent/.cache/noe-qjs-repl/noe-openai-live-ropouzp3`にある。ブラウザでの新規登録はfixture検証、実接続は既存credentialの更新・利用で確認した。OpenAI APIキー経路はローカルfixtureであり、従量課金APIへの実接続は行っていない。

## 総合検証

- `go test -tags=e2e ./...`成功。
- `go vet -tags=e2e ./...`成功。
- credentials・provider・CLI・runtimeの今回の変更経路に対するrace detector成功。
- CGOなしのLinux実行バイナリ、Windows amd64・macOS arm64向けビルド成功。後二つはクロスコンパイルで、各OS上の実行確認ではない。
- `git diff --check`、設定JSONとローカル文書リンクの確認成功。
