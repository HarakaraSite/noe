# OpenRouter Go SDKをOpenCode Goへ接続するスパイク

2026-10-17、Linux amd64 / Go 1.27.2 / `github.com/OpenRouterTeam/go-sdk v0.9.59`で実施。利用者が許可したhjhの`opencode-go-api-key`を参照し、キーのコピー・表示は行っていません。

## 結論

**既存のOpenRouter Go SDKを、OpenCode GoのChat Completions・Responsesへ流用できました。** 接続先と認証を変更し、NOE用のUser-Agent・会話IDをHTTP境界で付与する構成です。別SDKは追加していません。ただし、NOEのthinking処理と固定SDKのResponses要求構築に、以下の調整が必要です。

この作業は独立した[スパイク](../../spikes/opencode-go/README.md)です。本体のprovider追加・CLIやJS APIの変更は行っていません。[機械可読結果](opencode-go-sdk-spike-results.json)。

## 接続と識別

```text
base URL: https://opencode.ai/inference/go/openai/v1
SDK認証: OpenCode GoのAPIキー
User-Agent: noe/v1.2.0
x-opencode-session: run contextのID
```

公式資料：[OpenCode Goの送信先とクライアント条件](https://opencode.ai/v2/docs/console/go/)、[OpenRouter SDKの接続先変更](https://github.com/OpenRouterTeam/go-sdk#override-server-url-per-client)。

`User-Agent`は`noe/`と`cli.Version`から構築します。`x-opencode-session`は共有clientの可変フィールドではなく、各要求のcontextから取得します。同じrunのtool往復では固定し、親・子は別IDにします。親→子→親のヘッダ値はローカルHTTPサーバーで確認し、実接続ではtool往復2要求に同じIDが付くことを記録しました。

スパイクは各caseへ論理的なrun IDを割り当てています。managedな親子agentの起動は行っていないため、本実装では実際のNOE run IDをこの境界へ接続します。

## 実接続の結果

| ケース | モデル | 実要求 | 結果 | 所要時間 |
| --- | --- | --- | --- | --- |
| SDK Chat通常応答 | `deepseek-v4.1-flash` | 1 | 型付き応答を取得し、指定文字列を確認 | 1,619ms |
| NOE Chat tool往復 | `deepseek-v4.1-flash` | 2 | SSEの`run_js`要求を受け、実際に`noe eval`でJSを実行。stdoutの`42`を返し、最終応答も`42` | 3,025ms |
| SDK Responses通常応答 | `gpt-6-luna` | 1 | 型付き応答を取得し、指定文字列を確認 | 1,438ms |
| SDK Responses streaming | `gpt-6-luna` | 1 | SDK iteratorで指定出力と`response.completed`を確認 | 904ms |

5要求はすべてHTTP 200で完了しました。Chat tool往復は既存の`provider.NewOpenRouter`・`Generate`を使い、base URL・key file・HTTP clientを差し替えています。モデルは`console.log(6 * 7);`を生成し、NOEで実行した結果をtoolメッセージとして返しました。

所要時間はcase単位の実測です。usageの合計はinput/prompt 865 tokens、output/completion 159 tokens、計1,024 tokens。API応答には費用の数値がなく、金額は算定していません。OpenCode Goの契約枠を消費します。

## 見つかった差分

### thinkingの形式

OpenCode GoのDeepSeekはSSEの`delta.reasoning_content`で思考を返しました。最初のtool要求では10断片、合計101文字でしたが、既存NOE層の組立て結果は空文字です。既存accumulatorは`reasoning`を連結し、未知の`reasoning_content`は最後の断片で上書きするためです。観察側もこのフィールドをthinkingとして処理しません。

今回の簡単なtool往復は成功しましたが、正しい思考の再送・表示が成立したとは扱いません。本実装ではproviderごとのreasoningフィールドに対応し、全断片の保持・次要求への再送・観察を確認する必要があります。SDKの接続成功と、NOE側のprovider対応完了を分けます。

### 固定SDKのResponses要求構築

`ResponsesRequest.ServiceTier`を省略すると、固定SDKの既定値`auto`のJSON処理で`invalid character 'a' looking for beginning of value`となり、通信前に失敗しました。通常のSDK型で要求を構築しても発生し、`ServiceTier`へ`ResponsesRequestServiceTierAuto`を明示すると送信できました。OpenCode GoのHTTPエラーではありません。

初回のJSONからの要求復元と、型付き要求での省略値の失敗記録を保存しています。いずれもHTTP送信0回です。修正後はResponsesの2ケースだけを実行し、成功済みのChatを再送していません。依存SDKの更新や上流への報告は行っていません。

## 検証と残る範囲

ローカルのヘッダ検証とスパイクpackageのtest・vetを実施。実接続は上記4ケースです。SDKの自動retryは無効です。

Messages形式、Responsesのtool往復、明示effort、画像、長時間のagent実行、実プロバイダでの取消し、managedな親子の起動は今回の対象外です。NOE本体では現在OpenRouter用の要求・履歴処理を使っているため、OpenCode Goへの正式対応は別の実装になります。

再現手順は[スパイクREADME](../../spikes/opencode-go/README.md)。JSON/SSE、選択したヘッダ、実行JS、tool結果、summaryとtest logは次に保存しました。

```text
/home/agent/.cache/noe-qjs-repl/noe-opencode-sdk-3g1e5kpn
```
