# NOE版stdlib：ステップ5・レビュー記録

2026-10-19。[要求受信streaming計画](stdlib-step5-receive-plan.md)と、その後の実装に対する独立した通常レビュー・再レビューの記録です。実装と総合確認まで完了し、最終未解消P0〜P3は0件です。[実装・検証記録](stdlib-step5-receive-implementation.md)も参照してください。

## 対象・基準

- 基点：v1.11.0公開後の`41e40d46d304a2f996e58459cd861d7d6a376d38`。
- 対象：ステップ5の計画。段階案との整合、需要駆動の受信と上限、Request/Responseの共有、HTTP/1のdrainとdeadline、入力cancel/応答/upgradeの生存期間、Go-only終了、shutdown・各実行経路と検証の実現性を確認しました。
- 担当：独立レビュアー`/root/stdlib_step5_plan_review`。会話履歴は継承せず、限定context packet・計画と指定sourceをread-onlyで確認しました。
- 対象外：実装・テスト実行、一般的な全二重保証、依存更新、ステップ6・7、provider呼出し、commit・push・release。
- 初回30分、根拠・中間結論が10分間なければpartialで中断。再レビューは変更箇所と既存指摘だけを各15分以内で確認。利用者の明示依頼に従い、P0/P1/P2未解消0まで修正・再レビューを反復しました。いずれも上限内に完了しています。

| 版 | SHA256 |
| --- | --- |
| 初回対象 | `c616874200aa5f4b89cb4ab2715b09257d408d0d175b000df7486c833123ec3f` |
| 修正後・再レビュー1対象 | `79c61859467e8c2f6e837849a48c1d852df4a545f5b43e233d15541b6bd8eb72` |
| 再修正後・再レビュー2対象 | `083146a037ff6e31b5e4c5a94a4bcabf0192b9f3f45ab763f16a7d2e689b9ec6` |

## 初回レビューと修正

初回はP0:0、P1:0、P2:1、P3:0。親は次の指摘を採用しました。対象hashはレビュー開始・終了時に一致しています。

| ID | 重要度 | 指摘と根拠 | 計画への反映 |
| --- | --- | --- | --- |
| RC-01 | P2 | Expect: 100-continueの入力と応答の順序が不足。Go 1.27.2 transfer.go:851のReadとserver.go:1433の初回flush/bodyRemainsが同じbody lockを取得するため、cancel済みReadをflush後まで残すと循環待ちになる。逆に初回Readより前に最終WriteHeaderするとserver.go:582のdisableWriteContinue(true)で入力が閉じ、Response(request.body)等の後続Readが失敗し得る | 明示100を本文read前に送信し、最終headersのflush完了後にResponse pumpを開始。cancel済みpending Readは初回flush前にdeadlineで解除し、局所的なr.Context取消しを応答停止と分離。独立pending readと応答の任意の全二重進行は非保証と明記。独立Expect clientによる初回Read遅延/Response本文転送/cancel後の有限・遅延応答と真の切断の試験、英文docsを追加 |

通常の受信需要駆動・prefix上限・Body共有・Go-only cleanup・server close・WebSocket所有権・各経路とスライスについて、追加P2以上は報告されませんでした。sourceの検討だけであり、実行上の成立は後の実装と試験で確認します。

## 再レビュー1と追加修正

対象hash一致を確認し、変更箇所とRC-01の解消だけを確認しました。明示100とcancel済みReadの先行解除は妥当とされましたが、RC-01は部分解消でP2が1件残りました。P0/P1/P3は0件です。

残存理由は、既定ReadableStreamがHWM 1でreader取得前からpullでき、sourceのstartも実行されるためです。serveの応答pumpを待機しても、利用者sourceが既に開始した入力readは止められません。これは単に「独立したread」と呼ぶだけでは、Response producer自身の先読みを含む保証範囲が不明確でした。

親は残存指摘を採用し、gateが止めるのはserve自身の需要だけと修正しました。ExpectのEOF前進行保証は`Response(request.body)`とHWM 0・startでreadせずpullで入力を読むproducerで成立させます。利用者sourceのstart/先行pullや独立readがpendingなら、初回flush前にbytes/EOF/cancelが必要というGo上の制約を明記しました。既定HWM 1の通常producerについても100後にclientがprefixを送ってreadを解放し、要求EOF前に応答prefixを得るcaseを別に確認します。任意の全二重保証や利用者source自体の全量待ちと、NOE bridgeによる全量bufferingを混同しません。英文docsと試験対象も揃えました。

## 再レビュー2

利用者のP2以上0まで反復という依頼に従い、変更箇所とRC-01残存の解消だけを15分以内で確認しました。追加の探索や範囲拡大は依頼していません。最終対象hashは一致しています。

レビュアーは、pumpが制御する需要と利用者sourceの先読みが区別され、保証範囲・Goのlock制約・独立clientによる検証が整合したとして、RC-01を解消と判定しました。最終の未解消は**P0:0、P1:0、P2:0、P3:0**です。

