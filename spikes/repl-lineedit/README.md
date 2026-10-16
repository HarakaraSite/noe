# NOE REPL行編集ライブラリ比較スパイク

> この文書と比較結果は行編集ライブラリ選定時の記録です。製品はその後stdlibへ移行しており、当時のホストAPIやHTTP中断の期待値は現行仕様を示しません。[現在の移行記録](../../docs/increment-12.md)を参照してください。

`chzyer/readline v1.5.1`と`peterh/liner v1.2.3`を、Go 1.27.2でNOEの既存Sessionへ接続して比較する。スパイク専用のGo moduleであり、製品のCLIとルートの依存定義には行編集を導入しない。

比較では、**現在のLinux CLIには`peterh/liner v1.2.3`を選ぶ**。両候補は同じ28シナリオを通ったが、既存の終了契約を維持するための追加処理はlinerの方が少ない。readlineも公開APIを使ったアダプターで適合できる。詳細は[比較結果](../../docs/repl-lineedit-spike.md)に記載する。

## 再実行

このスパイクはLinux専用。GoとPython 3を使い、画面描画の確認には`pyte`を使う。下のコマンドはこのディレクトリから実行する。

```sh
python3 -m pip install -r requirements.txt
GOTOOLCHAIN=go1.27.2 CGO_ENABLED=0 go build -o /tmp/noe-lineedit-spike .
python3 compare.py /tmp/noe-lineedit-spike --output results.json
```

実際に入力する場合は、候補を指定する。

```sh
/tmp/noe-lineedit-spike --editor readline
/tmp/noe-lineedit-spike --editor liner
```

`--hooks`は継続プロンプトと静的な補完候補の接続だけを試す。JSに基づく補完や複数行の評価は実装していない。`--fail-input`は入力失敗時の終了と端末復元を比較するためのfault injection、`--trace`は入力・評価結果をJSONLへ記録するためのオプション。

## 比較方法

両候補とも同じGoプログラムから既存の`runtime.NewSession`を使う。1行の読み取りが完了してから評価し、次の読み取りは評価後に開始する。raw modeで編集中のCtrl+Cと、通常モードで評価中のCtrl+Cを区別する。

`compare.py`は子プロセスに制御端末を割り当て、PTYへ実際のキー列を送る。通常は80列、折り返し確認では40列に設定する。描画を`pyte`で解釈し、編集後の画面・カーソル位置と、Sessionへ渡った入力・評価結果を確認する。各終了経路では`termios`を比較して、元の端末設定に戻ったことを確認する。

pyte 0.8.2は、全角文字が右端の残り1列から始まる場合に文字を切り詰め、実際の端末とのauto-wrapの差が生じる。この境界のカーソル位置は適合判定に使わない。40列での日本語ケースは、文字列の先頭に空白を置き、全角文字を2列境界に揃えて折り返し・編集・評価を確認する。右端の残り1列へかかる場合の実端末表示は未確認として残す。

HTTP確認はローカルサーバーだけを使う。サーバーが接続を受けた後にCtrl+Cを送り、終了コード130と通信の切断を確認する。固定sleepではなく、編集モード、画面、評価結果、HTTP開始を待ってキーを送る。

## 候補ごとの設定

| 設定 | readline | liner |
| --- | --- | --- |
| 履歴 | `HistoryLimit: 1000`、セッション内の自動記録。保存先は未設定 | 成功した入力で`AppendHistory`。標準の履歴上限1000 |
| 折り返し | 標準の再描画 | `SetMultiLineMode(true)`。論理的な複数行入力ではなく、長い1行の表示 |
| Ctrl+C | `ErrInterrupt`を130へ対応。検索・補完中も同じ意味にするフィルター | `SetCtrlCAborts(true)`、`ErrPromptAborted`を130へ対応 |
| 評価中の端末 | Readlineの終了時にraw modeを解除 | `TerminalMode`で保存した通常モードへ戻し、次のPrompt前に編集用モードを適用 |
| 外部SIGINT | 保持した`CancelableStdin`をCloseしてからInstanceをClose | StateをCloseして端末設定を復元し、CLIを終了 |
| 入力失敗 | 下位Readerのエラーを保持して、ライブラリが返すEOFと区別 | Promptが返す読み取りエラーを使う |

入力失敗の注入方法は公開APIの違いに合わせる。readlineはReaderを差し替え、linerはfd 0を一時的に読み取り不能なdescriptorへ差し替える。linerではPrompt終了後にfd 0を戻してから端末設定を復元する。通常の入力ケースとキー列は両候補で共通。

## ファイル

- `main.go`: 共通のSession接続、端末とパイプの切替、終了処理。
- `readline.go`、`liner.go`: 候補ごとのアダプターとfault injection。
- `compare.py`: PTY・画面・入力・終了・通信の比較。
- `results.json`: Go 1.27.2・CGO無効のビルドを使った比較結果。
- `race-results.json`: raceビルドでの限定ケースとパイプ入力の記録。
- `environment.json`: 現在のGo・候補ライブラリのバージョン。
- `history/go124-liner122/`: Go要件変更前の比較とレビューの記録。

race検査は別のビルドで、対象ケースを指定して実行できる。

```sh
CGO_ENABLED=1 go build -race -o /tmp/noe-lineedit-spike-race .
python3 compare.py /tmp/noe-lineedit-spike-race --output /tmp/noe-lineedit-race.json \
  --case history --case history-search --case ctrl-c-search \
  --case ctrl-c-completion --case sigint-idle --case ctrl-c-eval --case ctrl-c-http
```
