# JSONの加工・集計・ファイル操作

[実行例一覧へ戻る](../README.md)

コマンドはリポジトリのルートで、ビルド済みの`./noe`を使って実行します。

## JSONの加工と保存

```sh
DEMO_DIR=$(mktemp -d)
printf '{"value":21}\n' > "$DEMO_DIR/input.json"
./noe run examples/json/transform-json.js "$DEMO_DIR/input.json" "$DEMO_DIR/output.json"
cat "$DEMO_DIR/output.json"
```

入力のプロパティを保持し、`processed: true`を追加します。保存先の親ディレクトリは事前に用意します。

## 標準入力とディレクトリの集計

```sh
printf '[100,200,300]\n' | ./noe run examples/json/summarize-stdin.js
# {"count":3,"total":600,"average":200}

./noe run examples/json/collect-json.js examples/json/data/numbers
# ファイルごとの結果と、count: 5、total: 800、average: 160を表示
```

入力は有限の数値だけを含むJSON配列です。空配列のaverageはnullです。ディレクトリ集計は直下のファイル名が`.json`で終わるものを名前順に調べます。`readdir()`は名前だけを返し、`lstat()`で通常ファイルを確認します。シンボリックリンクは対象にしません。再帰しません。

出力を保存し、次のスクリプトの標準入力として渡すこともできます。

```sh
DEMO_DIR=$(mktemp -d)
./noe run examples/json/collect-json.js examples/json/data/numbers > "$DEMO_DIR/summary.json"
./noe eval 'const report = JSON.parse(process.stdin.read()); console.log(report.total)' < "$DEMO_DIR/summary.json"
# 800
```

## 処理済みファイルの移動

この例は入力ファイルを移動します。サンプルを作業ディレクトリへコピーして試します。

```sh
DEMO_DIR=$(mktemp -d)
mkdir "$DEMO_DIR/input"
cp examples/json/data/numbers/*.json "$DEMO_DIR/input/"
./noe run examples/json/process-json.js "$DEMO_DIR/input" "$DEMO_DIR/output"
cat "$DEMO_DIR/output/report.json"
ls "$DEMO_DIR/output/processed"
```

入力と出力は別のディレクトリを指定します。全入力の解析後、出力ディレクトリを作り、一時レポートを`report.json`へ名前変更し、入力を`processed/`へ移動します。成功すると作業マーカーを削除します。

`mkdir(path, {recursive: true})`で出力先を作ります。既存の`report.json.tmp`・`.processing`は上書きします。名前変更の置換可否はOSに従い、Linuxでは同名の通常ファイルを置換します。ファイルシステムをまたぐ移動はコピーで補いません。処理はトランザクションではなく、失敗・中断までの変更や作業ファイルは残ります。
