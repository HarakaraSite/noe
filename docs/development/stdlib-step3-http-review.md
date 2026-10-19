# NOE版stdlib：ステップ3・レビュー記録

2026-10-19。[noe:http入口計画](stdlib-step3-http-plan.md)と、その後の実装の独立した通常レビューを記録します。

## 対象・評価基準・方法

- 対象：`stdlib-step3-http-plan.md`。基点は`ec6391af316d2a7144964b01da4b848751b7ea14`。
- レビュー対象計画のSHA256：`e2f99a38dd80084b8d9bc8579fa23fdc641b39cc1c3e260ba8d88a7fe1532ecc`。
- 担当：独立レビュアー`/root/stdlib_step3_plan_review`。親の会話履歴を継承せず、計画・段階案・指定された関連sourceとテスト構造をread-onlyで確認した。
- 評価基準：目的と段階案への適合、2関数の公開契約、設定matrixと権限の維持、stdlib配置・Install・main/Worker/agentの接続、HTTP/Web型/Streams/WebSocket/終了の共有、docs・旧IDの維持、後続との境界、スライスと受入条件の実現性。
- 対象外：実装、要求streaming、HTTP一般修正、外部Goライブラリ化、依存更新、release。
- 時間上限：初回30分、新しい根拠・中間結論が10分ない場合は中断。再レビューは変更箇所と既存指摘の解消を15分以内で確認する。
- 解消条件：利用者の明示依頼に従い、P0/P1/P2の未解消指摘が0になるまで修正・再レビューする。P3は任意改善として区別する。

## 初回レビュー結果

P0・P1・P2・P3の指摘はいずれも0件。初回で解消条件を満たし、計画への修正要求がなかったため修正・再レビューは行っていません。

| 観点 | 確認結果 |
| --- | --- |
| 登録とWorker | Install終端の共通helperからmain・Worker・孫Workerへ導入可能。既存bindLoopが通信設定を子のloopへ結び直す |
| 設定と既存入口 | Fetch/Serveの有無によるexportのmatrixは現在の権限境界を維持できる。同一の関数値を公開し、二重導入を避けられる |
| HTTPと寿命 | buffering、timeout、32 MiBの扱い、同期closeの説明が現行sourceと整合する |
| Web型とWebSocket | serveの同一値・同じServe設定により、型・upgrade・終了経路を共有できる |
| module解決 | 既存noe:予約とnative優先解決を利用でき、Node形式26名の一覧変更は不要 |
| docsと検証 | 1 topic・2 detail追加と既存IDの維持、CLI/Worker/run_jsの確認が現在のcatalogとテスト構造に沿う |

## 判定と限界

親agentはレビュー結果を採用し、計画は実装へ進める状態と判断しました。未解消のP2以上の指摘は0件です。初版は既存fetch/serveの公開入口を追加する範囲で、要求送信・受信のstreamingはステップ4・5へ残します。

これは実装前の静的レビューです。source変更、テスト実行、実プロバイダ呼出しは行っていません。実装後のAPI同一性・設定の維持・HTTP往復・資源回収・docs・配布対象での成立は、計画にある実装テスト・コードレビュー・総合検証で確認する事項です。

計画レビュー時点では計画とレビュー記録・索引だけを作成し、実装は未着手でした。以下は、その後の利用者の実装依頼に基づくコードレビューです。

## 実装後の独立した通常コードレビュー

関連テスト・重点E2E・変更前後のAPI比較の後、`/root/stdlib_step3_code_review`が親の会話履歴を継承せずread-onlyで確認しました。対象は登録helper・Install接続・各実行mode/設定/Allow/終了のテスト、内蔵英文docs、現行ガイドと例の24ファイルです。初回manifestのSHA256は`4165a1529fec90584d40e103fc67dd2f46219ce3f2c067fa732e6e929d503297`。初回30分、新しい根拠・中間結論が10分なければ中断する条件で依頼しました。

評価基準は、2 named exportsと同一性、設定matrixと既存権限、main/Worker/agent接続、Web型・Streams・WebSocket・closeの共有、docsの正確性と既存ID維持、検証対象の妥当性です。要求streaming、HTTP一般修正、外部library化、依存更新・releaseは対象外としました。

初回結果はP0:0、P1:0、P2:1、P3:0でした。

| 指摘 | 根拠・影響 | 対応 |
| --- | --- | --- |
| P2：fetchの本文上限超過をread失敗と説明 | fetch.goはloopありで上限到達を正常EOFとし、loopなしでもLimitReaderのprefixを成功返却する。既存TestFetchBodyLimitも切詰めを期待する。利用者が32 MiB超をrejectで検出できると誤認する | noe-http.mdのfetch詳細を、上限までのprefixを返し超過自体ではrejectしない説明へ修正。HTTP本体は維持 |

同一性、設定4通り、Allow、native解決、Worker・孫Worker・Web Worker、agentが新sourceからのCLIを使う経路、Streams・WebSocket・close共有には追加指摘がありませんでした。API inventoryと関連成功ログも照合しています。

親の自己確認で挙げたabort testのchunk境界については、一般のbyte streamで1回のreadが送信単位と一致する保証はないものの、現fixtureでの具体的な誤失敗は確認できずfindingへは含めないとの評価でした。局所改善として、5文字分を蓄積してから照合する形へ変更し、abortによるread拒否とserver側取消しの確認は維持しました。

## 限定再レビュー

同じ独立レビュアーが、上のdocs・abort testの2ファイル差分とP2解消だけを対象に15分以内のread-only再レビューを行いました。関連するabort・catalogテストの修正後成功証跡も確認しています。

再レビュー結果はP0〜P3のすべてが0件。本文上限の説明が実装に一致し、chunk境界への依存解消が確認条件を弱めていないと判定しました。P2以上の未解消指摘は0件で、親は総合検証へ進める判断を採用しました。

最終24ファイルmanifestのSHA256は`43db74b32d733b7b93d46d6126f5812f8c197baed9be551c8f2c602e2c8dc561`。証跡は`/tmp/noe-stdlib-step3-20261019-84fpyjw_/`の`reviewed-files.json`・`reviewed-files-initial.json`と各テストログです。ファイル別hashと総合結果は実装・検証記録に保存します。

コードレビュー中に編集・テスト再実行・実プロバイダ呼出しは行っていません。全体テスト・race・full E2E・vet・配布対象build/compile・実provider基本フローはレビュー後の検証事項であり、この静的レビューの成功に含めません。commit・push・releaseも行っていません。
