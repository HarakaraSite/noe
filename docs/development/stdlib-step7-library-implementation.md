# NOE版stdlib：ステップ7.1・実装と検証

2026-10-19。[承認済み計画](stdlib-step7-library-plan.md)のS7-A〜Eを実装・検証完了。基点 `15e34b8ab060f73b580b3d8114a30b6879ff24de`。agent外部接続の7.2は対象外。commit/push/releaseは行わない。

## 実装範囲

- root moduleを `forge.harakara.site/littleisland/noe/v2` へ移行。依存version/go.sumを維持し、default CLI versionはv1.13.0のまま。
- 公開 `stdlib` packageは内部composerへ一度委譲。設定型のaliasと構造的な出力interfaceを提供し、導入・資源管理を二重実装しない。
- 本体runtimeとGo例を公開入口へ切替。英文GoDoc・[外部Go利用ガイド](../go-embedding.md)・go.mod templateを追加。
- checkout外consumerのlocal replaceと、隔離cache/local GOPROXYのreplaceなしartifactを検証するhelperを整備。
- 独立した過去のREPL/agent-loop nested moduleと履歴のmodule名は変更しない。利用者JSONL・_refs・root配布binaryは対象外。

## 状態と証拠

関連テスト→独立コードレビュー→総合テストの順に完了。[レビュー記録](stdlib-step7-library-review.md)と[構造化結果](stdlib-step7-library-results.json)へ結果を保存した。raw artifactは `/home/agent/.cache/noe-stdlib-step7/`、Go一時directoryは `/var/tmp/n7`。

公開Go APIの実装と公開tagでの取得成功は区別する。synthetic artifact試験はremoteへの公開を伴わず、実tagのgo get/build/runは別途許可されるv2公開後のチェックへ残す。常用の安定性確認とv2.0.0公開の利用者方針は維持する。

## レビュー前の関連確認

- 内部のcomposer・stdlib/wsproto・nativeio・hostio・runtime・apidocsの7packageが成功。重点raceはcomposer・nativeio・hostioの3packageが成功。
- CLI build、default `noe v1.13.0`、新linker pathの注入値 `noe stdlib-smoke`、公開入口のGo例のdocs/pipe実行が成功。
- profile/workflowのldflags一致と6対象、go.modのmodule行以外とgo.sumの不変、独立nested moduleの不変を静的確認。
- 既存Go変更152ファイルの正規化比較では、148件はmodule prefixだけ、4件はruntime三ファイルとGo例の公開入口切替。protocolのUser-Agentやcredential subpathは維持。
- `scripts/check-stdlib-consumer.py`の初回実行は12commandすべて成功。checkout外のlocal replaceと、replaceなし/隔離cacheのartifact双方でportable/reference flowが成立。artifactは668fileで、利用者JSONL・root binary・_refs・nested moduleを含めない。engineの実選択はv0.27.0。

consumer作成中に、server.urlへpathを足す際の区切りとCore/native output設定に試験コードの誤りを発見し修正した。本体の不具合として扱わず、最終成功に上書きせず履歴へ残す。raw notes/logは上記artifact directoryへ保存する。

## 独立コードレビュー

新しいreviewerへ限定contextだけを渡した通常レビューを実施し、P0/P1/P2の未解消指摘は0件。修正対象がないため再レビューは不要。168fileの凍結manifest（SHA256 `cc2936eb39abe4a47e77711aafac77712da10ff46400711fa2b35c00f9675ad6`）はレビュー開始・終了と総合テスト後に一致した。最終記録・段階案などの状態更新はこのsource manifestと分けて扱った。

## 総合テスト

Linux amd64 / Go 1.27.2。全体検証は初回ですべて成功した。

| 検証 | 結果 |
| --- | --- |
| 全体unit `CGO_ENABLED=0 go test ./... -count=1 -timeout=300s` | 成功、46.998秒。通常gateの外部portable consumerを含む |
| E2E `CGO_ENABLED=0 go test -tags=e2e ./tests -count=1 -timeout=10m` | 成功、36.471秒。準備済みMCP SDK bundleを明示指定 |
| `go vet ./...` / `go vet -tags=e2e ./...` | 成功、0.836秒 / 0.753秒 |
| 全体race `CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... ./stdlib/... -count=1 -timeout=300s` | 成功、69.704秒 |
| Linux/macOS/Windows × amd64/arm64 | CLI 6build、影響test 36compile、外部consumer 6compileが成功。Linux amd64以外は実行していない |
| 最終sourceの外部consumer | local/artifactのnative flowと6対象compile、18command成功。replaceなしartifactは669file、cache隔離。CLI/runtime/agent/認証/SDKを含まないproduction closureは257package |
| Go例の外部利用 | main.goとgo.mod templateをcheckout外へcopyし、local replaceでtidy/run成功 |
| 追加CLI/native smoke | 基本11・docs/IO 9・REPL削除9、fixture agent/hook、HTTP送信/受信のEOF前到達、docs CLI/JS一致、有限例とlistener再bindが成功 |
| JS API比較 | main/Node子/孫/Web子のglobalsが119/119/119/129、各26module・41topic・100detail。export・型identity等を含むinventory全体がbaselineと同一 |

追加HTTP受信smokeの初回は、実行scriptのsource path適合時に`Path`を`pathlib.Path`と書いてNameErrorとなった。製品sourceは変えず、そのsmokeだけ修正再実行し成功。初回と再実行のraw結果を両方保持した。全体suiteや成功済みの他smokeを再実行して置き換えてはいない。

profile/workflowには既存の生成整合testがなかったため、ldflagsと6対象build行を静的照合し、実CLIの固有version注入を実行確認した。依存version/go.sum・歴史的nested moduleは検証後も不変。root配布binary・利用者JSONL・_refsは読み取り/変更/配布対象に含めない。

## 実プロバイダ

OpenCode Go / `deepseek-v4.1-flash` / effort `auto`。input.jsonのamount 7/11/13を集計し、report.jsonへ保存して再読込み検証し、英文で完了を返す1課題。docs参照を指示せず、JavaScriptだけで外部command/delegationを使わない条件を与えた。上限6モデル要求・115秒。

初回で成功、9.999秒、モデル要求3回、run_js 2回（両方exit0）。保存内容はrowCount 3・total 31、state completed、英文最終回答を確認した。この課題ではdocs呼出しを観測していない。docs自発参照の新しい成功証拠としては扱わない。認証内容は記録へ出さず、raw events/実行コード/結果をartifactのlive-smokeへ保存した。

## 到達点と後続

**7.1完了・7.2未着手**。正規/v2 moduleと公開stdlibをNOE自身・外部Goホストの双方から使える。公開aliasのfields/methods/type identityと所有契約を今後の互換性対象とする。API docs・GoDoc・利用ガイド・例は英文。

今回commit/push/releaseは行っていない。default CLI versionはv1.13.0で、公開済みv1.13.0タグや配布binaryを書き換えていない。常用での小さな修正と安定性判断、agent外部接続7.2、実v2 tag公開とclean remote consumerの取得/build/runは後続。moduleの/v2移行やlocal artifact成功をv2公開完了と取り違えない。
