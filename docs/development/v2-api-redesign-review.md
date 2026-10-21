# v2 API再設計：通常レビュー・批判的レビュー

2026-10-21。基点：`c1006f1c1604194aaa9ca61bdb07aed2666702b5`。状態：通常/批判的レビュー、P2の4件と任意提案の反映、限定再確認を完了。残る必須指摘なし。計画として実装へ渡せると判定し、ここで停止する。

利用者は全機能/APIへの再設計提案を採用し、破壊的変更を許可して計画見直しを依頼した。旧JS互換/8slice/RPC維持の設計レビューとは別の、変更した要求に対する新しい計画レビューである。旧合格判定を転用しない。

## 対象と分担

| 正本 | 初回SHA-256 |
| --- | --- |
| [構成案](v2-agent-runtime-plan.md) | `22f0c0e8f5ee1d8e4cb42de33c1955af5bb56babd7201f19ed83ee3267021c4b` |
| [API再設計](v2-api-redesign.md) | `88b415585d833b6fc2f8a8e1098943017ca55b18096cfffe9018cd6c29cee735` |
| [実装計画](v2-agent-runtime-implementation-plan.md) | `375c8f2bbc886911b68750c1e94666a14e5b2217bc647d5f8e34d56f065d9ade` |
| [責務・経路台帳](v2-responsibility-boundaries.md) | `afb5b7f8c73f93986c32d45b83d3bdfd0dae300e8dda6e5af3d2a1261b7c5a15` |

既存reviewerを再利用し、担当を分けて依頼した。

- `v2_runtime_review`：通常レビュー。型/依存と共有FS・loader・model/bindingの接続、Host/Clientの所有/finish/終了、11sliceの順序と受入が実装可能か。
- `v2_separation_review`：批判的レビュー。責務分離の効果、複雑さの移転、機能喪失、不要な公開面/移行負担、scopeとNOE方針の残存に具体的反例があるか。

初回各30分、証拠/中間結論10分なしなら暫定返却。read-only、追加agent/変更/test/build/実provider/API/外部書込みは禁止。必要な再確認は各同じ担当で既存finding/変更だけ1回15分以内。採否・設計判断・正本更新・最終検証は親が担当する。

入力は`/tmp/codex-agent-context/v2-breaking-api-review/{normal,critical}/context.md`。初回snapshotは`/var/tmp/noe-v2-plan-review-20261021/breaking-api-review/`、旧計画は同親directoryの`breaking-api-baseline/`。正本はrepositoryの上記4文書であり、packetを完了状態の正本にしない。

## 初回の結果と親の採否

通常レビューはP2が3件、批判的レビューはP2が1件。分離の方向と11スライス順序には追加の必須指摘なし。親は以下を採用した。

| ID | 具体的な不足/影響 | 採用した修正 |
| --- | --- | --- |
| N1・P2 | agent.Attachのrt/loop/hostだけではAttach先の実行FSを取得できない。Host.Runtimeを代用するとRoot/ReadOnlyや保存先が別になる | managed/advanced共通のBindings viewを返し、VM/Loop/context/Environment/実行FSを各bindingへ明示供給。agent.Attachの引数をviewへ変更し、異なるHost/Attach先FSでS2/S5/S7を確認 |
| N2・P2 | 現行SpawnのnormalizeHookModulesはHost cwd/import-mapで登録時にOS pathへ解決する。任意factoryのbare/memory hookを拒否し得る | 登録時はspecifier/index固定だけ、task loader作成後に解決/読込みしcanonical ID＋indexで同定。エラーはmodel前のtask失敗へ移し、queueで再解決。S2/S6/S7へ受入を追加 |
| N3・P2 | Host.Runtimeのstdioと、task/toolの専用capture/空入力の優先順位が不明。流用/書換えで結果欠落や並行干渉が起こり得る | templateのstdioはnil予約、非nilならNewHostで設定error。実行別コピーへcapture/空入力を束縛し、initializerも派生viewを使う。S7で元Profile非変更とcaptureを確認 |
| C1・P2 | auth等を独立化したがProfileにはbindingの導入経路がない。rootだけのAttachではfresh task/toolに届かず、機能欠落かNOE既定の隠れた補完になる | Profile.Initializeをowner上で各実行に呼び、Bindingsでauth/log等を選択導入。cleanup/途中失敗/借用を明記。NOE initializerはWorker情報で分岐し、agentexecにNOE既定を残さない。S2/S5/S7/S9へ接続 |

