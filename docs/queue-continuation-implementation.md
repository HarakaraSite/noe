# queue会話継続の実装・検証

2026-10-18。v1.6.0公開後に修正。契約は[計画](queue-continuation-plan.md)を参照。

## 実装

- Goのagent instanceが現在有効な会話snapshotを保持し、次taskの認証済みstart応答でJSへ渡す。各taskのプロセス分離、run ID、maxSteps、取消しscopeは維持。
- 初回systemと開始したtask指示、適用済みsteerを保護。queue指示は順番が来たときにuser messageとして追加し、待機中は現在の会話へ混ぜない。同じinstanceのsystemは初回構成時の内容で固定する。
- 全tool結果が揃ったblockと最終assistant objectを保存。reasoning・署名・native fieldも保持。steerで差し戻された完了回答候補も保存する。
- trim確定時にcanonical snapshotを置き換え、削除済みblockは次queueでも復活させない。trim回数・直近trim・前要求の使用量を引き継ぎ、送信済み通知を再送しない。
- run単位interruptやtask失敗後は最後の完全な履歴から継続する。instance終了時に会話snapshotを解放。別agentは独立した会話を持つ。
- README、API、モデル向けinstruction、hook・観察・instruction資料を更新。過去releaseの記録は維持。

## 挙動の確認

実runtimeとfixture providerへの要求本文で次を確認した。

- 初回tool往復・最終回答と全provider fieldを2つの後続queueへ引き継ぎ、role・順序を維持する。
- instruction fileの変更は同じinstanceへ反映せず、新しいinstanceでは読み込む。hook moduleの状態はtaskごとに新しくなる。
- 別runで同じsteer command IDを使っても、新しい追加指示を落とさない。
- provider失敗後、または2本目のrun_js実行中のinterrupt後も、1本目の完了blockは次queue要求に残る。途中で止まったtool交換は残らない。
- 最終回答がsteerで差し戻された直後にinterruptしても、その回答候補を失わない。
- CLIのqueue・steer・interruptを組み合わせても会話を継続する。run/eval/replの観察と保存JSONLも確認。
- 後続queueでも追加trimが起こり、指示保護・削除済みtool IDの非復活・累積trim回数を維持する。

旧仕様の「先頭userを現在taskとみなす」fixtureと、snapshotなしでfinalを確定するprivate fixtureは新契約に更新した。停止済みbootstrapのstart拒否テストで見つかったnil command参照も修正した。

## 検証結果

すべて成功。

```sh
go test ./...
go test -tags=e2e ./...
go vet -tags=e2e ./...
GORACE=atexit_sleep_ms=0 go test -race ./internal/agent ./internal/runtime -run 'TestQueue|TestRejectedFinal|TestConsumedTrim|TestLifecycle|TestContext|TestAgentInstructions|TestPublicRunInterrupt|TestHookModuleMap|TestCommittedAnswer|TestInstanceInterruptClosesBootstrap' -count=1
git diff --check
```

独立reviewerが保存境界・trim非復活・停止・raw field保持を確認し、対応必須findingなし。bootstrap修正とfinal確定fixtureの追加差分も再確認済み。実プロバイダへの追加通信は行っていない。

修正完了時点ではlocal source変更のみ。後続依頼によるcommit・v1.6.1 releaseとバイナリ更新の確認は[release checkpoints](release-checkpoints.md)へ記録する。
