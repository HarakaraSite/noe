# NOE版stdlib：ステップ4・レビュー記録

2026-10-19。[要求送信streaming計画](stdlib-step4-upload-plan.md)と、その後の実装に対する独立した通常レビュー・再レビューを記録します。

## 対象・方法・基準

- 基点：ステップ3のコミット`0de9351072ceaaadae7063a30ab52448e93b1d58`。
- 対象：`stdlib-step4-upload-plan.md`。段階案への適合、Request入力と本文所有権、stream upload、Go HTTPのredirect/早期応答、owner境界・背圧・取消し・終了、nil Loopの維持、docsと検証スライスの実現性を評価した。
- 担当：独立レビュアー`/root/stdlib_step4_plan_review`。会話履歴を継承せず、親が作成した限定context packet、計画、指定した関連sourceとGo toolchainのHTTP/pipe周辺だけをread-onlyで確認した。
- 対象外：実装、受信streaming、全二重の保証、依存更新、公開Go package、実プロバイダ呼出し、commit・push・release。
- 時間上限：初回30分、新しい証拠・中間結論が10分ない場合は中断。再レビューは変更箇所と既存指摘の解消だけを15分以内で確認。両方とも上限内に完了した。
- 解消条件：利用者の明示依頼に従い、P0/P1/P2の未解消指摘が0になるまで修正・再レビューする。P3は任意改善として区別する。

| 版 | SHA256 |
| --- | --- |
| 初回レビュー対象 | `533a62d9f2a19964277ddfbcfd8f5f49d8e79fbde0e29ce0fc8fe8d4e67d10d1` |
| 修正後・再レビュー対象 | `64383c2bc5c7805c35de82ba3932688d10bdcd8b5ab9f56872bf234a1bab24c5` |

## 初回レビューと修正

初回はP0・P1が0件、P2が1件、P3が1件でした。親は両方を採用し、計画を修正しました。

| ID | 重要度 | 指摘・根拠 | 計画への反映 |
| --- | --- | --- | --- |
| UP-01 | P2 | 早期Responseでもpipe両端を閉じる方針では、PipeReader.ReadがEOFではなくErrClosedPipeになる。HTTP/1 Transportが本文読取り失敗で接続を閉じ、受信中のResponse本文を壊す。Go 1.27.2のio/pipe.goのreadCloseErrorとnet/http/transport.goのwriteLoopで確認 | 正常EOF・意図した本文放棄はwriterを正常closeし、readerはTransportのBody.Closeへ委ねる。実失敗・全体取消しの両端closeと分離。停止に伴うwriteエラーをproducer失敗へ昇格させない。HTTP/1で応答本文後半をupload停止後まで遅らせるbarrier試験を追加 |
| UP-02 | P3 | 通常のGo HTTP serverは応答前に未読要求をauto-drainするため、開いたproducerを使うearly-response試験自体がEOF待ちになり得る。net/http/server.goの応答前drain経路で確認 | fixtureでResponseController.EnableFullDuplexと明示flushを使うことを具体化。これは試験側の設定で、NOEの全二重API追加ではないと明記 |

そのほか、Requestの移譲・override・clone、nil Loop、backpressure、Loop.Post破棄後のGo-only cleanup、runtime/Workerの終了経路に追加P2以上はありませんでした。301/302/303はGo clientが次hop構築前に元Body.Closeを呼ぶため、計画の通知経路で扱えることも確認されました。

## 再レビューと判定

修正後hashの一致を確認したうえで、早期応答の終了方針・内部cleanup・S4-Bのfixture/遅延応答試験・検証表と、初回2件の解消だけを再レビューしました。UP-01・UP-02とも解消と判定され、**未解消P0・P1・P2・P3はすべて0件**でした。

親は結果を採用し、計画は実装へ進める状態と判断しました。計画作成中の変更は計画・この記録・開発index・段階案への導線だけです。source、内蔵API docs、例、テスト、依存は変更せず、テスト実行・実プロバイダ呼出しもしていません。

以上は計画レビュー時点の結果です。その時点では実装未着手であり、実行時の成立はその後の実装・コードレビューと試験で確認する範囲としました。

## その後の独立コードレビュー

計画後の実装依頼に基づき、S4-A〜Cの実装と関連テストを行った後、別の独立レビュアー`/root/stdlib_step4_code_review`へ通常コードレビューを依頼しました。会話履歴を継承せず、限定packet・承認計画・実装/テスト/docs差分と関連sourceをread-onlyで確認しています。時間上限は初回30分、変更箇所と既存指摘だけの再レビュー15分で、両方とも上限内に完了しました。

対象はfetch.go・新しいfetch_upload.go・Streamsの消費状態、stdlib単体テスト、runtime/Worker/fixture agent/CLI E2E、英文API docsとHTTP例です。EOF前到達・背圧・Request所有権、redirect・早期応答・Go/JS owner境界・取消し/終了回収を評価しました。受信streaming、全二重保証、依存更新や公開Go package化は対象外です。

初回はP0:0、P1:0、P2:1、P3:0でした。

| ID | 重要度 | 指摘・根拠 | 修正 |
| --- | --- | --- | --- |
| UP-C01 | P2 | fetch(Request, {signal: preAbortedSignal})や、継承signalがabort済みのfetch(Request, {})は、pre-abort判定前にRequestを構築し、本文移譲で元streamをlockする。reject後もused/locked状態が残り、送信していない本文を再利用・cancelできない。レビュアーが一時CLIで両経路を再現 | init override→Request継承から有効signalを選び、Request構築より前にpre-abort判定を移動。direct/継承＋空init/上書きsignalの3経路でused・locked・pull・cancel・Allowが変わらない回帰テストを追加 |

親はUP-C01を採用して修正し、関連再テストが成功しました。再レビューはfetch.goとfetch_upload_test.goの変更、既存findingの解消とhash一致だけを確認し、**未解消P0〜P3はすべて0件**と判定しました。早期Responseをwriter正常EOFで保つこと、停止後のwriteエラーを失敗へ昇格させないこと、owner上のbytes copy、reader解放とGo-only cleanupにも追加指摘はありません。

初回・修正後のファイル別manifestは実装記録にあるartifact内のreviewed-files-initial.json・reviewed-files-final.jsonへ保存しています。計画レビュー時の上記hashは、当時の計画本文のhashとして保持します。レビュー後の全体テスト・race・E2E・6対象compile・実接続の結果は[実装・検証記録](stdlib-step4-upload-implementation.md)へ記録します。
