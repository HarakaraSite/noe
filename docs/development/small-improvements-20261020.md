# 小規模改善5件 — 2026-10-20

## 合意した範囲

候補の発見と独立した妥当性レビューを経て、利用者が次の5件を採用した。実装・回帰テスト・独立コードレビュー・総合テストを行う。リリース作業とversion変更は含めない。

| 項目 | 修正方針 | 維持する契約 | 状態 |
| --- | --- | --- | --- |
| 保存ログのtool出力混入 | 完了したtoolの捕捉バッファを解放する | stepをまたぐID再利用、並列tool、cursor再観察 | 実装・独立レビュー完了 |
| providerヘッダの上書き・削除 | ヘッダの既存側も正規化してマージする | モデル側nullの削除指定、一般設定のマージ、他モデルへの非波及 | 実装・独立レビュー完了 |
| pipeの不正signal | ロック前の検証と確実な後始末 | 有効なAbortSignal、既存のsignalなし扱い、正常転送・取消し | 実装・独立レビュー完了 |
| Go公開API・bridge説明 | 現行の入口文書と実装を整合させる | 公開API実装済みとv2タグ未公開の区別、歴史文書の保持 | 実装・独立レビュー完了 |
| Root付きFSのリンク自身への操作 | 境界検証と操作対象を分け、末尾リンクを保持する | 外向きリンク拒否、Rootなし、通常のstat/read/writeのリンク追従 | 実装・独立レビュー完了 |

FSはlstat/readlink/unlinkだけでなく、同原因のrm・renameも対象とする。rmdirとrenameの両端、ReadOnly、Promise版、親ディレクトリのリンクを回帰確認する。

## 今回の対象外

- Root外へのリンクと不存在宛先を組み合わせた新規作成の迂回。dangling symlinkも含めた別の修正単位とする。本作業でFS.Rootの完全な封じ込めやTOCTOU解決を主張しない。
- standalone `noe observe`の出力詰まり時の取消し。custom writerとの互換性を含む別の修正単位とする。
  2026-10-24に別件として対応した。[修正・検証記録](observe-cancellation.md)を参照。
- 新機能の追加、大規模なリファクタ、依存更新、外部providerへの実接続、タグ作成・公開。

## 実施手順・受入条件

1. 各問題を検出する回帰テストを先に追加し、現行実装での失敗を確認する。
2. 変更範囲を限定して実装し、関連既存テストと回帰テストを実行する。
3. 実装担当とは別の担当が差分をレビューする。指摘は修正・再検証し、未解消事項を記録する。
4. 新しいCLIをビルドし、全体unit、E2E、race、vet、6配布対象のcompile確認を行う。外部サービスは使わない。
5. 保存JSのBiome lint、Goのgofmt、diff検査、文書の参照確認を行う。
6. 実行結果・レビュー結果と残る制約をこの記録へ追記する。

## 実装・検証結果

### 回帰テストと関連検証

- 観察出力: `TestObserveReusedToolCallIDOutput` / `TestRunStateReleasesOnlyFinishedToolOutput` を変更前で失敗させ、完了バッファ削除後に成功。観察package全体・関連race・vetも成功。
- provider: `TestRegistryModelHeaderMerge` を変更前で失敗させ、最初の修正後に成功。provider package全体と20回反復も成功。下記の独立レビュー指摘も修正・再レビュー済み。
- pipe: 不正signalとcleanup例外のテストを変更前で失敗させ、修正後に成功。正常転送・falsy/構造的signal・事前abort・待機read/write取消しとロック解放を含む関連テスト、10回反復、Biome lintが成功。
- Root付きFS: `TestFilesLinkEntries*` を変更前で失敗させ、修正後に成功。既存FS・streamと関連raceも成功。同期/Promise・削除/rename・親リンク・ReadOnly・外部リンク拒否を検証。

### 独立コードレビュー

実装担当とは別の担当が、ソース差分、新規テスト、関連契約を確認した。

