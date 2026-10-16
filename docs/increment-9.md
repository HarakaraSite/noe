# NOE インクリメント9 バイナリ入出力

> この文書は実装当時の計画・検証記録です。現在の仕様は[APIリファレンス](api.md)と[stdlib移行記録](increment-12.md)を参照してください。旧`noe.*` API、独自I/O実装、当時の対象外・中断動作は現行仕様ではありません。

画像・PDFなどをHTTPで取得してそのままファイルへ保存できるよう、Uint8Arrayのファイル入出力とHTTPレスポンス取得を追加する。Go 1.27.2、go-quickjs v0.25.3と既存AsyncWorkを使い、依存は追加しない。状態はスパイク・実装・テスト・レビュー・総合E2E完了。

## スパイクの結果と設計

使用中のgo-quickjsではNewBytes([]byte)がコピーしたUint8Arrayを作る。Value.Bytes()はビューの範囲を返すが、JSと同じストレージなのでworkerへ渡す前にコピーが必要。普通の[]byteをEncodeすると数値配列になるため、JS値への変換はAsyncWorkの生存確認後に所有goroutineで行う。空のUint8Array、部分配列、厳密な型判定を実行確認した。コードは作業用一時ファイルに置き、repositoryへ追加しない。

## APIの契約

| API | 動作 |
| --- | --- |
| `noe.readFile(path)` | ファイル全体を読み、Promise<Uint8Array>を返す。空ファイルは長さ0。NUL・非UTF-8もそのまま保持。呼出しごとに独立したストレージ |
| `noe.writeFile(path, bytes)` | Uint8Array（派生クラス含む）のビューの範囲だけを上書き保存し、Promise<void>を返す。呼出し時に内容をコピーするので、その後のJS側の変更・detachに影響されない。親は事前に作る。権限・相対/絶対/日本語パス・OSエラー・終了時待機は既存テキスト書込みと同じ |
| `response.bytes()` | fetchのレスポンス全体をPromise<Uint8Array>として返す。空bodyは長さ0。繰り返し呼べて毎回独立したコピー。取得した配列の変更は後続のbytes/text/jsonへ影響しない |

writeFileは文字列・普通の配列・ArrayBuffer・DataView・他のTypedArray・Proxy・detach済みのバッファをrejectし、型違いでファイルを開かない。Uint8Arrayという型の契約を確認するために組込みのTypedArray型タグgetterを捕捉し、Objectのタグ文字列やprototype変更で別の型を受け付けない。

バイナリ読取り・HTTPのJSバイト列生成はランタイム所有goroutine。workerはGoのバイト列だけを扱う。HTTPは原本を保持し、bytesのコピーと既存のtext/jsonを同じbodyから提供する。text/jsonの既存動作、fetchの文字列リクエストbody、4xx・5xxのレスポンス、--timeout、REPL await/中断、通常終了の完了待ちを維持する。新APIもPromiseを返し、OS・通信失敗はreject。

## スライスと受入条件

| スライス | 範囲 | 完了条件 |
| --- | --- | --- |
| 1 | AsyncWork所有goroutineでの結果変換・共通fileio・readFile/writeFile | 全バイト値、空ファイル、部分ビュー、日本語/相対/絶対パス、snapshot、独立コピー、成功undefined、型違いで未書込み、OSエラー・REPL回復・未await完了待ちを確認。既存テキスト/FIFOキャンセル経路も維持 |
| 2 | HTTPのバイト原本とbytes() | 非UTF-8/NULのレスポンスを保存・照合。空body、反復コピー、コピーへの変更後のtext/json、既存HTTPメソッド/文字列body、HTTPエラー・timeoutを確認 |
| 3 | ダウンロード例・README・実バイナリE2E | HTTP取得→バイナリ保存→再読込み・照合、部分コピー、REPL、各失敗、通常終了とtimeout。全レビュー後の総合E2Eを成功させる |

