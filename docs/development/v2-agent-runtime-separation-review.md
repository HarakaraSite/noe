# v2実装計画：NOE固有依存の分離に関する追加批判的レビュー

> 後続の利用者承認により、破壊的変更を採用した[API再設計](v2-api-redesign.md)と[改訂レビュー](v2-api-redesign-review.md)へ移行した。本記録の合格/状態は記載した過去snapshotに対する当時の判定であり、現在の改訂へ適用しない。

2026-10-21。対象は[具体設計・実装スライス](v2-agent-runtime-implementation-plan.md)。状態：追加批判的レビュー・docs確認・設計修正・同じreviewerによる限定再確認を完了。残る必須指摘なし。計画を実装へ渡せると判定し、計画段階で停止する。

その後の利用者の追加依頼で[責務・経路台帳](v2-responsibility-boundaries.md)を作成し、通常/hookのログ観察、control既定値、各層の所属と受入を追加具体化した。この追加差分のレビューも完了し、必須指摘なし。上記と本文の過去の合格判定は、それぞれ記載したsnapshotの対象範囲を指す。

利用者から「パスは利用者が気づいた一例であり、他にも注意して分離すべき箇所がないか」という追加の批判的レビューを明示依頼された。[前回レビュー](v2-agent-runtime-implementation-review.md)の既存指摘の解消確認とは別の観点である。

## 依頼範囲

外部Goアプリの利用者から見て、NOEコマンドに残す方針、stdlibの汎用機構、agentlibの機構、アプリ側で選ぶべき設定の境界が成立するかを評価する。

path以外にもprocess全体の状態・初期化・signal・stdio・所有と終了、provider/HTTP/モデル選択、CLIの表示/保存/control、module/API/builtin説明、複数hostの共用を確認対象の候補とする。全repo監査や機能追加の網羅表を目的にしない。必須指摘には具体的なsource経路と利用者影響、最小の分離案を必要とする。

新しい独立reviewer `v2_separation_review` 1体へ、会話履歴を継承せず目的・権限・正本を渡した。前回の妥当判定を前提にしない視点を得るための起動で、追加agentは使わない。上限30分、証拠更新10分なしなら暫定返却、必要な再確認は変更と既存findingのみ1回15分以内とした。

コード・文書の変更、test/build、実provider接続、外部書込みはreviewerの対象外。親が採否と正本の更新を行う。製品実装・実provider利用・commit/push/releaseへは進めない。

初回対象SHA-256：`7ea2dcbf3418f02ad4d6536129de672853243f3c9faf53bb241e84fd62db698b`。基点ソース：`c1006f1c1604194aaa9ca61bdb07aed2666702b5`。対象snapshotは`/var/tmp/noe-v2-plan-review-20261021/separation-initial-plan.md`に保存した。

## 結果と反映

初回結果は、必須P2が2件、別途の設計判断が1件。親は以下のsource経路を確認し、2件を計画の不足として採用する。実装前に境界を再設計する必要があり、前回の「計画を実装へ渡せる」という判定をそのまま維持しない。

| ID・分類 | 根拠と具体的な影響 | 親の判断・最小対応案 |
| --- | --- | --- |
| R1・P2：agent所有のGo I/O | [`agentbridge`](../../internal/agentbridge/bridge.go)はnoe:imageを導入し、[`read_image`](../../internal/agent/images.go)はimageapi.ReadFileからOS fileへ、[`generate_image`](../../internal/agent/images_generate.go)はSaveFileからOSへの保存へ進む。[`observe(logPath)`](../../internal/observation/log.go)も独自にos.Openする。Runtime.StdlibのFS=nil/Root限定と別経路である | 採用。Runtime.Stdlibだけで全agentのI/Oを選べるとする説明は不十分。agent所有のGo I/Oについて、アプリが明示する設定/adapterで選ぶか、別権限として導入する契約にするかを具体化する。NOE側には既存のOS実装を渡す。S6/S7へ設定とconsumer受入を追加する必要がある |
| R2・P2：builtinにNOEの運用・表示方針が混在 | [`instructions.md`](../../internal/agent/instructions.md)には機構説明に加え、人への入力/承認要求をしない既定、run_js先頭の英語コメント、decisionの自律選択等がある。英語コメントは[`CLI描画`](../../internal/cli/observe_render.go)でも利用される。config.systemはinstance別の追記で子へ自動継承されないため、アプリ全体の既定方針を差し替える入口にならない | 採用。API/実行環境の事実と、行動・表示上の既定方針を分ける。NOE既定はNOE profileから供給し、外部アプリは自分の方針を供給できる形をS1/S7で具体化する。新しいcallbackの導入が必須という指摘ではない |
| D1・設計判断：supervisorとアプリ初期化 | [`process_posix.go`](../../internal/process/process_posix.go)のStartはconsumer binary自体をexecし、process.initでprivate supervisorへ入る。その前に実行される依存packageのinitは再実行され得る。固定port/PID lock等を扱う初期化があれば、mainを避けても監督入口より前に失敗/待機し得る | S4前に判断する。「mainを再実行しない」だけでは受入条件が不足する。自己再起動を許容する明示的な対応条件か、アプリ初期化を再実行しないhelper境界を選ぶ。自己再起動を一律禁止する提案ではない。実行再現はまだ行っていない |

