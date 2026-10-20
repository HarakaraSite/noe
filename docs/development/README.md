# NOE 開発ガイドと記録

[ドキュメント一覧](../README.md) · [README](../../README.md) · [初期開発の履歴](../archive/README.md) · [リリース](../releases/README.md)

## ビルドと検証

Go 1.27.2以降を使います。現在の検証環境はLinux amd64です。通常のビルドはCGO不要で、race detectorの実行にはCツールチェーンが必要です。以下はリポジトリのルートで実行します。

```sh
CGO_ENABLED=0 go build -o noe ./cmd/noe
go test ./...
go test -tags=e2e ./... -count=1 -v
CGO_ENABLED=1 go test -race ./internal/... ./stdlib/... -count=1
go vet -tags=e2e ./...

# 6配布対象のcompile確認（リンク・配布物の生成は行わない）
(
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  GOOS=${target%/*} GOARCH=${target#*/} CGO_ENABLED=0 go list -export ./... >/dev/null || exit
done
)
```

配布対象6種の確認は、ローカルでは`go list -export ./...`で行います。各packageと依存のコンパイル結果をGoのビルドキャッシュへ用意し、main packageもコンパイルしますが、リンクや配布バイナリの生成は行いません。`go build ./...`は結果を保存しなくてもmain packageを一時領域でリンクするため、この目的には使いません。ループは最初の失敗で停止して非ゼロの終了コードを返します。サブシェルで実行するため、端末へ貼り付けても対話シェル自体は終了しません。これはプラットフォーム固有のコンパイルエラーをタグ前に検出する確認であり、リンクと配布物生成はタグ起点の[ワークフロー](../../.forgejo/workflows/release.yml)で行います。

一時ディレクトリが小さい環境では、Go自身の一時ディレクトリを空きのある場所へ向けます（例：`mkdir -p /var/tmp/noe-gotmp`を実行してから`go env -w GOTMPDIR=/var/tmp/noe-gotmp`）。上の確認もコンパイル用の一時領域とビルドキャッシュを使います。通常ビルドやテストで行うリンクには最終バイナリ以上の一時領域が必要で、リンク時のENOSPCはソースの失敗に見えます。

[E2Eの構成と実行方法](../../tests/README.md)を参照してください。通常のE2Eは一時ディレクトリとローカルサーバーを使い、外部サービスに接続しません。Linux固有のprocess groupやOSシグナルのテストは対象OSで実行します。

NOEの`run_js`からテストを実行するときも、外側の`NOE_AGENT_CONNECTION`を手で外す必要はありません。CLI単体テストとE2Eの`TestMain`が、テストスイートを開始する際にこの変数だけを削除します（空文字への上書きではなくunset）。CLI単体テストのmanaged child再実行は削除より先にdispatchするため、テスト内のbrokerが発行した親子接続は保持されます。製品の環境継承・接続処理は変更しません。`TestCLITestMainIsolatesInheritedAgentConnection`は空値・不正値・ローカルのダミー接続を渡してスイートを再実行し、外側への要求がなく、ローカルproviderと子agentへの委譲が動くことを確認します。

実プロバイダの検証は別の明示的な設定・認証を必要とします。過去の[OpenRouterの検証](../archive/increment-13-implementation.md)や[OpenCode Goの検証](execution-hooks-opencode-validation.md)は実施時点の記録であり、新しい変更の確認はそのソースに対して行います。

## エラー無視（`_ =`）の方針

原則としてエラーは伝播させ、複数のエラーは`errors.Join`で集約します。無視してよいのは次の3つに限ります。

- 処理結果に影響しない冪等な後始末としての`Close()`（`os.ErrClosed`は正規化して無視する）。書き込みの確定・flushなど、成功可否に影響する`Close()`のエラーは伝播させる
- 失敗しても後続の結果に影響しないベストエフォートの処理
- 直後に`os.Exit`する経路

無視する場合は、同じ行に理由をコメントで残します。機械的なlintゲートは設けず、上記3条件をコードレビューで判定します。CIの品質ゲート（`go test`・`go vet`）は現状のままです。

## 実装を変更するときの入口

| 対象 | 文書 |
| --- | --- |
| 採用した小規模改善5件の範囲・実装・検証 | [対応記録](small-improvements-20261020.md) |
| agent内部の責務・JSとGoの分担・所有関係・同期 | [agentの構成](agent-architecture.md) |
| agentへ渡す組込みの説明 | [instructions.md](../../internal/agent/instructions.md) |
| 利用者が追加する共通・workspace指示 | [インストラクションガイド](../instructions.md) |
| 依存の互換性確認と追随 | [依存更新の記録](dependency-updates.md) |
| 配布対象・品質確認・公開手順 | [リリースチェックポイント](../releases/release-checkpoints.md) |

組込みinstructionはGoのembedでバイナリへ含めるため、変更の反映には再ビルドします。公開`noe:agent`は起動・観察・操作を提供し、モデルのtask loopはprivate moduleで実行します。

