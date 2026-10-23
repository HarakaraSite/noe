# 実行結果と診断の受け取り・移行

[ドキュメント一覧](README.md) · [JS検査](javascript-checks.md) · [API説明](reference.md) · [イベント](events.md)

このページはv2.3.0以降の契約です。v2.1.1から、run_jsの暗黙agent importを廃止し、返却dataと診断を標準出力から分けています。

## run_jsのデータ返却

通常のES moduleとして、使うAPIを明示importします。agent操作も同様です。

```js
// Inspect available models and return their identifiers.
import { list_models } from "noe:agent";

const models = await list_models({kind: "agent"});
export default models.map(({provider, model}) => ({provider, model}));
```

これはmodel生成要求を送らず、設定済みcatalogを返す例です。run_jsの返却は次のように分かれます。

| field | 意味 |
| --- | --- |
| `data?` | default exportを待機してJSON化した値。defaultなしならfieldなし。明示nullは`data: null` |
| `stdout` / `stderr` | 実際に書き込んだ出力。診断を合成しない |
| `exitCode` / `reason` | 実行状態。reasonはcompleted/failed/timeout/cancelled |
| `diagnostics?` | NOEが観測した失敗情報。出力とは別に保持 |
| `stdoutOmittedBytes?` / `stderrOmittedBytes?` | 各streamの既存64 KiB捕捉上限で省いたbyte数 |

成功例は`{"stdout":"","stderr":"","exitCode":0,"reason":"completed","data":[...]}`です。最後の式だけではdataになりません。`console.log`を使った既存コードは引き続きstdoutを返します。

defaultのPromise/thenableは待機します。default関数は自動で呼びません。JSON化は一度だけ行い、標準JSON.stringifyの規則を使います。root undefined/function/Symbol等にJSON表現がない場合や、BigInt・循環・変換例外等では失敗します。nestedな省略やnull化、toJSON/getterの実行も標準規則の範囲です。nested Promiseは深くawaitしないため、必要な値を先にPromise.all等で確定させます。

entry/defaultとLoop上の仕事が完了してからJSON化し、JSON化が追加した仕事も同じcontext/Loopで処理してから成功を確定します。JSON化を再実行しないため、その後の値の変更はsnapshotへ反映しません。後続仕事が失敗した場合はdataを破棄します。cleanupだけが失敗した場合は、確定済みdataとcleanup診断を返します。
未処理例外や取消しによる後続失敗ではdataを破棄します。単にprocess.exitCodeが非zeroとなった場合は、その終了codeを返し、確定できたdataは保持します。dataを途中で切断して成功扱いにしたり、自動で全文artifactを保存したりはしません。大きな成果物はファイルへ保存して、小さな要約とpathを返せます。

## 診断の読み方

診断要素は`{code, stage, message, stack?, position?, exception?}`です。

- code: execution_error、serialization_error、cancelled、timeout、cleanup_error。
- stage: setup、execute、serialize、cleanup。NOEが観測した段階で、engine内部のparse/link/evaluateを正確に分けたものではありません。
- stack: 取得できた原文。全frameの構造化や推測による位置補完はしません。
- position: engineから実際に得られたsource・1始まりのline/column。取得できない成分は省きます。
- exception: own data propertyから得たcode/signal/stdout/stderr等。NOEの分類codeと例外のcodeは別です。

command失敗時のstdout/stderrは`exception`内に保持します。`encoding: "buffer"`等のbyte出力は`{"encoding":"base64","data":"..."}`です。診断をstdout/stderrの省略で消しません。

上流公開APIでは、動的importの拒否で構文エラーの構造化位置を取得できないことがあります。その場合はpositionを省き、得られたmessage/stack原文を残します。位置を補うための再parse・追加の静的検査は行いません。

stageがserializeの場合は、ファイル変更や外部処理を済ませた後で返却変換だけが失敗した可能性があります。NOEは元sourceを再実行しません。副作用を確かめてから修正してください。catch済みの失敗を表示しただけの場合も、NOEは文面から実行失敗を推測しません。

## hook・履歴・観察

runner結果のdata/diagnosticsはafterToolの入力へ渡ります。afterToolによる任意のresult加工は維持し、モデルには加工後の結果を渡します。JSONLの`tool.finished`にrawResultとresultを別々に保存します。hook自体が失敗した場合は既存のhookError/modelResultAvailable経路を使います。

`run_js.finished`は元のdata/diagnosticsを記録します。agent task自体のsetup/VM/cleanup診断は`execution.diagnostic`へ記録し、agentId・commandIdと、判明している場合だけrunIdを付けます。まだ存在しないrunIdは補いません。
CLIの観察では診断を別項目に表示します。ログ解析の`includeOutput`はstdout/stderrと返却dataに適用し、diagnosticsは省略しません。起動前の診断などrunIdがないeventは、runIdで絞ったsnapshotには属さず、全体ログ・CLI観察から確認します。
画像は従来の専用経路で配送し、dataへ埋め込みません。provider native履歴やsession/cacheの識別方法も変更しません。

## Go consumer

`stdlib.Execute(ctx, source, profile) (stdlib.Result, error)`は維持します。Result.Codeに加え、DataとDiagnosticsを使えます。Dataはjson.RawMessageで、nilなら返却なし、`"null"`なら明示nullです。JS値をVM外へ渡しません。

```go
result, err := stdlib.Execute(ctx, stdlib.Source{
    Name: "[result]", Virtual: true, Module: true,
    Code: "export default {answer: 42};",
}, profile)
if err != nil && len(result.Diagnostics) == 0 {
    // Error classification and native causes remain available via errors.Is/As.
    fmt.Fprintln(stderr, stdlib.Diagnostic(err))
}
for _, diagnostic := range result.Diagnostics {
    fmt.Fprintln(stderr, diagnostic.Code, diagnostic.Stage, diagnostic.Message)
}
if result.Data != nil {
    // This is JSON, including an explicit null when requested.
    fmt.Fprintln(stdout, string(result.Data))
}
```

この例の表示先は呼出し元が選びます。stdlibが診断をProfile.Environment.Stderrへ自動追記することはありません。単なるscript（Module=false）のcompletionとWorker entryの実行方式は維持し、新しいdefault dataはmanaged moduleの結果として返します。

既存`Diagnostic(err) string`とGo causeの追跡も維持します。Resultへfieldを追加したため、位置指定のstruct literalを使うconsumerはfield名付きへ変更する必要があります。

## 移行する箇所

1. run_jsの`run_agent`等を暗黙bindingとして使っていたコードへ、必要な明示importを追加する。
2. 構造化した返却値が必要なら、最後の式やconsole.logだけに依存せずdefault exportを使う。
3. stderr内のNOE診断文字列を調べていた処理は、diagnosticsのcode/stage/exceptionを読むよう変更する。実際のstderrは従来どおり扱う。
4. afterToolの任意加工結果とraw runner結果を区別し、diagnosticsを落とすhookがあれば目的に応じて更新する。

Biome/tscの利用は任意の既存ツールのままです。この変更でchecker、導入処理、新しい実行前ゲートは追加しません。
