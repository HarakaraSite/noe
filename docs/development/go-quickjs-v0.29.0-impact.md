# go-quickjs v0.29.0の影響調査 — 2026-10-26

[開発ガイド](README.md) · [依存の記録](dependency-updates.md) · [stdlib由来情報](../../internal/stdlib/UPSTREAM.md)

後続指示により取り込みも実施した。[取り込み実施](#6-取り込み実施--2026-10-26)を参照。
以下の1〜5節は依存変更前の影響調査記録として保持する。

## 結論と範囲

**現時点の確認では、NOEの互換性修正なしで取り込み可能。**
ただしengine単体ではなく、go-intlも更新され、Intl/Date等の出力変更を伴う。
性能改善や全localeの出力不変を保証する調査ではない。

基準はNOE `10053d031d9bb914c4c7399db40f332d1f9e1f47`（v2.7.0公開後の記録commit）。
候補は正式tag `v0.29.0`、commit
`44ddbaa01bcc4fa431e3e95978a18cb9ee4f21fc`。
上流の[比較](https://github.com/go-quickjs/go-quickjs/compare/v0.28.0...v0.29.0)を読取り、
別worktreeで依存だけを更新して検証した。

影響調査時点では、本体のgo.mod/go.sum・製品実装・配布定義・実行バイナリは未変更。
その時点でcommit/push/releaseは実施しておらず、作成物は本文書と入口リンクのみだった。

## 1. 実際に変わる依存

| module | 現行 | 候補で選択された版 |
| --- | --- | --- |
| github.com/go-quickjs/go-quickjs | v0.28.0 | v0.29.0 |
| github.com/go-quickjs/go-intl | v0.6.1 | v0.8.0 |

候補の `go get github.com/go-quickjs/go-quickjs@v0.29.0` と全module graphを照合し、
変更はこの2moduleだけだった。Go 1.27.2、x/sys・provider SDK等は維持。
上流engineの最低Goは1.24.0のまま。go-intl v0.8.0にも新たな外部module要求はない。

取り込む場合の必須差分はgo.mod/go.sumと説明・検証記録が中心となる。
後述の既知退行に対応する確認を採用し、NOE側の互換性修正が不要という今回の証拠を再利用できる。
engine checksumは `h1:oyhW8n67CkgfwV+8vq0P6zMc9SYzKr8CX/dGPGInGEE=`。

## 2. NOEの実利用への影響

| 領域 | 影響と判断 |
| --- | --- |
| 公開Go API | root production Go全比較ではquickjs.goのコメント以外に差分なし。Eval/Value/Promise/jobs/module loader/context取消し等のシグネチャ変更なし。NOEのadapter修正は今回不要だった |
| CLI/run_js/agent/hook/Worker | 共通engineが変わるので、すべてのJS実行に波及。特にmodule評価、generator/async、try/finally、iterator、RegExp等の内部実装が更新される |
| module globals | global getter/setterのthisとglobal variable代入先を実global objectへ是正。旧挙動に依存したscriptは結果が変わり得る。根拠は上流commit 43b12e1とmodule_binding_test.go |
| String/Number/Math等 | 非10進Number.toStringやMath.hypotの丸め末尾、padStart/Endの変換順序、iterator getterの取得回数等にも仕様是正あり。単なる速度改善だけの更新ではない |
| NOE管理stdlib | internal/stdlibはv0.27.0由来のローカルsnapshotのまま。上流stdlib/・internal/wsproto/はv0.28.0..v0.29.0で全内容無変更。NOE独自のHTTP streaming、Worker所有、同期exec、symlink等を置換・再importする必要なし |
| JIT | 自動有効化されない。上流はquickjs_jitタグとWithJIT()の両方が必要。NOE通常runtime/Workerはoptionなしquickjs.New()、配布にもタグなし。今回JIT導入を混ぜない |
| 配布・外部Go利用 | CGO不要、6対象compile成功。public stdlib/agent外部consumerも既存unit内で成功。最低Go・配布定義の変更不要。公開moduleの実取得確認は未公開候補なので今回の対象外 |

Promise/job/取消しの公開adapterは不変だが、内部実装全体の不変を意味しない。
unitはruntime、Worker、agent、hook、取消し等の既存経路を含み、E2Eは実CLIを含む。

## 3. 既報2件はNOE経由でも解消

公開済みv2.7.0バイナリ（engine v0.28.0）と候補バイナリを同じprobeで比較した。

| 問題 | v0.28.0の実測 | v0.29.0の実測 |
| --- | --- | --- |
| [上流#4](https://github.com/go-quickjs/go-quickjs/issues/4): module block内の関数宣言とshadowing | ReferenceError、および期待7に対して外側の99 | 両方7 |
| [上流#5](https://github.com/go-quickjs/go-quickjs/issues/5): colon付きnamed re-export | node:pathの再exportをnodeへ誤解決しexit 1 | 正しく解決しfile.txt、exit 0 |

上流修正はそれぞれ `f984c46` と `58f869f`。module_binding_test.goに回帰testあり。
現在のimport＋local exportによるNOE回避策は有効なままで、更新のために戻す必要はない。

## 4. Intl/日時は出力差を明記する

go-intl更新とstandards modeの是正により、NOEの通常設定でも結果が変わる。

| 同じNOE probe | v0.28.0 | v0.29.0 |
| --- | --- | --- |
| Telugu/Buddhistの月・年formatのresolvedOptions().year | fieldなし | numeric |
| Africa/Abidjan、1900-01-01のgetTimezoneOffset() | 16 | 16.133333333333333 |
| ko-KR環境のIntl.Locale("ak").getCollations() | emoji, eor | emoji, eor, searchjl, unihan |

runtimeのlocaleがICU fallbackのdefaultにも適用される。
Intl/Date/TemporalやlocaleCompare等を使うscript、特に出力の固定文字列比較に影響し得る。
ロケール・timezoneの明示は環境差を減らすが、今回の仕様是正による版差をすべて消すものではない。

今回選んだ日本語/独語の通貨表示、日本語日付、NFC正規化、日本語分割、独語照合は旧新同じだった。
全locale/corpusの同一性確認ではない。NOEはWithNodeQuirksを指定しておらず、
互換性のために新たに指定する変更も行っていない。

## 5. 隔離候補で行った検証

環境: Linux amd64、Go 1.27.2、CGO_ENABLED=0、JITなし。

| 確認 | 実行内容/結果 |
| --- | --- |
| 全体unit | go test ./... -count=1: PASS、約30秒 |
| 全体local E2E | go test -tags=e2e ./tests -count=1: PASS、約44秒 |
| native CLI | 候補go build、上記module/Intl probe: 成功 |
| 6対象compile | GOOS/GOARCHごとのgo list -export ./...: Linux/macOS/Windows × amd64/arm64すべてPASS |
| module整合性 | go mod verify: all modules verified |
| 保存probe JS | 4mjsへのBiome lint: PASS |

6対象compileはリンク・配布artifact生成・他OSのnative実行ではない。
race、長時間/負荷試験、性能benchmark、実provider通信は今回未実施。
現時点で導入を妨げる失敗は見つからず、未測定性能を理由とした追加設定・制限は不要。

## 証拠と次の作業

証拠directory: `/var/tmp/noe-q290-impact-PqTX81`。
- upstream-analysis.md: 独立した上流の静的差分調査、API/VM/stdlib/JITの詳細根拠
- candidate/: go.mod/go.sumだけ変更した隔離worktree
- dependency-delta.diff、module-delta.json、candidate-modules.txt
- unit.log、e2e.log、cross-compile.json、module-verify.log
- probes/、behavior-comparison.json、probe-lint.log、candidate-build-info.txt

取り込み実施時は2module更新を明示し、既報修正とIntlの意味論変更を利用者向けに説明する。
stdlibの由来版の書換え、JIT有効化、不要なAPI変更、配布設定変更を同時に行わない。
今回と同じsource/dependency候補なら成功済み検証を再利用し、変更理由のない全体再実行は不要。

## 6. 取り込み実施 — 2026-10-26

利用者の「v0.29.0を取り込み、回避コードも素直な書き方に戻す」指示により、
main作業ツリーのengineをv0.29.0、go-intlをv0.8.0へ更新し、go mod tidyした。

task.jsは次の直接named re-exportへ変更した。

```js
import { executeToolWithHooks } from "noe:agent/tool-pipeline";
// ローカルのtool実行にも使うimportは維持する。
export { executeToolWithHooks } from "noe:agent/tool-pipeline";
```

export-fromだけではローカルbindingは作られないため、task loopが呼ぶimportは残す。
wrapperやfallbackは追加しない。既存の実executor fixtureはtask経由のexportと
pipelineからの直接importが同じ関数であることを確認する。

既報#4はNOEのdynamic-import entry wrapper経由でのasync block関数宣言と
外側の同名関数へのshadowingの回帰テストを追加した。
NOE管理stdlibの再import・由来情報変更、JIT導入、CLIバージョン変更、
ローカルバイナリ差し替え、commit/push/releaseは行わない。

依存だけを更新した同一候補の全体unit/E2E・6対象compileは上記の成功結果を再利用する。
追加変更で影響するexecutor、entry loader、task loop/hook/toolとCLI利用経路は
対象を絞って確認する。適用後の検証・レビュー結果は以下に追記する。


### 適用後の確認結果

| 確認 | 結果 |
| --- | --- |
| 選択module graph | 影響調査の成功候補と全module/版が完全一致 |
| 追加変更の関連unit | internal/agentexec・internal/jsexec・internal/runtimeの全test成功 |
| CLI利用経路 | TestHookFailuresCLIWithoutEventFileのrun / eval_observeとも成功 |
| 同期・所有/取消しのrace | internal/jsexec・internal/agentexec・internal/runtime・internal/stdlibの4package成功 |
| 関連vet | 上記unit対象3packageで成功 |
| 保存JS/整形/差分 | task.jsのBiome lint、変更Goのgofmt、git diff --check成功 |

追加のre-export変更では既存の関数identity検査と実task loop/tool/hook経路が成功した。
新しいblock関数宣言の回帰testも成功。新たな本体不具合は見つからなかった。

独立した読取りreviewでは実装・依存・回帰testに要修正なし。
開発入口の旧「上流修正待ち」表示・切れたfragment・旧engine版の指摘を受け、
入口を解消済み/取り込み記録へ変更し、現行API・Go組込み・構成説明もv0.29.0へ揃えた。
過去のrelease/調査記録やstdlibの原型v0.27.0は維持した。
これらは文書修正のため製品testの再実行は行わず、親が差分と参照先を確認した。

検証出力は同証拠directoryのapply-checks.json、apply-affected-unit.log、
apply-cli-e2e.log、apply-runtime-race.log、apply-affected-vet.log等へ保存。
review原文はapply-review.md。全体unit/E2E/6対象compileは
依存graphと該当製品sourceが同じ範囲の既存成功結果を再利用し、追加変更だけを検証した。
適用完了。実行バイナリとCLI版は変更せず、commit/push/releaseは未実施。

### 後続のcommit・リリース指示

上記の取り込み完了後、利用者からcommit・リリース指示を受けた。
実装は `591b7621c6e6495be6286a2148b58bc3a575ed08` としてcommitし、
[v2.7.1リリースノート](../releases/v2.7.1.md)と
[公開検証記録](../releases/release-checkpoints.md)へ公開状況を記録する。
前節の「commit/push/release未実施」は取り込み作業完了時点の記録。
