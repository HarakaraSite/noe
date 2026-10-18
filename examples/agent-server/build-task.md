NOE上で動く、HTTP経由で指示を受け付けるagentサーバーのexampleを実装してください。

目的：
ブラウザやcurlから仕事を依頼し、agentの進捗・回答を確認し、追加指示や停止を行えるようにする。

実装場所：
examples/agent-server/ 以下にJS・HTML・説明書を配置する。
examples/README.mdに案内を追加する。
既存のNOE APIを組み合わせ、NOE本体のGoコードは変更しない。

実行環境：
NOEはgo-quickjsを使ったJavaScriptランタイムです。
Node.jsとの完全互換を前提にせず、README・docs/api.md・docs/observe.md・examples/http/http-server.jsを確認して、実際に利用できるAPIで実装してください。
テストや動作確認にOSのコマンドを使って構いません。

構成：
・serve()でHTTPサーバーを起動する。
・spawn_agent(..., undefined, {keepAlive:true})でagentを一体、idle状態で起動する。
・queue_agent()で仕事を渡し、同じagentの会話を継続する。
・待受agentの既定設定はprovider:"openai-chatgpt"、model:"gpt-6.1-sol"、effort:"high"。
・provider・model・effortと待受ポートは起動時に変更できるようにする。
・依頼と結果は、このサーバープロセスのメモリに保持する。

HTTP API：
POST /tasks
  {"instruction":"指示文"}
  完了を待たず202で受付IDを返す。

GET /tasks
  依頼の一覧と状態を返す。

GET /tasks/:id
  その依頼の状態、取得できたrunId、完了時の回答または失敗情報を返す。

POST /tasks/:id/steer
  {"instruction":"追加指示"}
  実行中の該当taskへsteer_agent()で追加指示を送る。
  受付と適用完了を区別する。

POST /tasks/:id/interrupt
  実行中ならrunIdを指定してtask単位で停止する。
  agent instanceと他の予約は残し、後続の仕事を実行できるようにする。
  未開始ならサーバー側の予約を取り消す。

重要な動作：
・複数のPOSTを受け付け、順番に処理する。
・すべての依頼について結果をID別に保持する。
・observe_agent()のlastResultは最新一件なので、単純なポーリングで途中の完了結果を取りこぼさない構成を選ぶ。
  例えば、サーバー側に予約を保持し、現在taskの結果を保存してから次をqueue_agentへ渡す方法でよい。
・後続taskは前の会話と回答を引き継ぐ。
・未知のID、不正なJSON、空の指示、操作できない状態には適切なHTTPエラーを返す。
・失敗やtask停止の後も、サーバーと待受agentを使い続けられるようにする。

ブラウザ画面：
GET /で小さな操作画面を表示する。
指示の送信、依頼一覧、進捗・回答の表示、追加指示、停止を操作できるようにする。
定期的なGETで表示を更新する簡単な構成でよい。

検証：
HTTP API、複数依頼の結果保持、会話継続、失敗後の継続、追加指示、task停止を確認する。
自動テストはローカルfixtureを中心にし、正常終了・停止時に所有するagentや子プロセスが残らないことも確認する。

説明書：
起動コマンド、設定方法、curlでの操作例、ブラウザでの使い方を記載する。

実装・テスト・動作確認まで自律的に進めてください。
最後に作成ファイル、起動方法、検証結果、残った制約を日本語で簡潔に報告してください。
コミット・push・releaseは行わないでください。
