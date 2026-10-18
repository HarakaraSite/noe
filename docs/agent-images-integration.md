# Agent image integration: S6 live checks

2026-10-18に、最新sourceを`/tmp/noe-image-integration-20261018T114848Z/noe`へbuildし、通常の公開JavaScript APIから少数の実provider確認を行った。[実行script](../spikes/agent-images/integration.js)は`run_agent`・`read_image`・`generate_image`だけを使う。認証情報は既存のNOE設定からホストが読み、scriptや記録へコピーしていない。

## Vision

初回runではprompt内のJSON例に期待回答`{"left":"red","right":"blue"}`が含まれていた。このため、初回Vision表の正答は視覚認識の独立した証拠としては弱い。以下の追加確認では期待色をpromptから外し、異なる4色fixtureを使った。過去の出力自体は履歴として残す。

各agentは`maxSteps: 4`、`maxTokens: 4096`、`effort: "low"`で、`read_image`を呼び、赤が左・青が右の二分割PNGを認識するよう指示した。OpenRouterの最初の2run後、2run目が4 requestsを使ったため、後続のrunでは`read_image`成功後に画像が次のmodel requestへ添付されることを明記し、JavaScriptによるpixel解析を止める指示へ更新した。provider call全体の20回上限を守るため、後続runは`executionLimits.providerCalls: 2`でも制限した。集計は`model.started`を実際のmodel provider requestとして数えた。

| provider / model | run | model requests | `image.read` | 時間 | prompt / completion tokens | 報告cost (USD) | 結果 |
| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |
| OpenRouter / `openai/gpt-6-luna` | 1 | 2 | 1 | 11.521 s | 7,375 / 458 | 0.00074608 | 正答 `{"left":"red","right":"blue"}` |
| OpenRouter / `openai/gpt-6-luna` | 2 | 4 | 1 | 18.073 s | 16,488 / 1,276 | 0.001395295 | `maxSteps=4`で終了し、最終回答なし |
| OpenCode Go / `gpt-6-luna` | 1 | 2 | 1 | 4.744 s | 7,415 / 257 | 未提供 | 正答 `{"left":"red","right":"blue"}` |
| OpenAI API / `gpt-6-luna` | 1 | 2 | 1 | 6.302 s | 7,414 / 241 | 未提供 | 正答 `{"left":"red","right":"blue"}` |
| OpenAI API / `gpt-6-luna` | 2 | 2 | 1 | 8.837 s | 7,417 / 590 | 未提供 | 正答 `{"left":"red","right":"blue"}` |
| ChatGPT / `gpt-6-luna` | 1 | 2 | 1 | 7.997 s | 7,415 / 80 | 未提供 | 正答 `{"left":"red","right":"blue"}` |
| ChatGPT / `gpt-6-luna` | 2 | 2 | 1 | 6.239 s | 7,409 / 80 | 未提供 | 正答 `{"left":"red","right":"blue"}` |
| OpenRouter parent Vision / `openai/gpt-6-luna` | 1 | 2 | 1 | 3.762 s | 8,616 / 94 | 0.000718005 | OpenAI API生成物を読み、中央の赤丸・白背景・文字なしを確認 |

OpenRouterの2回目もprovider要求と`image.read`は成功したが、モデルが画像内容への回答前にPNGのpixelやscanlineをJavaScriptで解析するtool callを繰り返し、4 stepを使い切った。原因はイベントから確認できたため追加retryはしていない。全体のcall上限に合わせ、OpenCode Goは1 runだけにした。

## Image generation

通常JSから同じpromptとparametersを渡し、各providerで1枚ずつ生成した。両方とも成功し、`image.started` / `image.completed`は各2件、`image.failed`は0件だった。

| provider / model | 時間 | 保存結果 | usage |
| --- | ---: | --- | --- |
| OpenRouter / `openai/gpt-image-2.5-flare` | 5.459 s | [openrouter-image.png](/tmp/noe-image-integration-20261018T114848Z/openrouter-image.png)、PNG 1024×1024、720,782 bytes | 16 prompt + 196 image = 212 tokens、reported cost `$0.00596`。request IDなし |
| OpenAI API / `gpt-image-2.5-flare` | 7.014 s | [openai-api-image.png](/tmp/noe-image-integration-20261018T114848Z/openai-api-image.png)、PNG 1024×1024、685,667 bytes | 16 input + 196 image output = 212 tokens、request ID `req_bce00d12df1141f59ff34c5e4ee7d51a` |

