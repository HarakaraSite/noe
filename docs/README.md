# NOE 開発記録

[READMEへ戻る](../README.md) · [APIリファレンス](api.md) · [REPLガイド](repl.md) · [実行例](../examples/README.md)

現在の利用方法はREADMEと各ガイドを参照してください。以下は設計・受入条件・実装と検証の記録です。[MVP案](mvp.md)の対象外は初期段階の範囲で、その後の追加機能は各インクリメントに記録しています。

## 現在のAPIと移行

[OpenAIガイド](openai.md)では、公式SDKのResponses・ChatGPTサインイン・APIキー・外部JS認証ヘルパー・ローカル互換接続を説明しています。[実装・検証記録](openai-implementation.md)も参照してください。

[モデルカタログ](model-catalog.md)では、JSONによる候補の管理・外部上書き・`list_models`・Jevによる起動前選択を説明しています。

[provider設定](providers.md)では、組込みデフォルトと外部JSONの上書き、OpenRouter・OpenCode Go、モデルごとのChat/Responses経路を説明しています。

実行中の観察は[観察の入口](observe.md)と[JSONLイベントのガイド](events.md)を参照してください。`run`・`eval`・`repl`共通の`--events`で記録し、人間はGoの`noe observe`でinstance状態とtaskを読みやすく表示、親はJSの`observe_agent`でlive snapshotまたは保存ログの状態・差分・結果を取得できます。

