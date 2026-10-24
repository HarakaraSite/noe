# JSONLイベントログの比較

NOEのJSONLイベントログをローカルで読み、ファイルごとのtoken・cache利用・tool出力超過を比較します。Node.js / npm / 追加依存 / provider認証は不要です。NOE v2.4.0で検証しています。

## CLI

repositoryルートから、PATH上のNOEで実行します。

```sh
noe run examples/log-analysis/compare.js before.jsonl after.jsonl
noe run examples/log-analysis/compare.js --format json --below 0.8 before.jsonl after.jsonl > comparison.json
noe run examples/log-analysis/compare.js --format markdown --below 0.5 before.jsonl after.jsonl > comparison.md
noe run examples/log-analysis/test.js
```

- `--format json|markdown`: 既定はMarkdown。JSONはファイル順のreport配列です。
- `--below 0..1`: 低ヒット要求の閾値。既定は`0.5`（50%未満）。既知の比率を昇順で表示し、比率不明・入力0の要求も末尾に残します。総計には閾値を適用しません。`1`でも100%ヒット要求は除外します。
- `--`: 以後はすべてファイル名として扱います。`--help`で用法を表示します。
- 出力は標準出力へ。不正なオプション、読取り失敗、不正JSONでは標準エラーへエラーを出し、非0で終了します。不正JSONはファイル名と1始まりの行番号を含みます。全入力を解析してから出力するため、途中までの比較結果は出しません。空行は無視します。

## 集計の意味

- 要求数は`model.finished`の件数です。tokenはそのイベント直下の`usage.promptTokens`（なければ`usage.inputTokens`）と`usage.cachedTokens`だけを使います。`execution.usage`等の累積snapshotや他のイベントのusageは加算しません。
- 非cache tokensは、同一要求で入力・cachedが両方既知の場合だけ`input - cached`で求めます。cacheヒット率は同じ両方既知の要求集合で`sum(cached) / sum(input)`を計算します。要求ごとの比率の平均ではありません。
- metricは`{ value, knownRequests, totalRequests }`で表現します。欠落・null・非数値・負値は不明扱い。既知値が一つもなければ`value: null`、一部だけならその部分合計とcoverageを表示します。`0`は既知の0です。入力とcachedの独立した部分合計を割って比率を捏造しません。cachedが入力より大きい要求も非cache・比率は不明とします。
- cache比率の`value`は0〜1の小数（Markdownでは%）、分母0では`null` / `unknown / n/a`です。比率metricには計算に使った`inputTokens`と`cachedTokens`もあります。要求行の`rateStatus`は`known` / `unknown` / `zero-input`です。
- tool数は`tool.finished`の件数。出力超過は**モデル向けの`result.outputLimit.complete === false`**の件数です。`rawResult`やstdoutの既存capture省略・省略を示す文章は数えません。resultが存在しoutputLimitがない場合は「超過なし」。正常なJSON結果の`result: null`も既知の「超過なし」です。result field自体の欠落、またはhook失敗等の`modelResultAvailable: false`の場合は不明としてcoverageに反映します。
- `requests`には閾値未満と不明の要求だけを載せます。入力・cached・非cache・比率に加えてファイル内行番号、execution/run/model-step/request ID、modelを記録します。完全なtool入力・出力本文は出力しません。IDがない場合も行番号で元ログに戻れます。

## ライブラリ

`analyze.js`から`analyzeJSONL(text, fileLabel, { below: 0.5 })`と`toMarkdown(reports)`をimportできます。CLI入口は`compare.js`です。`fixtures/`と`test.js`はsnapshot二重加算、token加重比率、captureとの区別、欠落と0、分母0、行番号付きエラーなどを小さな人工ログで検証します。

ファイル全体をメモリに読み込むバッチ処理です。追記追従・イベント重複排除・異なるログ間のrun統合はしません。モデル要求が途中で終わっていて`model.finished`がない場合、その要求のtokenは推測しません。
