# ローカルESMとimport map

[実行例一覧へ戻る](../README.md)

リポジトリのルートで実行します。認証やnpmは不要です。

```sh
./noe run --import-map examples/import-map/import-map.json examples/import-map/main.js
# 6
./noe eval --import-map examples/import-map/import-map.json 'import {sum} from "app/math"; console.log(sum([1,2,3]))'
```

`import-map.json`の`app/math`が`lib/math.mjs`を参照します。targetの相対パスはmapファイルの位置が基準です。詳細は[import mapガイド](../../docs/import-map.md)を参照してください。
