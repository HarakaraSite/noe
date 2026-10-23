# 生成したJavaScriptを既存ツールで確認する

[ドキュメント一覧](README.md) · [APIを調べる](reference.md) · [agent](agents.md) · [import map](import-map.md)

Biome／TypeScriptは任意の外部ツールです。NOEの実行に必要な依存ではなく、NOEが自動で導入・起動する機構もありません。使えるツールとprojectの設定に応じて、生成したJSの実行前に確認します。

## 1. 利用できる検査を選ぶ

作業するprojectのルートで、既存の設定・package scripts・ローカル実行ファイルを先に確認します。`node_modules/.bin/biome`、`node_modules/.bin/tsc`があれば、そのprojectの版を優先します。以下のPATH確認は実行ファイルの所在を調べるだけで、取得・インストールは行いません。

```sh
pwd
command -v biome
command -v tsc
```

PATHにない場合もproject内に導入されていることがあります。どちらにもなければ、その検査は未実施とします。実行ファイルがある場合だけ、選んだものの`--version`を確認してください。

| 環境 | 行うこと・報告すること |
| --- | --- |
| 両方ある | Biomeで構文・lint、必要な型情報がある範囲をtscで検査する |
| Biomeだけある | 構文・lintを確認する。型検査は未実施と報告する |
| tscだけある | 設定と型情報に基づく検査を行う。Biomeのlintは未実施と報告する |
| 両方ない | 静的検査は未実施と報告する。API説明を読み、依頼範囲で必要な実行確認を行う |
| 実行ファイルはあるが起動・設定・型情報に問題がある | 検査が成立しなかった理由を報告する。コードの合格と扱わない |

未導入だけを理由に、元の作業を自動で中断する必要はありません。静的検査が必須の依頼なら、その条件が未達であることを伝えます。それ以外は利用できる確認を進め、未確認範囲を残します。導入が必要ならprojectの管理方法と利用者の指示に従い、検査名目で環境や依存を自動変更しません。
この手順では、packageを取得する可能性がある`npx`／`npm exec`を起動確認の代用にしません。

## 2. 生成したJSをファイルへ保存する

例はprojectルートの`generated/task.js`です。相対importを含む場合は、検査用に保存した場所と実際に実行する場所の違いを確認します。

```js
/** @param {number[]} values */
export function sum(values) {
  return values.reduce((total, value) => total + value, 0);
}

export default await Promise.resolve(sum([10, 20, 30]));
```

run_jsは作業ディレクトリを基準に相対importを解決し、ファイルのmoduleはそのファイルを基準に解決します。同じ内容でも保存先が変われば同じ依存を読むとは限りません。[import map](import-map.md)も確認してください。
後続の検査・実行は、修正後の同じ内容を対象にします。

## 3. Biomeがある場合：構文・lint

既存のBiome設定があるならそれを使います。以下はBiome 2系向けの小さな設定例です。新しい検査用projectで`biome.json`へ保存するもので、利用中の設定を上書きする手順ではありません。

```json
{
  "linter": {
    "enabled": true,
    "rules": {
      "recommended": false,
      "correctness": {
        "noConstAssign": "error",
        "noUndeclaredVariables": "error"
      }
    }
  }
}
```

選択した実行ファイルで、対象と設定を明示します。以下はPATH上のものを使う例です。project内のものを使う場合は`biome`を`./node_modules/.bin/biome`へ置き換えます。

```sh
biome --version
biome lint --config-path ./biome.json ./generated/task.js
```

