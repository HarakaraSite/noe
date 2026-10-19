# 第2グループ低レベルAPI：実装・検証

2026-10-19。[計画](low-level-group2-plan.md)のS1〜S6を完了。Linux amd64、Go 1.27.2、go-quickjs v0.27.0。S0のprototypeとは別に本体へ公開moduleを登録した。通常・批判的レビューと限定再レビューを完了し、未解決のP2以上はない。

## 実装と所有

| slice | 実装 | 主な確認 |
| --- | --- | --- |
| S1 file | `internal/nativeio/file.go`・`js/file.js`。非同期open、位置指定I/O、部分失敗、同期的なclose開始とGo処理終結の追跡 | 対象外bytes、短いread/EOF、fake部分read/write、入力snapshot、safe integer・Go slice長の境界、close競合、遅いopen成功時のfd回収 |
| S2 connect | `internal/nativeio/net.go`・`js/net.js`。方向別の状態とI/O待機、開始用signalの解除、所有ref | TCP/UnixのFIN後応答、read cancel/write abortの独立、close時のStream完了、開始済みI/Oと遅いdial成功の回収 |
| S3 listen | `internal/nativeio/net_listener.go`。accept要求ごとのGo処理、受理済みconnectionとの独立所有、Unix identity照合 | port 0、pending accept取消し、listenerだけのkeepalive、既存path保護・置換file/socket保護、通常/強制終了後のpathname |
| S4 signals | `internal/signals/`のprocess broker、`internal/nativeio/signals.go`・`js/signals.js`のruntime別Iterator | FIFO・coalescing・return/close、同名/異名のmainと実Worker、Worker terminate、外部NotifyContext、最後のStopと世代競合 |
| S5 integration | `Host.Install`がmain/Worker共通で登録。REPLの既存drain/active回収を利用。ガイド・例・組込みinstructionsを更新 | run/eval・REPL・Worker・run_js・ローカルfixture agent・events/observe |
| S6 acceptance | 公開moduleを組み合わせた実CLIの往復と停止、既存回帰、OS build、通常/批判的レビュー | 総合E2E・race・vet・6target build成功。レビュー修正後の再確認完了 |

file open自体はAsyncWorkを保持せず、I/Oだけが実行を維持する。socket/listener/購読は明示closeまでrefを持つ。runtime終了ではGoのみでclose/unsubscribeを開始し、JSの配送・finally・finalizerを待たない。mainとWorkerは同じmodule登録を通り、resourceは生成runtimeに属する。

公開名は、標準の`node:fs`との使い分けを示す`noe:fs-handle`、生ソケット操作を示す`noe:sockets`、OSシグナル購読の`noe:signals`とした。ファイルハンドルとソケットは包括的なfilesystem/network APIの置換ではなく、標準APIに追加する機能として位置付ける。

命名確定後に登録・import・利用例・ガイド・組込みinstructionsを揃え、通常テスト全体、LowLevel E2E全体、vetとCLI buildを再確認した。新名のexportと旧名のaliasが存在しないこと、TCP/Unixの同梱例も実CLIで確認した。

## レビューと修正

各sliceを主担当がsource-to-impactでレビューし、対象テストの後に依存sliceへ進めた。file、socket、signalの実装は担当ファイルを分けて進めた。

| 指摘 | 対応 |
| --- | --- |
| fileのclose開始がgoroutine任せで同じJS turnの新規I/Oを受け付ける | ownerでclosingへ遷移してからGo closeを起動。同じturnのread/write拒否を検証 |
| socketの方向別停止が共通WaitGroupで反対方向も待つ | read/writeの待機を分離。両方向pendingでwrite abort後もreadできる検証 |
| connection.closeでpending readのJS配送を抑止しPromiseが残る | Stream controllerの終端化とnative closeを接続。read/writeとclosed Promiseを検証 |
| 取消し後の遅いdial成功でnative回収完了より先に終了を表示できる | Go workerの終結とresource closeを追跡するgate検証を追加 |
| 通常レビュー：safe integer内でもGo slice上限を超えるread長でpanicする | 回復可能なmakeslice長エラーだけをERR_INVALID_ARGのrejectへ変換。巨大長をbackendへ渡さない回帰テスト。通常OOMの回復を保証する変更ではない |
| 批判的レビュー：Unix bind後の初回identity取得前に置換されたpathを所有物として記録する | private directoryでbind・identity取得後、no-overwrite hard linkで要求pathへ公開。公開前のfile/socket作成競合と公開後の置換を別々に検証 |
| REPL補完中のProxyがnative購読/listenerを作ると回収判定から漏れる | 補完の新規処理判定にnative I/Oのactiveを含め、既存のsession終了・Go回収へ接続。購読とlistenerの回帰テストを追加 |

