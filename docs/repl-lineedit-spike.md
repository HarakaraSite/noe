# NOE REPL行編集ライブラリ比較結果

> この文書と比較結果は行編集ライブラリ選定時の記録です。製品はその後stdlibへ移行し、入力UIも[qjs固定版からの取り込み](qjs-repl-integration.md)へ置き換えました。当時の採用ライブラリ・ホストAPI・HTTP中断の期待値は現行仕様を示しません。stdlibについては[移行記録](increment-12.md)を参照してください。

インクリメント3の選定基準に沿って、`chzyer/readline v1.5.1`と`peterh/liner v1.2.3`を既存のNOE Sessionに接続して比較した。**現在のLinux CLIには`peterh/liner v1.2.3`を採用する。** 両候補は公開APIを使うアダプターで必須条件を満たしたが、既存の割込み・入力エラーの契約を維持する接続処理はlinerの方が少ない。

この比較で作ったのは[再実行できるスパイク](../spikes/repl-lineedit/README.md)。その後、[インクリメント3](increment-3.md)のスライス2で製品CLIにもliner v1.2.3を接続した。ルートとスパイクのGo要件は、利用環境に合わせて1.27.2へ揃えた。

## バージョンと環境

| 項目 | 確認した条件 |
| --- | --- |
| OS | Linux amd64 |
| Go | NOE全体の要件である1.27.2 |
| 通常ビルド | CGO無効 |
| エンジン | 現在のNOEが使うgo-quickjs v0.25.3。エンジン変更なし |
| 端末 | 制御端末を持つPTY、`TERM=xterm-256color`、24行、80列と40列 |
| 画面の確認 | pyte 0.8.2、wcwidth 0.2.13 |
| 候補 | readline v1.5.1、liner v1.2.3 |

