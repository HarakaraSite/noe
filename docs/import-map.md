# ローカルESMとimport map

自作JSや動作確認済みのESMを名前でimportできます。`run`・`eval`・`repl`で同じJSONを指定します。

```sh
./noe run --import-map examples/import-map/import-map.json examples/import-map/main.js
./noe eval --import-map examples/import-map/import-map.json 'import {sum} from "app/math"; console.log(sum([1,2,3]))'
./noe repl --import-map examples/import-map/import-map.json
```

REPLではdynamic importを使います。

```js
const math = await import("app/math")
math.sum([1, 2, 3])
```

`--import-map path`と`--import-map=path`に対応します。optionはscriptやevalのコードより前に置きます。script名以後と`--`以後は従来どおり引数として渡します。自動探索や環境変数によるmap指定はありません。共通mapを使う場合も`--import-map ~/.config/noe/import-map.json`で明示します。

## 配置と名前解決

```text
project/
  config/import-map.json
  lib/math.mjs
  scripts/task.js
```

`config/import-map.json`：

```json
{
  "imports": {
    "app/math": "../lib/math.mjs"
  }
}
```

`scripts/task.js`：

```js
import { sum } from "app/math";
console.log(sum([1, 2, 3]));
```

mapファイルの指定は起動時のcwd基準、map内の相対targetは**mapのあるディレクトリ基準**です。ライブラリ内部の相対importは**そのライブラリのファイル基準**になります。利用するscriptの配置が変わっても、aliasのtargetは変わりません。

対応する指定は以下です。

- 組込み名：`node:fs`・`fs`・`noe:agent`等。既存の意味を保ち、mapから上書きできません。
- mapの完全一致bare名：`app/math`・`@example/helpers`等。
- 相対path：`./lib.js`・`../lib.mjs`。
- OSの絶対path、ローカル`file:` URL：空authorityまたは`localhost`。URLのquery・fragment・非ローカルauthorityは未対応です。

ローカルファイルは`.js`か`.mjs`を明示します。拡張子補完、ディレクトリのindex探索は行いません。Windowsのdrive絶対pathとnative UNC pathも扱います。drive相対pathは未対応です。通常path中の`#`・`%`はファイル名の一部で、file URLの場合はpercent-encodingを使います。

初回のmap形式はtop-levelの`imports`だけです。末尾`/`のprefix、`scopes`、`integrity`、nullによるblock、複数mapのmerge、path/URL自体のremap、別aliasへの連鎖は未対応として診断します。HTML Standardのimport map全体を実装したものではありません。

重複keyや型不一致、組込み名との衝突は起動時エラーです。未使用targetの存在や構文は起動時に調べず、実際のimport時に確認します。

## agentとWorkerへの継承

起動時にmapのtargetを絶対化し、変更不能の対応表にします。同じ対応表をWorker、agentのtask bootstrap、各`run_js`、子agentへ渡します。元mapを実行中に編集・削除しても再読込みしません。agentの一時ソースからも`await import("app/math")`を使えます。モデルへは使わせたいaliasとAPIをタスクかsystem指示で伝えてください。

```js
import { run_agent } from "noe:agent";

const snapshot = await run_agent(
  { provider: "openrouter", model: "deepseek/deepseek-v4.1-flash", effort: "auto" },
  'run_jsでapp/mathのsum([10,20,30])を実行して結果を報告してください。'
);
console.log(snapshot.lastResult.answer);
```

Workerの**内部のimport**にmapを使えます。Worker constructorの入口名は従来のpath/file URL指定です。Workerからの`noe:agent`・`noe:decision`・`noe:auth`利用は引き続き未対応です。任意の`noe:process.spawn()`で起動する外部プログラムへはoptionを自動注入しません。

## ソースの寿命と互換性

固定するのは対応表です。ライブラリ一式を起動時にsnapshot化する機能ではありません。root・Worker・各agentプロセスは独立したruntimeで、最初のimport時にファイルを読みます。同一runtimeでは読取り済みsourceとmoduleを再利用し、別alias・別referrerでも同じnamespaceになり、副作用は一度だけ実行されます。ファイルを変更して反映するには新しいruntime/プロセスを起動します。読取りに失敗したファイルは、作成後に再試行できます。

Executeのin-memory入口も保持します。Worker入口はstdlibの専用読込みhookを通るため、この保持保証の対象外です。Worker入口そのものを削除し、その入口を新aliasから自己importすると失敗しうる点は残ります。Workerがimportした依存ライブラリは保持対象です。

module IDは字句上の絶対pathで統一します。symlink・hardlink・filesystemの大小文字による別名まで統合する保証はありません。永続cacheやruntime間のsource/JS Value共有はありません。

ESM形式でも、必要なホストAPIがNOEにあるかは別途確認が必要です。npm/package.json/node_modules探索、CommonJS loader、TypeScript変換、HTTP/HTTPS importは提供しません。外部ツールで準備したbundleと、NOE向けに適合させたJSを利用できます。[MCP SDKの分割例](../scripts/spikes/process-io-mcp/README.md)を参照してください。

[API](api.md) · [REPL](repl.md) · [実装・検証記録](local-import-map-implementation.md)