R1はRuntime.StdlibのAPI導入設定と、agentのGo処理が使う資源設定の区別である。新しい汎用権限frameworkやNOE既存機能の一律制限を要求するものではない。R2も事実説明や必要なtool契約をなくす提案ではなく、アプリ固有の運用方針を機構から分ける提案である。

### 確認済みの境界

| 境界 | 確認結果 |
| --- | --- |
| cwd/env・stdio・Loop/Exit | 既存計画に明示あり |
| instruction/provider認証の探索 | NOE adapterとの分離あり |
| root/client Host・会話・取消し | 所有境界の計画あり |
| signal共有 | 購読ごとの解除を使い、他のGo登録をResetしない。確認範囲で追加必須指摘なし |
| catalog/model・HTTP注入・control | 確認範囲で追加必須指摘なし |

## 利用者の追加質問：docs

利用者から「docsの取り扱いは大丈夫？」と追加質問があったため、同じreviewerへdocsに限定した確認を依頼した。上限10分、source参照だけで、API接続/test/build/変更は行わない。

参照機構とCLI表示の依存、NOE executableなしでの案内、Docs=false等のprofile、実行方式を説明する本文を確認する。現行docsは全APIカタログであり、導入済みcapabilityの一覧ではないという既存契約を維持する。profileごとの全ページ生成や非有効APIの自動非表示を当然の要件にしない。

**参照機構の分離は成立しているが、案内と内容の条件は補足が必要だった。**

- CLIとJS adapterは同じ[`apidocs.Lookup`](../../internal/apidocs/catalog.go)を使う。内容正本はembedされたcontentとmetadataで、検索機構にCLI・agent・provider/runtime依存はない。stdlibへagent SDK依存を持ち込まない。
- **R2へのP2追記：** `Docs`はzero=falseで、true時だけnoe:docsを導入する。一方でbuiltinとtaskのtool説明は常時docs参照を指示する。rootのDocs=trueは子のRuntime.Stdlib.Docs=trueを意味しない。false時は存在しない参照先を必須案内せず、勝手にDocsを有効化しない条件を具体化する必要があった。
- [`noe-run-js`](../../internal/apidocs/content/noe-run-js.md)のfresh process/temporary source説明、topic metadata、NOE概要のOS権限説明、catalog/errorのCLI案内もS7の更新対象へ明記する。利用ガイドだけの更新では足りない。CLI例自体はCLI用と明記して維持できる。
- 全APIカタログと導入済み能力一覧の区別は維持する。profile別の全ページ生成や非導入API文書の削除は不要。

親は採用し、実装計画§3へdocsの共通正本、root/子それぞれの有効化選択、false時の案内、本文/metadata/error案内の更新、S7のtrue/false consumer受入を追記した。R2全体の運用方針分離まで解消したとは扱わない。

## 初回の反映と停止時点

計画へ追加レビューの要修正状態と§10の未解決事項を明記し、READMEおよび前回レビューから最新結果を参照できるようにした。現在のAPI案をそのまま実装可能と扱わない。

| 残る事項 | 状態 |
| --- | --- |
| R1：agent所有Go I/O | 指摘採用。設定/adapterか別権限契約かの具体設計が必要 |
| R2：builtinのNOE運用方針 | 指摘採用。docs条件は計画へ反映、アプリ全体の方針供給・合成・子への適用は再設計が必要 |
| D1：supervisor初期化 | 対応条件またはhelper境界の判断が必要。実行再現は未実施 |

