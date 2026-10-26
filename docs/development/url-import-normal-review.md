# HTTP/HTTPS URL import設計案：通常レビュー

## 結論

**設計検討・文書化の成果として妥当。通常レビューで修正を要する重要な指摘はなし。**

正本 `docs/development/url-import-plan.md` を、利用者の明示合意と現行のloader / Profile / Worker / agent契約に照らして確認した。合意・技術提案・未決が分離されており、合意した利用経路を成立させるための変更境界も示されている。

これは製品実装の承認、公開CDNの動作保証、実装に必要な全詳細の確定を意味しない。特にredirect後のmodule同一性と同期取得中の取消しは、正本が明示する実装前の確認事項として残る。

## 重要な指摘

**指摘なし（N系列の修正要求なし）。**

未実装であることや、正本が明記した未決事項に実行証拠がないことだけを、不具合・仕様矛盾とは扱っていない。

## 判断の根拠

| 観点 | 正本の対象section | 現行コード根拠と判断 |
| --- | --- | --- |
| 合意した利用形 | §1–3、§5.1、§5.3 | `internal/modulemap/snapshot.go:122–173,220–265` は現在、完全一致mapとローカル解決を行う。URL target / URL referrerをこの共通解決へ追加する提案は、直接URL・alias・依存グラフの経路と整合する。npm探索や全面的なimport map対応を合意へ混入させていない。 |
| URLとローカルpathの分離 | §4、§5.1–5.2 | `internal/modulemap/path.go:12–85`、`virtual.go:10–63`、`loader.go:60–100` にはpath正規化・拡張子検査がある。正本はremoteをこれらへ流さない変更を明記し、ローカルの.js/.mjs制限とURL拡張子不問を区別している。 |
| キャッシュとmodule同一性 | §2、§5.2、§6.2 | `internal/modulemap/loader.go:10–56,77–88` はruntime-localの成功source保持を行う。`stdlib/loader.go:35–47` はfactory呼出しごとにloaderを作る。ダウンロードキャッシュを追加せずこの保持を維持する整理は現行契約と整合する。redirectのcanonical ID統合は保証済みではなく、engine確認を要する提案として扱われている。 |
| Goホストの能力選択 | §6.1–6.3、§10 | `stdlib/loader.go:14–48` は選択FS以外の読取り能力を与えず、`internal/jsexec/profile_options.go:31–55` はloaderとFetchを独立に選ぶ。既存FileLoaderをHTTP対応へ変えず、別factoryを明示選択させる提案はこの契約を維持できる。仮Go APIも未実装と表示されている。 |
| CLIとWorker入口 | §3、§6.4、§11 | CLIは `internal/cli/cli.go:77–91` で入口をローカルファイルとして読む。一方 `internal/jsexec/execution_loader.go:180–198` のWorker入口準備は汎用loaderを呼ぶ。`internal/stdlib/workerscript.go:60–68` では文字列とURLオブジェクトで入口処理が異なるため、JS側の文字列検査だけに頼らず準備側でURL入口を除外する提案は適切。依存のURL importまで禁止する案にはなっていない。 |
| Worker・親子agentへの設定継承 | §6.3、§9 | `internal/jsexec/profile.go:130–143` はWorkerに親Profileを適用する。`agent/host.go:18–25,45–66` はRuntimeをsnapshotし、`internal/agentexec/executor.go:48–92` は各実行を新しいruntimeで行う。Host設定を共有しsource/module状態を共有しない方針はこの構造と合う。親agentの権限昇格APIも追加していない。 |
| 同期取得と取消し | §7.1、§8、§11 | loaderは `internal/jsexec/execution_loader.go:139–164` で実行contextを用い、`internal/jsexec/runtime.go:53,103–112` のCancelは別goroutineからcontextを取り消せる。Workerも `internal/jsexec/profile.go:94–107` でloopの終了をcontextへ接続する。同期HTTPへcontextを渡す案には現行構造上の接続点がある。ただし本文読取りまで中断できることはコード読解だけで実証しておらず、正本のfocused fixtureを必要な残件とする。 |

能力通知についても、現行の `LoaderFactory` が関数型であること（`internal/jsexec/execution_loader.go:36–37`）を踏まえ、§9でmetadataの接続を未決としているのは正確。全APIカタログや指示文を能力の根拠にせず、標準factoryとcustom loaderを区別する方針に矛盾はない。

## 残件：今回の不備ではなく、実装前に必要な判断・確認

| 残件 | 今回の設計成果としての評価 | 実装前に必要なこと |
| --- | --- | --- |
| MIME許容範囲（§7.2、§12） | 利用者未選択と明記されており十分。拡張子不問をtext/plain許可へ読み替えていない。 | text/plain・ヘッダーなし等を許すか選択し、JS MIME集合とcharset/BOMの処理を固定する。 |
| 能力metadata（§9、§12） | 現行factoryから推測不能という境界が明記されており十分。 | Hostへの互換的な情報経路を決める。有効・無効・不明を実設定と一致させ、custom loaderを一律に有効としない。全API設計の完成は今回の完了条件ではない。 |
| URL正規化・redirect・fragment・同一性（§5.2、§11–12） | 提案と検証前の保証を区別しており十分。 | 実際のID規則を固定し、固定版go-quickjsで元URL・最終URL・alias・循環のnamespaceと評価回数が成立するか、小さなfixtureで確認する。 |
| 同期HTTPの取消し（§8、§11–12） | JS timer非進行とGo側取消しの必要性を明記しており十分。 | ヘッダー待ち・本文待ちからHost/task/Runtime取消しで実行が戻ることを確認する。別runtimeの独立性も実装の前提として維持する。 |
| 実際の公開ESM利用（§1、§11） | CDN例を未検証の利用例と表示しており、現状の説明は正確。 | 実装段階で選定した公開ESMのexportを一件利用し、目的の利用経路が成立することを確認する。今回は接続しない。 |

上記の未決・確認対象を閉じる前に、実装可能性や公開ESM対応を保証済みとはしない。反対に、これらを今回の文書レビュー中に実装・外部接続・全体テストで解消することも求めない。

## レビュー範囲と実施上の制約

- `AGENTS.md`、正本、上表の関連現行コードと現行run_js / Worker説明を参照した。
- 他レビュアーの結果は読まず、独立に判断した。
- 製品コード、設計案、索引は変更していない。作成・更新したのは本ファイルのみ。
- 外部サービスへの接続、テスト実行、追加委譲は行っていない。したがって実行による成立確認は未実施であり、本レビューは静的な設計レビューである。