両ファイルのPNG signatureと1024×1024 dimensionsを確認した。OpenAI API生成画像は上表のVision agentで読み直し、指定した赤丸を確認した。OpenRouter生成画像は形式・寸法のみを確認した。generation responseの`responseModel`はどちらも返らなかった。

## Calls, logs, and artifacts

合計はmodel provider requests 18回と画像生成requests 2回で、上限の20回以内。8件のVision runで`image.read`を各1回記録した。会話全体のscript内計測時間は約80秒。ChatGPT credential fileの更新時刻は前後で同一で、token refreshとhjh同期は不要だった。

JSONLは`/tmp/noe-image-integration-20261018T114848Z/events.jsonl`、各runのmetadata reportと生成画像も同じdirectoryにある。credential内容やimage Base64は保存せず、イベントファイルにdata URL・Bearer token・OpenAI/OpenRouter keyのpatternがないことを確認した。

総合要件と受入範囲は[画像機能計画](agent-images-plan.md)、初回SDK実接続の結果は[provider spike](agent-images-spike.md)を参照。

## S6追加確認

初回Vision promptのJSON例に期待値が漏れていたため、追加確認では緑・マゼンタ・青・黄の256×256四分割PNGを別途作成し、値を含まない`{"topLeft":"<color>",...}`を回答schemaとして提示した。期待値はscriptのローカル照合にのみ渡し、モデルpromptには含めていない。4providerとも`image.read`は成功し、detailはすべて`auto`だったが、四象限を全問正しく識別したproviderはなかった。誤答の原因は未確定であり、追加retryはしていない。

Fixture: [input-followup.png](/tmp/noe-image-integration-20261018T114848Z/input-followup.png)（TL green / TR magenta / BL blue / BR yellow）。

| provider / model | 時間 | prompt / completion tokens | 回答 | 照合 |
| --- | ---: | ---: | --- | --- |
| OpenRouter / `openai/gpt-6-luna` | 6.800 s | 7,512 / 164 | `{"topLeft":"cyan","topRight":"magenta","bottomLeft":"blue","bottomRight":"yellow"}` | TL誤答 |
| OpenCode Go / `gpt-6-luna` | 3.082 s | 7,515 / 94 | `{"topLeft":"magenta","topRight":"red","bottomLeft":"blue","bottomRight":"yellow"}` | TL・TR誤答 |
| OpenAI API / `gpt-6-luna` | 9.107 s | 7,517 / 522 | `{"topLeft":"blue","topRight":"magenta","bottomLeft":"yellow","bottomRight":"cyan"}` | TL・BL・BR誤答 |
| ChatGPT / `gpt-6-luna` | 6.460 s | 7,511 / 90 | `{"topLeft":"red","topRight":"yellow","bottomLeft":"blue","bottomRight":"green"}` | TL・TR・BR誤答 |

続けてOpenCode Go `gpt-6-luna`の1 run内で、OpenRouter `openai/gpt-image-2.5-flare`による画像生成と同じ画像の`read_image`を実行した。生成は6.398秒、PNG 1024×1024・688,858 bytes、usage 16 prompt + 196 image tokens、reported cost `$0.00596`。同runのagentは「中央の赤丸、白背景、文字なし」と回答し、生成promptと一致した。保存先は[opencodego-openrouter-generated-followup.png](/tmp/noe-image-integration-20261018T114848Z/opencodego-openrouter-generated-followup.png)。このケースはmodel requests 2回とimage generation 1回で、follow-up全体は合計11 provider calls（Vision 8 + 同run確認2 model + 1 image）だった。前回の20-call実接続枠とは別の追加枠である。

詳細reportは`/tmp/noe-image-integration-20261018T114848Z/report-followup.json`、JSONLは`/tmp/noe-image-integration-20261018T114848Z/events-followup.jsonl`。redaction scanでdata URL・Bearer token・OpenAI/OpenRouter key patternは見つからなかった。親agentが前回生成した2枚を直接確認し、どちらも赤丸・白背景だったとの記録もある。このfollow-up画像の直接目視確認は親agentに依頼済みで、ここでは同一runのagent readback結果を記録した。

