# URL import：実装・検証記録

正本：[設計案](url-import-plan.md)・[スライス計画](url-import-slices.md)。

状態：計画の通常/批判的再レビュー、S1〜S4の実装/対象テスト/独立コードレビュー、公開ESM・外部Go consumer、最後の総合検証を完了。文書期待値の修正も元レビュアーが確認済み。本記録を含む今回の変更を単一の機能コミットへまとめる。

## 計画再レビュー

通常・批判的レビューが独立に確認し、ともに実装着手可・重要指摘なし。利用者判断待ちなし。

- [通常再レビュー](url-import-plan-rereview-normal.md)
- [批判的再レビュー](url-import-plan-rereview-critical.md)

API互換、4スライスの依存順、HTTP取消し・redirect ID・Worker入口・能力metadataの実装境界を確認した。これは実装・実行証拠の承認ではなく計画の承認。

## スライス進捗

| スライス | 実装 | 対象テスト | 独立コードレビュー |
| --- | --- | --- | --- |
| S1 resolver | 完了：URL helper / snapshot / virtual / local拒否境界 | go test ./internal/modulemap 成功、gofmt・diff check成功 | [S1承認・重要指摘なし](url-import-code-review.md) |
| S2 HTTP loader / Go provider | 完了：structured provider / HTTP reader / 最終ID保持 / metadata | go test -v ./internal/modulemap ./internal/jsexec ./stdlib 全成功（consumer含む） | [S2承認・重要指摘なし](url-import-code-review.md) |
| S3 CLI / Worker / agent | 完了：CLI既定provider、Host能力facts、実経路fixture | 影響5packageとURL/指示E2E成功。新fixtureのtrace設定修正後に対象再確認 | [S3承認・重要指摘なし](url-import-code-review.md) |
| S4 docs / consumer / 公開ESM | 完了：利用ガイド・日英README・Go API説明・公開例・consumer | 外部Go consumer、Biome lint、CGO_ENABLED=0 build、公開esm.shの直接/URL alias利用が成功 | [S4承認・重要指摘なし](url-import-code-review.md) |

## 最終検証・コミット

| 確認 | 結果 |
| --- | --- |
| go test -tags=e2e ./... | 全E2Eと他の全package成功。internal/apidocsの旧文言assertだけ1件失敗し、下記の修正/再確認で解消 |
| go test -tags=e2e ./internal/apidocs -v | 旧literal HTTP importsを新URL契約のassertへ更新後、package全成功 |
| go vet -tags=e2e ./... | 成功 |
| CGO_ENABLED=1 go test -race ./internal/modulemap ./internal/jsexec ./stdlib ./agent ./internal/runtime | 5package成功 |
| gofmt・保存JSのBiome lint・文書リンク・差分 | 変更対象で確認。公開例2ファイルlint成功、主要新規参照は解決 |

総合testの最初の失敗はS3の内蔵docs変更へ到達する旧文字列期待値であり、今回と無関係な既存不具合として逃がしていない。製品実装/docは変えず、catalog testをCLI enabled・Go profile optional/fetch独立・JS MIME+text/plain・TS/npm/cache無しという新契約の確認へ更新した。既存alias/local/fresh runtimeのassertも維持し、元レビュアーが限定再確認して承認した。

このtest-only修正後に影響するapidocs packageを再実行し成功。製品source/他packageは総合検証候補から不変なので、全メニューの再実行ではなく既存成功証拠を再利用した。全E2Eは46.897秒、総合実行は59秒、affected raceは68秒で終了。ベンチマーク値や性能保証ではない。

詳細log：/var/tmp/noe-url-import-final-integrated.log、final-apidocs-recheck.log、final-vet.log、final-race.log（後3件もprefix /var/tmp/noe-url-import-）。対応する再レビューは[コードレビュー記録](url-import-code-review.md)末尾。

今回のsource・tests・利用文書・設計/レビュー/実装記録だけをコミット対象にする。既存の無関係な.noe/events、examples/agent-server/__pycache__、一時binary/log、workspace既存noe binaryは含めない。公開リリース、version変更、配布binaryの更新は行わない。

## スライス別の実行証拠

- S1：go test ./internal/modulemap。URL解決・map/virtual/local境界、既存engine testsが成功。
- S2：go test -v ./internal/modulemap ./internal/jsexec ./stdlib。HTTP/HTTPS、static/dynamic/re-export、redirect/alias/循環のidentity、query/fragment、本文保持と独立runtime、MIME/UTF-8/BOM/上限/close、借用client、FS/Fetch独立、context/Runtime.Cancelが成功。
- S3：go test ./agent ./internal/agent ./internal/runtime ./internal/cli ./internal/noeprofile。CLI run/eval、global Workerのlocal alias入口・URL依存とURL入口非接続、Host facts snapshot/非漏洩、Host CancelTask中の本文停止が成功。
- S3 E2E：TestAgentInstructionFilesRunAndEval、TestURLImportAgentDelegationAndFreshRunJSSourcesが成功。後者は親の2回のrun_jsと子のrun_jsで同じURLを3回取得し、配信元を1→2へ更新した結果と独立module状態を確認。初回はfixture既定traceがstderrへ出る設定の期待不一致だけを修正し、影響する新E2Eだけ再実行した。
- S4：go test ./stdlib -run TestExternalConsumerPortable -v。checkout外のGo moduleへfixtureをcopyし、公開URL provider/APIをcompile/run。既存FileLoader consumerも維持。
- S4：biome lint examples/url-import/main.js examples/url-import/mapped.js。2ファイル成功。
- S4：CGO_ENABLED=0 go build -o /var/tmp/noe-url-import/noe ./cmd/noe。成功。workspaceの既存noe binaryは上書きしない。

### 公開ESMの実利用

新しいtemporary binaryで以下を実行した。どちらもexit 0、stderrなし、stdoutは `[[1,2],[3,4],[5]]\n`。

```sh
/var/tmp/noe-url-import/noe run examples/url-import/main.js
/var/tmp/noe-url-import/noe run --import-map examples/url-import/import-map.json examples/url-import/mapped.js
```

https://esm.sh/lodash-es@4.17.21 のchunkを実際に利用した。後者はaliasのstatic importと直接dynamic importのnamespace同一性も確認する。この一件の成立を任意npm互換、配信内容のhash固定、今後の外部サービス可用性の保証とはしない。通常のGo/E2E testsは外部CDNへ接続しない。

作業時の詳細logは /var/tmp/noe-url-import-s1-tests.log、s2-tests.log、s3-unit-tests.log、s3-e2e-recheck.log、s4-consumer.log、s4-js-lint.log、s4-build.log、s4-public-esm.log（いずれもprefix /var/tmp/noe-url-import-）に保存。恒久的な根拠は本書のコマンド・確認動作・結果と対応test codeを使い、一時logの存在へ依存しない。
