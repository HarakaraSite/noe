# queueで同じagentの会話を継続する修正

2026-10-18、利用者が修正を依頼。v1.6.0の「queueごとに新規履歴」は意図と異なるため修正する。初期依頼はlocal修正までとし、後続依頼でcommit・v1.6.1 releaseが許可された。既存releaseタグは変更しない。

## 契約

- 同一instanceのqueueは、前の作業の会話・完了済みtool往復・最終回答を引き継いで続ける。
- 次の指示は実行を開始する順番が来た時点でuser messageとして追加し、最初の指示・適用済みsteerと同様に以後の切り捨てから保護する。待機中の予約は現在のtaskへ混ぜない。
- roleと時系列を維持し、固定部分を会話の先頭へ移動しない。固定入力が増えた分だけ可変履歴予算を再計算する。
- 切り捨てたblockを復活させず、残っている履歴・削減回数・直近の削減事実を継続する。providerのreasoning・署名・native itemを加工しない。
- 別instance・子・孫は独立した会話。各queue taskのrun ID、maxSteps、取消しscopeは従来どおり別とする。共有execution limitsの集計は維持する。既存cacheKey/session設定は変更しない。

## 保持と確定の境界

現在のtaskプロセス分離と停止・子孫回収は維持する。そのため、現在有効な会話の確定snapshotをGoのinstanceへ保持し、次taskの認証済みstart応答で渡す。ファイル・argv・env・公開observe payloadへ本文を追加しない。JSは実行中のworking copyを持つ。

保持するのは削減後の会話だけで、過去全文の別archiveは作らない。trim候補作成だけでは確定snapshotを置換せず、既存の候補commit時に原子的に置換する。終了instanceから会話本文を解放する。

最初の実行で組込み／共通／workspace／明示systemを一度構成する。同じ会話のqueueではそのsystem本文を保持し、instructionファイルを再読込みして差し替えない。新instanceの最初の実行では新しく読む。idle起動は読込みを行わない。最初の読込み失敗で会話がまだなければ、次の予約時に再度初期化できる。

保存境界は次のとおり。

1. startで初期systemと実行開始した指示、または既存会話と新しいqueue指示を確定する。
2. context準備・trim commitで保護entryと最新の削減状態を確定する。
3. 全tool結果が揃った完了blockを確定する。途中のassistant/toolの一部だけは次へ渡さない。
4. steer適用確認と保護entryの保存を同じ受付で行う。command IDの重複判定はrun内に限定し、別runの同名IDを捨てない。
5. 最終回答受付とproviderの最終assistant objectの保存を同時に行う。`committed:false`でsteerを返す場合も、完了した候補objectは応答前に保存する。steerは続く適用確認で保護entryとして確定する。

trim回数・直近trim・未送信通知は会話scopeとし、未送信通知と原因IDを実provider送信時に消費した事実もinstanceへ反映する。`pendingPlan`・prepared digest・runtime情報・modelStep ID・providerCalls等のrun/step状態は継承しない。これら2点は独立した設計レビューで明確化した。

失敗・run単位interrupt後も、最後に確定した完全な履歴から次queueへ進める。実行途中で取消されたtool callや部分結果は引き継がない。受け付けただけの未適用steer、実行前に取り消されたqueue指示は保護履歴へ入れない。instance全体のinterruptはqueueも回収して会話本文を解放する。

## 実装と確認

1. privateな会話snapshotの復元・検証、instance所有の保存、startでの再開を実装。
2. context/steer/tool完了/最終回答の確定経路を接続し、run境界を越える削減状態を保持。
3. 旧「新規履歴」前提のfixtureを新契約へ更新し、継続・queue予約の非混入・固定指示保護・削減済み履歴非復活・最終回答の全field・取消し・別agent分離を確認。
4. README/API/instructions/hooks/observation資料とモデル向け説明を更新。過去release notesの旧仕様説明は履歴として維持する。
5. コードレビューの必須指摘を解消し、unit/E2E/vetと関連raceで総合確認する。既存process・hook・control・instruction loadingの回帰を含む。

## 完了記録

2026-10-18、上記1〜5を実施。[実装・検証記録](queue-continuation-implementation.md)を参照。