組込みinstructions増加で既存のcontext継承fixtureが早くtrimするため、固定説明量に応じてfixtureのmodel windowを算出するよう調整した。製品のcontext計算・trim条件は変更せず、0.2の継承と0.1既定値を区別し、toolとresultをblock単位で残す検証を維持する。

## 検証

主担当は対象テストから全体検証まで実施した。S6で修正したUnix公開と補完の回収を含むソースで、E2E・race・vet・6target buildを確認した。最後に追加した配送待ちacceptの回帰テストとfixtureの後片付け修正は、net全体の通常・raceで再確認した。

| コマンド・確認 | 結果 |
| --- | --- |
| `go test ./...` | 成功 |
| `go test -tags=e2e ./... -count=1` | 成功。新APIの全ケースに加え、既存agent queue/steer/interrupt、HTTP、process/io、import map、REPL等の回帰を含む |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1` | 成功。共有broker・native I/O・runtime・processを含む |
| `go vet -tags=e2e ./...` | 成功 |
| `CGO_ENABLED=0 go build`：Linux/Darwin/Windows × amd64/arm64 | 全6target成功。Linux amd64以外はcross buildのみ |
| `CGO_ENABLED=0 go build -o noe ./cmd/noe` | 成功。作業ディレクトリのCLIを更新 |
| 同梱のindex-roundtrip例（TCP/Unix）・wait-signal例（SIGTERM） | 成功。前者は対象3bytesだけ更新、後者はtimerなしで一度受信・解除して終了 |
| 相対文書リンク・gofmt・`git diff --check` | 成功 |

通常レビューではfile allocationのP2を解消し、S6で不足していた複数pending accept、置換socket保護、Worker終了後のmain通信を補った。批判的レビューではUnix identity取得のP2を再現・修正し、両レビューの限定再確認でP2以上の残件なしを確認した。独立レビューの実行確認もLinux amd64に限定する。

`tests/low_level_e2e_test.go`は位置指定file I/O→TCP/Unixの並行通信→部分更新を同じJSで実行する。各clientは送信FIN後に最終応答を読み、128KiB以上のbinaryを照合し、元fileの対象外bytesを比較する。`low_level_linux_e2e_test.go`はaccept/read/write/購読のtimeout・SIGINT、Worker terminate、fixture agent interrupt/親timeout、通常closeとSIGKILL後のUnix pathを扱う。

## OS差と限界

native実行はLinux amd64。Darwin/Windows、arm64のnative動作はこの環境では未確認。signal対応名はLinux/DarwinでSIGHUP・SIGINT・SIGTERM・SIGUSR1・SIGUSR2、WindowsでSIGINT。その他はERR_UNSUPPORTED_SIGNALを同期throwする。

通常fileの特殊filesystem/device/FIFO I/Oの即時取消し、durability・crash consistencyは保証しない。Go処理が残る間は回収完了を返さない。Unix socketの公開はsocket hard link、長いpathのbindは短いsymlink aliasを使うため、filesystem/OSが非対応なら明示的なOSエラーとなる。bind前に既存pathを消さず、通常closeではidentityを比較する。照合とremoveはatomicな比較削除ではなく、任意の同時path書換えへの保証はない。SIGKILL後のpathname・公開途中の一時directory残留はアプリの回収対象で、自動stale判定は追加しない。

実provider・外部クレデンシャルは使用しない。run_jsはローカルprovider fixtureで検証する。公開/releaseはこの作業の範囲に含めない。

[公開API](../low-level-io.md) · [利用例](../../examples/low-level/README.md) · [計画レビュー](low-level-group2-review.md)
