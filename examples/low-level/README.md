# ファイル・ソケット・signal

```sh
./noe run examples/low-level/index-roundtrip.js tcp
./noe run examples/low-level/index-roundtrip.js unix
./noe run examples/low-level/wait-signal.js SIGTERM
```

index-roundtripは専用の一時directoryにfileとserverを作り、索引の一部分を読み、socket越しの応答でその部分だけ更新します。送信FIN後も応答を受信します。出力は`[0, 1, 12, 13, 14, 5, 6, 7]`です。Unix socketは実行OSの対応が必要です。

wait-signalは補助timerを使わず待機します。Linux/Darwinでは別端末から表示されたPIDへ`kill -TERM PID`を送ると、一度受信して購読を解除し終了します。SIGINTは既存CLIの取消しも起こすため、この例の正常終了にはSIGTERM等を使います。Windowsの対応名はSIGINTだけです。

[公開API・所有と停止](../../docs/low-level-io.md) · [例の一覧](../README.md)
