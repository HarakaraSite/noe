# agentの画像読み込み・画像生成計画

状態：2026-10-18、S0〜S6の実装・関連テスト・コードレビュー・総合テスト・実プロバイダ検証を完了。[実装記録](agent-images-implementation.md)と[実接続結果](agent-images-integration.md)を参照。モデルの認識精度と送信経路の検証結果は区別する。

利用者との議論に基づく計画正本。最初に[スパイク](../../spikes/agent-images/README.md)でSDK・実際の接続経路を調べ、[結果](agent-images-spike.md)を本書へ反映する。本体実装・テスト・コードレビュー・総合テストと実プロバイダ接続は利用者が許可済み。本体実装後、利用者がcommit・push・v1.7.0 releaseを許可した。

## 目的

1. 画像のパスを指示したとき、agent自身がその画像を見て判断できる。
2. 親agentが、Jevへの判断依頼と同様に、必要なとき画像生成モデルを呼べる。
3. 生成画像をファイルへ保存し、そのパスから親agentが読み直して確認できる。

JSへ秘密情報やBase64化の手順を持たせず、Goホストが読取り・送信・認証・保存を担当する。モデルへ公開するtoolは引き続き`run_js`一つとする。画像生成先は親agentのproviderと独立して明示指定できる。

## 初期の利用方法

以下を今回の実装契約とする。

### パスから画像を見る

通常JSの起動方法は維持する。まずは文字列のinstructionでパスを知らせる。

```js
import { run_agent } from "noe:agent";

await run_agent({
  provider: "openai-chatgpt", model: "gpt-6-luna", effort: "auto",
}, "./tmp/screenshot.pngをread_imageで見て、画面の問題を説明してください。");
```

agentの`run_js`内：

```js
// Inspect the saved screenshot
const { read_image } = await import("noe:image");
await read_image("./tmp/screenshot.png");
```

- `read_image(path, {detail?})`は呼出し時点の画像を読み、その`run_js`の結果に画像を添付する。戻り値は`imageId`・絶対path・MIME・bytes・取得できた幅/高さ等の小さなmetadata。
- 戻り値を`console.log()`するかどうかに関係なく、成功した呼出しの画像は添付候補になる。`run_js`とhookの完了後、下記の採否・履歴確定規則に従ってモデルへ渡す。stdoutの文字列を画像指定として解釈しない。
- `detail`は`auto`・`low`・`high`、省略時`auto`。providerの対応形式へ変換する。
- 相対pathは呼出し元プロセスのその時点のcwdを基準に解決する。ホストが後から別のcwdで解決し直さない。
- この関数はagentの`run_js`で現在の呼出し元モデルに画像を渡す操作。通常のrun/eval/REPLには画像を見るモデルがいないため、この関数の呼出しは文脈エラーとする。通常JSでのファイル読取りには既存のfsを使える。
- `run_agent`・`spawn_agent`・queue・steerのinstructionを画像objectへ拡張することは今回含めない。起動時も途中も同じ「パスを知らせる→agentが読む」経路を使う。

### 画像を生成する

通常のrun/eval/REPLと、agentの`run_js`の両方から呼べる独立したAPIにする。

```js
// Generate an icon with a separately selected provider
const { generate_image, read_image } = await import("noe:image");
const result = await generate_image({
  provider: "openrouter",
  model: "openai/gpt-image-2.5-flare",
  prompt: "A simple flat icon: one red circle on white, no text.",
  output: "./tmp/icon.png",
  parameters: { quality: "low", size: "1024x1024" },
});
await read_image(result.path);
```

- `provider`・`model`・`prompt`・`output`を明示。親のprovider/model/effortは継承しない。
- 初期はテキストから一枚の画像を生成する。保存先の親ディレクトリは呼出し側で用意する。既存の保存先は通常のファイル出力と同様に更新するが、応答の受信・画像確認が完了するまで置き換えない。
- `parameters`はprovider固有の画像生成option。認証・接続先・model・prompt・画像枚数等のNOEが管理する値は上書きさせない。モデル別の対応optionを実プロバイダで確認した範囲から文書化する。
- 正常時は保存先の絶対path、MIME、bytes、寸法、要求model、取得できる応答model・request ID・usage/costを返す。Base64をJSやイベントの戻り値へ混ぜない。
- 生成だけでは親モデルへ画像を添付しない。必要なら`read_image(result.path)`で見せる。画像を見られない親でも生成と保存は行える。
- `Promise.all`による独立要求は可能。出力先は呼出し側で分ける。勝手な別provider/modelへのfallback・再生成は行わない。