JavaScriptの`noe`グローバルは廃止済みです。`fs/promises`・`process`・`timers/promises`とグローバルの`fetch`・`serve`などは、go-quickjs標準ライブラリへ統一しました。NOE固有の機能としてCLI・REPLの操作と、インクリメント13で追加した`noe:agent`モジュールがあります。[移行表](api.md#旧apiからの移行)と[インクリメント12の記録](increment-12.md)、[agent API](api.md)を参照してください。

agentの共通インストラクションは[`internal/agent/instructions.md`](../internal/agent/instructions.md)で管理します。Goの`embed`でバイナリに含め、モデルのtask loopを実行するprivate moduleが利用します。公開`noe:agent` moduleは起動・観察・queue・steer・interruptのAPIを提供します。文面の変更を実行へ反映するには再ビルドします。

利用者の追加指示は共通設定とworkspaceの`instructions/*.md`からrunごとに読み込みます。
配置・合成・`system`との併用は[インストラクションガイド](instructions.md)、
確認結果は[実装・検証記録](instructions-implementation.md)を参照してください。

以下のインクリメント1〜11、レビュー、比較スパイクは当時の設計・検証履歴です。そこに記載された旧API・対象外・ソース構成・中断動作は、現在の仕様や最新コードの検証結果を示すものではありません。

## インクリメント

| 計画・記録 | テーマ |
| --- | --- |
| [1](increment-1.md) | CLI、JavaScript実行、ローカルモジュール、ファイルとHTTP |
| [2](increment-2.md) | REPL、状態保持、エラー回復、非同期入力 |
| [3](increment-3.md) | 行編集ライブラリの選定と導入 |
| [4](increment-4.md) | 履歴、前回結果、Tab補完、複数行入力 |
| [5](increment-5.md) | 標準入力、ディレクトリ一覧、ファイル情報 |
| [6](increment-6.md) | ディレクトリ作成、名前変更、削除 |
| [7](increment-7.md) | 実行時間の上限 |
| [8](increment-8.md) | HTTPメソッドと文字列body |
| [9](increment-9.md) | バイナリ入出力 |
| [10](increment-10.md) | 共通イベントループと簡易HTTPサーバー |
| [11](increment-11.md) | 旧sleep API（履歴） |
| [12](increment-12.md) | stdlibへの統一、旧`noe.*` APIと独自I/O実装の削除 |
| [13](increment-13.md) | OpenRouter agent loop、子JS実行、親子agent、Go側の認証・通信（[実装・検証記録](increment-13-implementation.md)、[計画レビュー](increment-13-review.md)） |

現在の入力UIは[qjs固定版からの取り込みと保守手順](qjs-repl-integration.md)を参照してください。過去の選定記録として[比較スパイク](repl-lineedit-spike.md)と[インクリメント3・4の計画レビュー](increment-3-4-review.md)もあります。

## 今後の候補

[ローカルESMとimport mapの計画](local-import-map-plan.md)では、動作確認済みのJSを通常スクリプト・Worker・agent生成JSから同じ名前で再利用する構成を扱います。HTTP importや永続cacheは含めず、MCP SDKとNOE向けtransportを最初の応用例にします。実装の進捗は[検証記録](local-import-map-implementation.md)、利用方法は[import mapガイド](import-map.md)を参照してください。

[汎用プロセスAPIと標準I/Oストリームの計画](process-io-plan.md)に従って、第1グループのNOE拡張を実装しました。[利用ガイド](process-io.md)と[実装・検証記録](process-io-implementation.md)を参照してください。

[意思決定API](decision.md)の実装・テスト・コードレビューを完了しました。`noe:decision`の`decide`を通常agentと別の入口とし、既存OpenRouter SDK・認証・要求管理を共用します。[実装記録](decision-implementation.md)と[3モデルの実接続確認](decision-openrouter-validation.md)に結果を記録しています。

[起動と待受](agent-lifecycle-plan.md)の実装・検証・コードレビューが完了しました。結果とレビュー指摘の採否は[実装・検証記録](agent-lifecycle-implementation.md)を参照してください。公開APIは破壊的に変更し、互換aliasはありません。steer・明示的なinterrupt・人間用操作CLIも[実行中の操作計画](agent-control-plan.md)に沿って実装・総合検証・レビューを完了しました（[実装・検証記録](agent-control-implementation.md)、[OpenRouter実接続確認](agent-control-openrouter-validation.md)）。

[次の機能候補](future-candidates.md)には、Web検索の認証ラッパー、Workerからのagent利用、呼び出し元終了後も続く独立agent、実行コンテキスト圧縮、Jevなどの意思決定モデル、REPLとqjsの比較を記録しています。ライフサイクル作業で扱う親子agentの起動・観察・queue、推論エフォート、ログ観察の進捗は[実装・検証記録](agent-lifecycle-implementation.md)にあります。control操作の仕様と完了結果は[実行中の操作計画](agent-control-plan.md)と[実装・検証記録](agent-control-implementation.md)を参照してください。候補文書には設計経緯も残しています。v1.0.0時点の候補として、import mapはローカルの完全一致名に対応し、HTTP URLからのimportは未対応です。標準ライブラリ導入後、ストリーミング、暗号・ハッシュ、外部コマンドは利用できるようになりました。

## ビルドと検証

[依存ライブラリの互換性確認と追随](dependency-updates.md)に、go-intl・SDK・JSON関連・OS/端末関連の更新判断、性能測定、適用後の検証を記録しています。

[OpenRouter SDK → OpenCode Goの接続スパイク](opencode-go-sdk-spike.md)では、既存SDKでChat・Responsesの実接続を確認しています。本体にはChat・Responses対応を追加済みです。[provider設定](providers.md)と[本体の検証記録](provider-config-validation.md)を参照してください。

Go 1.27.2以降を使います。現在の検証環境はLinux amd64です。通常のビルドはCGO不要で、race detectorの実行にはCツールチェーンが必要です。以下はリポジトリのルートで実行します。

```sh
CGO_ENABLED=0 go build -o noe ./cmd/noe
go test ./...
go test -tags=e2e ./... -count=1 -v
CGO_ENABLED=1 go test -race ./internal/... -count=1
go vet -tags=e2e ./...
```

[E2Eの構成と実行方法](../tests/README.md)も参照してください。E2Eは実際のCLIを別プロセスから起動し、出力・終了コード・ファイルの内容・HTTP応答を確認します。通常のE2Eは履歴とファイルに一時ディレクトリ、HTTPにローカルサーバーを使い、外部サービスに接続しません。OpenRouterへの実接続は、[インクリメント13の記録](increment-13-implementation.md)に従い、別途`agent_live`タグと認証設定を明示して実行します。LinuxのPTYテストは編集、日本語、履歴、複数行、補完、Ctrl+C中断、終了後の端末設定の復元を確認します。

過去の検証結果は各インクリメントに、stdlib移行時のローカル検証は[インクリメント12](increment-12.md#検証記録)に記録しています。新しい変更では、その時点のソースに対して上のコマンドを実行してください。

## リリース

[リリースプロファイル](../.forgejo/release-profile.yml)に配布対象とビルド・品質確認の契約を記載し、[リリースワークフロー](../.forgejo/workflows/release.yml)で`v*`タグのpush時にビルド・公開します。通常テストとvetはForgejoの配布ゲート、raceとPTYを含むE2EはVM201でのリリース前確認です。手順と確認記録は[リリースチェックポイント](release-checkpoints.md)を参照してください。

## 参照と方針

[txiki.js](https://txikijs.org/)の小さなランタイムという方向を参考にし、エンジンには[go-quickjs](https://github.com/go-quickjs/go-quickjs)を使います。汎用JavaScript APIは固定したgo-quickjs標準ライブラリを通じて提供します。NOE固有の機能はCLI、実行期限、REPLの操作、`noe:agent`モジュールなどです。

参照資料・ソースを置く`_refs/`はローカル専用の未追跡ディレクトリです。ビルド・テストの依存には含めません。

npm・パッケージ管理とNode.js/Denoの完全互換は対象外です。Web APIとNode形式APIの互換範囲は固定したgo-quickjs標準ライブラリに従います。現在はJavaScriptを対象としています。
