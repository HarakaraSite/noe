# NOE版stdlib：ステップ1・固定版の取り込み計画

2026-10-19。[段階案](../future-candidates.md)のステップ1だけを具体化する計画です。計画レビュー後に実装・テスト・コードレビュー・総合テストを完了しました。[実装・検証記録](stdlib-step1-import-implementation.md)を参照してください。ステップ2のNOE本体の切替、ステップ3以降の`noe:http`追加と機能拡張は含めません。

## 目的と到達点

go-quickjs v0.27.0のstdlibを、NOEが変更・保守できる内部packageとして取り込みます。既存の実装・テスト・fixtureを利用し、エンジンは現在のGo moduleを引き続き使います。

このステップの到達点は「取り込んだstdlibを単独でビルド・テストでき、NOE実行コマンドは従来のstdlibで動いている」です。通常のCLI・Worker・agent・`run_js`へ新しいstdlibを登録するのはステップ2です。

## 正本と確認済みの事実

| 項目 | 固定する内容 |
| --- | --- |
| 上流module | `github.com/go-quickjs/go-quickjs` |
| 採用版 | `v0.27.0`。依存versionの更新は行わない |
| upstream commit | `bef3107cc7eeb5e392cdd9a3768f64fcbf0e609c`（module cacheのorigin情報で確認） |
| module checksum | `h1:TVemuGmYmaR+4CCNIVIcHk3dhrqYRq+PGljCkT1EqAE=`（`go.sum`とmodule cacheで一致） |
| 取り込み元 | 同版の`stdlib/`、`internal/wsproto/`、rootの`LICENSE` |
| ライセンス | upstreamのMIT本文と著作権表示を保持する |
| 検証環境 | Go 1.27.2、Linux amd64。配布対象はLinux/Darwin/Windows × amd64/arm64 |

`stdlib/`はproduction Go source 26件、Go test 9件、`testdata/` 3件です。Go sourceにはJS文字列も含まれます。`internal/wsproto/`はproduction 1件とtest 1件です。実装開始時に同じ固定版から一覧を再確認し、漏れや別versionの混在を防ぎます。

production sourceの上流非公開packageへの直接依存は、次の2種類でした。

- `serve.go`・`websocket.go` → `internal/wsproto`：WebSocket handshakeとframe/message処理。
- `console.go` → `internal/hostaccess`：stdlib自身のJSを、debugger用に計装せず評価する経路。`evalInternal` helperを通じて他のstdlib sourceからも利用される。

計画時に、固定版そのものについて次の基準テストを実行しました。両packageとも成功しました（stdlib 8.846秒、wsproto 0.003秒）。これは移植後の成功を示すものではありません。

```sh
CGO_ENABLED=0 go test github.com/go-quickjs/go-quickjs/stdlib github.com/go-quickjs/go-quickjs/internal/wsproto -count=1 -timeout=120s
```

## 配置と取り込み方針

```text
internal/stdlib/
  UPSTREAM.md                 # 英文の由来・固定版・範囲・NOE差分
  LICENSE                     # upstreamのMIT本文をそのまま保持
  *.go                        # upstreamの26 sourceと9 test
  testdata/                   # upstreamの3 fixture・生成元JS
  internal/wsproto/
    wsproto.go
    wsproto_test.go
```

Go import pathは`noe/internal/stdlib`、Go package名は上流どおり`stdlib`とします。将来の公開Go import path、別module・別repositoryの作成は今回決めません。既存の`internal/nativeio`や`internal/agent`は移動しません。

取り込むsourceとfixtureは、原則として上流のファイル構成・内容を保持します。許容する変更は次の範囲です。

1. 上流stdlib自身をimportする8件の外部packageテストの参照先を、`noe/internal/stdlib`へ変更する。`package stdlib_test`と内部テストの`package stdlib`は維持し、上流stdlibを誤って試験しないようにする。
2. `serve.go`・`websocket.go`の`wsproto` importを、取り込んだ`noe/internal/stdlib/internal/wsproto`へ変更する。protocol実装とテストの挙動は変えない。
3. 次節の評価helperを公開エンジンAPIへ適合させ、関連コメントを実際の挙動に合わせる。
4. ビルドに必要な最小の調整と、その理由・確認結果を記録する。新機能、既知不具合の一般的な修正、整形だけの大きな差分は混ぜない。

