# 常駐プロセスと逐次I/O

[実行例一覧へ戻る](../README.md)

コマンドはリポジトリのルートで、ビルド済みの`./noe`を使って実行します。

## 常駐プロセスと逐次I/O

```sh
./noe run examples/process/process-worker.js
printf '{"id":1,"value":"hello"}\n' | ./noe run examples/process/stdio-jsonl.js
printf 'bytes through two children\n' | ./noe run examples/process/process-pipeline.js
```

`process-worker.js`は一つの子へ3件送り応答を取得します。`stdio-jsonl.js`は入力のEOFを待たずJSONLの行ごとに応答し、`process-pipeline.js`は二つの子をWeb Streamsで接続します。認証・外部provider・Nodeは不要です。[プロセス・標準I/Oガイド](../../docs/process-io.md)を参照してください。
