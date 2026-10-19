# バイナリに内蔵されたAPI説明を調べる

`noe docs`は、そのバイナリに組み込んだNOEとgo-quickjs stdlibに対応する英文API説明を返します。インターネット接続や認証、リポジトリの配置は不要です。v1.8.0以降で利用できます。

```sh
noe docs
noe docs stdlib
noe docs node:fs
noe docs node:child_process execFile
noe docs noe:sockets connect
```

引数なしで概要付きtopic一覧、topicを指定すると概要と詳細memberの一覧、memberも指定するとその説明を表示します。返却objectの操作も、表示されたIDで参照できます。

```sh
noe docs noe:fs-handle FileHandle.readAt
noe docs noe:sockets Connection.close
```

通常のJSやWorker、agentの`run_js`からも同じ本文を取得できます。

```js
import { docs } from "noe:docs";
console.log(docs());
console.log(docs("noe:fs-handle", "FileHandle.readAt"));
```

`docs()`は同期でMarkdown文字列を返します。全文を一度に読む必要はありません。module概要で用途と制約を確認し、必要なmemberを選んでください。`fs`などの対応するbare名も`node:fs`と同じ本文を返します。

stdlibは全対応moduleの概要とexport一覧を収録し、よく使うAPIから個別詳細を整備しています。個別詳細が未収録でも、そのAPIが未対応とは限りません。未登録のtopic/memberでは、CLIはstderrに説明を出して終了コード2、JSは`code: "ERR_DOC_NOT_FOUND"`のErrorをthrowします。JSで引数型が違う場合はTypeErrorです。

`noe:run_js`はagent toolを説明するtopicです。同名のmoduleをimportすることはできません。NOEの起動方法や設定は[agent起動ガイド](agents.md)・[APIとCLIのガイド](api.md)、APIの使い分けは[プロセスとストリーム](process-io.md)・[ファイル・ソケット・シグナル](low-level-io.md)を参照してください。
