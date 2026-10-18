# 基本の実行とREPL

[実行例一覧へ戻る](../README.md)

コマンドはリポジトリのルートで、ビルド済みの`./noe`を使って実行します。

```sh
./noe run examples/basics/hello.js
# Hello from NOE!
```

## 待機とREPL

```sh
./noe run examples/basics/sleep-demo.js
```

`timers/promises`の`setTimeout()`を使い、`実行 1`から`実行 3`まで1秒間隔で表示します。待機中もHTTPやファイル処理を進められます。

[repl-demo.js](repl-demo.js)を`./noe repl`へ1行ずつ貼り付けると、変数・関数の保持を試せます。↑で入力を呼び出し、割引率を編集して再実行できます。詳細は[REPLガイド](../../docs/repl.md)を参照してください。
