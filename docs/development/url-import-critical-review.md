# HTTP/HTTPS URL import設計案：独立した批判的レビュー

## 結論

**設計検討の成果として概ね妥当。重大・致命的な設計矛盾は見つからなかった。**
ただし、Worker入口制限とURL targetの交点（C1）およびremoteからの明示的file URLの解決順序（C2）は、実装前に短く明文化したい。

本書は製品実装の承認・公開CDNでの動作保証ではない。MIME、能力metadata、URL正規化・redirect/fragment、取消しの実証を未決として残したこと自体は、今回の設計検討の不足とは判定しない。正本は、その区別と実装前の作業を既に明示している。

## 範囲と方法

- 正本：`docs/development/url-import-plan.md`（303行の版）。
- `AGENTS.md`を参照。他レビュアーの結果は読んでいない。
- resolver/source保持、公開Go loader契約、execution/Worker入口、取消し、Host Profile適用とfacts生成の関連コードを静的に確認した。
- engineの根拠は`go.mod`で固定された`github.com/go-quickjs/go-quickjs v0.29.0`。ローカルmodule cacheの同版ソースでも確認し、別版の参照checkoutだけで判断しなかった。
- 製品コード・設計案・索引は変更していない。外部接続、製品実行・テスト、追加委譲は行っていない。

## 指摘

### C1 — 中：Worker入口の拒否対象にURL map targetを含め、取得前の境界を明示する

**対象**：§3、§5.3、§6.4（193行）、§11のWorker確認（284行）。

**現行コード根拠**：

- `internal/jsexec/execution_loader.go:180–196`の`prepareWorker`は、非eval入口を通常の`loadModule(specifier, referrer)`へ渡す。入口専用の純粋な解決段階は現在ない。
- `internal/modulemap/snapshot.go:110–117`の`Resolve`は、完全一致mapのtargetを最初に返す。従って入口文字列と読取り先の種類は同じとは限らない。
- `stdlib/loader.go:26–48`ではmapの解決とsource読取りが同じloaderの内部にある。`loadModule`の戻り値を見てから拒否すると、読取り後になる。

**具体的な経路と影響**：

```json
{"imports":{"worker-entry":"https://modules.example/worker.mjs"}}
```

```js
new Worker("worker-entry", {type: "module"});
```

§6.4は汎用loaderへのHTTP追加によるURL入口の偶発的有効化を正しく認識している。ただし、継承されるmapのURL targetも「URL入口」に含むか、どこで拒否するかは書かれていない。raw specifierのHTTP/HTTPS判定だけなら上記経路を防げず、resolved IDの事後判定だけなら既にHTTP接続してしまう。これは公開ESMへの依存importとは別であり、「URLをWorker入口にしない」という合意との境界である。

**最小修正案**：

§6.4へ「URL targetを持つalias経由もURL入口として扱い、入口用のHTTP取得より前に拒否する」と補足する。local targetの入口aliasを引き続き認めるのか、入口の指定形式をpath/file URLに限定するのかも併記する。通常の依存importへこの入口制限を適用しない。

新しい権限機構は不要。§11の既存Worker確認にこのalias経路を含めればよく、別の広いテスト計画は不要である。これは未実装コードの不具合断定ではなく、既に掲げた境界の具体化要求である。

### C2 — 低：remote referrerからの明示的file URLの扱いを解決順序へ反映する

**対象**：§5.1の手順5–6（99–100行）と§10（255行）。

**現行コード根拠**：

- `internal/modulemap/snapshot.go:122–129`はfile URLをreferrerの種類によらずローカルIDへ解決する。
- `stdlib/loader.go:42–47`が実際の読取りを選択済み`origin.FileSystem`へ限定している。URL構文の解決自体がFS権限の付与ではない。

**具体的な経路と影響**：

選択FSが提供するローカルhelperを、remote moduleが`import "file:///workspace/helper.mjs"`する場合。§10はoriginだけでこれを新たに禁止せず、ProfileのFS設定へ従うとしている。一方、§5.1ではfile URLの現行規則を「ローカルreferrerから」に限定し、その後に「その他schemeは未対応」と置いている。解決順序だけを実装すると、FSを明示許可したホストでもremoteからのfile URLがschemeエラーになる読み方が残る。

**最小修正案**：

§5.1に「明示的file URLはreferrerによらず既存のローカル解決へ渡し、読取り権限は選択FSに従う」と例外を明記する。remoteからの`/dep.js`はHTTP originのpathである、という既存提案は変えない。FSなしのホストへファイル権限を増やす修正ではない。

## 重点確認の結果：追加の指摘にしなかった事項

### 公開ESM CDNのURL解決・redirect・依存graph

- origin絶対path、scheme-relative参照、query付きURL、bare依存と完全一致mapの区別は、CDNのESM graphを扱うために必要な経路を押さえている。npm探索へのfallbackを要求する根拠はない。
- 固定engineの`internal/vm/module.go:350–398`では、同一requestは`requested[referrer,specifier]`から返し、新しいrequestではloaderを呼んだ後、戻されたresolved IDで既存moduleを探す。この順序は、最終URLをIDとして返す提案と整合する。返したIDはmodule内の依存のreferrerにもなる。
- 従ってredirect統合は原理的に不可能とは読めない。ただし、この静的確認だけで循環・fragmentを含む実際の統合を保証したことにはならない。§5.2・§11のfocused fixtureと規則確定は適切な残件である。
- 公開CDNで実際に返されるgraphやexport利用は未確認。正本も例示を実接続の証拠にしておらず、実装時の一件の利用確認を残している。この依頼で全面互換調査を追加する必要はない。

