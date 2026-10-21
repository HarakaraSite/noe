# v2実装計画：通常レビュー・批判的レビュー

> 後続の利用者承認により、破壊的変更を採用した[API再設計](v2-api-redesign.md)と[改訂レビュー](v2-api-redesign-review.md)へ移行した。本記録の合格/状態は記載した過去snapshotに対する当時の判定であり、現在の改訂へ適用しない。

2026-10-21。[実装設計・スライス計画](v2-agent-runtime-implementation-plan.md)を対象とする。状態：通常レビュー・批判的レビュー、指摘反映、限定再確認を完了。計画を実装へ渡せるとの判定。今回は計画段階で停止する。

この判定は本記録の対象hashに対する当時のもの。利用者の追加依頼による[NOE固有依存の分離レビュー](v2-agent-runtime-separation-review.md)で追加の設計事項を確認した。その修正と限定再確認の最新状態は同記録を参照する。本記録の当時の判定だけで実装開始を決めない。

## 依頼と対象

利用者は「複数スライスそれぞれで実装・テスト・コードレビュー、最後に総合テスト」となる設計を依頼し、**今回は計画・通常レビュー・批判的レビュー・反映後に停止**するよう指定した。実providerの必要時利用は後続作業に許可されているが、今回は使わない。

前回の構成案を確認した独立reviewer `v2_runtime_review`を再利用し、目的・権限・正本・評価段階を更新した入力を渡した。通常と批判的の両観点を合計30分以内、証拠更新10分なしなら中断、追加agentなしで依頼した。再確認は変更と既存findingだけを1回15分以内とした。

構成案で段階Aへ残した実行・設定・所有・終了・公開面を具体化し、実装へ渡せる計画かを評価する。通常レビューはsource整合・実現可能性・不足、批判的レビューは構成の必要性・移行負担・公開面・代替案・スライスの妥当性を確認した。repository全体の監査や実装試験ではない。

基点ソース：`c1006f1c1604194aaa9ca61bdb07aed2666702b5`。

| 対象 | SHA-256 |
| --- | --- |
| 初稿 | `244ef985bec1e6b5d684ffb39a43e65d4479dcc4166e3a9f2cd1567ec5bba7f7` |
| 指摘反映後 | `7ea2dcbf3418f02ad4d6536129de672853243f3c9faf53bb241e84fd62db698b` |

初稿・修正版・差分は`/var/tmp/noe-v2-plan-review-20261021/`に保存した。方針の正本は上記repository内の計画であり、一時的なレビュー入力を正本にしない。

## 通常レビューの指摘と親の採否

初回はP2の必須指摘3件。全て採用し、計画へ反映した。コードの不具合を修正したという記録ではなく、実装前に設計の不足を解消した記録である。

| ID | 根拠と影響 | 採用した修正 |
| --- | --- | --- |
| N1・P2：managed設定の優先順位 | 現行stdlib.Installは個別FS/Fetch/DNS/Run/Serve/Sockets.Loopを保持する。Core.Loopだけの検査では別runtimeのLoopを使い得る。Process.Exitも外部callbackを直接呼ぶ。Unhandledと出力も設定次第 | 全LoopとExit/Unhandledのmanaged所有を明示し、非nilは事前error、コピーだけへ所有値を束縛。agent Runtime templateのstdioは予約してcapture/空入力へ接続。低水準Installは従来どおり |
| N2・P2：同期stdioの取消し不足 | process.stdin.readはowner上のio.ReadAll、consoleは通常Write。Loop.Closeだけで通常pipeの待機/満杯出力を解除できず、終了手順が進まない | jsexecが評価前に通常Read/Writeを実行context付きhostio ReadContext/WriteContextへ結ぶadapterを導入。owned adapterと借用元のCloseを区別し、I/O error保存、pipe取消し、元fileの継続使用をS5へ追加 |
| N3・P2：child env除去の退行 | 旧Runnerは4つのAPIキー変数と古いNOE_AGENT_CONNECTIONを除去。NOE root envをそのままtemplate化すると以前は渡さなかった本文がtool/コマンドへ渡る | NOE task/tool profileで既存filterを維持し、ConnectionはGo引数へ。provider用EnvとJS用Envは分離。外部アプリの明示Envへ一律filterは追加しない |

修正後も、新しい公開Executor framework、stdio tee、custom credential storeは追加しない。managed fresh実行に必要な所有項目と入力条件を定め、caller所有runtimeへの低水準Installの自由度は維持する。

## 批判的レビューと任意提案

構成・8スライスの分け方への必須指摘はなかった。S3/S4を実行切替より前に置き、旧backendをS8で整理する順序、内部RPC再利用、公開Executor非導入は妥当との判定だった。

任意提案：exec.Commandの名前解決はchild Envとは別にhost PATHを参照するため、runtime別のPATH/PATHEXTも扱うべき。親は採用し、S3でcwd/envを使う共通内部resolverと2profileの混線検査を追加した。単にEnvをcmdへ渡せば名前解決も変わるとは扱わない。

親の追加整理として、NOE adapterの既定pathの遅延解決を内部resolverで行う方法も明記した。公開の明示設定経路には入れず、NOE互換のInstall等でHOME解決errorを新たに早期発生させない。

## 限定再確認

同じreviewerへ修正後hashと差分を渡し、N1〜N3と任意提案の解消、変更箇所の矛盾に限定して確認した。修正後SHA-256の一致を確認済み。

**通常レビュー：既存P2の3件は全て解消。追加の必須指摘なし。** 全Loop/Exit/Unhandledとstdioの所有、同期Read/Writeへのcontext接続、I/O errorの保持、NOE env filterの維持が具体化された。

**批判的レビュー：変更箇所に新しい矛盾や過剰な公開面は見つからない。** PATH提案とNOE既定pathの遅延resolverも責務分離に沿っている。

reviewerは変更箇所と既存findingに限定した再確認として、計画を実装へ渡せると判定した。親も採用する。製品動作や実装後のテストが成立済みという判定ではない。

## 今回の検証と停止範囲

確認済みのsource範囲は、必要なruntime・stdio・process・設定・instruction・provider/credential・bridge接続点。実装後の動作、全設定組合せ、各OSのnative動作、性能は未確認。reviewerはtest/build/API接続・変更を行っていない。

親は計画の相対リンク・hash・空白差分と、8スライスの実装/対象テスト/コードレビューおよび最終総合テストの記載を確認した。結果を反映したここで停止し、製品実装・試験・実provider接続・commit/push/releaseへ進まない。