reviewerが静的確認した対象は初回hashの計画と必要なsource。親の反映後hashは`bf403d3c1d4a1eb17de50d635752e7bab8fc8be461380d137d39bdd4b4606d97`。snapshotと差分は`/var/tmp/noe-v2-plan-review-20261021/separation-reflected-plan.md`と`separation-reflection.diff`へ保存した。この反映後に技術的な未決事項を解消したとして再承認を受けたわけではない。

親は参照リンク、差分の空白、snapshot hashを確認した。今回は追加の批判的レビューと記録・計画への注記までで停止する。

## 利用者の修正依頼への対応

続く利用者の「修正して」を受け、製品実装ではなく計画の不足を修正した。8スライスと最終総合テストを維持し、各変更を実装・対象テスト・独立レビューの対象へ割り当てた。

| 項目 | 採用した設計 |
| --- | --- |
| R1 | agent.Config.Filesで画像・logPathのOpenFile/SaveImageを明示する。nilは対象file操作なし。NOEは現行OS実装のOSFilesを渡す。JSのFSから補完せず、保存callback不足は生成provider呼出し前に検出する。credential/instruction等の別設定との境界も明記した |
| R2 | 共通のAPI/実行環境の事実と、NOEの運用方針を分離する。Config.PolicyをHost全体へ固定し、事実→Policy→dirs→instance.systemで合成する。子へHost方針を一度適用し、親固有systemはコピーしない。英語コメントの条件はNOE profileへ移す |
| docs | 共通content/metadata/検索機構を維持する。Docs=falseに必須案内せず、root/子の別設定を尊重し、本文・metadata・error案内もS7で更新する。カタログを導入済みAPI一覧とは扱わない |
| D1 | stdlibの既定は直接起動＋owned processscopeとし、consumer自身を暗黙に再起動しない。NOEは既存のowner死亡時回収を明示helperで維持する。library initの引数捕捉をなくし、mainから明示dispatcherを呼ぶ。self-helperの再入init条件、別helperの選択、直接方式ではPOSIX host SIGKILL時の回収を保証しない限界を明記した |

限定再確認の対象SHA-256：`2a0dbb4df5811f91d6301e9f06d6333b96824b58146e508fa0fe60fe5e865a15`。snapshotは`/var/tmp/noe-v2-plan-review-20261021/separation-fixed-plan.md`、差分は`separation-fix.diff`。入力は`/tmp/codex-agent-context/v2-separation-review/fix-review.md`。

前回のreviewerを再利用し、R1/R2/D1とdocs、および変更箇所の矛盾に限定した再確認を依頼した。1回15分以内、read-onlyで、追加agent・test/build・API接続は行っていない。reviewerは対象hashの一致を確認した。

### 限定再確認の結果と親の判断

**残る必須指摘なし。今回の対象範囲では計画を実装へ渡せる。**

| 対象 | reviewerの判定 |
| --- | --- |
| R1 | Filesの対象・nil時・所有/取消し・生成前の保存設定検査・NOEのOSFilesが明記され、計画上解消 |
| R2 | 事実とPolicyの分離、合成順、子への適用、二重挿入防止、空Policyの意味が明確となり、計画上解消 |
| docs | 共通正本、root/子別の選択、false時の案内、本文/metadata/error更新と全カタログ契約を扱い、解消 |
| D1 | 直接方式と明示helper、library init捕捉の廃止、host SIGKILL時の保証差、self-helper初期化条件と別helperの受入が明示され、設計判断として成立 |

reviewerは公開面が確認された利用経路に限定され、汎用権限frameworkへ広がっていないこと、S1/S4/S6/S7の成果物・受入へ対応していることも確認した。親はこの判定を採用する。実装・OS上の動作の成功を保証する判定ではなく、各スライスの実装/テスト/コードレビューと最後の総合テストは未実施である。

再確認後の計画の変更は、冒頭と§10の状態を「限定再確認完了」へ更新した2箇所だけ。技術的な契約は再確認対象から変更していない。最終計画SHA-256：`5c4280b77bc2f4e5794164787f5b3c3c114801084ccef5a636ff8c626f2a5466`。最終snapshotは同一保存先の`separation-final-plan.md`、状態更新差分は`separation-final-status.diff`。親は文書リンク・空白差分・snapshot/hashを確認し、今回は計画修正と結果保存までで停止する。

## 責務・設定・所有・経路による追加具体化

