# NOE版stdlib：ステップ7.2・計画/コードレビュー記録

2026-10-19。[計画](stdlib-step72-agent-plan.md)の独立した通常レビューと限定再レビューを完了した。今回は製品実装・test/build・実provider・commit/push/releaseを行わない。7.1の既存未コミット差分は保持する。

## 対象と評価基準

public agent APIの型closure、caller所有runtime/stdlibとの接続、内部bridgeの共通化と依存方向、CLI root/private connectedの同一経路、別NOE指定、module/hook/認証/provider/観察、Operation/keepalive/取消し/終了、外部consumer/portable CI/artifactと公開前後の区別を確認する。

初回30分、10分新しい証拠/結果/中間結論なしなら打切りと暫定報告。再レビューは変更部と既存findingだけを各15分以内で行う。利用者の指定どおりP2以上がなくなるまで修正と再レビューを反復する。findingの採否と最終判断は親が担当する。

## 初回レビュー

reviewer `/root/stdlib_step72_plan_review`を会話履歴なしで起動し、限定contextと必要sourceだけをread-onlyで渡した。対象SHA256は `6b69ad3aa75d74217df5467626550aa3095ae374af5fcd32d9743cdb40ef88c3`。

結果はP0:0 / P1:0 / **P2:1** / P3:0。

**S72-P01（採用）**：provider.Optionsのaliasはadapter専用Profileを含むが、Host.NewHost→NewRegistry→Registry.load/resolveはそれを参照しない。公開Definitionでendpoint・認証・headers等を設定したつもりでも既定JSONが使われる。計画を独自ProviderOptionsへ変更し、registryで有効な6fieldだけを公開する。Profile/Definition/ImageGenerationDefinitionの公開をやめ、詳細provider/model設定は既存ConfigFile schemaから渡す。

同時にartifactの対象を、未コミットのpublic agentだけでなくinternal/agentbridge・埋込み資産・外部例まで明示した。これはP2 findingではなく、既存のmanifest要件の明確化。

修正版は変更部とS72-P01の解消だけを同じreviewerへ15分上限で再レビュー依頼する。初回では他のP2以上はなく、製品の実装動作・test/build・providerは未検証。


## 限定再レビューと最終結果

同じreviewerが変更部とS72-P01の解消を15分上限でread-only確認。修正版SHA256は `a481fe341b0b874f649745edf02d3e25a1e4e27a016f4fbec07935fff0174bcd`。

結果は **P0:0 / P1:0 / P2:0 / P3:0**。独自ProviderOptionsの6fieldがregistryの実効設定に対応し、Profileと関連Definition型の公開が除かれたことを確認した。bridge/埋込み資産のartifact対象の明記も整合し、変更に起因する追加指摘はなかった。

親は結果を採用し、7.2を実装へ進める計画として確定した。最終版hash、相対リンク、whitespaceと変更範囲を確認した。今回は計画・この記録・開発索引・段階案だけを変更。7.1の凍結source manifest168fileは不変、既存未コミット差分もこの2索引文書の更新以外は保持した。利用者JSONL・_refs・root配布binaryは対象外。

実装・test/build・実provider・公開tag取得の成功を示すものではない。製品の実装動作は未検証で、commit/push/releaseも行っていない。実装時には改めてsource manifestを取り、関連試験→独立コードレビュー→総合テストを行う。

raw計画版・修正diff・レビュー結果は `/home/agent/.cache/noe-stdlib-step72-plan/` に保存した。


## 実装コードの独立通常レビュー

利用者の追加依頼により7.2を実装し、関連試験後にfresh reviewer `/root/stdlib_step72_code_review`を会話履歴なし・read-onlyで起動した。公開facade、共通bridge、root/connectedとAuth/controlの既定、loader/import map、Closeのgate/join/ownerと借用資源、外部consumer・artifact/helper・英文ガイド/例を対象とした。初回30分上限、限定再レビュー15分上限。初期manifest SHA256は `a61e3fc09613a3b27b6b516fb9e92a7ae421ec756bb18fd5d86381b806817a72`。

初回はP0:0 / P1:0 / **P2:1** / P3:0。**S72-C01（採用）**：live consumerはrun_agentのfulfilled snapshotを捨て、reportだけで成功判定していた。report保存後のprovider失敗や上限到達でもtask失敗を検出できない。stopped・lastResult completed・非空answerを確認し、taskへ英文最終回答を明記した。

同じreviewerがS72-C01の解消と変更されたlive/reference fixture、削除済みtracked fileを除外するhelperだけを限定再レビュー。全snapshot hashが一致し、**P0:0 / P1:0 / P2:0 / P3:0**。製品facade/bridge/runtimeは初回後に変更していない。referenceのcurrent CLI成功ログも確認した。独自go listによるstdlibのagent/SDK依存とagentのCLI/runtime依存は0件。実providerの呼出し・ファイル変更はしていない。

再レビュー対象SHA256：live.go `bd0a412246b6b6c9a4f3d71ebf13b8e5a9583a141caf0a6302568ed3f5c5c587`、reference.go `7a14846d8cdfd2130afeeface349a433946783114d5682b346659c209d817f2a`、helper `3d0a4ec7c3fa3e128a0c5a2f7d4f640c6301d2b417513ec3b6c093e1e26e1c0b`。実装source manifest（状態記録などの可変文書を除く）24項目のSHA256は `694958ef9efaa1b04f10cff67256beb8594914bd984d5a62244ff9c7f03ef3aa`。総合テストはこのsourceから開始した。raw結果は `/home/agent/.cache/noe-stdlib-step72/code-review-initial.json` と `code-review-final.json` に保存。