linerの最新タグ[v1.2.3](https://github.com/peterh/liner/blob/v1.2.3/go.mod)はGo 1.26を要求する。NOEのGo要件を1.27.2へ揃えたうえで、v1.2.3を固定して再比較した。Go 1.24とliner v1.2.2を使った以前の比較は[履歴](../spikes/repl-lineedit/history/go124-liner122/environment.json)に残す。

## 必須条件の比較

| 選定基準 | readline v1.5.1 | liner v1.2.3 |
| --- | --- | --- |
| 純Goとビルド | 適合。Go 1.27.2・CGO無効で成功 | 適合。Go 1.27.2・CGO無効で成功 |
| 基本編集と履歴 | 適合。文字・単語移動、削除、履歴呼出しと修正を確認 | 適合。同じキー列と入力で確認 |
| 日本語と長い行 | 確認したケースに適合。日本語の途中削除、40列を超えるASCIIと全角文字が2列境界に揃う日本語を確認 | 同じケースに適合。折り返し設定を有効にして確認 |
| 割込みと終了 | 適合。検索・補完中も130にする調整と、外部SIGINTで終了待ちを解除する処理が必要 | 適合。Ctrl+Cの中断設定と、評価中の通常モードへの復帰が必要 |
| 端末復元 | 適合。EOF・明示終了・入力失敗・各割込み後にtermiosが一致 | 適合。同じ終了経路で確認 |
| 入出力への接続 | 適合。Configでstdin・stdout・stderrを設定できる | 現在のCLIの標準入出力に適合。端末編集はプロセスのfd 0・1を使う |
| 次の改善への接続 | 適合。変更したプロンプトと静的補完を実行し、履歴保存APIを確認 | 適合。同じプロンプト・補完を実行し、履歴のRead/Write APIを確認 |
| 固定とライセンス | 適合。固定したmoduleとsum、MIT | 適合。固定したmoduleとsum、MIT相当のCOPYING |

両候補とも、画面表示だけでなく、編集後の入力が既存Sessionへ渡り、期待した値に評価されることを確認した。JSの操作は共通プログラムの評価側だけで行い、入力・補完側では静的な文字列だけを扱った。

## 接続に必要だった処理

### readline

入力待ち中の外部SIGINTで、`Instance.Close`が終了しない現象を再現した。`FillableStdin.Close`が内側の`CancelableStdin`をCloseしないため、保持したCancelableStdinを先にCloseする処理で解消した。[同じ終了待ちを扱うissue](https://github.com/chzyer/readline/issues/217)もある。ライブラリのforkやpatchは不要だった。

下位Readerの読み取り失敗は、そのままではEOFとして返される。NOEの「入力読み取り失敗はコード1」という契約を維持するため、Readerの非EOFエラーを保持して読み取り結果へ戻す処理を追加した。

ライブラリ標準のCtrl+Cは検索・補完モードを抜けるために使われ、直ちにREPLを終了しない。インクリメント3の終了契約に合わせ、公開フィルターとOperationの公開APIで先にそのモードを解除し、通常の割込みへ対応させた。

### liner

`SetCtrlCAborts(true)`でPromptの割込みを終了コード130へ対応させた。検索・補完中も同じ設定で終了できた。履歴は、読み取りに成功した入力を`AppendHistory`へ渡す。

`NewLiner`は端末を編集用のモードにし、Prompt終了後もそのモードを保持する。`TerminalMode`で生成前の通常モードと生成後の編集用モードを保存し、Promptの前後で切り替えた。これにより評価中のCtrl+CをOSのSIGINTとして受け取り、JS・HTTPの実行を既存のcontextで止められた。

端末用の公開APIはプロセスの標準入出力を使う。実装時はCLIの実際のfd 0・1にだけこのアダプターを接続し、差し替えたReader/Writerやパイプ入力は現在の行読み取り経路で扱う。任意の端末Reader/Writerを直接差し込む必要が生じた場合は、Configを持つreadlineの方が接続しやすい。

## 採用理由

最優先の比較基準である「現在のCLIへ接続するコードと責務の少なさ」でlinerを選ぶ。端末モードと履歴の扱いを明示すれば、現在の終了・読み取り失敗の契約へ対応できる。readlineでは、それに加えてCancelableStdinの明示Close、Readerエラーの保持、検索・補完中の割込み調整を保守する必要があった。

履歴保存、継続プロンプト、補完への公開APIは両候補にある。今回、プロンプト変更と静的補完は実行確認した。インクリメント4のJS補完、複数行の構文判定、評価だけを中断して続行する処理は、NOE側で実装する。

linerには追加の実行時依存として`github.com/mattn/go-runewidth v0.0.31`と、その間接依存である`github.com/clipperhouse/uax29/v2 v2.7.0`がある。一方readlineはこのLinuxビルドで、ライブラリ本体以外の非標準パッケージを使っていない。この点はreadlineが有利だが、今回の比較順位では接続処理の少なさを優先した。

## 依存と保守状況

スパイクの実際のmodule graphでは、`golang.org/x/term v0.40.0`と、liner v1.2.3が要求する`golang.org/x/sys v0.48.0`が選ばれた。製品接続でも同じバージョンとliner v1.2.3をルートmoduleへ固定した。

| 候補 | ライブラリが宣言する直接依存 | このLinuxビルドで追加される実行時パッケージ |
| --- | --- | --- |
| readline | chzyer/test v1.0.0、chzyer/logex v1.2.1、x/sysの旧バージョン | ライブラリ本体。test・logexは今回の実行バイナリに入らない |
| liner | go-runewidth v0.0.31、x/sys v0.48.0。間接依存にuax29/v2 v2.7.0 | ライブラリ本体、go-runewidth、uax29/v2 |

調査時点のreadlineのmainには2025年6月の変更があり、linerのmainには2026年10月の変更がある。mainの変更は固定したタグに含まれるとは扱わない。readlineのClose・履歴検索・再描画に関する公開issueと、linerの端末モード・履歴・貼り付けに関する公開issueを確認した。issueの存在だけでは不適合とせず、NOEの操作で再現した終了待ちと割込みの差を採用判断に使った。

## 検証結果と限界

Go 1.27.2・CGO無効のバイナリを使った共通28シナリオが、両候補とも成功した。[入力と評価結果を含む記録](../spikes/repl-lineedit/results.json)と[比較プログラム](../spikes/repl-lineedit/compare.py)を残した。

Go 1.27.2のraceビルドで、履歴・検索・検索中のCtrl+C・補完中のCtrl+C・入力待ちの外部SIGINT・無限ループの中断・HTTP待ちの中断と、パイプ入力を確認した。両候補とも成功し、検査した経路でraceの報告はなかった。[race検査の記録](../spikes/repl-lineedit/race-results.json)も残した。スパイクの`go vet ./...`、NOE全体の`go test -tags=e2e ./... -count=1`、CLIのCGO無効ビルドも成功した。

変更前のGo 1.24とliner v1.2.2に対する独立した批判的レビューは、[以前のレビュー記録](../spikes/repl-lineedit/history/go124-liner122/review-results.json)として残す。liner v1.2.3への更新では、同じ比較とrace検査をGo 1.27.2で再実行した。製品への接続・レビュー・E2Eは[インクリメント3の実装記録](increment-3.md#実装とレビューの結果)に記載する。

日本語と再描画の確認は、指定した文字列と80列・40列のPTYを対象とする。カーソル位置の確認を追加した際、全角文字が右端の残り1列から始まる入力では、pyte 0.8.2が文字を切り詰めるauto-wrapのモデル差が見つかった。このケースをライブラリの不適合とは判定せず、共通の40列日本語ケースは全角文字を2列境界に揃えて確認した。右端の残り1列へかかる場合の実端末表示は未確認として残す。

結合文字、絵文字、IME、実際の端末エミュレーターごとの表示、他OSの適合性まで実証したものではない。履歴の永続化は公開APIの確認に留まり、複数行履歴や保存失敗の製品動作は4の詳細計画で決める。選定は現在のLinux CLIとGo 1.27.2要件に対するものとする。

## 参照

- [インクリメント3の選定基準](increment-3.md#必須の選定基準)
- [readline v1.5.1](https://github.com/chzyer/readline/tree/v1.5.1)
- [liner v1.2.3](https://github.com/peterh/liner/tree/v1.2.3)
- [readlineのCloseに関するissue](https://github.com/chzyer/readline/issues/217)
- [linerのTerminalMode API](https://pkg.go.dev/github.com/peterh/liner@v1.2.3#TerminalMode)