`UPSTREAM.md`にはmodule/version/commit/checksum、取り込み元と配置先の対応、ライセンス、NOE側の変更一覧と理由を記載します。未変更ファイルは同版の原本と照合し、変更ファイルは上記の適合作業として説明できる差分に限定します。生成済みのpath corpusを利用し、Node.jsを新しいテスト依存にせず、fixtureの再生成も行いません。

## 非公開の評価経路の扱い

`internal/hostaccess`はエンジン内部のVMや初期化済みcallbackを参照するため、そのまま複製しません。エンジンのfork、非公開APIの公開、`unsafe`・`go:linkname`・reflectionによる迂回も行いません。

取り込み先の`evalInternal(rt, name, src)`を、公開APIの`rt.EvalFile(name, src)`へ置き換える方針です。helperの引数・戻り値とsource名を維持し、`evalWithHost`のclosureや`cloneAs`によるstructured cloneのbrand登録は維持します。

固定版の実装では、両経路とも`runIn`を使い、実行後のPromise job処理を行います。違いは、上流の非公開経路がdebug用のcompileを常に無効にする一方、公開経路はruntimeのdebug設定に従う点です。通常のNOEとWorkerは`quickjs.New()`で作られ、debuggerを有効にしていません。

したがって、今回確認する互換範囲は現在のNOEと同じdebugger無効のruntimeです。`WithDebugger`を使う外部hostでstdlib内部のscriptを隠す挙動は維持できないため、NOE差分として英文で記録し、将来の公開ライブラリ設計へ渡します。debugger有効runtimeを新たに拒否する処理は追加しません。

公開経路への置換は実装時にテストで確認します。初期化、console、エラーの伝播とsource名、structured clone、Worker、loopと終了処理を重点とし、既存テストに不足がある場合だけ目的に対応するテストを追加します。置換に起因する不一致が出た場合は、期待値を変えて通すのではなく原因を確認し、この適合方針を見直してから先へ進みます。

## スライス

### S1-A：取り込み範囲と由来の固定

- 固定版・commit・checksum、26 source/9 test/3 fixtureとwsproto 2件、ライセンスの取り込み一覧を確定する。
- `internal/stdlib/UPSTREAM.md`と`LICENSE`を用意する。説明は英文とする。
- 上流の基準テスト、実装環境、許容するローカル変更を記録する。

完了条件：別versionやエンジン内部のVMを持ち込まず、取り込むファイルと調整箇所を追跡できる。productionのGo importやJS登録は変更されていない。このスライスはmetadataだけで成立し、非公開packageをimportする未調整のGo sourceを中途半端にcommitしない。

### S1-B：source・test・fixtureの移植と最小適合

- source、内部/外部テスト、fixture、wsprotoとそのテストを、一覧どおり取り込む。
- 自己import・wsproto importと評価helperを、上記の方針で適合させる。
- `UPSTREAM.md`の変更一覧を実際の差分へ合わせる。
- 全取り込みテストがローカルpackageを使うことを確認し、単独テストを実行する。

完了条件：新packageとwsprotoがビルドでき、取り込んだテストが成功する。エンジンの非公開packageや上流stdlibへの依存は取り込み先に残らない。HTTPの要求buffering、timeout/本文上限、module名、公開exportなどの既存仕様は変えない。

### S1-C：独立性・対応target・差分の検証と記録

- 次節の検証を実施し、source対応表とローカル変更をレビューする。
- 実装・検証記録を`docs/development/stdlib-step1-import-implementation.md`へ記載する。コマンド、結果、環境、skip、未実施事項を区別する。
- 候補メモではステップ1だけを完了へ更新する。ステップ2以降は未着手のままとし、productionの利用先と内蔵docsの版説明は従来のまま維持する。

完了条件：以下の受入条件を満たし、通常のNOEが依然として上流stdlibを利用することを確認できる。移植後の差分とdebuggerに関する制約を、次のステップの入力として渡せる。

各スライスは順に実施します。ここでは実装の分割単位を定め、commit・releaseの実行は含めません。

## 検証と受入条件

実装時には、まず取り込み先で次を実行します。

