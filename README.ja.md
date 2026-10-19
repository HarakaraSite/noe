# NOE — No Ordinary Engine

Just JavaScript. Until you need an agent.

[go-quickjs](https://github.com/go-quickjs/go-quickjs)を使った、純Go製の小さなJavaScriptランタイムです。小さなスクリプト、ファイル加工、HTTPサーバー、自律agentのタスクを実行できます。agentはファイル操作・JSやコマンドの実行・修正・回答まで、人間の承認を待たずに進めます。

この文書は日本語版です。正本は[英語版README](README.md)です。

[v1.14.1リリースノート](docs/releases/v1.14.1.md) · [バイナリのダウンロード](https://forge.harakara.site/littleisland/noe/releases/tag/v1.14.1)

## ビルドと実行

上のリンクからバイナリを取得するか、Go 1.27.2以降でビルドします。Cのツールチェーンは不要です。

```sh
git clone https://forge.harakara.site/littleisland/noe
cd noe
CGO_ENABLED=0 go build -o noe ./cmd/noe
./noe --version
# noe v1.14.1
```

以下の例はリポジトリのルートを作業ディレクトリとします。

## JavaScriptを実行する

```sh
./noe eval 'console.log(1 + 1)'
# 2
./noe run examples/basics/hello.js
# Hello from NOE!
```

短い確認には`eval`、スクリプトの実行には`run`を使います。ES modules・`async/await`・ファイル操作・Fetch・HTTP待受・子プロセスを利用できます。JSON加工、レポート、ダウンロードなどは[実行例](examples/README.md)を参照してください。

`./noe docs`、`./noe docs node:fs`、`./noe docs noe:sockets connect`で、実行バイナリと同じ版の英文API説明を参照できます。JSとagentからは`import { docs } from "noe:docs"`で同じ本文を取得します（[使い方](docs/reference.md)）。v1.8.0以降で利用できます。

## agentを実行する

この例ではOpenCode GoのAPIキーを`~/.config/noe/credentials/opencode-go-api-key`へ保存します。`XDG_CONFIG_HOME`を設定している場合は`$XDG_CONFIG_HOME/noe/credentials/opencode-go-api-key`です。OpenRouter・ChatGPTサインイン・OpenAI APIキーも利用できます。認証とproviderの選択は[agent起動ガイド](docs/agents.md)を参照してください。

次を`task.js`として保存します。

```js
import { run_agent } from "noe:agent";

const result = await run_agent({
  provider: "opencode-go",
  model: "deepseek-v4.1-flash",
  effort: "auto",
}, "README.mdを読んで、日本語で3点に要約してください。");

if (result.lastResult?.state !== "completed") {
  throw new Error(result.lastResult?.error || result.reason || "Agent failed");
}
console.log(result.lastResult.answer);
```

```sh
./noe run --observe task.js
```

root agentには`provider`・`model`・`effort`の明示指定が必要です。`--observe`で進捗と最終回答を表示します。ログ保存、ファイルからの指示、次の使い方は[agent起動ガイド](docs/agents.md)へ進んでください。

## 目的別ガイド

| やりたいこと | ガイド |
| --- | --- |
| 認証を登録し、agentを起動・観察する | [agent起動ガイド](docs/agents.md) |
| API引数・CLIオプション・実行制御・終了コードを調べる | [APIリファレンス](docs/api.md) |
| 自作JSや検証済みESMを再利用する | [import map](docs/import-map.md) |
| provider設定やモデルを選ぶ | [provider設定](docs/providers.md) · [モデルカタログ](docs/model-catalog.md) |
| 指示・hook・コンテキスト方針を設定する | [インストラクション](docs/instructions.md) · [hookとcontext](docs/hooks.md) |
| 実行中の様子やログを読む | [観察の入口](docs/observe.md) · [JSONLイベント](docs/events.md) |
| 画像を読取・生成する、構造化判断を依頼する | [画像](docs/agent-images.md) · [意思決定](docs/decision.md) |
| 子プロセスの逐次I/OやHTTPを使う | [プロセスとストリーム](docs/process-io.md) · [HTTPの実行例](examples/http/README.md) |
| ファイルの位置指定I/O・TCP/Unix通信・OSシグナルを使う | [ファイル・ソケット・シグナル](docs/low-level-io.md) |
| 外部Goアプリへ汎用APIを組み込む | [stdlib](docs/go-embedding.md) · [agent](docs/go-agent-embedding.md)（英文） |
| 実行例を探す、NOEを開発する | [実行例](examples/README.md) · [ドキュメント一覧](docs/README.md) · [開発ガイド](docs/development/README.md) |

旧スクリプトの移行は[API移行ガイド](docs/api.md#旧apiからの移行)を参照してください。

## 実行権限と互換性

**NOEはsandboxではなく、スクリプトとagentは起動ユーザーのOS権限で動作します。** ファイル・環境変数・コマンド・ネットワークへアクセスできます。

固定したgo-quickjsをJavaScriptエンジンに使い、go-quickjs v0.27.0を原型とするNOE管理の標準ライブラリでWeb API・Node形式APIを提供します。Node.js/Denoの完全互換、npm・パッケージ管理、HTTP import、QuickJS本来の`std`モジュールは対象外です。対応範囲は[APIリファレンス](docs/api.md#利用できる機能と権限)を参照してください。

## ライセンス

[MIT](LICENSE)。第三者のコード・データには、それぞれのライセンスが適用されます。
