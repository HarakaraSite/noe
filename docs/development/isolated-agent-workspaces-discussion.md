# 長時間agent作業用の隔離環境 — Docker・microVM・外部ライブラリの検討

[機能候補メモ](../future-candidates.md#microvm) · [開発ガイド](README.md)

利用者との議論とWeb調査をまとめた検討記録。Goへ組み込めるmicroVMの調査から始まり、長時間の仕事を別環境で動かし、ファイルをVCS経由で届ける構成へ整理した。特定ランタイムの採用、製品API、実装時期、優先順位を決定した文書ではない。

公式README・設計資料の確認と、vm201内の読取り専用の環境確認を行った。VM/コンテナの起動、ツールの導入、権限変更、Go SDKのビルド、NOEとの接続、性能・隔離性の実測は行っていない。外部プロジェクトの開発ブランチの情報を含むため、実装に進む際は対応版・公開APIを再確認する。

## 1. 利用者が示した用途・希望

- 大きく長い仕事は、NOEから起動した別の作業環境内でagentに任せたい。小さな修正は今までどおりNOE側の環境で行う。
- 持ち帰りたい成果は**ファイル**。agentの最終応答は捨ててもよい。回答の保存や転送を成果配送の必須条件にしない。
- 成果はVCS（議論ではGitを想定）のremote経由で届ける。送信先はNOE側ホストのリポジトリの場合も、別サーバのForgejo/GitHubの場合もある。
- 現在は物理ホストharakara02上のvm201でNOEを使っている。その中から作業環境を起動できるとよい。
- KVMやmicroVMにはこだわらない。QEMU、LXC、Docker等も含め、軽量でこの環境内から使えるものを考える。
- Dockerfileでよく使うツール一式を渡せること、VS Codeから作業環境を覗けることを利点として評価した。
- GoへVMM自体を直接組み込むより、外部の環境管理ツールをNOEから呼び、`dockerlib`・`vmlib`のようなライブラリで扱いやすくする案へ議論が進んだ。名前・実装方法・正式APIは未決定。

以下の具体的な責務分担、寿命、候補の絞り方は、この用途を基にした提案であり、実装承認とは区別する。

## 2. 作業と成果配送の全体像

```text
harakara02（物理ホスト）
  └─ vm201（NOE側ホスト）
       └─ NOE：仕事の起動・観察・追加指示・停止
            └─ 作業環境（コンテナまたはVM）
                 └─ NOE / agent
                      ├─ リポジトリ取得
                      ├─ ファイル作成・編集、コマンド・テスト実行
                      └─ commit / push
                           ├─ vm201上のリポジトリ
                           └─ Forgejo / GitHub等の別サーバ
```

「ホスト」は原則としてNOEが動くvm201を指し、物理ホストharakara02と区別する。作業をホストで実行したままVMを追加しても、その作業は隔離されない。対象のagent・JS・シェル・外部コマンドを作業環境側で実行する。

### 成果はファイル、配送先はVCS

取得元と送信先を別の指定として考える。元リポジトリの開始コミットから作業し、同じremoteの作業ブランチ、別のリポジトリ、ホスト上の送信先等へ成果を届ける。ホストの生の作業ディレクトリを直接共有する必要はない。

仕事に渡す情報は、少なくとも「指示」「取得元と開始点」「送信先とブランチ等」「使用する環境」を想定する。agentの最終応答を捨てても、実行状態と送信先ref/コミット等の所在情報はNOE側から確認できるとよい。必要な説明・調査報告・検証結果はファイルやコミットへ残せるが、毎回の報告書作成は必須にしない。

- **配送完了**：必要なファイルがコミットされ、指定先へのpushが成功したこと。「完了しました」という応答だけでは判定しない。
- **内容の成立**：依頼を満たす変更・必要な検証ができたかは別の判断。push成功だけで内容の正しさを保証しない。
- **取り込み**：配送と本流へのマージを分ける。ホストのfetch、別agentによる継続、PR、自動マージ等を選べる。人間の承認やPRを常に必須にはしない。

Gitへ記録していないファイルは、この配送方式では届かない。push失敗時は「作業済み・未配送」として扱い、再送や環境保持の方針を決める。外部VCSへ届けば、ホストへ同じファイルを再ダウンロードすることは必須ではない。

### 作業環境の寿命と観察

基本フローは「環境起動 → 取得 → 作業・検証 → commit/push → 配送確認 → 環境削除」。ただし、task完了と環境削除は分け、追加依頼や調査のため残す使い方も考える。会話の継続とファイル・インストール済み依存等の継続が食い違わないようにする。

長い仕事はIDを返して後から観察・追加指示・停止できると自然。既存の観察・steer・取消しに近い操作感を目指すが、現在の公開APIへVM/コンテナ操作が追加済みという意味ではない。

現在の子agentは親taskに所有される。`keepAlive:true`は起動元終了後の独立実行を意味しない。作業環境の存続と、その中のagentの独立実行は別の契約であり、起動元task終了やNOE終了時に仕事をどう扱うかは未決定。[現行のagent利用ガイド](../agents.md)・[API](../api.md)を参照する。

親子agentは一つの作業環境を共有する案をまず考えた。子ごとの別環境や成果の統合は別の拡張で、既定方式としては未決定。

## 3. Dockerfileと人間の途中参加

DockerfileはNOE、Git、SSHクライアント、言語処理系、ビルド・テスト・調査ツール等の「道具一式」を定義する。仕事の指示・取得元・送信先は別の実行設定にし、認証情報はイメージへ焼き込まず、必要なものを実行時に渡す構成を考える。プロジェクト固有の環境は派生イメージとして用意できる。

VS Codeは「Remote - SSHでvm201へ接続 → Dev Containersで実行中の作業コンテナへ接続」という利用を想定する。ファイル閲覧、ターミナル、テスト再実行、必要なら編集を行える。実際の接続は未確認で、VS Codeからvm201のDockerへアクセスできる設定が必要。agentへDockerソケットを渡すことは前提にしない。

`.devcontainer/devcontainer.json`による拡張機能・接続ユーザー・作業ディレクトリ等の設定共有も選択肢だが、NOE側の実装やagent実行の必須条件にはしない。agentと人間が同じファイルを同時編集する場合の調整は運用上の論点。

「使い捨てだが途中から人間も入れる仕事場」として、Dockerfileによる再利用と可視性を重視する。短い起動時間だけをDocker選択の根拠にしない。

## 4. NOE本体・外部ライブラリ・仕事スクリプトの分担案

```text
NOE上のJS / agent
  └─ 仕事のスクリプト（必要になれば仕事管理ライブラリ）
       ├─ dockerlib → Docker / Podman
       └─ vmlib等  → VMランタイム
                         └─ 作業環境内のNOE / agent
```

- **NOE本体**：既存のプロセス実行・入出力等を提供する。新しいVMMやGo SDKの組込みを最初から前提にしない。
- **環境操作ライブラリ**：imageのbuild/pull、環境のcreate/start/stop/remove、exec、inspect/list/logs、IDからの再接続等を扱う。Gitやagentの仕事内容を抱え込まない。
- **仕事のJS**：取得、指示、agent起動、観察、VCS配送確認、環境の保持/削除を組み立てる。最初は通常のスクリプトでよく、専用ライブラリ化は必須ではない。

まずは既存CLIを引数配列で呼び、状態はJSON形式で取得する薄い外部JSラッパーを考える。Docker Engine API直結やGo実装は別の選択肢。これらは未実装であり、`dockerlib`・`vmlib`という公開moduleが存在するわけではない。

`run_js`は呼出しごとに新しいruntimeなので、前のJSオブジェクトの存続に依存せず、環境ID・名前で後から取得できるAPIが必要になる。環境管理ツールが維持する環境と、一回のCLI呼出し・JS runtimeの寿命を分ける。

DockerとVMを最初から万能な共通APIへ押し込まない。Dockerの仕事を具体化し、別の実行先が必要になったらその操作を用意し、実利用で共通していた部分だけを仕事側で揃える案とする。

## 5. ランタイム候補と調査結果

当初の「Goへの直接組込み」から、既存ツール経由も含む「長い仕事用の環境起動」へ評価軸を広げた。長時間の仕事では、起動時間だけでなくNOE/Git/開発ツールの利用、ビルド・テスト互換性、実行中の資源負担、観察・停止・継続、vm201への導入を重視する。

### コンテナと追加候補

| 候補・一次資料 | 今回の用途との関係 | 条件・未確認事項 |
| --- | --- | --- |
| Docker / Podman | Dockerfile/OCIイメージから仕事ごとの環境を起動する基本候補。外部CLI経由でも利用できる | vm201のカーネルを共有する。Podmanのrootless運用等には環境設定が必要。今回NOEとの実行確認はしていない |
| LXC / Incus | OS一式に近い環境、複数サービスやsystemdを使う仕事向けの候補 | 同じくホストカーネルを共有。実際の管理・接続方法は未調査 |
| QEMU＋TCG | KVMなしで独立カーネルのVMを実行できる候補 | CPU処理の負担が大きくなりやすく、長いビルド・テストでの性能は未測定。QEMU＋KVMとは区別する |
| [Podman＋krun](https://github.com/containers/crun/blob/main/krun.1.md) | crunのkrunモードがlibkrunを呼び、OCIワークロードをmicroVM内で動かす。コンテナに近い操作感でmicroVMを比較できる | Linux/KVM。libkrun対応crun等が必要で、通常のPodman導入だけで使えるとは限らない。Debianへの導入・対応版は未確認 |
| [BoxLite](https://github.com/boxlite-ai/boxlite) | OCIイメージ、コマンド実行と出力、状態保持、独立した環境の存続、CLI/SDK/RESTを提供するagent向けmicroVMランタイム | Linux/KVM、macOS/HVF。[Go SDK](https://docs.boxlite.ai/reference/go)はCGO経由。CLIならNOEへの直接リンクは不要。長時間運用やNOE接続は未検証 |
| [gVisor / runsc](https://gvisor.dev/docs/user_guide/platforms/) | KVM不要のsystrap方式もあり、公式がVM内の選択肢として案内する。通常コンテナとmicroVMの間で比較する候補 | microVMとは別物。[Linux機能の互換性制約](https://gvisor.dev/docs/user_guide/compatibility/)があり、必要なビルド・テストとの相性は未確認 |

Docker/PodmanやLXCを通常のLinux VM内で動かすことは、ハードウェアのnested virtualizationを要求しない。vm201内でDockerを使うだけならDocker-in-Dockerでもない。agent自身にさらにコンテナを起動させる場合は別に考える。

一方、libkrun・Firecracker・BoxLite等のLinux上のmicroVMは基本的にKVMが必要。「microVMだからnested virtualizationを回避できる」という意味ではない。

### 「組込み」の区別と候補

VMM自体を同じGoプロセスへリンクする方式と、Goライブラリから別プロセスのVMMを起動・制御する方式を分ける。後者もGoアプリへ統合できるが、単一プロセス・依存なしの配布になるわけではない。

| 候補・一次資料 | Goとの接続 | 環境・依存 | 調査時点の判断 |
| --- | --- | --- | --- |
| [libkrun](https://github.com/libkrun/libkrun) ＋ [libkrun-go](https://github.com/mishushakov/libkrun-go) | C APIをCGO経由で呼ぶ | Linux/KVM、macOS Apple Silicon/HVF。libkrun共有ライブラリ等が必要 | ライブラリとしての直接組込み候補。ただし既存Goバインディングの旧APIと本体2.0開発版を区別する |
| [stacklok/go-microvm](https://github.com/stacklok/go-microvm) | Go APIから専用runnerを起動し、runnerがlibkrunを呼ぶ | Linux、macOS Apple Silicon。親ライブラリはCGO不要、runnerはCGOとlibkrunが必要 | OCI取得・rootfs展開/キャッシュ・ポート転送・virtio-fs・VM寿命管理をまとめて扱える。公式に実験段階・本番非推奨 |
| [Firecracker](https://github.com/firecracker-microvm/firecracker) ＋ [Go SDK](https://github.com/firecracker-microvm/firecracker-go-sdk) | 外部FirecrackerプロセスをAPIで制御 | Linux/KVM。VMMバイナリ、ゲストカーネル、rootfsが必要 | Linuxで本番運用を重視する場合の有力候補。AWSでの本番利用実績はVMM本体のもの。SDKは未対応機能があり、必要機能と版の対応確認が必要 |
| [gokvm](https://github.com/bobuhiro11/gokvm) | Pure GoのVMM・KVMラッパー | Linux/x86-64、`/dev/kvm` | Linux起動、virtio-net/blk等に対応。自作・学習の参考になるが、公式に実験段階・本番利用非推奨 |
| [gocracker](https://github.com/misaelzapata/gocracker) | Pure Go実装。`pkg/vmm`・`pkg/container`を公開 | Linux/KVM、x86-64・ARM64 | OCI、snapshot/restore等を掲げる追加候補。安定した組込みAPIや本番実績は今回確認できていない。機能・性能の公称値を実測結果とは扱わない |

[Hyperlight](https://github.com/hyperlight-dev/hyperlight)もアプリ組込み型VMMだが、公式の中心はRustで、通常のLinux環境ではなく専用ゲストを動かす方式。既存NOEや任意のシェルコマンドをそのまま隔離する用途の第一候補にはしない。今回、Goホスト向けの公式組込み経路は確認できなかった。

### libkrunのライフサイクルと版の注意

既存のlibkrun-goが公開する`CreateContext()`・`StartEnter()`と、libkrun本体の2.0開発版は同じ契約ではない。

- **旧API**：`krun_start_enter()`は成功すると呼出し元へ戻らず、VM終了時にプロセスも終了する。[go-microvmの設計資料](https://github.com/stacklok/go-microvm/blob/main/docs/ARCHITECTURE.md#the-two-process-model)では、親アプリを維持するため専用子プロセスへ分離している。単にgoroutineから呼べば通常のGoアプリと共存できるとは扱わない。
- **2.0開発版**：本体`main`は1.xとAPI/ABI非互換で、安定リリース前の変更もあり得る。[VM Lifecycle](https://github.com/libkrun/libkrun/blob/main/docs/vm-lifecycle.md)は`VmmBuilder`・`Vmm`・`VmmHandle`を説明し、`run()`はVM終了までブロックする。既存Goバインディングがそのまま対応する根拠にはならない。本体は本番向けソース利用に最新の`stable-*`ブランチを案内している。

採用検討時は本体とGoラッパーの対応版を固定し、呼出し元プロセスの存続、停止・取消し、VM終了の回収を確認する。「libkrunは常にプロセスを乗っ取る」と新APIまで一括りにしない。

### 用途が固まった後の候補の見直し

Firecracker＋Go SDKは実現の土台として有力だが、カーネル・rootfs・ネットワーク等の管理範囲が広い。今回の用途なら、まず通常のDocker/Podmanと、OCI作業環境を扱うlibkrun系（go-microvm、Podman＋krun）、BoxLite等の上位ランタイムを比較する案になった。Hyperlightは専用ゲスト中心で今回の用途とは異なり、gokvmは自作・学習寄り。gocrackerの安定性を判断する材料は不足している。

これは採用順位の確定ではない。Goへ直接リンクできることやPure Goであることは必須要件ではなく、外部CLI等に環境管理を任せる選択肢を残す。

VMとコンテナの隔離を同一視しない。またVMでも、共有したホストパスやネットワークが自動で制限されるわけではない。[libkrunのSecurity Model](https://github.com/libkrun/libkrun/blob/main/docs/security-model.md)や[Firecrackerの本番ホスト設定](https://github.com/firecracker-microvm/firecracker/blob/main/docs/prod-host-setup.md)を参照し、用途に必要な公開範囲とVMMのホスト権限を選ぶ。ホスト全体・Dockerソケット・全認証情報をagentへ渡すことは前提にしない。

## 6. vm201で確認したこと

議論中、NOE実行環境から以下を読取り専用で確認した。**確認時点の記録であり、現在の設定を保証しない。**

| 項目 | 観測結果 |
| --- | --- |
| ホスト名・アーキテクチャ | vm201、x86_64 |
| カーネル | 6.12.111+deb13-amd64 |
| CPU仮想化フラグ | Intel VMX（`vmx`）が見える |
| KVMデバイス | `/dev/kvm`が存在 |
| vm201内のnestedパラメータ | `kvm_intel`は`Y` |
| 実行ユーザー | `agent`。実行プロセスのグループは`agent`・`sudo`で、`kvm`なし |
| デバイス権限 | `root:kvm`、`crw-rw----` |
| デバイスopen | Permission denied。KVM APIバージョン取得には進めなかった |

KVMの前提が一部見えているが、harakara02の設定確認や子VMの起動成功の証拠ではない。権限の追加・設定変更は行っていない。通常コンテナ方式を選ぶ場合、このKVM権限は前提にしない。

子VMの資源もvm201の割当範囲で使う。nested環境でのCPU/I/O性能、メモリ・ディスク消費、同時実行数は未測定。

## 7. 未決定事項と記録の範囲

- 最初に試すランタイム、作業イメージの内容・配布方法、CLI/SDKの選択。
- 仕事の設定形式、公開API・ライブラリ名、観察・追加指示との接続。
- task完了、起動元終了、NOE終了、取消し時のagentと環境の寿命、再接続方法。
- VCSの開始点・送信先指定、並行した仕事のブランチ運用、push失敗時の再送と環境保持、取り込み方法。
- モデルAPIとGitの認証情報の置き場所・渡し方、必要なネットワーク・共有パス。
- 実際のNOE・Git・ビルド/テスト・VS Code接続が対象環境で成立するか。

この依頼は議論の文書化であり、NOE本体のsandbox化、既定のローカル実行の変更、新しい自動承認・権限制御、ランタイムの導入、VM/コンテナ起動、外部ライブラリ実装、リリースを承認したものではない。