### 「キャッシュなし」の意味

- `internal/modulemap/loader.go:10–22,61–87`のruntime-local source保持は、新しいreferrerからengineがloaderを再度呼ぶ場合にも必要になる。これをダウンロードキャッシュ禁止に含めて除去すると、同一runtime内の本文一貫性が崩れうる。
- 正本はこの保持とruntime間・プロセス間のダウンロード再利用を明確に分けている。factoryを再利用してもruntimeごとにsource状態を作り直す§6.2の方針も整合する。
- 未知のredirect先を初めて知るための要求と、既知canonical IDのsource再利用は区別して実装する必要があるが、それを理由に永続キャッシュやlockを追加する必要はない。

### 同期loaderと取消し

- `internal/jsexec/runtime.go:48–58,108–112`の`Runtime.Cancel`はGo contextを直接取り消す。取消しのためにruntime owner上のJS jobを実行する必要はない。
- `internal/jsexec/profile.go:97–109`でfactoryへ渡るcontextにつながり、`execution_loader.go:128–133`でfactoryがそのcontextを受ける。`internal/agentexec/executor.go:88`も実行contextを`stdlib.Execute`へ渡す。
- Workerも`internal/stdlib/worker_runtime.go:87–99`で取消可能なloopを作ってから入口を準備し、`worker.go:359–362`のterminateがcontextを止める。同期HTTP中の取消しに使えるGo側経路がある。
- これはHTTP実装の取消し実証ではない。要求とbody読取りへcontextを接続すること、ヘッダー待ち・body待ちから戻ることは§7–8・§11どおり実装時に確認すべきである。JS timerの非進行を明記する案も妥当。engine非同期loaderへの変更を今回必須にする理由はない。

### Goホストの権限と能力通知

- `stdlib.FileLoader`は選択FSだけを使い、`FetchOptions`からHTTP importを許す処理はない。別factoryで明示有効化し、FS・fetchとは独立とする案は現行契約に合う。
- `agent/host.go:42–62`がRuntimeをsnapshotし、executorへ渡す。親agentのspawn引数によるホスト権限昇格APIは不要であり、案も追加していない。
- `internal/agent/host.go:92–94`のfactsは現状では汎用`Facts()`のみで、公開`LoaderFactory`も関数型である。標準factoryを選ぶだけでHostへ能力が自動伝播する既存経路はない。
- 正本§9はその不整合を未決として明示し、custom loaderを不明と扱い、秘密を通知せず、指示文による毎回の手動補完も成立条件にしないとしている。この認識は正しい。metadataの形まで今回代行設計する必要はないが、能力通知の実装完了を主張する前には接続APIが必要である。

## 残件と終了判断

今回の文書レビューとしては、上記指摘・根拠・最小修正案の記録で終了する。正本への採否・反映は編集権限を持つ担当へ委ねる。

実装前・実装時に必要な残件は、主として正本に既記載の次の事項である。

1. 利用者判断：MIMEの範囲。JavaScript MIME基本案を合意済みとはしない。
2. 技術判断：能力metadataの互換接続、URL正規化・redirect/fragment・最終IDの規則、仮Go APIの確定。
3. 実行証拠：固定engine上の同一性・循環、同期HTTP取得中の取消し、選定公開ESMのexport利用。現段階は未検証であり、今回の既存不具合としては扱わない。
4. 本レビューの追加明文化：C1の入口alias境界、C2の明示的file URL例外。

ダウンロードキャッシュ・hash/lock・新しい権限昇格API・全面互換・大量反復を追加する要求はない。

## 追記：C1/C2反映後の限定再確認

対象は正本の§5.1、§6.4、§11におけるC1/C2への対応のみ。通常レビュー結果は読まず、製品コードの追加調査・編集、正本編集、外部接続、テスト、追加委譲は行っていない。初回記録は変更せず、以下で指摘の状態を更新する。

- **C1：対応済み（文書設計上クローズ）。** §6.4は直接URLとURL target aliasを入口用HTTP取得前に拒否し、純粋な解決と読取りの間で判定する構成を明記した。global Workerの既存local target aliasは維持し、node Workerの入力形式は拡張しない。制限を入口の一回の読取りへ限定し、依存のURL importとeval入口は維持する。custom LoaderFactory内部のI/Oを事後に抑止できる保証とはせず、ホスト責任とした点も境界として整合する。§11のWorker確認にもalias経由の接続前拒否とlocal alias維持が反映されており、指摘した経路への対応として十分。
- **C2：対応済み（文書設計上クローズ）。** §5.1の手順5は、明示的file URLをreferrerがローカルでもremoteでも既存のローカル解決へ渡し、実際の読取りは選択FSに従うと明記した。remoteからの `/dep.js` をHTTP originのpathとして扱う手順4とも区別でき、初回指摘の曖昧さは解消した。

**結論：今回の2件に追加の文書修正要求・未解決事項はない。** 上記の「対応済み」は設計記述への判定であり、製品の実装・動作確認済みを意味しない。実装時の確認は正本の既存計画に従うもので、今回の再確認の終了条件には加えない。
