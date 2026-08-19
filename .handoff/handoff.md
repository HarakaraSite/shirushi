## 2026-07-23 20:39 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: v0.5.4 リリース

### 実施したこと
- 50/100/200件のページサイズ切り替えと設定保存、一覧上下のページネーションを追加した。
- 起動メッセージのURL表示を修正し、README・API・機能一覧・ロードマップを現行実装へ更新した。
- Goテスト（coverage 57.7%）、vet、JavaScript構文、4対象のCGO無効ビルド、差分チェックに成功した。

### 次のタスク候補
- v0.5.4タグpush後のForgejo Actionsと4バイナリのRelease掲載を確認する。

### 連絡・注意事項
- 既存release.ymlはv0.5.3時点と同一で、FORGE_URLとRELEASE_TOKENを使うため変更していない。
- メタデータ画像のtwitter:image/faviconフォールバック案は効果と視認性を検討して中止した。

## 2026-07-25 19:30 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: README・API リファレンスの英日二言語化

### 実施したこと
- `README.md` と `docs/api.md` を英語版にし、日本語版を `README.ja.md` と `docs/api.ja.md` に分離した。
- 各 README と API リファレンスに相互リンクを追加し、言語に対応する API 文書へリンクした。

### 次のタスク候補
- v1.0.0 リリース時に、配布先で README の表示と言語リンクを確認する。

### 連絡・注意事項
- API 仕様は変更していない。英日ともに 18 エンドポイントを記載し、`git diff --check` は成功した。

## 2026-07-26 17:28 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP 実装計画

### 実施したこと
- concept revision 7 の採用済み要件を維持した実装計画を `docs/henji-summary-mvp-implementation-plan.md` に保存した。
- 抽出本文40,000 Unicode文字と、選定モデルの実効 `max-input-chars` から算出するHenji投入上限を明文化した。

### 次のタスク候補
- 代表ページの本文抽出と候補モデル比較を行い、利用者が `API / model ID / L_selected` を選択する。

### 連絡・注意事項
- 本文抽出規則または入力上限が代表ケースで成立しない場合は、要件を勝手に変えずconcept reviewへ戻す。

## 2026-07-26 17:40 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP 増分1

### 実施したこと
- 既存SSRF防御を保つHTML取得共通ヘルパーと、静的HTMLから本文候補を抽出する `summary.go` を追加した。
- README優先、除外領域、本文量閾値、Unicode上限の単体テストを追加し、CGO無効テストとvetに成功した。

### 次のタスク候補
- 代表URLで本文抽出のlive確認を行い、Henji候補の実効 `max-input-chars` を調査する。

### 連絡・注意事項
- Henji起動、API、UI、DB更新は未実装。増分1の確認後にのみ進める。

## 2026-07-26 17:42 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP live確認と入力上限調査

### 実施したこと
- Fil-C、Senpai、YAMA HACKで本文候補が成立し、Sakana AIでは本文不足となることをliveテストで確認した。
- Henji v2.1.7と候補3モデルの個別 `max-input-chars` を、秘密情報を表示せず実行環境の設定から確認した。

### 次のタスク候補
- 利用者の承認後、固定prompt・provider別schema・framingから `L_selected` を算出し、候補モデルを比較する。

### 連絡・注意事項
- provider/modelは未選定であり、Henji起動、API、UI、DB更新は未実装のままにしている。

## 2026-07-26 17:47 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP 入力予算

### 実施したこと
- Henji v2.1.7の`max-input-chars`がUTF-8バイト数であることに合わせ、固定prompt・provider別Schema・framing・安全余裕を含む投入予算を実装した。
- Google、OpenRouter、OpenAIの候補3件が、最悪4 bytes/Unicode文字の見積りでも40,000 Unicode文字を投入できることをテストした。

### 次のタスク候補
- 費用を伴う比較の候補3件・最大27回のHenji呼出しを利用者が承認した後、合成本文と代表3件を比較する。

### 連絡・注意事項
- provider/modelは未選定。Henjiへの実際のモデル呼出しは0回で、Henji起動、API、UI、DB更新も未実装。

## 2026-07-26 17:56 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP 第1段階モデル比較

### 実施したこと
- GoogleとOpenRouterは合成本文・Fil-C・Senpai・YAMA HACKの各1回、合計8成功結果を記録した。
- OpenAI候補はstrict schemaを公式仕様に合わせて縮小後も合成本文で2回連続失敗し、今回の比較対象から外した。

