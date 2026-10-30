# NOE — No Ordinary Engine

あなたとagentのためのJavaScript。

NOEは、自律型AI agentを組み込んだ純Go製のJavaScriptランタイムです。
自分でスクリプトを書くことも、agentにタスクを渡し、作業に必要な
JavaScriptの作成と実行を任せることもできます。

agentはJavaScriptを使い、ファイルの読込み・編集、HTTP APIの呼出し、
コマンドの実行、他のagentへのタスク委譲を行います。自分のスクリプトからも
同じagent APIを使い、作業の調整、結果の確認、次の処理の判断ができます。

NOEはコマンドラインから実行できるほか、ランタイムとagentをGoアプリケーションへ
直接組み込めます。go-quickjsを通じてQuickJSを利用しています。
Cのツールチェーンは不要です。

## できること

- **JavaScriptで自動化する。** ES modulesとasync/awaitを使い、ファイル、HTTP、
  ストリーム、プロセスを扱えます。
- **agentに実行可能なツールを渡す。** agentはランタイムAPI、自作モジュール、
  既存のコマンドラインツールを組み合わせたJavaScriptを書いて実行します。
- **agentのワークフローを作る。** 異なるモデルのagentを連携させ、結果を組み合わせ、
  分岐や繰返しをコードで表現できます。
- **agentの動作を制御する。** 実行中のタスクを観察し、追加指示や取消しを行えます。
  JavaScriptのhookでモデル呼出しやツール実行をカスタマイズできます。
- **Goに組み込む。** 公開Go APIを使い、スクリプト実行と管理されたagentを
  アプリケーションへ追加できます。

[はじめる](#はじめる)
· [agentガイド](docs/agents.md)
· [Goへの組み込み](docs/go-agent-embedding.md)
· [実行例](examples/README.md)
· [English](README.md)

この文書は日本語版です。正本は[英語版README](README.md)です。

[v2.10.1リリースノート](docs/releases/v2.10.1.md) · [バイナリのダウンロード](https://forge.harakara.site/littleisland/noe/releases/tag/v2.10.1)

v2.10.1 fixes `mkdtempSync` and `fs/promises.mkdtemp` so temporary
directories are created under the supplied prefix's parent directory.
Relative prefixes use the configured filesystem cwd, and root confinement
and ReadOnly permissions remain enforced. Public APIs and dependencies
are unchanged. See the [release notes](docs/releases/v2.10.1.md).

過去の変更と移行方法は、[リリース履歴](docs/releases/README.md)、
[v2移行ガイド](docs/v2-migration.md)、[実行結果ガイド](docs/execution-results.md)を参照してください。

## はじめる

上のリンクからバイナリを取得するか、Go 1.27.2以降でビルドします。Cのツールチェーンは不要です。

```sh
git clone https://forge.harakara.site/littleisland/noe
cd noe
CGO_ENABLED=0 go build -o noe ./cmd/noe
./noe --version
# noe v2.10.1
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
| 自作JSや検証済みESMを再利用する | [import map](docs/import-map.md) · [URL import](docs/url-import.md) |
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

固定したgo-quickjsをJavaScriptエンジンに使い、go-quickjs v0.27.0を原型とするNOE管理の標準ライブラリでWeb API・Node形式APIを提供します。HTTP/HTTPSのESM importをCLIで既定有効、Go組込みで明示選択として提供します（[URL import](docs/url-import.md)）。Node.js/Denoの完全互換、npm・パッケージ管理、QuickJS本来の`std`モジュールは対象外です。対応範囲は[APIリファレンス](docs/api.md#利用できる機能と権限)を参照してください。

## ライセンス

[MIT](LICENSE)。第三者のコード・データには、それぞれのライセンスが適用されます。
