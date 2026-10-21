# 依存ライブラリの互換性確認と追随 — 2026-10-17

v1.3.1を基準に候補を個別確認し、全候補の組合せも検証した。その結果を受け、go-intlを先に適用して関連テストを確認し、残りを保守更新として適用した。NOEの実装コードや公開APIの変更は不要だった。

## 更新対象

| module | 更新前 | 更新後 | 判断材料 |
| --- | --- | --- | --- |
| `github.com/go-quickjs/go-intl` | v0.6.0 | v0.6.1 | 正規化・照合の高速化、Localeの参照共有修正 |
| `github.com/OpenRouterTeam/go-sdk` | v0.9.59 | v0.9.61 | 主にBatch型と説明文の変更。NOEが使う経路の互換性を確認 |
| `github.com/tidwall/gjson` | v1.19.0 | v1.20.0 | 整数オーバーフロー、UTF-8、Pathの修正 |
| `github.com/tidwall/pretty` | v1.2.1 | v1.2.2 | エスケープを含むJSONのin-place圧縮を修正 |
| `github.com/tidwall/match` | v1.1.1 | v1.2.0 | 既存の照合は互換。新しい大文字小文字非区別APIはNOE未使用 |
| `github.com/spyzhov/ajson` | v0.8.0 | v0.9.6 | Cloneの親参照などを修正。既存のcursor/data抽出を確認 |
| `golang.org/x/sys` | v0.48.0 | v0.49.0 | OS定義・システムコール更新。NOEの配布対象でビルド成功 |
| `golang.org/x/term` | v0.40.0 | v0.47.0 | x/sys v0.49.0と組み合わせて確認。PTY・端末復元は互換 |

go-quickjs v0.27.0、OpenAI SDK v3.79.0、websocket v1.8.15、sjson v1.2.5は維持した。qjsとmdcliの取り込み済みソースも更新不要だった。Goの要求バージョンは1.27.2のまま。