各スライス・計画を通常／批判的レビューする。コピーと共有メモリ、JS値の所有、ビュー範囲・型判定、元のtext/jsonとの整合、キャンセル時の遅延completion、テストが実データの保持を確認することを独立確認。初回15分、再レビューは変更/findingに限って一回15分以内。親agentが採否と最終検証を行う。

## 実用例と検証

`examples/download-file.js URL output-file`を追加する。成功レスポンスのbytesをwriteFileで保存し、件数ではなくバイト数・保存先を表示する。HTTP失敗は診断・コード1、引数不足は2。親ディレクトリは利用者が用意する。検証はローカルHTTPと一時ディレクトリだけを使い、外部サービスへ接続しない。

対象Goテスト、CGO無効ビルド、CLI/runtimeと実CLIのrace、go vet、全スライスレビュー後の`go test -tags=e2e ./... -count=1 -v`を行い、直下のnoeを更新する。

## 対象外

バイナリのHTTPリクエストbody、arrayBuffer()/Blob、バイナリ標準入出力、ストリーム/部分I/O、画像/PDF解析、エンコード指定、サイズ上限、Web全面互換、npm・Node/Deno互換、TypeScript、名称変更は含めない。

## 実装・レビュー記録

| 段階 | 結果 |
| --- | --- |
| スパイク・計画 | 使用中の依存の実装と実行で、NewBytesのコピー、Bytesのビュー範囲、空バッファ、組込み型タグを確認。計画の通常／批判的レビューで必須指摘なし |
| スライス1 | 共通fileio・バイナリAPI・所有goroutineでの結果変換を実装。0〜255、空データ、日本語/相対/絶対、部分ビュー、独立コピー、変更とdetach後のsnapshot、派生クラス、型偽装・不正型での未書込み、REPL回復、未await完了を確認。テキスト/FIFOキャンセルテストも成功。通常／批判的レビューで必須指摘なし |
| スライス2 | HTTPworkerのGo原本から、所有goroutineで既存textと私有Uint8Arrayを生成。毎回のbytesコピー、全256値、返却配列の変更・detach、後続text/json保持、空204・503bodyを確認。既存メソッド・文字列body・キャンセル経路も成功。通常／批判的レビューで必須指摘なし |
| スライス3 | download-file.js・README・実バイナリE2Eを追加。保存ファイルの全バイト照合、部分ビューsnapshot、HTTPコピー変更後の再保存、空データ、REPL回復、未await完了、404/保存先エラー/usageを確認。レスポンス途中のtimeoutでserver側キャンセル・コード124・未保存も確認。通常／批判的レビューで必須指摘なし |
| 総合E2E | 全レビュー後、既存CLI/API/REPL/端末操作を含む全E2Eが成功 |

検証環境はGo 1.27.2、Linux amd64。以下の確認が成功した。

```sh
go test ./internal/runtime -run 'TestBinaryFileAPIs|TestUnawaitedBinaryWriteCompletesBeforeExecuteReturns' -count=1 -v
go test ./internal/runtime -run '^TestFetchBytes' -count=1 -v
go test ./internal/runtime -count=1
CGO_ENABLED=0 go build ./...
go test -tags=e2e ./tests -run '^TestBinary' -count=1 -v
CGO_ENABLED=1 go test -race ./internal/... -count=1
go test -tags=e2e ./... -count=1 -v
go vet -tags=e2e ./...
CGO_ENABLED=0 go build -o noe ./cmd/noe
git diff --check
```

一時Go overlayでTestMainのCLIビルドを実際のraceビルドへ切り替えて追加バイナリE2Eも成功し、race検出なし。通常の総合E2Eと直下のnoeはCGO無効でビルドする。スパイク・overlay・ログは作業用一時ファイルに置き、repositoryへ追加しない。実装上の未解消指摘はない。

## 参照

- [製品方針・MVP](mvp.md)
- [インクリメント8](increment-8.md)
- [現在のAPI](../README.md#api)