親は結果を採用し、計画は実装へ進める状態と判断しました。今回の変更は計画・この記録・開発index・段階案だけです。リンクと差分の静的確認を行い、source・API docs・例・依存・テストは変更していません。実装テスト・provider呼出し・commit・push・releaseは未実施です。実際の動作成立は、後の実装・コードレビュー・試験で確認します。

以上は計画レビュー時点の履歴です。その後の実装依頼に基づくコードレビューを以下に記録します。

## その後の独立コードレビュー

その後の実装依頼に基づき、S5-A〜Cの実装と関連テスト後、別の独立レビュアー`/root/stdlib_step5_code_review`へ通常コードレビューを依頼しました。会話履歴を継承せず、限定packet・承認計画・製品/テスト/docs/例14ファイルと必要な固定sourceをread-onlyで確認しました。初回30分、修正箇所と既存findingだけの再レビューは各15分を上限とし、すべて上限内に完了しました。

受信の需要駆動・上限・JS owner境界、取消しと遅延通知、入力と応答の寿命、HTTP/1 drain/flush/deadline、Expect、graceful/force終了、hijack、テストの根拠と英文docsを評価しました。初回はP0:0、P1:0、P2:1、P3:1。開始・終了時のmanifestは14/14一致しています。

| ID | 重要度 | 指摘・根拠 | 修正 |
| --- | --- | --- | --- |
| RC-C01 | P2 | serve_body.goがExpect値を完全一致で比較し、Go expectsContinue/hasTokenが受理する`100-continue, 100-continue`等を取りこぼす。明示100/ready gate/取消read先行解除が適用されず、最終WriteHeader先行時に入力を閉じてResponse(request.body)の応答をabortし得る | Goと同じHeader.Getのfirst-valueとASCII space/tab/comma境界でtoken判定。単一・カンマ反復・他tokenと大文字混在の3形式×direct/HWM0/HWM1/cancelの12 raw HTTP caseを確認 |
| RC-C02 | P3 | bounded read単体はGo helper直接呼出しで、JS需要とnative readの対応を直接計数していない | 実際のserveJSのdispatchとGo Promise bridgeを使い、owner checkpoint barrierで無需要0 read・1 pull1 read・32 KiB bufferを計数する試験を追加 |

親は両方を採用しました。加えてcaptured listenerをforce/Shutdown成功後に明示closeし、Serve goroutineによるlistener登録前の終了でもportを同期的に解放します。再レビュー1の変更範囲はserve.go・serve_body.go・serve_body_test.goの3ファイルだけです。重点テストとraceを確認しました。

再レビュー1でP0/P1/P2は0件となり、Expectの修正とlistener解放は確認されました。RC-C02は、同期checkpointがsource初期化のmicrotaskやGo read goroutineより先行し得るとしてP3の部分解消でした。親は計測を追加修正し、JS handlerがPromise.resolveをawaitして先に登録された初期microtaskを進め、raw.readから元のGo bridgeを呼ぶ直前に同期atomic計数を取ります。これによりGo goroutine未実行による偽の0を避け、native callと実IO両方の0/1を確認します。修正した単独race試験が成功しました。

再レビュー2は変更した計数テストと既存RC-C02だけを対象にしました。RC-C02も解消とされ、最終未解消は**P0:0、P1:0、P2:0、P3:0**です。各回の開始・終了とも対象manifestは14/14一致しました。親は結果を採用し、その後の総合テストへ進めました。

ファイル別の初回・修正後manifestは、一時検証先`/tmp/noe-stdlib-step5-20261019-baecbtxg/`のreviewed-files-initial.json・reviewed-files-final.jsonへ保存しています。上の計画hashは計画レビュー時点の履歴として保持します。

## 総合確認で見つかった終了テストの修正

総合raceで、新規Worker終了テストが「最初のReadでbytesが返った」だけで接続が残ったと判定しました。Go HTTPが終了前にbufferへ書いた有限応答bytesが届くことは、期限内の接続終了と両立します。受入条件はbytesが一切届かないことではなく、待機IOと接続が終了しportを再利用できることです。

親は`internal/runtime/http_receive_test.go`の終了確認helperだけを修正し、既存2秒deadline内でbuffer済み応答をdrainしてEOF/transport切断を確認するようにしました。timeoutは引き続き失敗とし、port再bindも維持します。製品コードは変更していません。重点race試験が成功した後、同じレビュアーが変更helperと終了要件だけを15分以内でread-only確認しました。

この限定再確認もP0:0、P1:0、P2:0、P3:0。対象manifestは開始・終了とも14/14一致し、その後の最終総合確認へ反映しました。`GOTMPDIR`の容量/Unix socket長制限による環境上の失敗と回復は、製品・テストのfindingとは分けて実装記録へ残します。