上流の差分：[go-intl](https://github.com/go-quickjs/go-intl/releases/tag/v0.6.1)、[OpenRouter SDK](https://github.com/OpenRouterTeam/go-sdk/compare/v0.9.59...v0.9.61)、[gjson](https://github.com/tidwall/gjson/compare/v1.19.0...v1.20.0)、[pretty](https://github.com/tidwall/pretty/compare/v1.2.1...v1.2.2)、[match](https://github.com/tidwall/match/compare/v1.1.1...v1.2.0)、[ajson](https://github.com/spyzhov/ajson/compare/v0.8.0...v0.9.6)、[x/sys](https://github.com/golang/sys/compare/v0.48.0...v0.49.0)、[x/term](https://github.com/golang/term/compare/v0.40.0...v0.47.0)。

## 互換性と性能の確認

基準のソースはv1.3.1タグの`b94b275ed6e382d80c20ec6c6a74646ca81ea5f3`。一時コピーと候補別modfileを使い、実行依存の解決結果を記録した。各候補は単独で比較し、x/termだけは要求されるx/sysとセットにした。8候補すべての関連テストが成功した。

- providerのfixtureでChat・Responses・Decisions、reasoningの保持、認証、ヘッダ、取消しを確認した。
- SDKの通常応答では、大きい整数`9007199254740993`と未知フィールドのraw JSONを保持した。
- gjsonでは、範囲外整数の変換が折返しから飽和へ変わった。SDKの型付きデコードにも反映する。無効なUTF-8のJSON表記は変わるが、デコード後の文字列は同じだった。効果がなくなる非推奨のglobal変数は、NOEと利用SDKで参照していない。
- prettyの修正対象はJSON圧縮であり、Markdown表示用のmdcliとは別。比較したエスケープ付き3入力中、旧版の2入力で標準JSON圧縮と一致せず、新版はすべて一致した。
- ajsonはClone後の子の親参照が修正された。SDK形式の`$.cursor`・`$.data`抽出結果は維持した。
- x/termの`Terminal.ReadLine`修正は、NOEが使うqjs由来の入力処理とは別。NOEで使うraw mode・復元・サイズ取得と、PTYによる編集・中断・EOFを確認した。

go-intlの性能は、Linux amd64 / Intel Core i9-13900HX / Go 1.27.2で、同じNOEコードと用意した文字列を使い、基準版・候補版の順を入れ替えて5回測定した。初期化とwarmupは計測区間から外した。中央値による結果は次のとおり。

| 処理 | 速度比（更新後／更新前） |
| --- | --- |
| ASCIIのNFD正規化 | 約1.04倍、測定のばらつきと重なる範囲 |
| ラテン文字のNFD正規化 | 約2.34倍 |
| 日本語のNFD正規化 | 約1.29倍 |
| 英語の照合 | 約2.14倍 |
| 日本語の照合 | 約1.19倍 |

これらは指定した文字列処理の測定で、一般のagentタスク全体の速度を示すものではない。

## 組合せと本体への適用

全候補の組合せでは、unit・E2E・vet・総合race、Linux/macOS/Windows × amd64/arm64の6ビルドとLinux版の起動確認を実施した。初回の並列raceでは、300ms以内の通信開始を前提にした`TestCLIAgentTimeoutPreservesExit124`だけが失敗した。終了コード124は正しく、データ競合の報告はなかった。基準版・候補版の各5回の再確認と、他のビルドと並行させない総合raceは成功した。更新固有の再現はなく、負荷による時間切れと考えられるが、初回と同じ失敗を基準版で再現したわけではない。

本体への適用では、go-intl単独でruntimeの関連テストを再確認した。その後、残り7候補を適用して`go mod tidy`し、解決した全モジュールとバージョンが事前検証の組合せと完全一致することを確認した。

| 適用後の確認 | 結果 |
| --- | --- |
| `CGO_ENABLED=0 go test ./internal/runtime -run 'TestStdlib\|TestException\|TestSession' -count=1`（go-intl単独） | 成功 |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1` | 全体unit・E2E成功 |
| `CGO_ENABLED=0 go vet -tags=e2e ./...` | 成功 |
| `CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o noe ./cmd/noe` | 成功。手元の`./noe`を更新 |
| バイナリの依存情報 | 対象8モジュールの更新後バージョンを確認 |
| version・算術・組込みmodule・Intl・認証ヘルパーhelpの5起動確認 | exit 0、stderr空で成功 |

raceと6対象ビルドは、同じ実装ソース・同じ依存構成で行った上記の事前検証結果を利用する。ネイティブ実行はLinux amd64のみ。実プロバイダへの要求や認証ファイルの更新は行っていない。

個別確認・性能測定・組合せ検証のログと機械可読結果：

```text
/home/agent/.cache/noe-qjs-repl/noe-dependency-compat-dci6fgko/
```

本体への適用と適用後の検証記録：

```text
/home/agent/.cache/noe-qjs-repl/noe-dependency-update-cyv6hdvn/
```

## go-quickjs v0.28.0への更新 — 2026-10-21

engineをv0.27.0からv0.28.0へ更新する。Go 1.27.2、go-intl v0.6.1、provider
SDKと他の依存は維持する。JITのビルドタグとruntimeオプションは追加しない。
NOE管理stdlibはv0.27.0原型のままで、取り込み直しや原型の由来情報の変更は行わない。
公開CLI/Go/JS APIの変更とNOE実装の互換性修正は不要だった。

上流の公開production Go 14ファイル、go.mod、stdlib production Goは両版で同一。
内部のBigInt/object最適化とprimitive deleteの仕様適合修正について、既存テストと
NOE経由のsmokeで確認する。NOE用途の性能改善は測定・主張していない。
consumerのv0.27.0要求は維持し、MVSがv0.28.0を選ぶことを確認する。

計画の通常・批判的レビュー、コードレビューと検証結果は
[取り込み計画・実施記録](go-quickjs-v0.28.0-plan.md)に記録する。
関連テスト、BigInt/object/Worker smoke、stdlib/agent両方の外部consumerがPASS。
通常・批判的コードレビュー後の全体unit/E2E・race・長時間Go Client・vet・6対象compile・
最終CLI smokeもPASS。native実行確認はLinux amd64のみで、公開・配置は行っていない。

初回の総合raceで、既存の外部コマンドowner-deathテストがプロセス消失時のESRCHを
失敗扱いにしていた。旧engineでも自然再現することを確認し、停止後pollだけを
同repositoryの既存判定に揃えた。修正後の反復・制御試験と通常/批判的再レビューで、
生存・権限・停止前の異常を引き続き検出することを確認してから総合テストを再実行した。
最終変更は依存・説明/記録と、この既存テストの最小修正のみ。
初回の失敗も含む作業ログ・結果は`/var/tmp/noe-q280-XrXP1S`に保存している。