参照画像による編集、マスク、複数枚生成、途中画像の逐次表示、URL直接読込み、SVGのラスタライズは初期範囲に含めない。必要になった機能は別sliceとして追加できる。

## 確認済みの土台とprovider方針

| 項目 | 現状・方針 |
| --- | --- |
| 画像入力 | Chat Completionsの`image_url`とResponsesの`input_image`は形式が異なる。共通画像参照からGoで変換する。現在のResponses adapterはuser contentをそのまま転送するため、変換の追加が必要 |
| 既存履歴 | `TaskHistory.completeBlock`はtool結果をJSON文字列で保存し、block検証はassistant＋tool結果だけを想定している。画像を文字列化せず、画像付きの完全なblockとして保存・検証する変更が必要 |
| OpenRouter生成 | 固定版Go SDK v0.9.61に`Images.Generate`があり、`POST /api/v1/images`からBase64画像を得られる。初期adapterに採用 |
| OpenAI APIキー生成 | 固定版OpenAI Go SDK v3.79.0に`Images.Generate`がある。`POST /v1/images/generations`の専用経路を初期adapterに採用 |
| ChatGPTサインイン | Visionは実接続成功。専用Images APIは401、Responsesの画像生成toolは400・`subscription_sharing_unsupported_capability`で非対応と明示された。初期の画像生成はOpenRouter/OpenAI APIキーのみ。ChatGPTからAPIキーへ無断で切り替えない |
| OpenCode Go | 画像生成adapterの初期対象にはしない。LunaへのVision要求は完了したが、左右の回答を逆転する例があった。接続成功と内容の正確性を分けて扱う。要求には`User-Agent: noe/<version>`と、run内で固定した`x-opencode-session`を付ける |
| 既存decision | `noe:decision`はそのまま。状態入力の自動圧縮や画像生成への転用を追加しない |

資料：