## 機能別の計画と検証記録

各文書は実施時点の設計・受入条件・結果を記録しています。後続の変更で契約やファイル構成が変わった場合、現在の利用方法は[利用ガイド](../README.md)を参照してください。初期agent loopとstdlib移行以前の記録は[archive](../archive/README.md)にあります。

| テーマ | 計画・調査 | 実装・検証 |
| --- | --- | --- |
| agentの起動・待受・queue | [計画](agent-lifecycle-plan.md) | [実装記録](agent-lifecycle-implementation.md) |
| steer・interrupt・人間向け操作 | [計画](agent-control-plan.md) | [実装記録](agent-control-implementation.md)・[OpenRouter検証](agent-control-openrouter-validation.md) |
| provider設定とOpenCode Go | [計画](provider-config-plan.md)・[SDK接続スパイク](opencode-go-sdk-spike.md) | [検証記録](provider-config-validation.md) |
| OpenAI SDK・ChatGPT認証 | [利用ガイド](../openai.md) | [実装記録](openai-implementation.md) |
| 意思決定API | [計画](decision-plan.md) | [実装記録](decision-implementation.md)・[実接続](decision-openrouter-validation.md) |
| プロセス・標準I/Oストリーム | [計画](process-io-plan.md) | [実装記録](process-io-implementation.md) |
| 第2グループ：file・socket・signal | [実装計画](low-level-group2-plan.md)・[計画レビュー](low-level-group2-review.md) | [実装・検証記録](low-level-group2-implementation.md)・[計画確定前スパイク](low-level-group2-spike.md) |
| ローカルESM・import map | [計画](local-import-map-plan.md) | [実装記録](local-import-map-implementation.md)・[MCPクライアントのスパイク](../../scripts/spikes/process-io-mcp/README.md) |
| hook・処理の連鎖・コンテキスト通知 | [計画](execution-hooks-plan.md) | [実装記録](execution-hooks-implementation.md)・[OpenCode Go検証](execution-hooks-opencode-validation.md) |
| 共通・workspace instructions | [利用ガイド](../instructions.md) | [実装記録](instructions-implementation.md) |
| 内蔵docs・stdlib参照・agent初期説明 | [計画](builtin-docs-plan.md) | [実装・検証記録](builtin-docs-implementation.md) |
| NOE版stdlib・ステップ1の取り込み | [計画](stdlib-step1-import-plan.md)・[計画/コードレビュー](stdlib-step1-import-review.md) | [実装・検証記録](stdlib-step1-import-implementation.md) |
| NOE版stdlib・ステップ2のruntime切替 | [計画](stdlib-step2-runtime-plan.md)・[計画/コードレビュー](stdlib-step2-runtime-review.md) | [実装・検証記録](stdlib-step2-runtime-implementation.md) |
| NOE版stdlib・ステップ3のnoe:http入口 | [計画](stdlib-step3-http-plan.md)・[計画/コードレビュー](stdlib-step3-http-review.md) | [実装・検証記録](stdlib-step3-http-implementation.md) |
| NOE版stdlib・ステップ4の要求送信streaming | [計画](stdlib-step4-upload-plan.md)・[計画/コードレビュー](stdlib-step4-upload-review.md) | [実装・検証記録](stdlib-step4-upload-implementation.md) |
| NOE版stdlib・ステップ5の要求受信streaming | [計画](stdlib-step5-receive-plan.md)・[計画/コードレビュー](stdlib-step5-receive-review.md) | [実装・検証記録](stdlib-step5-receive-implementation.md) |
| NOE版stdlib・ステップ6のAPI導入と所有関係 | [計画](stdlib-step6-composition-plan.md)・[計画/コードレビュー](stdlib-step6-composition-review.md) | [実装・検証記録](stdlib-step6-composition-implementation.md) |
| NOE版stdlib・ステップ7の外部Go提供 | [計画](stdlib-step7-library-plan.md)・[計画/コードレビュー](stdlib-step7-library-review.md) | [実装・検証記録](stdlib-step7-library-implementation.md)（7.1完了、agentは次行の7.2） |
| NOE版stdlib・ステップ7.2のagent外部接続 | [計画](stdlib-step72-agent-plan.md)・[計画/コードレビュー](stdlib-step72-agent-review.md) | [実装・検証記録](stdlib-step72-agent-implementation.md)（完了・v2未公開） |
| REPL削除 | [計画](repl-removal-plan.md) | [実装・検証記録](repl-removal-implementation.md) |
| 自動コンテキスト切り捨て | [計画](context-trimming-plan.md) | [実装記録](context-trimming-implementation.md) |
| queueの会話継続 | [計画](queue-continuation-plan.md) | [実装記録](queue-continuation-implementation.md) |
| Vision入力・画像生成 | [計画](agent-images-plan.md)・[スパイク](agent-images-spike.md) | [実装記録](agent-images-implementation.md)・[実接続](agent-images-integration.md) |
| agentの責務整理・捕捉出力上限 | [計画](agent-refactor-plan.md) | [実装記録](agent-refactor-implementation.md) |

