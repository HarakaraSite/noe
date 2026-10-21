# go-quickjs v0.28.0取り込み計画

## 目的と範囲

基点はNOE `e095493`（v2.0.1公開確認後）。JavaScript engineだけを
`github.com/go-quickjs/go-quickjs v0.27.0`から`v0.28.0`へ更新する。
既存のCLI、公開Go/JS API、agent・run_js・Worker・外部Go組み込みの機能を維持する。
計画の通常レビュー・批判的レビューを行い、P2以上の指摘を解消してから実装する。
実装・テスト・コードレビューの後に総合テストを実施し、合格後コミットする。
push、タグ、公開、既存配置バイナリの置換は対象外。
利用者の未追跡イベントログ2件は変更・コミットしない。

上流正本:

- [v0.28.0リリース](https://github.com/go-quickjs/go-quickjs/releases/tag/v0.28.0)
- [v0.27.0との差分](https://github.com/go-quickjs/go-quickjs/compare/v0.27.0...v0.28.0)
- v0.28.0 commit: `55048952b1b0d9b7f1c1168d87abbc1f21243408`
- module checksum: `h1:FWZ3BX1T1U+SK7v7hSMVPcLj791/AD27Y6WVNMjq6ek=`

## 維持する契約と判断材料

- JITは有効にしない。通常のビルドと`quickjs.New()`を維持し、
  `quickjs_jit`タグ、`WithJIT()`、JIT用の公開オプションを追加しない。
  CLI、managed task/run_js、Worker、VMの生成経路を確認する。
  caller-owned runtimeを受け取る既存Go APIの自由度は制限しない。
- API互換でも内部実装は変わる。BigIntの独自実装、object layout/property
  cacheの変更、primitive propertyのdeleteの仕様修正を確認対象にする。
  上流の仕様適合修正を旧版の挙動へ戻すための回避コードは追加しない。
- NOE管理stdlibの原型は引き続きv0.27.0。`internal/stdlib`を上流v0.28.0から
  コピーし直さず、UPSTREAMのorigin・checksum・license・取り込み履歴を維持する。
- Go要求版、provider SDK、go-intl等の他の依存は据え置く。
  `go.mod`/`go.sum`差分とMVSの選択結果で確認する。
- 速度向上を受入条件にしない。NOEの実ワークロードで未計測の性能改善を主張しない。
- 参照環境はLinux amd64、Go 1.27.2。その他5対象はcompile確認であり、
  ネイティブ実行確認とは区別する。実プロバイダは使用せずローカルfixtureで確認する。

## スライスと検証

### P: 計画確定

通常レビュアーは実施順、変更範囲、受入条件と既存検査の対応を確認する。
批判的レビュアーはAPI互換だけで見落とす動作変更、NOEとstdlibとengineの境界、
JIT非採用経路、外部consumerと検査の独立性を確認する。
両者はread-only。各初回30分以内、10分進展なしなら中間結果を返す。
修正後は同じ担当へ差分と指摘の対応を渡し、原則15分以内で再確認する。
利用者の指示に従いP2以上の未解決事項がなくなるまで必要な修正・再確認を繰り返す。
新規範囲・非互換対応が必要ならdefaultが採否を判断し、範囲を外れる変更は利用者へ返す。

### I1: engineの更新と実装確認

`go.mod`/`go.sum`のみを対象にengineを固定版へ更新する。
`quickjs.go`などNOE使用APIとmodule依存差分を照合し、意図しない追随更新を除く。
標準runtime、Worker、`node:vm`、managed run_jsの生成・取消し・close経路を読む。
既存のruntime/stdlib/public agentの関連テストを先に実行し、必要な修正だけを行う。
互換性エラーがあれば期待値を緩めず、旧版でも同条件を試して更新起因か切り分ける。

BigIntの64bit境界を越す演算・Workerメッセージ往復と、objectの追加/削除・
prototype更新を、NOEの実際の入口を通る一時smokeで確認する。
既存のESM/Promise/host callback、Worker、interrupt/cleanup、診断、managed run_js
のテストを利用する。上流engineの仕様テストをNOEに複製せず、NOE固有の未検査の
不具合を確認した場合だけ、その契約を守る回帰テストを追加する。

### I2: 利用者向け説明と外部Go consumer

`docs/go-embedding.md`と`docs/go-agent-embedding.md`の検証engine版を更新する。
README等では必要に応じて現在のengineとstdlibの原型を明確に分ける。
特に`docs/api.md`のengine版と「同版stdlib」、`docs/development/README.md`の
「同版stdlib」を、engine v0.28.0・stdlib原型v0.27.0の区別に合わせる。
過去リリース・過去検証結果やstdlibの由来をv0.28.0へ一括置換しない。
計画への導線と、`docs/development/dependency-updates.md`へ今回の実施記録を追加する。

既存の`scripts/check-stdlib-consumer.py`をstdlib/agent両方で実行する。
外部moduleのlocal replaceおよび合成module artifactからbuild/runし、Goの型境界と
MVSで選ばれたengineがv0.28.0であることを記録・確認する。
consumerの旧v0.27.0要求はMVSで新しいengineが選ばれる互換性確認として維持する。
合成artifactは未公開の作業ツリーの検証であり、正式公開moduleの取得とは表現しない。
consumer検査用スクリプトの変更は、実際の不足が確認された場合に限る。

### R: 実装のコードレビュー

I1/I2の実装と関連テストを終えた差分を、計画担当を再利用して通常・批判的にレビューする。
対象は依存差分、説明、検証結果、既存テストの維持、stdlib原型の維持、JIT無効。
上流engine全体の再監査や独立の性能評価は対象外。
P2以上の指摘は修正し、同じ担当が変更箇所と解消を確認する。

### T: 総合テストとコミット

コードレビュー後の最終ソースに対して、既存の配布プロファイルに沿って実施する。
短いTMPDIR/GOTMPDIRを用い、コマンド、終了コード、時間、ログ保存先を記録する。
下位コマンドが失敗したら検査全体を失敗として扱い、出力表示だけで成功にしない。

| 検査 | コマンド・確認内容 |
| --- | --- |
| 全体unit/E2E | `CGO_ENABLED=0 go test -tags=e2e ./... -count=1`。通常unitを含む |
| race | `CGO_ENABLED=1 go test -race ./internal/... ./stdlib/... ./agent/... -count=1` |
| 長時間Go Client | `CGO_ENABLED=0 go test -v -tags=slow_integration ./agent -run '^TestGoClientLongModelAndToolWait$' -count=1`。4ケース実行を確認 |
| vet | `CGO_ENABLED=0 go vet -tags=e2e ./...` |
| 6対象compile | `.forgejo/release-profile.yml`と同じlinux/darwin/windows各amd64/arm64で`CGO_ENABLED=0 go list -export ./...`。失敗で停止 |
| 外部consumer | 前段の両consumer結果を使用。コードレビューで関連変更があれば再実行 |
| Linux CLI smoke | 一時領域へ通常フラグでbuild。version・ESM・BigInt/objects/Worker、埋込みdocs、ローカルfixtureのagent/run_jsを確認。配置済み`noe`は置換しない |
| 差分 | `git diff --check`、依存選択、JITタグ/オプションを追加していないこと、対象ファイルのみのstageを確認 |

新たな失敗があれば再現と原因を記録し、関連する実装修正・コードレビュー・検査をやり直す。
総合テスト後に説明・証跡だけを追記した場合は、内容とリンクを確認し、無関係な重い検査は繰り返さない。
全検査PASS・P2以上の未解決指摘なしを記録してから今回の変更をコミットし、
commit ID、実際に試した範囲と未検査範囲を利用者へ報告する。

## 進行状態

- P: 通常・批判的の初回レビューでP2以上なし。実装着手可。
- P/I1/I2/R/T: 完了。通常・批判的レビューともP2以上の未解決指摘なし。
  初回raceの既存テストのprocfs判定を最小修正し、再レビュー後の総合検査はすべてPASS。
- レビュー結果・対応・実施証跡は本書末尾へ追記する。

### 計画レビュー結果 — 2026-10-21

通常レビューは実施順・配布プロファイルと検査の対応、consumerのlocal/artifactと
旧版要求からのMVS確認、生成経路・型境界を確認し、P2以上なし。
任意の明確化として挙がった`docs/api.md`と開発READMEの「同版stdlib」をI2へ明記した。
批判的レビューもJIT非採用、engine/stdlibの境界、BigIntのWorker clone変更、
object cache/layoutとprimitive delete、外部consumerを確認し、P2以上・追加提案ともなし。
両レビューとも初回でP2以上がなかったため、反復が必要な未解決事項はない。

### 実装と関連テスト — 2026-10-21

初回I1/I2の変更はgo.mod/go.sumのengine更新と現行ガイド・本記録のみ。
NOE実装・既存テスト・consumerスクリプト・stdlib原型・配布設定は変更していない。
全moduleの更新前後を比較し、選択版の変化はgo-quickjs 1件だけであることを確認した。
通常buildでengineの`zzjit_disabled.go`が選ばれ、公開APIの変更やJIT指定の追加はない。

| 関連検査 | 結果 |
| --- | --- |
| jsexec/runtime/内部stdlib/公開stdlib/公開agent/常駐Go例の既存テスト | PASS、26.283s |
| 一時CLI buildとversion | PASS、5.759s。CLI版はv2.0.1のまま、engineはv0.28.0 |
| BigInt/object/Worker/primitive delete smoke | PASS、0.065s |
| managed run_jsの低レベルAPI・tool履歴E2E 2件 | PASS、1.920s |
| stdlib外部consumer（local/artifact） | PASS、19.962s |
| agent外部consumer（local/artifact、portable/gohost） | PASS、30.033s |

BigInt smokeは64bit超の乗除算、64bitへの切り詰め、workerDataからparentPortへの
BigInt演算結果の往復を確認した。objectはwarm-up後のprototype書換え、300個の
動的property、delete/freezeを確認し、strictなprimitive deleteはTypeErrorになった。
これは一時smokeで、上流仕様テストの恒久的な複製は追加していない。
両consumerとも旧engine v0.27.0要求を残した状態でMVSがv0.28.0を選び、
合成artifact側のengine checksumも正規moduleのchecksumと一致した。
正式公開moduleとしての取得試験ではない。

ログ・コマンド・終了コード・時間・一時smokeソースは`/var/tmp/noe-q280-XrXP1S`。
`focused-results.json`、`consumer-selected-engines.json`、`dependency-diff.json`と
各consumerの`results.json`/`manifest.json`を保存した。

### コードレビュー結果 — 2026-10-21

計画担当の2名を再利用し、通常・批判的コードレビューともP2以上・追加提案なし。
依存選択・checksum、JIT無効のbuild、BigInt/Worker/object smoke、関連packageと
managed run_jsのE2E結果、外部consumerのmanifest・選択engine・バイナリ情報を確認した。
通常レビューはartifact内のGo/JS・依存定義510ファイルと現行ソースの一致、
批判的レビューは型境界・Worker関連9ファイルと両artifactの一致も確認した。
実装・既存テスト・原型stdlibの変更はなく、証跡と説明に不整合はなかった。
総合テストはコードレビュー後に実行する。

### 初回総合テストで確認した既存テストの競合

全体unit/E2EはPASS（47.899s）。続くraceは94.694sで失敗した。
`TestExternalCommandConsumerExplicitHelper/separate-owner-death`の終了確認中に
`read /proc/<pid>/stat: no such process`が発生したため、後続検査は実行せず停止した。
初回ログと終了コードは`full-race-attempt1.log`、`total-attempt1-results.json`へ保存した。

旧engine v0.27.0のclean worktree（基点e095493）と更新版で各10回、同じowner-death
raceテストを実行し、両版で同じESRCHの失敗を自然再現した。
さらに「消失後のreadでESRCH」を返す一時overlayでも両版の既存テストが失敗した。
更新固有の回帰ではなく、procfs lookup後にtaskが消える正常な競合を
終了確認側が認識していない既存テストの問題と判断した。

`stdlib/command_owner_posix_test.go`のSIGKILL後のpollだけ、既存のErrNotExistに加えて
ESRCHもプロセス消失として扱う。停止前のhelper PID取得は厳格なまま、
生存・権限エラー・timeoutの判定は維持する。
同じrepositoryの`internal/process/process_owner_linux_test.go`の`linuxProcessAlive`も
同じOS上の理由で両エラーを消失として扱っている。
批判的レビュアーも限定確認し、この変更は終了確認の契約を弱めないと判断した。
修正後の自然反復、ESRCHの制御試験、権限エラーと停止前ESRCHの失敗維持を確認し、
通常・批判的の差分再レビュー後に総合テストを再実行する。

修正後の自然反復10回はPASS（18.146s）。消失後ESRCHのoverlayもPASS（3.379s）。
EACCESのoverlayは`permission denied`でFAIL、kill前ESRCHのoverlayもFAILとなり、
正常な消失以外の失敗を引き続き検出することを確認した。
`baseline-owner-race-results.json`、`current-owner-race-results.json`、
`owner-fix-results.json`と各overlay/logに、旧版・更新版・修正前後の結果を保存した。
consumerのproductionソースは変わっていないため、前段の外部consumer検証は再利用する。
初回コードレビューのmanifest照合は、上記テストファイル修正前の時点の記録である。

差分限定の通常・批判的再レビューもP2以上・追加提案なし。
両者は追加差分、旧版・更新版の自然再現、修正後の反復と制御試験を確認し、
ESRCHを正常な消失として認識する修正が、生存中や停止前の異常を隠さないことを確認した。
この再レビュー後に、Tの総合テスト一式を最初から再実行する。

### 最終総合検証 — 2026-10-21

再レビュー後、最終コードでTの全検査を実行し、すべてPASSとなった。

| 検査 | 結果 |
| --- | --- |
| 全体unit/E2E、CGO無効 | PASS、46.437s |
| internal/stdlib/agentの全体race、CGO有効 | PASS、82.874s |
| 長時間Go Client | PASS、30.766s。model/tool各正常待機・取消しの4ケースを実行 |
| vet（E2Eタグ含む） | PASS、1.369s |
| linux/darwin/windows各amd64/arm64、CGO無効compile | 6対象PASS、合計26.985s。リンク・配布物生成は行わない |
| 最終Linux CLI build、BigInt/object/Worker/delete smoke | PASS、build 0.867s、smoke 0.065s |
| 最終CLIのローカルagent fixture・docs・同期コマンド失敗保持 | PASS、0.668s |
| module整合性、差分空白検査 | `go mod verify`、`git diff --check`ともPASS |
| 外部Go consumer | I2のproductionソース不変のため既検証結果を利用。stdlib/agent、local/artifactともv0.28.0でPASS |

最終CLIはgo-quickjs v0.28.0・CGO無効であることをbuild情報でも確認した。
通常のJIT無効設定を維持し、Go要求版・他の選択依存は不変。
総合テスト後の変更は本記録等の説明・検証結果のみで、コード・依存・既存検査の条件は変えていない。
追加した文書リンクを確認する。変更はengine依存、現在の説明、計画・検証記録、
既存テストのESRCH消失判定に限定し、利用者のイベントログは含めずコミットする。

全コマンドと15件の終了コードは`total-results.json`、最終確認は`final-verification.json`、
ビルド情報は`final-build-info.txt`、その他のログ・制御試験ソースも
`/var/tmp/noe-q280-XrXP1S`に保存した。初回の失敗記録も保持している。
実行環境はLinux amd64 / Go 1.27.2。その他5対象のnative実行、JIT動作、実provider、
性能改善は今回の検証対象外。公開・配置は行わず、配置済み2バイナリのhashが不変であることも確認した。
