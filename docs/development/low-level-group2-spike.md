# 第2グループ低レベルAPI：計画確定前のスパイク

2026-10-19。[計画](low-level-group2-plan.md)を最終化する前に、[再現コード](../../spikes/low-level-group2/README.md)を作成して実行した。本体にmoduleを追加した記録ではない。

## 環境と範囲

Linux amd64、Go 1.27.2、go-quickjs v0.27.0、開始時の本体HEADは`2200219`。通常テスト/buildから分離する`lowlevel2_spike`タグを使う。実シグナルと強制killはスパイク自身のhelper子プロセスにのみ送る。

Go backendだけの検査と、埋込みQuickJS＋stdlib loop上でJSを動かした検査を分けた。signalは実際のgo-quickjs Workerも使用した。NOE製品のCLI・REPL・agent run_jsへの新API接続、他OSのnative実行、性能比較は未確認で、計画の後続sliceへ残す。実プロバイダ・外部ネットワーク・実クレデンシャルは不要だったため使用していない。

## 結果と計画への反映

| スパイク | 実動作で確認したこと | 採用する判断・限界 |
| --- | --- | --- |
| [file](../../spikes/low-level-group2/file/RESULTS.md) | 既存range stream、同じhandleの逆方向を含む位置read、短いread＋EOF、対象外bytesを保つ部分write、二重close、read後closeエラー。pending read中のowner loop配送、open単独は非keepalive、runtime closeでfd回収 | readAt/writeAtを中核に採用。部分bytesとEOFを同時に返す。stat/truncate/syncは別用途として後続へ。prototypeのopenは同期で、製品の非同期open・部分失敗情報・cancel競合はS1の検証対象 |
| [net](../../spikes/low-level-group2/net/RESULTS.md) | TCP/Unix双方で送信close→peer EOF→最終応答受信。read cancel後もwrite可能、開始済み未完了writeのabort後もread可能。full close、accept解除、listener単独のkeepalive、runtime closeのGo回収 | duplexは方向別の状態で実装し、既存pipe endpointを直結しない。write abortはwrite方向の終端で再利用不可。複数client、開始用AbortSignal、pathname identity照合は本体sliceで確認 |
| [signals](../../spikes/low-level-group2/signals/RESULTS.md) | main/実WorkerへのOS配送、Worker終了後のmain受信、SIGTERM購読中の既定終了抑止、最後解除後の復元、SIGINT NotifyContextとの同時受信、timerなしのkeepalive、runtime close時の解除・Go参照解放 | process-wide brokerとruntime購読を分離。Go registrationだけを解除。製品では名前選択とAsyncIteratorを使う。prototypeの固定INT/TERM・callback形を製品契約と混同しない |

readAtは、既存range streamを範囲ごとに開き直すJSでも代替できる。writeAtは、既存の全面読込み＋書換えやCLIでも実現できる。新APIの追加価値は一つのfdの反復利用と、対象外bytesを触らない位置更新であり、「既存JSでは不可能」という判断ではない。時間・メモリの優位を測定した結果でもない。

## 停止とUnix pathname

通常listener closeではGo netのunlinkによりpathnameが消えた。helper子をbind・ready後に強制killするとsocket pathnameが残った。JS finallyやGo OnCloseでSIGKILL後の片付けを保証することはできない。製品では自動stale削除・新supervisorを必須とせず、この差を資料とテストへ残す。

スパイクはGo既定のunlinkを観測しただけで、置換されたpathの保護は実証していない。本体S3では自動unlinkを任せず作成物のidentityを確認し、close前に別file/別socketへ置換した場合に消さないことを検証する。

socketのread/writeはnative workerが呼出し直前まで進んだこと、取消し前に未完了であること、取消し後の終結と結果を確認した。write Promiseはabort前の未settledとabort後のError rejectionも検査した。acceptもcloseで終了した。Go netpoller/OS wait内部で既にblockしていたことまでは観測していないため、OS内部待機の直接証明とは扱わない。一方、通常fileのblock中I/Oや、特殊filesystemでの即時取消しは未実証であり、全I/Oを即時に中断できるという結論にはしない。

## 再現・主担当の一括確認

個別担当の実行記録は各RESULTS.mdへ保存した。主担当も3packageをまとめて以下で再実行し、すべて成功した。

```sh
go test -tags=lowlevel2_spike -count=1 -v ./spikes/low-level-group2/...
```

R1修正後にも一括テストを再実行し成功した。一括race・vet、通常テスト、実験packageの通常buildからの除外も確認した。

| コマンド | 結果 |
| --- | --- |
| `go test -tags=lowlevel2_spike -count=1 ./spikes/low-level-group2/...` | 3package成功。netのR1修正後に再実行 |
| `go test -race -tags=lowlevel2_spike -count=1 ./spikes/low-level-group2/...` | 成功。netのR1修正後に再実行 |
| `go vet -tags=lowlevel2_spike ./spikes/low-level-group2/...` | 成功。netのR1修正後にも確認 |
| `go test ./...` | 成功。本体コード・依存の変更なし |
| `go list ./...` | 実験の3packageが含まれないことを確認 |
| 文書のlocal link/heading確認・`git diff --check` | 成功。119文書、local link 800件、見出し参照38件。空白差分なし |

実証を追加したコードのgofmtは差分なし。通常E2E全体や6target配布buildは本体未変更のため今回繰り返さず、実装後のS5/S6の受入へ記載している。

## 実装前レビューの指摘への対応

- half-closeを具体的な往復で実証し、方向別close/cancel/abort/full closeを計画に固定した。
- signalは既定動作が変わることとWorker exitのscopeを実証し、SIGINTの既存取消しを維持する契約にした。
- listener/購読は補助JS timerを置かずに待機を維持し、単なるGo所有とAsyncWorkのrefの違いを確認した。
- fileのread価値とwrite価値を分け、追加操作を一括採用しなかった。

[計画レビュー記録](low-level-group2-review.md)には、計画作成後の通常・批判的レビューとP2指摘の修正・再レビューを記録した。両レビューとも最終的にP2以上の残件なし。