利用者から「本当にnoe/noestdlib/noeagentlibを分離する観点で責務を整理できたか」、続けて「その観点で計画をもっと詰めよう」と依頼された。親が[責務・経路台帳](v2-responsibility-boundaries.md)を作成し、以下を実装計画と構成案へ反映した。

- noestdlibをagentlibを含む総称として使わず、3者と共用の下位部品の所属・依存方向を明示。同一Go module内の分離と、別moduleでの独立配布を区別した。
- 機能ごとの実装主体・設定選択者・資源所有と、通常/子/hook/CLIの入口から到達先までを経路IDで対応付けた。S1〜S8の成果物・受入・レビューと実装記録へIDを接続した。
- 現行の通常ログ観察、hookのobservation直呼出し、CLI structured observeを確認。画像はroot RPC、ログはbinding側の明示openerに分け、Filesを内部Go引数でtask/toolへ渡す。保存済みログ観察のためにbrokerを新設しない。
- controlのDefaultControlDirとNOE環境規則、public/private Install分岐も移行対象として明記。NOE profileをinternal/noeprofileへ集約し、controlの既定場所は初broker使用まで解決を遅らせる。

今回の追加要求に対する初回レビューとして、既存reviewerを再利用した。対象は新規台帳と構成案/実装計画の追加差分だけ。15分以内、read-only、追加agent・変更・test/build・API接続は禁止。前回findingの無制限な再確認や全repo監査ではない。親が採否と最終記録を担当する。

入力は`/tmp/codex-agent-context/v2-responsibility-review/context.md`。snapshotは`/var/tmp/noe-v2-plan-review-20261021/responsibility-{outline,plan,boundaries}.md`、差分は`responsibility-changes.diff`。

| 追加レビュー対象 | SHA-256 |
| --- | --- |
| 構成案 | `12071004e7b99d6240e84f737a4c911acdb9f1ed509aec9a144947a2612c92c3` |
| 実装計画 | `17a265ac89407432c8e6a65ad27f3f5a46d131ef8d89a9e854dc0719c207364f` |
| 責務・経路台帳 | `b7d1b35d3b25810606f1a623092e14d15a26069998aa99e05846ea1dd4bd188d` |

### 追加差分のレビュー結果

**必須指摘なし。計画として実装へ渡せる。** reviewerは上記3文書のhash一致を確認した。

| 確認対象 | 判定 |
| --- | --- |
| L1/L2/L3 | 通常/hookの明示opener、context・受付gate・join、CLIのI/Oの区別は整合している |
| Filesの注入/借用 | Install時のfieldコピー、内部Go引数による子への供給、借用が明確。画像はroot RPC、ログはbinding側という現行経路に合う |
| control既定値 | DefaultControlDir/broker/runtime/CLIの全現在入口を移行対象に含め、noeprofileへ探索規則を集約。libraryからの逆依存は不要 |
| 共用部品/docs | apidocs/eventsの役割と文書保守の所属が整理され、CLI/agent実行への逆依存を要求していない |
| スライス/受入 | S1〜S8へ経路ID・撤去・受入を対応付け、S8で旧経路の残存を確認する構成となっている |

親は判定を採用した。追加の技術修正や再レビューは不要。受領後は状態の更新と、構成案の過去の段階Aへの引継ぎを現在の実装計画へ参照する説明に直しただけで、技術契約は変更していない。

最終snapshotは同じ保存先の`responsibility-final-{outline,plan,boundaries}.md`、レビュー後の説明/状態差分は`responsibility-final-status.diff`。最終SHA-256は以下。

| 文書 | 最終SHA-256 |
| --- | --- |
| outline | `6f8f6356f5be71a6806a6900fb0b58f4a0da4143c22fe957dd5ffd8bedd609c7` |
| plan | `4ed8ce121eba62d384ffb29a0d2845bcdd5f9b53b56009ff6b025adf3c69678e` |
| boundaries | `75c40351023fe35e4c7823f774361349da75616ee1d21e22f37b81d17af8d075` |

親は計画上の対応付け、source参照、文書リンク・経路ID・空白差分・snapshot/hashを確認した。製品動作の分離を確認済みとは扱わず、計画修正と記録までで停止する。

## 確認の限界

今回はsourceと設計の照合であり、全API監査、全設定組合せ、OS実機、helperの初期化副作用の実行再現は行っていない。製品実装の変更、実provider接続、commit/push/releaseも行っていない。実装後の受入は各スライスと総合テストで確認する計画である。
