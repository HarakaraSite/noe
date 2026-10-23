# v2.0.0への移行

[README](../README.md) · [リリースノート](releases/v2.0.0.md) · [APIリファレンス](api.md)

v2.0.0はGo組み込みAPIとagentの実行方式に破壊的変更を含みます。NOE CLIは引き続き設定・認証・instructionの既定場所を供給します。ライブラリへ組み込む場合、その場所や内容はアプリが明示します。

## Goへの組み込み

module pathは`forge.harakara.site/littleisland/noe/v2`です。公開前のcheckout検証には[stdlib例](../examples/embedding/README.md)と[agent例](../examples/agent-embedding/README.md)のlocal replaceを使います。公開後は次のように正式タグを指定し、local replaceを外します。

```sh
go get forge.harakara.site/littleisland/noe/v2/stdlib@v2.0.0
go get forge.harakara.site/littleisland/noe/v2/agent@v2.0.0
```

この正式タグの取得は公開後の確認項目です。公開前のlocal replaceや隔離module artifactでの成功は、公開先から取得できた証拠ではありません。

| 以前の入口・前提 | v2の対応 |
| --- | --- |
| `stdlib.Install`とまとまった`Config` | `stdlib.Profile`の`Environment`・`Modules`・`NewLoader`・`Initialize` |
| 呼出し側でVM駆動と回収を組み立てる通常利用 | `stdlib.Execute`、または`NewRuntime`→`Run`→`Close`。既存VMを借用する場合は`Attach` |
| `agent.Install`、実行ファイル・内部connectionの設定 | `model.NewService`→`agent.NewHost`→`Host.NewClient` |
| JSへagentを追加する | Profileのinitializerで`agent.Attach`し、`binding.Close`をcleanupとして返す |
| model・auth・ログ処理にagent Hostを作る | 独立した`agent/model`・`agent/auth`・`agent/log`を使用 |
| libraryによるHOME/XDGやinstruction directoryの探索 | アプリがcredential path、model設定、`Instructions`関数と`Policy`を供給 |
| 暗黙のOS環境・stdio | `Environment`へ明示。nilのEnvは空、nil stdinはEOF、nil stdout/stderrは破棄 |
| 一括I/O設定やDocsフラグ | 必要なModulesを個別選択。純粋な`noe:docs`は常時利用可能 |

`HostConfig.Runtime`はtask/tool用のテンプレートで、Stdin・Stdout・Stderrはnilにします。実行ごとの空入力と有界captureはexecutorが所有します。通常のstdlib実行へ独自writerを渡す場合は、対応するメモリwriter・`*os.File`・`io.Discard`または`ContextWriter`等を使います。

runtime・loader・JS値を別の実行へ使い回しません。initializerは各実行でbindingを作り、cleanupで閉じます。`Client.Close`はそのscopeの実行をcancel/joinし、`Host.Close`は全体を回収します。借用したHTTP clientやsinkはアプリが管理します。

`Instructions`はinstanceの最初のtaskで一度評価され、成功・失敗ともそのinstance内で固定されます。queueでファイルを再読込させる前提は外し、新しい内容を必要とする場合は新instanceを起動します。

POSIXコマンドの通常起動は直接実行です。ホストSIGKILL時の子孫回収も必要なら、アプリが`DispatchProcessHelper`を呼ぶhelperを明示します。NOE CLIはこの組立を供給します。

詳細と実装例は[stdlib組み込み](go-embedding.md)・[agent組み込み](go-agent-embedding.md)を参照してください。

## JavaScriptの停止とログ観察

| 以前の操作 | v2の対応 |
| --- | --- |
| `interrupt_agent`でtaskを中断 | `cancel_task({agentId, commandId})` |
| `interrupt_agent`でinstance全体を停止 | `stop_agent({agentId})` |
| `observe_agent`に保存ログpathやwaitを渡す | 呼出し側でfileを読み、`noe:agent-log.analyze_log`でdataを解析 |
| 実行中instanceの状態を読む | `observe_agent({agentId})` |
| 受領済みの特定taskを待つ | `wait_task({agentId, commandId})` |

`commandId`は`spawn_agent`の初回受付または`queue_agent`のreceiptから取得します。`runId`はsteerなど実行中runの識別子であり、`cancel_task`のcommandIdと置き換えられません。

```js
import {readFile} from "node:fs/promises";
import {analyze_log} from "noe:agent-log";

const input = await readFile("events.jsonl", "utf8");
const status = analyze_log(input, {runId: "対象のrun ID"});
console.log(status);
```

fileの追記待ちやopenは呼出し側が担当します。CLIの`noe observe`と`noe interrupt --run|--agent`は利用できます。

## 実行方式と維持される動作

task/toolは同じOS process内の別VMで実行します。`process.pid`をtaskの別プロセスIDとして使わず、task取消し・instance停止・Client/Hostの終了で回収します。`noe:process`等で起動する本物のOS子プロセスとは区別してください。

queueでの会話継続、instanceごとのsession ID、再委譲受付、保持結果のUTF-8範囲取得、hook、画像のmetadata-only受け渡しは維持します。通常の`run`・`eval`、credential file配置、NOEのinstruction directory、ログ保存・人間向けcontrolの使い方も維持します。

`run_js`のstdout/stderrはツール終了時のcapture結果として通知されます。途中のconsole出力をtool内の到達証明にしないでください。この挙動は旧1.16.0でも同じで、v2による退行ではありません。

## v2.2.0の追加変更

`run_js`のagent helperは暗黙importされなくなりました。`noe:agent`等から明示importし、
構造化データは`export default value`または`export default await operation()`で返します。
失敗情報はstderrと混ぜず`diagnostics`から取得します。[v2.2.0リリースノート](releases/v2.2.0.md)を参照してください。

独自Go bindingでquickjsの型を使う場合、importを`github.com/go-quickjs/go-quickjs`から
`forge.harakara.site/littleisland/noe/third_party/go-quickjs`へ変更します。alias `quickjs`は維持できますが、
異なるmodule pathの型同士は互換ではありません。engineはNOEとは独立したmodule/versionで配布します。