### 次のタスク候補
- 利用者がGoogleまたはOpenRouterを選ぶ、または失敗試行を含む最大28回の追加比較を明示承認した後、追加試行と選定候補の最終確認を行う。

### 連絡・注意事項
- provider/modelは未選定。モデル呼出しは成功8回とOpenAI失敗2回。Henji起動、API、UI、DB更新は未実装。

## 2026-07-26 18:03 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP OpenRouter Gemini比較

### 実施したこと
- HenjiのOpenRouter設定へ `google/gemini-2.5-flash-lite` を追加し、`--list-models --output json`で認識を確認した。
- 合成本文・Fil-C・Senpai・YAMA HACKの4件で比較し、4/4成功、約0.96〜1.32秒だった。

### 次のタスク候補
- 利用者がGoogle直結、OpenRouter DeepSeek、OpenRouter Gemini 2.5 Flash-LiteからMVP採用モデルを選ぶ。

### 連絡・注意事項
- provider/modelは未選定。Henji起動、API、UI、DB更新は未実装。

## 2026-07-26 18:14 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP 起動引数設定

### 実施したこと
- 本文要約の既定を `openrouter / google/gemini-2.5-flash-lite` とし、`--henji-api`と`--henji-model`を対で指定して上書きできる起動引数解析を追加した。
- 既定、対での上書き、片方だけの拒否をテストした。

### 次のタスク候補
- 選択済み起動設定を使うHenjiアダプターを実装し、stdin・JSON Schema・timeout・出力検証をテストする。

### 連絡・注意事項
- Henji起動、API、UI、DB更新は未実装。APIキーと設定画面はShirushiへ追加していない。

## 2026-07-26 18:24 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP アダプター

### 実施したこと
- PATHまたは`--henji-path`で選んだHenjiを、一度だけargv配列・stdin・一時Schemaで起動する内部アダプターを追加した。
- UTF-8バイト予算、stdout 16KiB、120秒timeout、JSONの`summary`だけ、5行・400文字を検証する単体テストを追加した。

### 次のタスク候補
- capability APIと非同期summary開始APIを追加し、最大3並行とExcerpt更新をテストする。

### 連絡・注意事項
- HenjiアダプターはまだHTTP APIから呼ばれない。UI、DB更新、非同期ジョブは未実装で、実Henji/providerもこの増分では呼んでいない。

## 2026-07-26 18:38 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP 非同期API

### 実施したこと
- 認証配下のcapability APIとsummary開始APIを追加し、202後にURL取得・Henji・Excerpt更新を行う非同期ジョブを実装した。
- Henji未導入時の204・ボタン非表示用capability、成功時のExcerpt更新、最大3並行をfakeでテストした。

### 次のタスク候補
- 編集モーダルへcapability確認と標準confirm付き「AIによる要約」ボタンを追加する。

### 連絡・注意事項
- UIは未実装。実Henji/providerをAPI経由で呼ぶ総合確認は未実施。ジョブ状態、ポーリング、再試行、再起動復元は追加していない。

## 2026-07-26 18:18 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP 実行ファイル指定

### 実施したこと
- Henji実行ファイルを固定パスにせず、既定でPATH上の `henji` を使い、`--henji-path PATH`で上書きできる起動設定に変更した。
- 既定値、API/modelの対指定、実行パスの上書きをテストした。

### 次のタスク候補
- 起動設定を使うHenjiアダプターを実装し、stdin・JSON Schema・timeout・出力検証をテストする。

### 連絡・注意事項
- Henji起動、API、UI、DB更新は未実装。APIキーと設定画面はShirushiへ追加していない。

## 2026-07-26 18:53 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP 編集モーダル

### 実施したこと
- 編集モーダルだけに、capability確認済みの場合だけ表示する「AIによる要約」ボタンを追加した。
- 標準`confirm()`の承認後にsummary開始APIを1回呼ぶだけとし、通知・ポーリング・再押下制御は追加していない。

### 次のタスク候補
- READMEと実装計画を最終仕様に合わせて更新し、実Henji/providerをAPI経由で呼ぶ総合確認を行う。

### 連絡・注意事項
- 新規登録時のURL blurは従来どおりOGP取得だけで、AI要約は開始しない。`CGO_ENABLED=0 go test ./...`、`go vet ./...`、`git diff --check`は成功。

