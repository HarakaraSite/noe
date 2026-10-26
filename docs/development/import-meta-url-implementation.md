# import.meta.url対応

[利用ガイド](../import-meta.md) · [開発ガイド](README.md)

## 目的と契約

各moduleが自身のURLを知り、`new URL("./asset.txt", import.meta.url)`で
module-relativeなasset URLを計算できるようにする。TypeScript/esbuildとは独立したJS互換性改善。
loaderの解決規則・module identity・filesystem/HTTP権限は変更しない。

- ローカルmoduleは解決済みパスをfile URL化。URLエンコードし、Windowsのdrive/UNCも表現する。
- HTTP/HTTPS moduleはloaderが返す最終canonical IDを保持する。
- rooted FSは実行環境内の仮想パスを使い、ホストの物理rootをURLに含めない。
- `Source.Virtual` entryはcwd配下の合成`<virtual-entry>.mjs` file URL。
  現在の相対importが使うbaseと共通化し、診断用のSource.Nameは維持する。
- CLI evalは既存の合成source名`<eval>.js`のfile URLを使う。
- host定義/独自loaderの絶対URI IDは保持し、bare IDにはopaqueな`noe:module/<escaped-id>`を与える。
- 標準で追加するmetadataは`url`のみ。resolve/main/dirname/filenameは対象外。

## 実装

- `internal/jsexec/import_meta.go`: module IDからURLへの変換とOnImportMeta hook。
- `internal/jsexec/profile.go`: 共通Attach経路でInitializeより前に登録。Workerにも同じ初期化が適用される。
- `internal/jsexec/execution_loader.go`: virtual entry IDの記録と、相対import/metadataの合成baseの共通化。
- public APIやrootのGo依存は追加・変更しない。ホストはInitialize内またはAttach後にhookを上書きでき、Prepareで上書きし返さない。

## 製品動作に対応する検証

- public stdlib: local entry・nested imports・static/dynamic/import-map identity、module-relative assetの読込み。
- local pathの空白/日本語/#/%、POSIXでの?と、pure unit testでのWindows drive/UNC URL表現。
- HTTP/HTTPS: redirect後URL、query/fragment、alias identityとasset取得。
- embedding: Attach/Prepare、通常/virtual entry、rooted FSの仮想path、診断ラベル保持。
- Worker: file/eval entryと依存moduleのURL。
- host定義module/custom loaderのIDと、Initialize/Attach後のhost hook上書き。
- CLI run/eval、agent executorのvirtual run_js entry。

対象テストは `internal/jsexec/import_meta_test.go`、`stdlib/import_meta_test.go`、
`tests/import_meta_e2e_test.go`、`internal/agentexec/executor_test.go`の追加case。
public APIの統合テストはcore実装担当と独立して作成した。
対象検査はすべて成功。独立reviewでも修正を必要とする不具合・退行の指摘はなかった。
review記録はGit追跡外の `_refs/import-meta-review.md`。

最終候補に対し、親が次をまとめて実行して成功した。

```sh
go test ./...
go test -tags=e2e ./tests -run 'TestImportMetaURLThroughCLI|TestURLImportAgentDelegationAndFreshRunJSSources' -count=1
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o noe ./cmd/noe
```

最終検査ログはGit追跡外の `_refs/import-meta-validation.json`。
ローカルの`./noe`も再ビルドし、CLI evalで合成entryのfile URLと、
それを基準にした`asset.txt` URLが得られることを確認した。
ドキュメントのリンクと`git diff --check`も確認した。
起動済みのNOE/agentホストはバイナリを置き換えても実行中のコードが変わらないため、反映には再起動が必要。

## 今回と区別した既存の別件

旧binaryでも、`new URL("./data 日本語.txt", "file:///tmp/library/main.mjs").href`は
相対参照の生の空白/非ASCIIを残して返す。metadata未設定とは別の既存URL serializerの挙動。
本修正の`import.meta.url`自体はnet/urlで正しくエンコードしている。
テストではエンコード済みasset参照やASCII basenameを使い、
特殊文字を含むmoduleのbase URLからの解決を確認した。

また、既存`node:url.fileURLToPath`はWindowsでslash付きdrive pathを返すため、
public APIテストのasset読込みfixtureではそのprefixを調整している。
URL全体・file URL helperの全面的なNode/Web互換化は本変更の範囲に含めない。
対照実験ログはGit追跡外の`_refs/import-meta-url-control.json`に保存した。

Windows実機での実行は行っていない。URI生成のunit testでpath形状を確認したことと、
OS上でファイルを読み込むことは区別する。
