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

## 2026-10-21の観測結果

実行対象は`b651d12`。外部instructionを使った2試験の機能検証は成功した。詳細な要求別数値・元artifactのpath/hashは[集計JSON](agent-efficiency-results.json)に保存した。

| 試験・担当 | モデル要求 | 入力tokens | cached tokens | 入力中のcache比率 |
| --- | ---: | ---: | ---: | ---: |
| 小作業3タスク・親 | 6 | 42,486 | 0 | 0.00% |
| 実装とreview・親 | 8 | 82,603 | 0 | 0.00% |
| 実装とreview・子2体 | 11 | 107,478 | 16,128 | 15.01% |

小作業は子0体。分担試験は子2体・孫0体で、実装担当1タスク、review担当2タスクだった。親の完了待ちで`includeAnswer:false`を4回、必要な本文の`read_task_result`を3回使用した。成果物は試験側でも独立に実行・検査し、成功した。これらは明示した分担・再確認の依頼に対する一試行の結果である。

実際のHTTP送信直前の25要求すべてで、instanceごとのsession key、固定instruction/toolのhash、前回送った入力prefixを維持していた。状態通知はuser messageとして末尾にあり、過去の通知を含むprefixも同一だった。親のqueue移行後もセッションは維持された。観測した送信本文に、固定instructionを要求ごとに変更して親の低ヒット率を説明できる経路は見つからなかった。

親のcache改善は確認できなかったため、「テストが良好なら内部へ移す」の条件を全体として満たしたとは扱わず、委譲方針は外部に維持した。機能テストの成功とcache改善の成否は分けて記録する。

### 同一入力の対照試験

追加の`TestAgentCacheProbeLive`では、組込みsystemと短いuser指示だけの同一入力を、同じsession/cache keyで2回連続送信した。モデル・provider・effortは上記と同じで、動的状態通知、tool、子の起動、queue、履歴の伸長は含めていない。2回とも入力4,791 tokens、cached tokens 0、cache write tokens 0だった。SSE `response.completed`内の生のusageとadapterが返すusageは一致しており、この対照で0を返したのはNOEの集計による欠落ではない。

```sh
NOE_AGENT_EFFICIENCY_LIVE=1 \
NOE_AGENT_EFFICIENCY_LIVE_DIR=/absolute/path/to/fresh-probe-artifacts \
go test -tags=agent_efficiency_live ./internal/agent \
  -run '^TestAgentCacheProbeLive$' -count=1 -timeout=4m -v
```

同じ試験の当時の診断モードで`NOE_AGENT_CACHE_PROBE_DIAGNOSTICS=1`を指定し、公式APIの`prompt_cache_options`を付けた要求も確認した。このChatGPTプラン接続・モデルはHTTP 400、`param:prompt_cache_options`、`message:prompt_cache_options is not supported on this model`を返した。初回とエラー詳細を取得した再確認の2要求が拒否され、公式diagnosticsの比較は実行できなかった。この応答は当該接続での事実であり、通常のAPI全体や別モデルでの未対応を意味しない。

この特別な診断モードは未対応が確認されたため退役した。現在の`TestAgentCacheProbeLive`は既定Registry経由で同一入力を2回送り、`prompt_cache_options`や`comparison_response_id`を追加せず、生usageとadapter usageの一致を記録する。

成功した推論は合計27要求、diagnostics用の拒否応答は2要求。元ログでは親16.17%・子8.56%だったが、今回とは作業内容・入力の長さ・時点が違うため、改善率のA/B比較にはしない。現在の結果は、固定キー・履歴保持だけでは親のヒット率の疑念が解消しないことを示す。provider内のcache受付、ルーティング、利用枠での計上等のどれが原因かは未確定で、製品のcache設定を推測で変更していない。

### 追加調査とChatGPTセッションヘッダーの修正

上記の時点では未確定だったが、hjhとの比較でNOEのChatGPT設定に`sessionHeader: "session_id"`が欠けていることが分かった。同一本文のままヘッダーの有無を切り替えた5要求（on/off/on/off/on）では、入力は各4,791 tokens、cached tokensは順に0/0/4,608/0/4,608だった。ヘッダーありの再送で96.18%、なしで0%となった。証拠は`/var/tmp/noe-hjh-cache-header-ti_iaifj/comparison.json`。本文の`prompt_cache_key`だけを固定した最初の対照試験では、このヘッダーがなかった。

利用者の修正依頼を受け、組込み`openai-chatgpt`設定に`sessionHeader: "session_id"`を追加した。既存のGo送信経路がinstanceのセッションIDをヘッダーへ設定するため、queue taskごとのrun ID変更では変わらず、子instanceには別IDが送られる。本文の`prompt_cache_key`と明示的な`cacheKey`指定は従来どおり。外部の委譲instructionやツールなしResponsesの扱いは今回の修正対象に含めていない。

修正後の既定設定をそのまま使う`TestAgentCacheProbeLive`を、NOEのOAuth認証で再実行した。probe独自のprofileやヘッダー挿入は使わず、2要求のrun IDを変えてセッションIDは維持した。

| 要求 | 入力tokens | cached tokens | cache比率 |
| --- | ---: | ---: | ---: |
| 初回 | 4,791 | 0 | 0.00% |
| 同一入力の再送 | 4,791 | 4,608 | 96.18% |

両要求のHTTP statusは200、入力hashとセッションヘッダーは一致し、生のSSE usageとadapterが返すusageも一致した。証拠は`/var/tmp/noe-chatgpt-default-header-fix-20261021/cache_probe/responses.json`。これは固定入力の再送での確認であり、実作業の履歴全体のヒット率が常にこの値になるという意味ではない。

回帰テストは組込み設定からSDKのHTTP送信までを通し、queue相当のrun ID変更、明示的なcacheKey変更、子instanceへの切替でもヘッダーが正しいことを確認する。修正前は全4要求でヘッダー欠落を検出し、修正後はproviderテスト一式とvet、上記の実接続試験が成功した。