通常レビューの任意提案を採用し、cwd基準のrelative importは仮想entryに限定、通常file/moduleはreferrer基準を維持すると明記した。advancedのentry準備も同じfactoryを使う。

批判的レビューの任意提案を採用し、control初回activationとerror返却先を明記した。NOEのObserve adapterがagent.acceptedをcontrollerへ通知し、アプリ所有の処理がHTTP/登録を開始する。単純run/evalやcatalogだけでは探索しない。登録失敗はapp contextを取消し、coordinatorがowned処理と登録を回収して実行errorへする。初回操作rejectからNOE全体の実行errorへ変わり、ready前のtask開始を禁止しない意図的変更として記録した。coreへのNOE resolver/activation callbackの逆注入は行わない。

親の補足として、Profile統合でRandom/OS情報/Worker上限等を失わないこと、Bindings/initializerの部分失敗回収とadvancedでの借用VM/Loop、source moduleの準備順も明記した。

## 限定再確認

同じ各reviewerへ、それぞれの既存findingと対応変更だけを1回15分以内で戻した。修正snapshotは初回保存先のfixed/、差分はfix.diff。入力は各packet directoryのfix-review.md。修正後hashは以下。

| 文書 | 修正後SHA-256 |
| --- | --- |
| 構成案 | `57da91de7b36f4a3d9fb53689fd4a3b63bb77a4d35432eee7a3685fa5cf76819` |
| API再設計 | `939b812acceb7a444c07082c64b78de068b355a029924fff1eca5c50d2912a73` |
| 実装計画 | `476bf827ab27dfe7321bafa6d5542f67ae5850673cfbfdcfa6cda814ea0f0ab2` |
| 責務台帳 | `e37a8bc3266eb312b082532ebf2218cf9d8cf36eab334b0817910fa5c014e08f` |

**通常レビュー：N1〜N3は全て解消、変更箇所に追加の必須指摘なし。計画を実装へ渡せる。** Bindingsの実行FS、hookのtask loader解決/同定/重複登録/queue再解決、stdioのnil予約/コピー/capture、相対importとslice受入を確認した。

**批判的レビュー：C1は解消、担当範囲に実装へ渡す妨げなし。** initializer/Bindingsのauth/log導入、owner/Worker/cleanup/借用を確認した。controlの遅延controllerと初回error返却先の意図的変更も具体化された。通常担当のN1〜N3を重複して再判定していない。

両reviewerは修正4文書のhash一致を確認。親は両判定を採用した。受領後の正本変更は状態と改訂レビューへのリンクだけで、技術契約は修正snapshotから変更していない。

## 最終記録

最終snapshotは同一保存先のfinal/、レビュー後の状態/参照更新はfinal-status.diff。正本はrepository内の文書。

| 文書 | 最終SHA-256 |
| --- | --- |
| v2-agent-runtime-plan.md | `1e37ae88110ec18d386f19f14a4f1125eb48543d1aca8b2c256a8d570eb7537e` |
| v2-api-redesign.md | `3e9192b870b5fa8970074bfd79695df2bc0fb6afc87fb18775a16997159ecc55` |
| v2-agent-runtime-implementation-plan.md | `57efa6d3550dd28e711d950446265cf21b1be8052bfdd5efc0bf6ac1d45f0758` |
| v2-responsibility-boundaries.md | `af52dd0c9adbb8f8ab74eee27ddb2d3acd9362a10f93842b1222da97d4fd0888` |

親は文書リンク・code fence・経路ID/11slice順序・空白差分・snapshot/hashを確認した。製品コードを変更せず、test/build/実provider/commit/push/releaseは行わない。改訂計画の作成とレビュー結果反映までで停止する。

## 現在の状態と限界

破壊的APIの変更は計画への許可であり、今回の製品実装やreleaseへは進まない。確認したのはsource接続点と設計で、移行済み・動作成功を意味しない。