## 構造化された検証結果

元の計画・実装記録と一緒に保存します。実行時に参照する設定ファイルではありません。

[NOE版stdlib・ステップ1の検証結果](stdlib-step1-import-results.json)には、取り込み範囲・原本比較・単独/race/総合テスト・cross-compile・production依存境界とレビュー対象の記録があります。

[ステップ2の検証結果](stdlib-step2-runtime-results.json)には、runtime切替、4モードのAPI比較、全体/race/E2E、6対象のビルドと統合test cross-compile、実プロバイダでの基本フロー、依存境界とコードレビューの記録があります。

[ステップ3の検証結果](stdlib-step3-http-results.json)には、noe:http登録・設定・各実行mode・英文docs、関連/総合テスト、6対象のbuild/compile、コードレビューと再レビュー、実プロバイダでのHTTP往復を保存しています。

[ステップ4の検証結果](stdlib-step4-upload-results.json)には、要求送信streaming・Request所有権・取消し/早期応答、4mode比較、関連/全体/race/E2E・6対象compile、コードレビュー修正、実プロバイダの初回失敗と再試成功を保存しています。

[ステップ5の検証結果](stdlib-step5-receive-results.json)には、要求受信streaming・需要計数・早期応答/Expect/終了回収、4mode比較、全体/race/E2E・6対象compile、コードレビュー修正、環境上の復旧と実プロバイダ基本フローを保存しています。

[ステップ6の検証結果](stdlib-step6-composition-results.json)には、汎用API組立・部分構成・設定/出力/Workerの所有、DNS子Loop、writer比較のレビュー修正、全体/race/E2E・6対象compile、実プロバイダ初回失敗と限定再試を保存しています。

[ステップ7.1の検証結果](stdlib-step7-library-results.json)には、公開Go API・正規module移行、外部consumer/local artifact・6対象compile、JS inventory維持、独立コードレビュー、全体unit/race/E2E/vetと実プロバイダ基本フローを保存しています。agent接続は次の7.2で実装しました。v2公開は後続です。

[ステップ7.2の検証結果](stdlib-step72-agent-results.json)には、公開agent API・共通bridge、外部consumer/local artifact・6対象compile、JS inventory維持、独立コードレビューと限定再レビュー、全体unit/race/E2E/vetと実プロバイダ基本フローを保存しています。7.1/7.2の実装・検証は完了し、常用確認とv2公開は後続です。

| 対象 | 結果 |
| --- | --- |
| agent操作 | [OpenRouter](agent-control-openrouter-results.json) |
| provider設定 | [OpenRouter SDK](provider-config-openrouter-sdk-results.json)・[OpenCode Goスパイク](opencode-go-sdk-spike-results.json) |
| OpenAI | [ChatGPT](openai-chatgpt-results.json) |
| 意思決定 | [OpenRouter](decision-openrouter-results.json) |
| コンテキスト切り捨て | [実プロバイダの挙動](context-trimming-live-results.json) |
| 内蔵docs | [実プロバイダの自発参照](builtin-docs-live-results.json) |

## 配置と製品方針

利用ガイドは`docs/`、機能別の計画・レビュー・実装と検証はこのディレクトリ、初期の旧API・インクリメントは`docs/archive/`、リリース記録は`docs/releases/`へ置きます。残る候補と完了区分は[次の機能候補](../future-candidates.md)を更新します。

[txiki.js](https://txikijs.org/)の小さなランタイムという方向を参考にし、エンジンには固定した[go-quickjs](https://github.com/go-quickjs/go-quickjs)、汎用JavaScript APIには同版のstdlibを原型とするNOE管理の`internal/stdlib`を使います。`internal/noestdlib`がstdlib・nativeio・docsの導入と共有出力を組み立て、`internal/runtime`がCLI・agent固有の設定と実行制御を接続します。[Goホストの利用例](../../examples/embedding/README.md)は公開`stdlib` packageを使い、[agentの組み込み例](../../examples/agent-embedding/README.md)は公開`agent` packageを追加します。Go向けの入口は同じ`/v2` moduleに実装済みですが、v2タグは未公開です。外部moduleで試す場合は[stdlib](../go-embedding.md)・[agent](../go-agent-embedding.md)のガイドに従ってローカル置換を使います。現在の対象はJavaScriptで、npm・パッケージ管理とNode.js/Denoの完全互換は対象外です。

`_refs/`はローカル専用の未追跡の参考資料で、ビルド・テスト依存に含めません。リリースの配布対象・品質確認は[プロファイル](../../.forgejo/release-profile.yml)と[ワークフロー](../../.forgejo/workflows/release.yml)に定義されています。