## 2026-07-26 18:58 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP 文書・受入手順

### 実施したこと
- 日英README、日英API、E2E手順を、PATH探索・起動引数・capability・非同期上書き・未導入時無機能の実装契約に更新した。
- 実装計画の固定パス記述を`--henji-path`対応へ直し、選択済み既定modelを反映した。

### 次のタスク候補
- テスト専用Henji設定で、Fil-C成功・Sakana AI本文不足・UI確認を実Henji/provider経由で受入確認する。

### 連絡・注意事項
- 実Henji/providerをAPI経由ではまだ呼んでいない（外部provider利用料金が発生し得る）。`node --check`、`CGO_ENABLED=0 go test ./...`、`go vet ./...`、Linux静的build、`git diff --check`は成功。

## 2026-07-26 19:10 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP 実機受入確認

### 実施したこと
- `fil-c.org/pizlix`は静的HTMLから17,136文字・293ブロックを抽出し、実Henji呼出しとJSON検証で日本語要約を取得した。
- ローカルのsummary開始APIでも202後にExcerpt更新を確認した。`rephial.org`は対象selectorがなく、本文不足として抽出0となった。

### 次のタスク候補
- 実機UIでPizlixの要約後に一覧を手動再読み込みして表示を確認し、不要になったローカルテストサーバーを停止する。

### 連絡・注意事項
- 要約開始後に進捗表示・完了通知・自動再読込はない。実Henji受入テストは`SHIRUSHI_LIVE_HENJI_TEST=1`を明示したときだけ実行する。通常の`CGO_ENABLED=0 go test ./...`、`go vet ./...`、`git diff --check`は成功。

## 2026-07-26 19:27 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP script本文抽出

### 実施したこと
- script/style等のraw text内にある`</...>`をHTMLタグとして誤認しないよう、終了タグまでまとめて読み飛ばす処理を追加した。
- Odin公式サイトは修正後に9,347文字・234ブロックを抽出でき、疑似タグを含むfixtureの回帰テストも追加した。

### 次のタスク候補
- 再起動済みのローカル8181でOdinを登録し、実Henji要約と手動再読み込みによる表示を確認する。

### 連絡・注意事項
- 8181は修正済みバイナリ、8182はHenji未導入を模擬する旧バイナリで稼働中。Odinの実Henji呼出しはまだ行っていない。`CGO_ENABLED=0 go test ./...`、`go vet ./...`、`git diff --check`は成功。

## 2026-07-26 19:49 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Henji 本文要約 MVP カードUIと編集最新化

### 実施したこと
- AI要約ボタンを編集モーダルから保存済みbookmarkカードへ移し、編集モーダルだけを大きくした。
- `GET /api/bookmarks/{id}`を追加し、編集を開く直前にDBの最新Excerptを読み込むようにした。

### 次のタスク候補
- 8181でカード上のAI要約、要約後にページ再読み込みなしで編集を開いた際の最新Excerpt、8182でのボタン非表示を手動確認する。

### 連絡・注意事項
- 8181は修正済み、8182は旧バイナリのHenji未導入模擬。`GET /api/bookmarks/1`で要約済みExcerptをHTTP確認済み。`node --check`、`CGO_ENABLED=0 go test ./...`、`go vet ./...`、Linux静的build、`git diff --check`は成功。

## 2026-07-26 20:20 JST

- 実行エージェント: Codex
- モデル: GPT-5 Codex
- 作業トピック: Shirushi v1.0.0 リリース準備

### 実施したこと
- Henjiの正規リポジトリへのリンクをREADMEへ追加し、日英の専用設定・仕様文書と日英CHANGELOGのv1.0.0項目を作成した。
- CGOなし詳細テスト（coverage 61.9%）、race test、release workflowと同じLinux/macOS・amd64/arm64の4ビルドを成功させた。

### 次のタスク候補
- 文書差分を確認後、利用者の明示承認を得てv1.0.0をコミット、push、tag、Forgejo Actionsのrelease asset確認へ進む。

### 連絡・注意事項
- 現在は`main`、最新tagは`v0.5.4`。release workflowは`v*` tagで4バイナリを生成し、release notesは現在genericな`Release <tag>`。コミット、push、tag、公開、配布環境の更新は未実施。

