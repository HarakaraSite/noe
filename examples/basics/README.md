# 基本の実行

[実行例一覧へ戻る](../README.md)

コマンドはリポジトリのルートで、ビルド済みの`./noe`を使って実行します。

```sh
./noe run examples/basics/hello.js
# Hello from NOE!
```

## 待機

```sh
./noe run examples/basics/sleep-demo.js
```

`timers/promises`の`setTimeout()`を使い、`実行 1`から`実行 3`まで1秒間隔で表示します。待機中もHTTPやファイル処理を進められます。