この例はJSを書き換えません。診断の位置・規則名・終了codeと、対象ファイルが実際に処理されたかを読みます。未処理、除外、サイズ超過等で対象を検査していなければ、そのファイルの合格にはしません。warningは終了codeが0でも残る場合があります。
合格は適用した規則の範囲に限られます。NOEで使えるmodule・export・Profile権限や型を保証するものではありません。
コマンドと設定の仕様は[Biome CLI](https://biomejs.dev/reference/cli/)・[設定ガイド](https://biomejs.dev/configuration/configure-biome/)を参照してください。

## 4. tscがある場合：JSの型検査

既存projectの検査手順があればそれを優先します。独立した小さな例では、次をprojectルートの`tsconfig.noe-checks.json`へ保存します。

```json
{
  "compilerOptions": {
    "allowJs": true,
    "checkJs": true,
    "noEmit": true,
    "strict": true,
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "lib": ["ES2022"],
    "types": []
  },
  "files": ["./generated/task.js"]
}
```

```sh
tsc --version
tsc --project ./tsconfig.noe-checks.json --pretty false
```

project内の版を使う場合は`tsc`を`./node_modules/.bin/tsc`へ置き換えます。`--project`とソースファイルの引数は併用せず、対象は設定の`files`等へ書きます。`files`の相対pathは設定ファイルの場所が基準です。
この例ではJSを型検査し、出力ファイルを生成しません。JSDocの`number[]`に対し`sum(["30"])`とすれば型不一致になります。[JS型検査](https://www.typescriptlang.org/docs/handbook/type-checking-javascript-files.html)・[noEmit](https://www.typescriptlang.org/tsconfig/noEmit.html)を参照してください。

`types: []`は、見えている`@types/node`等をglobal型へ自動追加しない指定です。`lib: ["ES2022"]`はこの純粋な計算例に必要な範囲です。NOEのglobalや独自moduleはこの設定だけでは型検査できません。`DOM`や`@types/node`全体を追加して、NOEで未対応のAPIまで使えることにしないでください。[types](https://www.typescriptlang.org/tsconfig/types.html)・[lib](https://www.typescriptlang.org/tsconfig/lib.html)の範囲も確認します。

### NOE APIの宣言ファイル

noestdlibの[宣言](../stdlib/types/noestdlib.d.ts)とnoeagentlibの[宣言](../agent/types/noeagentlib.d.ts)を別々に参照します。`noe:agent`だけでなく、`noe:model`・`noe:image`・`noe:decision`・`noe:auth`・`noe:agent-log`もagentlib側の所有です。これは本変更を含む開発版sourceの機能で、公開済みv2.1.1のsourceには未収録です。

宣言と下記の利用例はTypeScript 5.9.3で確認しています。古いcompiler等で宣言の構文を読めない場合も、コードの型不一致と区別して検査環境の問題を報告します。

バイナリだけの環境では、対応する版・commitのsourceから必要な宣言ファイルを取得し、projectへ配置してください。NOEがdownloadや単一ファイルへの統合を行う機構はありません。以下はこのrepositoryのルートをcwdとして、両ファイルを元の場所で参照する例です。別の場所へ置く場合は設定ファイルからの相対pathを変更します。

`generated/noe-task.js`:

```js
import { readdirSync, statSync } from "node:fs";
import { list_models } from "noe:agent";

const directories = readdirSync(".").filter(name => statSync(name).isDirectory());
const models = await list_models({kind: "agent"});
console.log(JSON.stringify({
  directories,
  models: models.map(({provider, model}) => ({provider, model})),
}));
```

`tsconfig.noe-api-checks.json`:

```json
{
  "compilerOptions": {
    "allowJs": true,
    "checkJs": true,
    "noEmit": true,
    "strict": true,
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "lib": ["ES2022"],
    "types": []
  },
  "files": [
    "./stdlib/types/noestdlib.d.ts",
    "./agent/types/noeagentlib.d.ts",
    "./generated/noe-task.js"
  ]
}
```

```sh
tsc --project ./tsconfig.noe-api-checks.json --pretty false
```

この例の`list_models`は設定済みcatalogを読む操作で、モデルへの生成要求は送りません。関数名が同じNode APIでも、NOEの署名は異なります。例えば`readdirSync`は文字列配列を返し、`withFileTypes`は受け付けません。
型不足の診断を実装バグと決めつけたり、空の`declare module`や`any`で抑えてNOE APIの型検査済みとしたりしません。[内蔵docs](reference.md)で契約を読み、型未確認の範囲を明示します。
宣言があっても、tscのmodule解決はNOEのimport mapやloaderを実行して検証するものではありません。型宣言は対応APIを説明するもので、Profileが選んだ能力やWorkerでの利用可否を自動判定しません。

## 5. 検査結果と実行結果を区別する

静的検査に合格しても、外部状態や実行時の値で失敗することがあります。例えば次は構文・型が正しくても実行時にJSONのparseで失敗します。

```js
export default JSON.parse("{");
```

`noe run generated/task.js`やrun_jsはコードを実際に動かします。ツール未導入時の「実行しない構文チェック」の代わりではありません。実行は元の依頼範囲で必要なときに行い、結果・例外・副作用を確認します。
NOE独自の静的検査API、Biome／tscの組込み、検査失敗時の自動再実行は追加しません。

報告ではcwd、対象の版・内容、使った実行ファイルと版、設定、command、終了code、検査対象数や未検査理由を必要な範囲で示します。例:

```text
対象: generated/task.js、修正後の内容
cwd: projectルート
Biome 2.5.15: biome lint --config-path ./biome.json ./generated/task.js
  終了code 0、1ファイル処理、適用した構文・lint検査に合格
tsc: 未導入のため型検査は未実施
NOE実行: 未実施
```

「未導入」「起動・設定エラー」「対象外」「コードの診断」「合格」「実行成功」は別々に扱います。未検査を合格や元の作業の失敗へ一律に置き換えません。
