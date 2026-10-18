# Agent内部の責務整理計画

2026-10-18。利用者の依頼に従い、スライスごとの実装・テスト・独立レビュー、その後の総合テストまで行う。

## 目的と維持する契約

`internal/agent`で変更する場所と依存関係を追いやすくする。ファイルの役割を揃え、ライフサイクル操作をHTTPから分離し、独立した子プロセス実行を下位packageへ切り出す。

- 公開JS API、Goの既存入口、設定JSONの省略/nullの意味、RPCの要求・応答・HTTP status・エラー文言、イベント形式と発行順を維持する。
- `Host.mu`による共有状態の保護、所有関係、取消し・回収・queue継続・会話/画像の所有境界を維持する。ロック分割や並行処理方式の変更は含めない。
- 直前に実装したstdout/stderr各64KiBの出力制限を維持する。既存の未コミット変更は作業開始時の入力として扱い、削除・巻き戻しをしない。
- プロバイダの追加、権限制限、SDK更新、挙動変更、実プロバイダへの追加通信、コミット・push・releaseは範囲外。
- ユーザーの`agent-server-build.jsonl`を変更・削除・作業資料へ複製しない。

## S1: 設定・Host・Operation・イベントの配置整理

同一packageのまま、`agent.go`をpackage説明に絞り、設定と接続契約、Host、Operation、観察通知、ID生成を役割別ファイルへ移す。関数本体は維持する。

確認: `go test ./internal/agent ./internal/cli ./internal/runtime`。レビューは移動漏れ、公開シンボル、初期化/close順、既存差分の保持を対象とする。

## S2: ライフサイクルとHTTP受付の分離

`lifecycle.go`にまとまったtask設定、instance状態、所有判定、実行loop、taskプロセス起動を役割別に配置する。spawn・observe/wait・queue・steer・interruptは、HTTPのdecode/encodeと型付きの状態操作を分離する。人間用controlとJS用RPCは同じ操作を呼ぶ構造を保つ。

状態操作はHTTPの`ResponseWriter`や`Request`へ依存させず、`Operation`、型付き要求、必要な取消しcontextを受け取る。既存のstatus/code/messageを保持する内部エラー型を使い、request取消し時の無応答も維持する。状態更新、unlock、イベント、ACK待ちの順は変更しない。

確認: lifecycle/control/steer/interrupt/queue/ownership/execution/conversation関連のagent・runtime試験、実CLIのcontrol/queue E2E。レビューはロック・イベント・重複command・取消し・ACKの前後関係を中心に行う。

## S3: 子プロセス実行を独立packageへ抽出

`internal/agent/runner`へOSプロセスの起動・監督、出力収集、終了結果を移す。下位packageは`agent`をimportせず、実行ファイル・引数・cwd・環境変数の明示入力だけを受け取る。既存`processscope`を利用する。

親packageはNOE固有のhelper import、task bootstrap、一時ソース/import-map配置、Connectionの環境変数への変換を担当する。一時モジュールの位置・CLI引数はNOEのimport契約に関わるため、OS実行の下位packageへ持ち込まない。既存の`agent.Runner`・`agent.Result`入口はwrapper/type aliasで維持する。出力上限の試験も新しい責務の配置へ移す。

確認: runnerの境界/UTF-8/大量出力/取消し/子回収/import-map試験、実NOEの出力制限E2E。レビューは依存方向、環境の明示受渡し、cleanup順、各64KiB上限と結果互換を対象とする。

## S4: 全体確認と開発者向け案内

`docs/agent-architecture.md`に入口、JS/Goの分担、ファイルとpackageの責務、共有状態とロックの規則、代表的な処理の流れを記載する。実装記録に各スライスの変更・試験・レビューの採否を残す。

総合テスト:

1. `CGO_ENABLED=0 go test ./... -count=1`
2. `CGO_ENABLED=0 go test -tags=e2e ./tests -count=1`（実CLI・PTY・local fixture、利用可能なら準備済MCP bundleも使用）
3. `CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... -count=1`
4. `go vet -tags=e2e ./...`、`git diff --check`
5. CGOなしでLinux/Darwin/Windows × amd64/arm64をbuild。Linux以外はcross-buildの確認であることを明記する。

総合レビューは、各スライスの境界がつながっていること、循環依存がないこと、実行/停止/queue/履歴/画像/hook/観察/出力制限の契約が維持されることを対象とする。必須指摘を修正し、一回の限定再レビューで解消を確認する。

各通常レビューは30分以内、修正の再レビューは15分以内に限定する。新しい根拠が10分ない場合は中間結論を受け取って中断する。
