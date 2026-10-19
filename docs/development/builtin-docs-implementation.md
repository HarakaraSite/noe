# 内蔵docsの実装・検証記録

2026-10-19。[計画](builtin-docs-plan.md)の実装記録です。実装・テスト・コードレビューと実プロバイダによる総合確認を完了しました。

## 実装

`internal/apidocs/`に英文MarkdownとGo catalogを置き、バイナリへembedしました。公開topicは40件（stdlib 26 module・stdlib概要・globals、NOE概要・tool・公開moduleの12件）、個別詳細は98件です。stdlibはgo-quickjs v0.27.0とNOEの設定を基準にし、全moduleの実export一覧と119件のglobal名を照合しました。

`noe docs [topic [member]]`と同期JS関数`noe:docs`の`docs(topic?, member?)`が同じ本文を参照します。CLIはruntimeやagent設定を生成する前に分岐します。main・Worker・nested Worker・agentの`run_js`から利用できます。catalogは初期化後不変で、検索時の外部I/Oはありません。

topic概要は最初のlevel-two headingより前の本文と詳細member一覧、個別参照は共有概要と指定memberの本文を返します。返却objectの操作には`FileHandle.readAt`等のIDを付けました。aliasは正規topicと同じ本文へ解決します。未登録項目は未収録と説明し、API未対応とは区別します。

`noe:run_js`はagent toolを説明するtopicで、同名のmoduleは追加しません。privateの`noe:agent/api`・`noe:agent/hooks`は公開一覧へ含めず、公開hookの登録・handler・渡されるAPIを`noe:agent`に説明しました。

初期instructionsにNOEの自己説明とdocsの参照方法を追加し、system I/Oの詳細は内蔵本文へ移動しました。本文は18,777 bytesから16,888 bytesへ変わりました。bytes/4の概算は約4,694から4,222 tokensで、特定モデルの実token数ではありません。親子・queueのinstruction snapshot契約は変更していません。

## 実装テスト

次の検証が成功しました。

- `go test ./...`
- `go test -tags=e2e ./... -count=1`
- `go vet -tags=e2e ./...`
- `CGO_ENABLED=1 go test -race ./internal/apidocs ./internal/runtime -run 'Test(Docs|Catalog|Markdown|InvalidCatalog|AgentLooksUp)' -count=1`
- Linux・Darwin・Windows、それぞれamd64/arm64のCGO不要build

catalogの到達性・alias・member境界・metadata分離・非公開項目、実module exportと説明の整合、CLIの無効agent設定からの独立性、main/Worker/nested Worker、バイナリ単体の移動とrun_jsとの本文一致、fixture agentのdocs取得からAPI使用までを確認しました。stdlibとNOE I/Oの代表的な例もローカル実行し、実プロバイダを必要とするauth・decision・image生成例はこの段階で実行していません。

本環境で実行したのはLinux amd64です。他の5環境はcross-buildによる確認です。

## 独立コードレビュー

reviewerが実装・テスト・40本文・計画・ガイドを確認しました。P1はなく、次のP2を本文で修正しました。

| 指摘 | 修正 |
| --- | --- |
| signalのSIGINT取消しとWorker-only SIGTERM購読の制約がreturn節でしか読めない | 共有概要へ移し、subscribe/next等の個別参照にも含めた |
| Workerがhost stdinを共有するように読める | Workerのstdinは即EOFであり、入力はmessage経由で渡すことを概要とstdin節へ追記 |
| Node形式Workerのtype optionを対応と説明 | 採用版では無視されること、inline script・NOE file loader・global Web Workerとの違いを説明 |
| spawnSyncの起動失敗時statusを-1と説明 | 採用版ではstatus:0とerrorが返る場合があり、errorの確認が必要と説明 |

利用者向け本文に混じったauthor向けの文言も削除しました。修正後のcatalog/CLI/runtime/modulemap関連テストとdocs E2Eは成功しました。再レビューでWorkerのinline sourceの自動module判定について追加訂正があり、親agentが採用版`stdlib/worker.go`の実行経路を確認して反映しました。`type`は無視されますが、scriptとして解析できないstatic importやtop-level awaitを持つsourceはmoduleへ自動判定されます。

## 実プロバイダ総合確認

利用者の明示許可により、通常の課題から組込みinstructionsを手掛かりにdocsを参照するか確認します。課題本文にはdocsを使う指示を含めません。追加の共通・workspace instructionを分離し、課題と組込みinstructionsを入力にします。

対象はOpenCode Go / `deepseek-v4.1-flash`で、位置指定ファイル更新とraw TCPのhalf-close往復の2課題です。tool.finishedの引数と結果で実際の参照を確認し、出力ファイルはテスト側でも検証します。各課題は最大10 provider要求・175秒のCLI実行時間に限定します。

```sh
NOE_AGENT_DOCS_LIVE=1 NOE_AGENT_DOCS_LIVE_DIR=/path/to/fresh/results \
NOE_OPENCODE_GO_API_KEY_FILE=/path/to/key \
  go test -v -tags='e2e docs_live' ./tests -run '^TestDocsOpenCodeLive$' -count=1 -timeout=7m
```

通常のE2Eでは実プロバイダを呼びません。

### 結果

1回ずつの初回試行で、両課題とも自発的に内蔵docsを参照し、成果物の検証が成功しました。課題文と初期instructionsは試行中に変更していません。[集計・課題文・参照toolのコード](builtin-docs-live-results.json)を保存しました。provider要求は合計9回です。

| 課題 | provider要求 | tool数 | docsを返したtool数 | 所要時間 | 結果 |
| --- | --- | --- | --- | --- | --- |
| positional_file | 5 | 4 | 2 | 18.0秒 | `noe:fs-handle`概要の後、open/readAt/writeAt/closeの個別詳細を調査。指定4bytesのみを更新し、前後8bytes・全体の不変部分・長さを確認 |
| tcp_half_close | 4 | 3 | 2 | 19.6秒 | `noe:sockets`概要の後、listen/connect/Listener/Connection操作を調査。loopback TCPでEOF後に返信し、clientが読み切ってfinallyで回収 |

docsを含むtoolの実結果と、その後の実行コードを親agentでも確認しました。ファイル課題では`readAt(3,8)`、`writeAt(5,...)`、`close()`を使用し、windowは`3456789a`から`34WXYZ9a`へ変化しました。TCP課題では送信側の`writer.close()`後も読み取りを続け、実際のportは46425、要求`ping`・返信`ACK:ping`でした。

ファイル課題の初期調査では、`Uint8Array.toString("hex")`をNode Buffer同様に使う付随的な誤用もあり、結果はhexではなく数値列でした。位置指定I/Oの契約はdocsで調べ、課題の更新とreportは正しく完了しました。この試験は1モデル・2課題での観測であり、すべてのモデルや課題での参照頻度を測ったものではありません。

raw成果物は`/tmp/noe-builtin-docs-live-20261019-f96cbyp3/`に保存しています。各caseのinstruction、task script、events.jsonl、stdout/stderr、summary、workspace成果物を保持しました。リポジトリの集計にはAPI key・認証headerを含めていません。
