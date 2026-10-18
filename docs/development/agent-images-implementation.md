# 画像入力・生成の実装と検証

2026-10-18。正本は[計画](agent-images-plan.md)、利用方法は[画像ガイド](../agent-images.md)、実接続の挙動は[実接続記録](agent-images-integration.md)。利用者の許可に従い、本体実装・sliceごとの関連検証・コードレビュー・総合テストを実施した。本体確認後、利用者がcommit・push・v1.7.0 releaseを許可した。リリース確認は[release checkpoints](../releases/release-checkpoints.md)へ記録する。

## Sliceと実装

| Slice | 実装と確認 |
| --- | --- |
| S1 | `noe:image`、PNG/JPEG/WebPの実体検証、固定bytes、run/model step/tool所有のpending、呼出し順ordinal、run間分離。子Operation closeでpendingを失わず、abort後の遅延登録を拒否 |
| S2 | Chatのimage_urlとResponsesのinput_imageへの変換、完全blockの画像参照、hookのmetadata/部分選択、checkpointでの移管、queue継続、受理済trim後の解放、画像token概算と明示的refusal情報 |
| S3 | OpenRouter/OpenAIの固定SDKで独立した画像生成adapter、専用endpoint設定と外部merge、image model catalog、選択的なproviderエラー保持。既存agent/decision interfaceを維持 |
| S4 | 通常JS/REPL/run_jsから生成、atomic保存、取消し、providerCalls/imageProviderCalls、JSONLと人向け表示・親向け観察 |
| S5 | README、API/provider/catalog/hooks/events/observe各ガイド、共通instructions、examples/image。モデルへ画像の到着タイミングも説明 |
| S6 | 全体E2E・race・vet・CGOなし6target build、公開APIの実接続、全体コードレビュー。実モデルの認識精度を通信成功と区別 |

画像bytesはGoに置き、JS・hook・history・JSONLには参照ID/metadataのみを返す。会話内の参照から実際の送信直前にdata URLを構成する。provider要求のbufferが画像を保持するため、取消しやtrimで別のin-flight要求のbytesを失わない。

## 主な受入証拠

| 挙動 | 継続実行できる試験 |
| --- | --- |
| ファイル変更後も固定bytes、子close後のpending、foreign run拒否、受理済trim解放 | `internal/agent/images_test.go` / `TestImageAttachmentsSurviveChildCloseAndCommit` |
| root read拒否、abort後の登録拒否 | `TestImageAdmissionRejectsRootAndAbortedLateReads` |
| 同一toolの並行読込み順・物理失敗での破棄 | `TestImageConcurrentRegistrationUsesInvocationOrder` |
| HTTP ACK欠落後もホストcheckpointが確定情報を保持 | `TestImageCheckpointRetainsHostCommitWhenAcknowledgementIsLost` |
| Base64サイズを使わない画像token概算 | `TestImageContextEstimateIgnoresEncodedSize` |
| console.logなしで画像添付、元ファイル変更後のsnapshot、queue継続、JSONLにpayloadなし | `internal/runtime/images_test.go` / `TestImageRunJSAutoAttachmentAndQueueContinuation` |
| hookの結果null置換・画像保持/除外・不正ID・例外・JS物理失敗・並行tool片側失敗 | `TestImageHookSelectionAndPhysicalFailure` |
| 画像を含む完全blockのtrim、最新block保持 | `TestImageHistoryTrimmingKeepsLatestCompleteExchange` |
| 通常JSの生成、agentから別providerで生成→readback、計数 | `TestImageGenerationPublicModuleSavesWithoutAutomaticAttachment` / `TestImageAgentGeneratesWithIndependentProviderAndReadsBack` |
| REPLモジュール再利用と非対応生成、入力ImageError、Vision拒否詳細 | `TestImageModuleLazyRootReadErrorAndREPLGeneration` / `TestImagePublicValidationUsesStructuredInputError` / `TestImageVisionRefusalPreservesExistingErrorAndDetails` |
| 専用SDK要求、設定上書き、拒否/入力/認証/利用枠/通信/不正応答、payload除外、取消し | `internal/agent/provider/image_test.go` と `internal/agent/images_generate_test.go` |
| Responses形式の正確なkeysと履歴非破壊 | `internal/agent/provider/responses_test.go` / `TestResponsesImagePartsContainOnlyNativeFields` |
| 人向け失敗種別・path表示、親のphase/model維持 | `internal/cli/image_observe_test.go` / `internal/observation/image_test.go` |
| FIFO入力で待機しない | `internal/image/codec_linux_test.go` / `TestReadImageRejectsFIFOWithoutOpeningIt` |

## レビューと採否

独立reviewerでS1 codec、S1/S2所有・履歴・hook、S3/S4 providerを確認した。次の指摘をすべて採用・修正し、関連する回帰試験を追加した。

- 通常ファイル以外の入力で読込みが止まる経路：事前にregular fileを確認。
- Go mapの再利用によりprovider画像partへ内部imageId等が残る：新しいmapへ置換し、native keysを試験。
- 並行read_imageが完了順に並ぶ：JS呼出し時ordinalを保持。
- 短い裸画像Base64がprovider messageに残る：decodeとMIME判定で除外。
- 明示的な機能非対応の誤分類：一般的input分類より優先。429のinsufficient_quotaもquotaへ分類。
- 公開JSの事前入力検証がTypeErrorを返す：ImageError/code:inputへ統一。

providerの限定再レビューと所有・履歴の修正確認では未解消P1/P2なし。最終全体レビューも合格し、未解消の必須指摘はない。任意P3のSol指定例・モデル比較へのリンクもexamples/imageへ採用した。

## 総合テスト

Linux amd64、Go 1.27.2で実行。通常試験はfixtureを使い、実providerを呼ばない。

| コマンド/確認 | 結果 |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS |
| `go test -tags=e2e ./... -count=1` | PASS。実CLI・Linux PTYを含む |
| `go test -race ./internal/... -count=1` | PASS |
| 画像関連6packageのfocused race | PASS。後続修正・追加試験も対象 |
| `go vet -tags=e2e ./...` | PASS |
| `go mod tidy -diff` / `git diff --check` | PASS |
| CGOなし `-trimpath -ldflags='-s -w'` build | linux/darwin/windows × amd64/arm64 の6targetすべてPASS |

配布buildの確認物は `/tmp/noe-image-build-check/`。Darwin/Windows/arm64のnative動作は未確認。追加dependencyはWebP decodeの `golang.org/x/image v0.47.0`。生成はOpenRouterとOpenAI APIキーに対応し、ChatGPTプラン/OpenCode Goによる画像生成は非対応。画像編集・mask・複数枚生成・入力URLは今回対象外。

## 実プロバイダでの挙動

公開APIの実CLI確認、生成PNGの直接目視、画像wireのSHA256照合とモデル別の認識結果を[実接続記録](agent-images-integration.md)へ記録する。最初の色テストは指示例に期待値が入っていたため、独立した認識精度の成功証拠には使わない。期待値を伏せた追加fixtureと、tool履歴を挟まない要求との比較を実施した。実送信SHA256は元画像と完全一致し、Lunaの直接要求も誤認した一方、Solは直接・agent経由とも4色すべて正答した。親OpenCode GoによるOpenRouter生成→同runのreadbackも成功した。通信成功を認識正答とはみなさない。
