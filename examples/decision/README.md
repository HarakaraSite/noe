# 意思決定モデル

[実行例一覧へ戻る](../README.md)

コマンドはリポジトリのルートで、ビルド済みの`./noe`を使って実行します。

## 意思決定モデル

`decision-classify.js`は一つの文章をチーム別に分類し、providerの応答JSON全体を表示します。第一引数が判断対象、第二引数がモデルで、モデル省略時は`typesafe/jev-1.14`を使います。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/decision/decision-classify.js 'Checkout failed.' typesafe/jev-1.14
```

モデルへ渡すtoolを増やさず、agentの`run_js`からも`await import("noe:decision")`で利用できます。並列化、画像part、REPL、失敗・取消し、観察は[意思決定APIガイド](../../docs/decision.md)を参照してください。