| 範囲 | 初回結果 | 対応・再確認 |
| --- | --- | --- |
| 観察出力・pipe・現行入口文書 | P0〜P3指摘なし。関連テスト・Biome・文書ローカルリンクも確認 | 完了 |
| Root付きFS・由来記録 | P0〜P3指摘なし。関連テスト・書式・差分を確認 | 完了 |
| providerヘッダ | P2: 再帰merge内で既存側を正規化すると `parameters.headers` の本文キーまで変わる | transport定義直下に限定し、本文キー保持の回帰テストを追加。独立再レビューで解消確認、追加P0〜P3指摘なし |

一般パラメータの空objectマージで `x-body-field` が `X-Body-Field` に変化し得る指摘。最初のヘッダ回帰テストでは未検出だったため、送信本文も検証するテストを追加した。修正前の失敗と修正後の成功を確認し、独立再レビューと最終ソースでの総合テストを実施した。

### 総合テスト

Linux amd64 / Go 1.27.2で実施。Goにはコマンド単位で `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOTMPDIR=/var/tmp/noe-gotmp` を設定し、外部provider・実認証は使わなかった。恒久設定やversion、依存は変更していない。

初回検証はprovider再修正と並行したため最終保証とは区別した。最終ソースで以下を逐次再実行した。

| 検査 | 最終結果 |
| --- | --- |
| `CGO_ENABLED=0 go build -o <検証dir>/noe ./cmd/noe` | 成功 |
| `go test ./... -count=1 -timeout=15m` | 成功 |
| `go test -tags=e2e ./... -count=1 -timeout=15m` | 成功 |
| `CGO_ENABLED=1 go test -race ./internal/... ./stdlib/... ./agent/... -count=1 -timeout=15m` | 失敗（下記） |
| `go vet -tags=e2e ./...` | 成功 |
| 6配布対象の `CGO_ENABLED=0 go list -export ./...` | 全対象成功。リンク・他OSでの実行確認ではない |
| 公開stdlib / agentのGo組み込み例 | 両方成功。agent例は新規ビルドCLIを使用 |
| 新規ビルドCLIでのagent-server Python統合テスト | 9件成功、skip 0 |
| `biome lint internal/nativeio/js/streams.js` | 成功 |
| 変更Goファイルの `gofmt -l` / `git diff --check` | 出力なし・成功 |

配布対象は linux/amd64・linux/arm64・darwin/amd64・darwin/arm64・windows/amd64・windows/arm64。実providerとGUIは未検証。rootの既存 `./noe` は置換せず、新規ビルドを一時検証ディレクトリに保存した。

### race検査の残件

- 初回は `TestNetReadCancelLeavesWriteUsable` が `closed socket retained active reference: true` で失敗。最終ソースでは `TestNetListenUnixCleanupAndExistingPathPreservation` が `Unix listener paths retained runtime work: busy=true` で失敗した。いずれもDATA RACE警告ではなく終了直後のBusy assertion。全検査成功とは扱わない。
- 前者は現行とHEADのstreams.jsへ差し替えたGo overlayの双方で反復再現した。対象テストは変更したstreams.js/pipeを評価・呼出ししない。既存の非同期完了通知とproducer終了の待合せ不足と判断した。後者は今回の差分に直接の変更はないが、変更前の独立再現までは実施しておらず原因未確定として残す。
- 今回の5件に紛れてネットワーク本体を修正したり、テストを緩和したりしていない。後続の別件として扱う。

### 証跡と進め方の反省

- 初回検証: `/var/tmp/noe-small-improvements-validation-TzGrkm3G/`
- 最終ソースの検証: `/var/tmp/noe-small-improvements-final-1792506051928/results.json` と同ディレクトリのコマンド別完全ログ。17検査中16成功、raceのみ失敗。
- 既存socketテスト調査: `/var/tmp/noe-net-readcancel-investigation-dcXRAg/`。変更前HEADとの差替え比較を含む。
- 小規模改善に対し、全体検査の再実行に加えて既存不安定テストを大量反復調査し、作業時間を広げすぎた。利用者の指摘を受け追加検証を終了。以後は関連回帰テストと1回の総合検査を基本とし、無関係な失敗の調査を別作業へ切り分ける。
- 実装・独立レビューは完了。総合検証には上記の未解消失敗が残る。コミット・タグ作成・公開は行っていない。
