# Provider設定とOpenCode Go追加

2026-10-17。利用者の追加指示：内部にデフォルトを持ち、外部設定で指定した項目だけ上書きする。OpenRouter SDK→OpenCode Goの[スパイク](opencode-go-sdk-spike.md)を土台にする。

実装・ローカル検証・OpenCode GoのChat/Responses実接続を完了。[実装・検証記録](provider-config-validation.md)と[設定ガイド](providers.md)を参照。

## 契約

- 組込みのJSONにprovider・API形式・base URL・path・認証ファイルの参照・ヘッダ・静的パラメータ・reasoning/effortの対応を定義する。
- 外部ファイルは`~/.config/noe/config.json`（XDG_CONFIG_HOME対応）、`NOE_CONFIG_FILE`で別pathを指定できる。ファイルなしは組込みだけで動く。objectは項目単位で再帰的に上書きし、配列・scalarは置換、nullはキー削除とする。
- 同一providerにモデル別の上書きを置ける。provider IDとmodelを別に受け取り、モデルに対応するAPI形式をGo側で選択する。独自のprovider IDも外部設定で定義できる。
- `run_agent`・`spawn_agent`のprovider/model/effort指定と子の継承は維持する。子の接続・認証は親Goホストで解決し、JSへキーを渡さない。
- 設定はホストが最初に利用するときに読み、そのホストでは固定。認証ファイルはモデル通信時に読み込む。通常JSやmoduleのimportだけでキーや外部設定を要求しない。
- OpenCode Goは`User-Agent: noe/<version>`、`x-opencode-session: runId`。同じrunの全要求で固定し、親・子・次のqueue taskは別ID。cacheKeyとは別の責務として扱う。
- 静的パラメータを土台に、model・messages/input・tools・stream・明示されたeffort/maxTokensを実行時に構築する。プロトコル変換とstream処理はGoの責務で、設定ファイルへ実行コードは置かない。
- 既存の`NOE_OPENROUTER_API_KEY_FILE`・`NOE_OPENROUTER_BASE_URL`とGoのfixture注入は維持。OpenCode Goのキーは既定のNOE用pathまたは`NOE_OPENCODE_GO_API_KEY_FILE`で参照する。今回の実接続では許可済みhjhのpathを明示する。

## 実装と確認

1. 組込みデフォルト＋外部上書きの読込みとprovider選択。
2. SDKのChat/Responses経路、provider別パラメータ、reasoning_contentの保持と観察。利用者の回答により、今回はスパイク済みのChat・Responsesに絞り、Messagesは含めない。
3. 親・子・queue・意思決定の既存経路へ接続し、設定と利用例を文書化。
4. ローカルfixtureで設定上書き、要求/応答・tool往復・reasoning再送・取消し・実際のrun IDとヘッダを確認。関連unit/E2E/vetとraceを実施。
5. 許可済みキーで小さなOpenCode Go実接続。既存OpenRouter経路の回帰も確認する。成功済み実要求を理由なく繰り返さない。

commit・push・releaseは今回の依頼範囲に含めない。