## 実送信bytesの照合とモデル比較

4色の誤答をモデル特性と即断せず、追加の6 model requestsで切り分けた。検証scriptは[wire_test.go](../spikes/agent-images/wire_test.go)。通常のtestではskipし、`NOE_IMAGE_WIRE_LIVE=1`等の明示設定がある場合のみ実接続する。

rootが`input-followup.png`を直接開き、左上green・右上magenta・左下blue・右下yellowであることを確認した。HTTP clientから実送信直前のimage partを確認し、Base64をdecodeしたSHA256が元ファイルと完全一致することを確認した。記録するのはhash・寸法・part type・keysだけで、body・Base64・認証headerは保存しない。

```text
source / transmitted SHA256:
4ed487dccd0569f5fdb5b9dc4e51d927b628af6ebac0f9529cd3e6260b78f1b4
PNG: 256 x 256
Responses type: input_image
keys: detail, image_url, type
```

| 経路 | model requests | 結果 |
| --- | ---: | --- |
| OpenAI API、tool履歴なしの直接要求、Luna、detail high | 1 | 全象限をpurpleと誤答 |
| 同じ直接要求、Sol、detail high | 1 | 4色すべて正答 |
| OpenAI APIのLuna agent、read_image→判断、detail auto | 2 | 左上magenta・右上cyanと誤答、下2色は正答 |
| OpenAI APIのSol agent、read_image→判断、detail auto | 2 | 4色すべて正答 |

全画像要求で送信hashが一致し、Sol agentは`run_js`で実画像を読み、次要求で4色の内容を正しく答えた。同じ画像・adapterでLunaの直接要求も誤るため、今回の誤答はLunaの認識精度によると推測する。NOEによる画像bytesの置換・欠落は確認されなかった。これはすべての画像やモデルで認識精度を保証するものではない。

診断結果は `/tmp/noe-image-integration-20261018T114848Z/wire-diagnostic.json` と `wire-agent-gpt-6.1-sol.json`。生成した3枚もrootが直接開き、いずれも白背景の中央の赤丸であることを確認した。今回の本体実接続は初回20 + follow-up11 + 切り分け6 = **合計37 provider requests**（model 34、画像生成3）。S0のSDKスパイク要求はこの集計に含めない。

## Solでの再確認 — 2026-10-18

利用者の指定により、最新sourceを新しい一時binaryへbuildし、親modelを`gpt-6.1-sol`に固定して公開CLI経由で再確認した。effortは前回と同じ`low`。期待色を指示に含めない4象限画像を使い、画像生成modelは別の`openai/gpt-image-2.5-flare`を使用した。

| ケース | 経路 | 時間 | 結果 |
| --- | --- | ---: | --- |
| 4色画像をread_imageで読む | OpenAI API / Sol | 5.895秒 | green / magenta / blue / yellow、4象限すべて正答 |
| 同じ画像を読む | ChatGPT / Sol | 9.768秒 | 4象限すべて正答 |
| 生成→同runでread_image→最終確認 | ChatGPT / Sol → OpenRouter画像生成 | 18.814秒 | 白背景・中央の赤丸・文字なしと回答。生成PNGも直接目視して一致を確認 |

今回追加はmodel requests 6 + 画像生成1 = 7 provider requests。`image.read`3件、生成成功1件、model/image/agentの失敗イベントなし。前節までと合わせて44 requests。結果とJSONL、生成画像は `/tmp/noe-image-sol-meso2_rq/` の `summary.json`・`events.jsonl`・`sol-generated.png`。イベントに画像data URLやcredential patternがないことも確認した。

確認中、integration.jsの`answerMatches`が回答全体を小文字化してからJSONとして読むため、camelCaseの期待keyと比較できない不具合が見つかった。小文字化を色の値だけに限定する修正を行い、保存済みの正答と誤答をNOE上で再評価し、正答の受理・誤答の拒否を確認した。元のSol色テストreportには`imageMatch:false`が残っているが、回答自体は正答であり、修正後の判定は`validation.json`と`summary.json`へ別に保存している。追加のAPI再要求は行っていない。

ChatGPT credentialが自動refreshされたため、承認済みの方針に従い、同一issuer・subject・client・hostのhjh recordへ最新情報を同期した。認証情報はログ・文書へ含めていない。
