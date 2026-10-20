# 子の起動とキャッシュ利用の実接続観測

2026-10-21の利用者依頼で、委譲方針をまず外部instructionとして試し、良好なら組込みへ移す。
元ログと同じ`openai-chatgpt`・`gpt-6.1-sol`・`effort:high`を使う。

通常テストはネットワーク接続しない。以下は明示opt-inの実接続試験で、ChatGPTプランの利用枠を消費する。

```sh
NOE_AGENT_EFFICIENCY_LIVE=1 \
NOE_AGENT_EFFICIENCY_POLICY_MODE=external \
NOE_AGENT_EFFICIENCY_LIVE_DIR=/absolute/path/to/fresh-artifacts \
go test -tags=agent_efficiency_live ./internal/agent \
  -run '^TestAgentEfficiencyLive$' -count=1 -timeout=20m -v
```

- `small_queue`: 小さなファイル処理を同じrootの3タスクで続ける。子0体、3つの異なるrun IDと一つのセッションID、成果物の内容を確認する。モデル呼出しはgroup全体で最大10回。
- `implementation_and_review`: 一つの子へまとまった実装、別の子へ独立reviewを渡す。追加ケースの確認に同じreview担当を再利用する。子2体・孫0体・子のqueue再利用と、親の独立した受入検査を確認する。モデル呼出しはgroup全体で最大24回。起動上限はrootを含め4体で、上限到達によるgroup停止を成功として扱わない。

通常の所有関係と寿命を維持し、試験用の一時workspaceだけを変更する。自動trimを無効にして、送信済みprefixの保持を観測する。委譲試験では分担と再確認を明示しているため、自然な依頼一般に対する委譲頻度の証明とは扱わない。

`events.jsonl`は通常のNOEイベント、`wire.json`は実際のHTTP送信直前に観測したmodel・session key・system/toolのhash・前回入力との共通prefix・状態通知のrole等。認証ヘッダーやHTTP本文を独自の観測記録へ保存しない。NOEの通常イベントには試験taskのtoolや回答が含まれる。

キャッシュ率は`model.finished.usage`の`sum(cachedTokens)/sum(promptTokens)`で親・子を分けて計算し、要求単位の値も残す。未報告のcachedTokensは0と区別する。初回を含む率と、各instanceの初回を除く率を併記する。queue移行直後も別に確認する。

固定instruction・tool・session key・送信済みprefixが同一でも、providerのキャッシュ利用率を保証しない。公式の[Prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching)と[diagnostics](https://developers.openai.com/api/docs/guides/prompt-caching/diagnostics)に沿って、送信条件と返却されたcached tokensを区別して記録する。元の長時間ログとの比較は条件が異なる観測であり、原因を切り分けたA/B比較とは扱わない。

組込み移行後は`NOE_AGENT_EFFICIENCY_POLICY_MODE=builtin`で外部ファイルを複製せず、同じ試験を選択できる。キャッシュ率に固定の合否閾値は置かず、起動・再利用・成果物・送信履歴の確認結果と併せて移行を判断する。
