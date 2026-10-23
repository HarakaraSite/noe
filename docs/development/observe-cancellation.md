# 単独noe observeの出力待機中の取消し — 2026-10-24

[開発記録](README.md) · [利用ガイド](../observe.md)

利用者の「noe observeの取消し改善をしよう」に従い、
[小規模改善時の別件メモ](small-improvements-20261020.md#今回の対象外)を対応した。
基点は`295e5e0`、Go 1.27.2 / Linux amd64。

## 原因と変更

単独`noe observe`のrendererとJSON encoderがstdoutへ直接同期書込みしていた。
書込みが詰まると、contextの取消しを確認するselectへ戻れなかった。
終了時の`renderer.finish()`による改行も同じ経路だった。

`internal/cli/observe.go`で、既存の`outputAdapter` / `internal/hostio`を使って
stdoutを書き込む。通常表示・最終改行・JSON encoderへ同じ取消しcontextを渡し、
取消しは終了コード130、実際の書込み失敗は診断付きの終了コード1にする。
取消しを受けた経路では追加の整形改行を書かない。
端末幅はwrapperを挟む前の元のstdoutから求める。

adapterは借用したstdoutを閉じず、所有する複製descriptorだけを回収する。
`WriteContext`を実装した独自writerも取消しを受け取る。
通常の独自`io.Writer`は従来どおり同期`Write`へ渡し、拒否や別goroutineへの放置はしない。
この最後の経路はwriter自身が戻る必要があり、実行中の任意の`Write`を強制取消しするものではない。
部分書込み後のエラーで同じbytesを再送しない。

## 検証

- 回帰テストは通常表示・最終改行・JSON出力それぞれの書込み開始を待ってから取消す。
  修正前は3ケースとも出力待機から戻らず失敗、修正後は130・stderr空で終了した。
  借用writerを閉じないことと、通常の独自writerのエラーを保持することも確認した。
- `CGO_ENABLED=0 go test ./internal/cli -run '^(TestObserve|TestObservation)' -count=1 -timeout=60s`：成功。
- 同範囲と`TestEvents`のrace：成功（1.741s）。
- 保存ログの人間向け表示/解析、失敗runのJSON観察、直接表示と保存ログ再生の一致の
  既存E2E 3件：成功（1.306s）。providerはローカルfixture。
- `go vet ./internal/cli`：成功。
- Windows amd64・Darwin amd64の`go list -export ./internal/cli`：成功。
  他OSでのnative実行は未確認。

Linuxで1 MiBの最終回答をpipeへ出力し、64 KiBを読んだ後に読み取りを止めてSIGINTを送った。
通常表示・JSONとも、公開v2.3.0は1秒経っても終了せず検証側で回収した。
修正版は両方とも約65msで130・stderr空で終了した。
読み手が閉じたpipeは、修正版でも`broken pipe`の診断と終了コード1になった。

初回のJSON unit fixtureは必要なevent envelopeが不足し、書込みへ到達しなかったため修正した。
実pipe確認の初回は先頭の小さなheaderを読んだ直後にSIGINTを送ってしまい、
旧版も出力詰まりに入る前に取消せた。大きい本文の書込み中と分かる64 KiBを読んでから
読み取りを止める方法へ直し、上記の差を確認した。製品仕様や判定を緩和したものではない。

結果・baseline記録・native検証fixtureとバイナリは
`/var/tmp/noe-observe-cancel-jg9ndkzw`へ保存した。
製品変更はCLIの単独observe経路に限り、上流・依存・共有hostio・実行中の`--observe`経路は変更していない。
必要な関連検証を完了し、全体unit/E2E/raceの再実行や実providerへの要求は行っていない。
バージョン更新・配置・公開は未実施。利用者の追加指示により、修正・回帰テスト・本記録をコミットする。