## 2026-08-19 17:10 JST

- 実行エージェント: Codex
- 作業トピック: bookmark 404チェック機能
- 実施: `POST /api/bookmarks/check-404`を追加し、最大5並行のGETで最終応答が404のbookmarkだけ`/404.svg`へ更新する処理と単体テストを実装した。既存のSSRF対策と10秒timeoutを共用した。
- 次: ステップ2として、サムネイルに表示する`static/404.svg`を追加して確認する。
- 注意: UIボタンは未実装。404以外と通信失敗は画像を変更せず、404から復活したURLの元画像復元は今回の範囲外。

## 2026-08-19 17:12 JST

- 実行エージェント: Codex
- 作業トピック: bookmark 404チェック画像
- 実施: 横長カード用の`static/404.svg`を追加し、正しいSVGとしてバイナリへ埋め込まれることを単体テストで確認した。
- 次: ステップ3として、右上の404チェックボタン、実行確認、結果通知、一覧再読み込みを実装する。
- 注意: ローカル画像ビューアーはSVG非対応だったが、XML解析、埋め込み読出し、通常テスト、vet、Linux静的buildは成功。UIボタンは未実装。

## 2026-08-19 17:14 JST

- 実行エージェント: Codex
- 作業トピック: bookmark 404チェックUI
- 実施: 右上へ404チェックボタンを追加し、実行確認、処理中の二重送信防止、確認・404・通信失敗件数の通知、現在条件を維持した一覧再読み込みを実装した。30秒を超える一括確認でも結果を返せるよう、このAPIだけ書込み期限を解除した。
- 次: 利用者が必要なら、実データを使ったブラウザ手動確認または明示依頼によるE2E確認を行う。
- 注意: 404チェック機能の3ステップは完成。browser E2Eと実在する外部URLへの一括アクセスは未実施。元画像の自動復元は範囲外。

## 2026-08-19 17:21 JST

- 実行エージェント: Codex
- 作業トピック: bookmark 404チェック非同期化
- 実施: POST開始APIを即時202応答のbackgroundジョブへ変更し、GET状態API、進捗集計、実行中の409二重開始防止を追加した。UIは1秒間隔で状態確認し、再読込後も実行中ジョブへ再接続する。
- 次: 利用者が必要なら、実データでの手動確認または明示依頼によるbrowser E2Eを行う。
- 注意: ジョブ状態はメモリ内で、プロセス再起動時は復元しない。404時は`image_url`と`modified_at`を更新する。通常テスト、vet、JS構文、Linux静的buildは成功。

## 2026-08-19 17:27 JST

- 実行エージェント: Codex
- 作業トピック: 404チェック中の編集競合修正
- 実施: reviewerの中優先度指摘に対応し、確認時と同じ`id + url`の場合だけ404画像と`modified_at`を更新するようにした。確認中にURLを編集しても新しいサムネイルを維持する回帰テストを追加した。
- 次: なし
- 注意: reviewerの低優先度指摘（再読み込み中に完了した場合の古い画面表示）は利用者判断により未変更で、完了後の手動再読み込みで解消する。

## 2026-08-19 17:33 JST

- 実行エージェント: Codex
- 作業トピック: staticcheck指摘対応
- 実施: Henji関連エラー文2件を日本語始まりへ直し、`currentSchemaVersion`をマイグレーション完了時のSQLite `PRAGMA user_version`へ記録して整合性テストを追加した。staticcheckは指摘0件になった。
- 次: なし
- 注意: 利用者判断によりgosec指摘へのコード変更は行っていない。通常テスト、vet、JS構文、Linux静的build、差分checkは成功。

## 2026-08-19 17:46 JST

- 実行エージェント: Codex
- 作業トピック: Shirushi v1.2.0 リリース準備
- 実施: Forgejo release profile/workflowと日英CHANGELOG・API・READMEをv1.2.0向けに整備した。通常test、vet、staticcheck、4対象のCGOなしbuildとchecksum検証、managed Chromiumで404チェックの非同期進捗・完了・サムネイル・`modified_at`を確認した。
- 次: 最終差分を確認してcommit・push・v1.2.0 tagを作成し、Forgejo Actionsと公開assetを確認する。
- 注意: 利用者判断によりrace testは選択していない（`ai-dev`にC compilerなし）。gosec指摘へのコード変更も行わない。
