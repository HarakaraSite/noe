# 画像入力・画像生成の実プロバイダスパイク

実施日：2026-10-18。利用者が実プロバイダ利用を許可。スパイク後に一度報告する指示に従い、本体実装には進まない。

[計画](agent-images-plan.md)のS0。実行コードは[spikes/agent-images](../spikes/agent-images/README.md)。OpenRouter Go SDK v0.9.61、OpenAI Go SDK v3.79.0、既存NOE provider adapterと登録済みcredentialを使用。依存更新・本体変更・commit・pushは行っていない。

## 結論

- パスから読んだ画像をBase64の画像パートにして、Chat Completions/Responsesへ渡す経路は実現可能。
- OpenRouterとOpenAI APIキーの専用Images APIで実際に一枚ずつ生成・保存できた。両方とも1024×1024のPNGとして開け、依頼した「白背景に赤い円」になっていた。
- ChatGPTサインインでは画像入力を利用できたが、画像生成は今回の公式経路では非対応。画像生成をChatGPTプランで使えると想定して実装しない。
- OpenCode GoのLunaでも画像付き要求は完了した。ただし左右を逆に答えた例があり、送信の成功と視覚判断の正しさは別に扱う。

## 確認結果

| 対象 | モデル・経路 | 結果 |
| --- | --- | --- |
| OpenRouter Vision | `openai/gpt-6-luna`、Chat Completions | HTTP 200、`Red, blue`。左赤・右青のfixtureと一致 |
| OpenAI APIキー Vision | `gpt-6-luna`、Responses | 初回は本文を確定できず失敗。同条件の再試行は200・`Red, blue`で一致 |
| ChatGPT Vision | `gpt-6-luna`、Responses | HTTP 200、`Red, blue`で一致 |
| OpenCode Go Vision | `gpt-6-luna`、Responses | 初期の出力上限512では`response.incomplete`・`max_output_tokens`。4096へ変更後は200・`response.completed`。ただし回答は`Blue, red`で順序不一致 |
| OpenRouter画像生成 | `openai/gpt-image-2.5-flare`、`POST /api/v1/images` | 200、一枚生成、PNG 1024×1024、724,428 bytes、約9.6秒 |
| OpenAI APIキー画像生成 | `gpt-image-2.5-flare`、`POST /v1/images/generations` | 200、一枚生成、PNG 1024×1024、720,965 bytes、約7.8秒 |
| ChatGPTトークンで専用画像生成 | `gpt-image-2.5-flare`、`POST /v1/images/generations` | 401。この認証では利用確認できず |
| ChatGPT Responses画像生成tool | `gpt-6.1-sol`＋`gpt-image-2.5-flare`、`POST /v1/responses` | 入力形式を修正後も400。`subscription_sharing_unsupported_capability`が`image_generation`非対応と明示 |

OpenCode Goには`User-Agent: noe/1.6.1`とケース内で固定した`x-opencode-session`を送り、実送信metadataに保持した。親子agentのID分離は本体実装後のE2Eで確認する。

## 失敗と調査経過

最初の8ケースと原因調査の再要求を合わせ、推論・生成endpointへの要求は15回。成功した生成を繰り返したり、別モデルへ自動でfallbackしたりはしていない。

- OpenCode Goの初期失敗では、HTTP 200だけでは成功とみなせなかった。SSE終端を確認すると、推論項目だけを出した後に出力上限へ達していた。512は小さかったため、今後のスパイクの既定は4096へ変更した。
- OpenAI APIキーのVisionは初回失敗し、同じ512の上限で再試行すると成功した。初回の終端詳細は保存できていないため、原因を出力上限と断定しない。
- 最初のスパイクではSDKとadapterのエラー表示が抽象的だったため、SSE終端・不完了理由・SDKエラー本文の診断を追加した。ChatGPTの最初の生成要求は`input`を文字列にしており、400の理由は`Input must be a list`だった。この経路では配列へ直す必要があった。
- 配列へ修正した最終要求では、別の400として画像生成toolの非対応が明示された。最初の400を根拠に非対応と判断せず、入力の問題を解消してから区別した。

最終的なChatGPTの応答は、プラン共有で許可されるtoolの一覧に画像生成を含めず、`param: tools`、`code: subscription_sharing_unsupported_capability`を返している。専用APIの401だけから一般化した判断ではない。将来の提供変更は別途確認する。

## 成果物・使用量

生の実行成果物は次のディレクトリに保持した。登録キー・トークン・画像Base64をreportへ保存していない。

```text
/tmp/noe-image-spike-20261018-eCJsWQ/
  input.png
  image-openrouter.png
  image-openai-api.png
  report.json
  diagnostic/report.json
  followup/report.json
  chatgpt-error/report.json
  chatgpt-list-input/report.json
```

主なusageは以下。APIが返した値であり、全要求の合計請求額ではない。

- OpenRouter画像生成：input 23、output 196、total 219 tokens、cost `$0.005995`。
- OpenRouter Vision：prompt 36、completion 441、cost `$0.0002241`。
- OpenAI APIキー画像生成：input 23、output 196、total 219 tokens。応答にドル額はない。
- ChatGPT Vision：input 36、output 251。OpenCode Go完了要求：input 36、output 448。

小さな画像でも、画像Base64の文字数と実測input tokensは一致しない。既存のJSON bytes/4へBase64を入れると過大な概算になるため、本体では画像を別途見積もる必要がある。

ChatGPT Vision要求で期限切れtokenの更新が発生した。以前許可された方針に従い、同じissuer・subject・client・hostの古いhjh credentialも最新NOE recordへ同期した。別アカウントは変更していない。

## 本体計画への反映と未確認

初期の生成先はOpenRouterとOpenAI APIキー。ChatGPTサインインはVisionのみを実確認済みとし、生成先へ追加しない。親がChatGPT/OpenCode Goでも、生成先として別の認証経路を明示する設計は維持する。

このスパイクはprovider境界の画像入出力を確認したもので、まだ`noe:image`、agentの`run_js`からの画像添付、queue履歴、画像blockのtrim、hook、取消し・観察の実装を検証したものではない。これらは計画のS1以降に残る。画像編集、複数画像、PNG以外、生成結果をagent自身が読み直す一連の経路も本体実装後の確認事項。

テストハーネスの`success`は接続・処理結果、`imageMatch`はfixtureへの回答の一致を別に表す。初期reportには`imageMatch`がなく、OpenCode Goの`Blue, red`も`success: true`と記録されたが、内容評価としては不一致である。Go testのPASSは全probeの記録完了を示し、全provider機能の成功を意味しない。

[開発記録へ戻る](README.md)