- [OpenAIのVision入力](https://developers.openai.com/api/docs/guides/images-vision)
- [OpenAIのImages/Responsesの選択](https://developers.openai.com/api/docs/guides/image-generation)
- [ChatGPTプランでの公式推論経路](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)
- [OpenRouterの画像生成仕様](https://openrouter.ai/docs/guides/overview/multimodal/image-generation)
- [OpenCode Goのモデル・クライアント要件](https://opencode.ai/v2/docs/console/go/)

## 画像の受渡しと寿命

```text
run_jsでread_image(path)
  → 子のOperationから親brokerへ読取りを要求
  → Goが画像bytesを固定し、親run/model stepのpending領域へ登録
  → run_js終了・afterTool後に、toolごとの添付候補を決定
  → 全toolの結果が揃い、ホストが履歴checkpointを受理した時に確定
  → 次のモデル要求直前に、参照をprovider用の画像パートへ変換
```

- `imageId`はホストが発行する内部参照。JSが任意のIDを作って他runの画像を取り込む契約にしない。
- ファイル内容は`read_image`成功時に固定する。次要求・再送・steerでの再準備・同一instanceのqueueで、同じpathを再読込みしない。ファイルが変更・削除されても既存の履歴画像は変わらない。
- 画像bytesは読取り時から親ホスト側で保持する。最初の所有者は履歴ではなく、親run/model stepのpending領域とする。子プロセス終了や子Operation closeでは解放しない。
- 複数のtool呼出し・複数画像はtoolCallIdと呼出し順で関連付ける。すべてのtool結果を対応させた後に画像メッセージを置く。Chat用には画像付きuser message、Responses用には対応するinput itemへ変換する。
- assistant/tool結果/画像を同じblockとして切り捨てる。最新blockは既存の方針どおり保護し、未送信画像を送信前に落とさない。
- 同じinstanceのqueueは画像を含む保持履歴を引き継ぐ。子agentや並列rootへ画像が暗黙に渡ることはない。必要なら各agentへpathを知らせる。
- 画像参照の履歴からの削除、instance終了時にホストの画像bytesを解放する。trim案を作っただけでは解放せず、ホストが履歴更新を受理した時に参照を外す。進行中のprovider要求が参照するbytesは、その要求の終了まで保持する。読取り元や生成された利用者のファイルは削除しない。
- 初期に読む形式はPNG・JPEG・WebPを基本とし、S1で純Goの形式確認・寸法取得手段を確定する。S0で実接続した形式はPNGのみ。実体からMIMEを調べる。画像変換・縮小・別モデルへの置換を黙って行わない。

### pendingから履歴への確定と破棄

pendingのキーは`runId`・`modelStepId`・`toolCallId`、画像の識別子は`imageId`とする。次の状態遷移をS1/S2の契約にする。

| 状態・契機 | 所有と処理 |
| --- | --- |
| read成功 | 親run/model stepのpendingへ登録。読取り元を再読込みせずbytesを保持する |
| 子終了・tool単位のhook成功 | pendingのまま。toolごとに採用候補を記録する。`tool_result`の受付だけでは会話へ確定しない |
| 全tool完了・履歴checkpoint受理 | ホストが完全なblock・tool ID・画像の採否を検証し、会話snapshotの更新と画像所有の移管を同じ確定処理で行う。採用外のpending画像は解放する |
| checkpoint応答を受け取れない | ホストの確定状態を正本とする。未確定ならabort対象。確定済みの画像を応答欠落だけでpending扱いに戻したり解放したりしない |
| batch・hook・checkpointの失敗、run取消し | 未確定のmodel stepをabortし、そのstepへの新規登録を閉じる。残る子scopeを取消し・回収し、未確定画像を解放する。既存の確定済み会話画像は残す |
| 確定後のqueue・trim・終了 | queueは会話の所有を引き継ぐ。受理済みtrimは削除blockの参照を外し、instance終了は残る会話画像を解放する |

全toolを扱う`Promise.all`が一つのhook例外で先にrejectしても、残るtoolが後から画像を再登録できないよう、abort時の受付停止とscope回収を対応させる。完了を待つ別toolのpendingを先に解放しない。

公開agentの確定点には既存の`conversation_checkpoint`を使う。内部の非managed実行も同じ検証・確定処理を使い、instance会話を持たない場合はrun内の履歴が所有し、run終了で解放する。ホストの会話更新と画像参照だけが片方成功する構造にしない。

tool Bが通常の失敗結果を返すだけなら、全toolの結果を含むblockは成立するため、成功したtool Aの画像は採用できる。Bのhook例外などでblock自体を確定できない場合は、Aを含むそのbatchの未確定画像を破棄する。後続queueへ未確定画像を持ち越さない。

## hook・失敗・取消し

- 既存の`beforeTool`・`afterTool`は`run_js`を囲む位置を維持する。画像専用hookは追加しない。
- privateな`run_js`応答では通常の実行結果と添付metadataを分ける。`afterTool`の`event.result`は従来の結果、`event.images`はそのtoolの添付候補metadataの配列とし、どちらもBase64を含めない。
- 既存の`undefined`／`{result}`というhook返却は画像候補を保持する。stdoutだけの変更や、結果全体を`null`・文字列等へ置換したことを、画像除外の暗黙の指示にはしない。
- `afterTool`だけに`{result, imageIds}`の返却を追加する。`imageIds`を省略すれば現在の候補を保持し、`[]`ならすべて除外、IDの配列ならその部分集合だけを残す。候補内の元の順序を保ち、複数hookでは直前の選択結果を次の`event.images`へ渡す。
- IDはその時点の`event.images`の部分集合で、重複を認めない。除外済みIDの再追加、他tool/runのID、未知のIDはhook入力エラーとしてそのbatchをabortする。metadataを書き換えて別画像へ差し替えることはできない。`beforeTool`の返却契約は変更しない。
- ホストは最終候補を自身のpending情報と照合する。`tool.finished`には変更前後の画像metadataを`rawImages`・`images`として通常の`rawResult`・`result`と分けて記録し、`modelResultAvailable: false`ならモデルへ渡す画像も空にする。画像metadataの有無をstdoutから推測しない。
- `run_js`の例外・取消し・`afterTool`の失敗で結果をモデルへ渡せない場合は、そのtoolの新しい添付も確定させない。read失敗は通常のJS例外として返し、agentが修正できるようにする。
- 生成要求は通常JSではevaluationのOperation、`run_js`では子scopeに所有させる。親runのinterrupt、timeout、Ctrl+CでHTTPと一時ファイル出力を取消し・回収する。生成要求の終端イベントは一度だけ記録する。
- 生成要求は既存execution groupの`providerCalls`上限に含め、`imageProviderCalls`も記録する。単なる画像読取りはprovider呼出し回数を増やさない。計数変更でagent/decisionの分類を壊さない。

### providerの拒否とエラーの返却

生成APIは失敗時にPromiseをrejectし、`name: "ImageError"`を持つ例外へ次の情報を載せる。RPC・JS・JSONLを通して保持し、SDKの`Error()`文字列だけに変換しない。

| フィールド | 内容 |
| --- | --- |
| `code` | NOE側の分類：`input`・`refusal`・`authentication`・`quota`・`rate_limit`・`unsupported`・`transport`・`response`・`filesystem`・`cancelled` |
| `message` | 呼出し元が読める失敗理由 |
| `providerCode`・`providerMessage` | providerが返したcode/message。取得できた場合に保持する |
| `statusCode`・`requestId` | 取得できたHTTP statusとprovider request ID |

- `moderation_blocked`などproviderが明示した内容拒否は`refusal`、明示された入力不正は`input`、機能非対応は`unsupported`とする。HTTP 400を一律に同じ分類へ落とさず、未知のエラーを内容拒否と推測しない。
- 認証・利用枠・レート制限・通信・保存・取消しも上表で区別する。HTTP 200でも画像がなく、明示的なrefusalだけがある場合は拒否として返す。空応答だけなら`response`とする。
- provider由来の情報はcode/message/request ID等の選択したフィールドだけを扱い、認証header・SDK request dump・画像Base64・応答body全体を付けない。provider messageにcredentialや画像payloadが混入する経路をfixtureで確認し、それらの値はJS・ログへ渡さない。
- `image.failed`には同じ分類・providerCode・取得できるproviderMessage/status/request IDを記録する。取消しは`image.cancelled`とし、拒否や入力不正で自動再生成・別providerへの切替は行わない。
- Vision要求でprovider/modelが明示したrefusalも、入力形式不正や通信障害と区別してrun結果・観察へ残す。既存agentのerror文字列は維持し、拒否の分類・取得できる理由を追加情報として保持する。新しい画像経路のために通常agent/decisionのエラー契約を一括置換しない。
- NOE独自の内容フィルターは追加しない。providerごとに拒否基準が異なり、すべてのセンシティブな内容が必ず拒否される保証をAPIの契約にしない。

## コンテキスト予算と観察

- Base64文字列を従来のJSON bytes/4で換算しない。テキスト概算に、画像の寸法・detail・確認済みのモデル仕様に基づく画像概算を加える。正確なtokenizer値とは区別し、公式式が不明な場合の推定方法とsourceをS2着手前に記録する。
- 画像添付後の見積→必要な切り捨て→切り捨て後の要求・通知、という既存順序を守る。保護された最新blockだけで閾値を超える場合は、既存の超過/保持不能表示で分かるようにする。
- `image.read`と画像生成の`image.started/completed/failed/cancelled`を既存JSONLへ接続する。imageId・path・MIME・bytes・寸法・生成model・duration・取得できるusageを記録する。
- provider用の画像bytes/Base64はイベント生成境界で除外し、`tool.finished.rawResult`やhook診断へ混入させない。保存ログ単体で画像会話を完全再生する機能は追加しない。
- `--observe`と親向け`observe_agent`では、読んだ画像・生成先・結果を短く確認できるようにする。画像生成のmodelで親agentのmodelを上書きしない。

## 実装スライス

| Slice | 対象・作業 | 受入条件 |
| --- | --- | --- |
| S0 スパイク | `spikes/agent-images/`、実接続結果、接続方式の確定 | 固定版SDKでVision・生成を確認。失敗理由・認証経路の差を記録し、APIキーとChatGPTプランを区別できる。本体未変更 |
| S1 画像読取りと所有 | `internal/image/`相当の独立契約、runtime module、親run/model stepのpending、broker/runnerへの画像登録・引渡し | path解決、bytes固定、MIME、複数画像、子close後のpending保持、abort後の遅延登録の拒否、run間分離をfixtureで確認。従来のテキスト結果を維持 |
| S2 Visionと履歴 | provider adapter、`js/history.js`・`js/task.js`、Goのconversation検証/checkpoint、context見積、hookの`event.images`/`imageIds` | Chat/Responsesへ画像パートを送る。hookの保持/除外、batch・checkpointのcommit/abort、reasoning再送、queue、受理済みtrimと解放を確認。未確定画像は後続queueに残らない。Visionの明示的なrefusalも区別 |
| S3 画像生成provider・catalog | 独立した生成interface、OpenRouter/OpenAI SDK adapter、provider JSON、catalogのimage kind検証/列挙/JS入口、構造化エラー | 専用endpoint・認証・設定上書き・一枚保存・usage・拒否分類・取消しが成立。image登録と既存agent/decisionの起動/列挙が共存。通常agent用Generate interfaceを全fixtureへ拡張しない |
| S4 生成のJS接続と観察 | `generate_image` RPC/native、Operation、execution、構造化エラーのJS/JSONL返却、CLI/親向け観察 | run/eval/REPLとrun_jsで使える。親と異なるproviderで生成しpathを取得。拒否・入力不正・取消し・通信失敗を呼出し元と観察で区別し、途中失敗で既存ファイルを破損しない |
| S5 利用文書・応用例 | `docs/agent-images.md`、API/provider/catalog/observe/events/hooksガイド、README、instructions、`examples/image/` | pathを知らせる→読む、通常JSで直接生成、生成→読む、hookでの画像除外、構造化エラーの利用を例示。S3のcatalog実装を文書化し、使える認証と未対応経路を明示 |
| S6 総合確認 | 実CLI fixture E2E、必要なrace、実providerの行動確認、全体コードレビュー | 下記の成功条件と回帰を確認し、未確認を完了扱いにしない。実接続記録と実装記録を残す |

順序はS0→S1→S2→S3→S4→S5→S6。各sliceで実装・関連テスト・コードレビュー・修正を完了して進む。VisionだけでもS2時点で試用できるようにする。

主な変更候補は`internal/agent/{broker_tools,broker_tool_result,context,context_history,execution}.go`、`internal/agent/js/{task,history}.js`、`internal/agent/provider/`、`internal/runtime/`、`internal/cli/`、`internal/observation/`。go-quickjsのstdlib自体は変更しない。

生成経路の接続先・パラメータはproviderのJSONへ持ち、組込みdefault＋外部差分上書きを維持する。通常agentのChat/Responses設定を画像専用経路で置き換えない。具体的なJSON項目はS3で既存model overlayの検証と合わせて追加する。

`catalog.kind: image`の導入は文書sliceへ後送りせず、S3の最初にGoの定義検証・query検証と公開JSの`list_models`入力検証へ追加する。その後に組込み/外部JSONへ画像モデルを登録する。S3の受入試験には`list_models()`・`kind: image/agent/decision`、登録済み画像モデルと同じregistryからの通常agent/decision呼出し、外部上書きを含める。image kindの導入によってregistry全体が不正になる状態を残さない。

## 総合テスト

通常テストはローカルfixtureで行い、実プロバイダを自動で呼ばない。

1. `run_js`が同梱画像を読み、次要求に実際の画像パートが入る。stdoutに画像path/Base64を書いただけのケースでは添付されない。
2. 二画像の順序・寸法・元bytes、異なるtoolCallId/agent/run間の分離。読取り後の元ファイル変更・削除で履歴が変わらない。
3. ChatとResponsesでassistant reasoning・tool IDを保ち、画像を含む完全なblockが成立する。テキスト専用の既存例は従来どおり動く。
4. keepAliveの次queueで画像を参照でき、context trim後は対象blockだけが失われる。未送信/最新画像は送る。trim案の段階では解放せず、受理後に不要画像を解放する。
5. hookの無変更、`{result}`によるstdout変更・null置換、`imageIds: []`、部分選択、複数hook、未知/重複/他toolのID、例外を確認する。モデルへ渡った画像と`rawImages`/`images`が一致する。
6. 子Operation close後、他toolの完了待ち、片側の通常JS失敗、片側のhook例外、checkpoint拒否/応答欠落、取消しと遅延readの競合を確認する。確定済み画像を失わず、未確定画像や参照が後続queueへ残らない。
7. 通常JS・REPL再利用・agentの生成要求で、path保存、内容拒否、入力不正、認証/利用枠/通信失敗、不正/空画像、保存エラー、取消しを区別する。同じHTTP 400でも`moderation_blocked`と入力形式エラーのcode/providerCodeが異なり、JS/JSONLで理由を取得できる。既存ファイルと他要求の生成物を巻き込まない。
8. 親のprovider/modelと生成先を別にし、生成結果を`read_image`で親へ返す。providerCalls上限とinterruptが生成要求にも効く。
9. NOEが管理する画像添付・providerエラーのJSONL・可読表示・親向け観察にBase64やcredentialが入らず、metadata・生成結果・usageを確認できる。任意の利用者JSが明示的に出したstdoutを内容判定で削る機能は追加しない。
10. image kindを含む同じ内蔵/外部設定で、agent/decision/image各catalog取得と通常のagent/decision実行が成立する。未知のkindの入力エラーも維持する。

関連確認後に`go test ./...`、`go test -tags=e2e ./...`、`go vet -tags=e2e ./...`を実行する。画像所有・取消しを変更したpackageはraceでも確認する。配布targetのCGOなしbuildを確認し、nativeでの実行確認とは区別する。

実接続はS0で確認できたprovider/modelを使い、少数のケースで次を確認する。

- 親が指示文のpathを受け、自発的に`read_image`を選んで、画像の内容に合う回答をする。
- 作業中に新たに作った画像を読み、次の判断に使う。
- OpenRouter/OpenAI APIキーの各一枚生成を保存し、画像として開けることと指示への適合を確認する。
- Vision対応の親が別providerで画像を生成し、その保存先を読み直して内容を確認する。

モデルの表現揺れや画像の完全一致を成功条件にしない。実際の要求モデル・経路・画像ファイル・usage・時間・失敗と再試行の理由を保存する。ChatGPTプランの画像生成が非対応なら、その結果を明示し、対応を装って別経路へ切り替えない。今回の実接続許可はこのスパイク・機能確認に引き継ぎ、既存の認証登録を利用する。

## 完了条件

pathを知らせたagentが画像を見て判断でき、独立した生成APIの結果をファイルに保存して必要なら見直せること。既存の履歴・queue・reasoning・hook・context・取消し・観察と整合し、対応provider/認証と実確認範囲を文書化する。本体実装とS1〜S6の検証を完了して初めて機能完成とする。結果は[実装記録](agent-images-implementation.md)へ残す。

## 計画レビュー指摘の反映

2026-10-18、独立reviewerのP2指摘4件を採用した。この節は実装着手前のレビュー履歴。実装後の結果は[実装記録](agent-images-implementation.md)に記載。

| 指摘 | 反映した契約 | 担当slice |
| --- | --- | --- |
| hook置換後の画像の採否が未定義 | `event.images`と`imageIds`を分離。既存の結果置換は保持、明示した部分集合で除外。ホストで照合し、変更前後の画像metadataを記録 | S2 |
| 登録から履歴確定までの所有と失敗時解放が未定義 | 親run/model stepのpending、ホストcheckpointでの移管、batch/checkpoint失敗・取消し時のabort、確定済み画像の保持を規定 | S1・S2 |
| image kindの実装sliceが不足 | Go/JSのkind検証・列挙をS3へ移し、JSON登録より先に実装。同一registryでagent/decisionとの共存を確認 | S3 |
| provider拒否理由の返却が未定義 | `ImageError`のcode/providerCode/providerMessage/statusCode/requestIdと、JS/JSONLでの返却を規定。Visionの明示的refusalも識別 | S2・S3・S4 |

[開発記録へ戻る](../README.md) · [検討メモ](../future-candidates.md#完了-agentのvision対応--2026-10-17)

## 実装時の画像token推定方針（S2着手前、2026-10-18）

履歴には画像IDのみを置き、Go所有の実寸法から画像tokensを別計算する。現対象のGPT-6 Luna/Solについて確認した公式表に固有の倍率がないため、全モデルに明示したfallbackを使う。32px patch数 `ceil(width/32)*ceil(height/32)` の1.2倍を切り上げる。`low`は512px四方内へ縮小した寸法、`high/auto`は原寸を使う。これは料金/tokenizerの正確値を装わない推定であり、providerのresizeで実量との差が出る。Methodへ`image_32px_patches_x1.2_fallback`を付け、画像分のtokensを別フィールドへ表示する。Base64文字数は算入しない。

根拠: [OpenAI Images and vision](https://developers.openai.com/api/docs/guides/images-vision) のpatch式を参考にしたfallback。固有モデルの仕様を後日確認できた場合に置き換える。