```sh
CGO_ENABLED=0 go test ./internal/stdlib/... -count=1 -timeout=180s
go vet ./internal/stdlib/...
CGO_ENABLED=1 go test -race ./internal/stdlib/... -count=1 -timeout=300s
```

loop・Worker・messaging・HTTP/WebSocket・runtime Closeを含む非同期資源管理も取り込むため、対象packageのrace検査を行います。失敗は上流の同じ固定版・同じ条件でも再現するかを比較し、取り込み差分と既存問題を分けます。取り込みに起因する失敗を残したまま完了にしません。上流でも失敗する場合は理由と影響をdefaultが評価し、今回必要な適合か別の問題かを決め、完了条件への影響を記録します。期待値変更やskipの追加だけで成功扱いにしません。

あわせて、次を確認します。

| 観点 | 確認方法・受入条件 |
| --- | --- |
| 原本と差分 | 取り込み一覧と固定版を照合し、未変更ファイルが一致する。変更は自己import・wsproto参照・評価helper等の説明済み範囲に限る |
| テストの参照先 | 外部テストはローカルstdlibをimportする。内部テストとfixtureも保持し、上流packageだけが成功する取り違えを避ける |
| 既存機能 | 上流のconsole、fs、fetch/serve、WebSocket、Streams、Worker、messaging、async hooks、vm、path、DNS、終了処理等のテストを維持する |
| テストの実行環境 | ローカルserver・fake resolver・一時ファイル・テスト用子processを利用する。helper単独起動用や既存OS条件付きskipは理由を記録し、外部provider・Node.js・外部サービスは必須にしない |
| 対応target | Linux/Darwin/Windows × amd64/arm64の6組で、CGO無効の`go test -c`によりstdlibとwsprotoのテストbinaryをそれぞれ一時出力先へcross-compileする。非Linux環境での実行成功とは記録しない |
| 通常のNOEへの影響 | `CGO_ENABLED=0 go test ./... -count=1 -timeout=180s`を一度実行する。`go list -deps ./cmd/noe`で上流stdlibが残り、ローカルstdlibがCLIのproduction依存に入らないことを確認する |
| ビルドと成果物 | 一時出力先へ`CGO_ENABLED=0 go build -o <temporary-output> ./cmd/noe`を行う。rootの既存`noe`バイナリを上書きしない |
| 説明 | 計画・実装記録は日本語、取り込みpackageの由来とローカル差分の説明は英文。現在の内蔵API説明は、本体の切替前に書き換えない |

rootの`go.mod`・`go.sum`、`internal/runtime`、`internal/nativeio`、agent、CLI、module予約一覧、実行時の内蔵docsは変更対象外です。新規依存は追加しません。`_refs/`と利用者の`agent-server-build.jsonl`は参照・変更しません。

## 今回含めないものと次の入力

次は後続のステップで扱います。

- runtimeのimport・Config・loop・WorkerをNOE版へ切り替えること、切替後のCLI/Worker/agent/`run_js`のE2E。
- `noe:http`、要求streaming、全二重、WebSocket拡張、HTTP設定の変更や既知不具合の修正。
- `noe:fs`への改名、`noe:fs-handle`の統合、`nativeio`やagentの移動。
- 外部向けGoライブラリ化、公開module pathの決定、依存moduleやJSエンジンのfork。
- 実provider検証、commit、push、release、上流への変更提出。

ステップ2へは、取り込み元の識別情報、ローカル変更、全テストとcross-compileの結果、評価経路の差分を渡します。productionの利用先が変わるまでは上流版と取り込み版がソースとして共存しますが、NOEの同じruntimeへ両方を導入しません。

## 計画レビュー

利用者の依頼により、計画完成後に独立レビュアーが通常レビューを行います。評価対象は、範囲の遵守、取り込み元・ライセンス・依存境界、移植の実現性、テストの参照先と受入条件、後続ステップとの分離です。

P0～P2は実装に進む前に解消し、P3は任意改善として区別します。初回は30分以内、新しい根拠や中間結論が10分ない場合は中断して確認済み範囲を返します。再レビューは変更箇所と指摘の解消を15分以内で確認します。今回の利用者指示に従い、P2以上の未解消指摘がなくなるまで修正と再レビューを繰り返します。結果は[レビュー記録](stdlib-step1-import-review.md)へ記載します。
