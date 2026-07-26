# Shirushi Henji 本文要約 MVP 実装計画

## 方針

concept revision 7の要件を維持し、検証済みの小さな増分で実装する。

1. 安全なHTML取得と本文抽出を実装・検証する。
2. 候補モデルの入力上限を確認し、Henji投入上限を算出する。
3. 同一本文でモデルを比較し、利用者がprovider/modelを選ぶ。
4. 選定済みのHenjiアダプターを実装する。
5. 最大3並行の非同期APIを追加する。
6. 編集モーダル専用の確認付きボタンを追加する。
7. 実環境の代表ケースで受入確認する。

H-001/H-002とprovider/modelの選択が承認されるまでAPI/UI統合へ進まない。

## 採用済み要件

- 新規登録は既存URL欄blurによるOGP説明文取得だけを行い、AI要約は保存済みbookmarkカードだけに置く。
- 押下後にブラウザ標準`confirm()`でExcerptの自動上書きを確認し、取消時は開始しない。
- URL取得は既存SSRF防御を維持し、JavaScript実行、headless browser、認証済みページ取得を含めない。
- 静的HTMLの本文候補が不足する場合はHenjiを呼ばずExcerptを変えない。
- 一件ごとの非同期処理で、Henjiプロセスは最大3件まで同時実行する。
- 処理中表示、完了通知、ポーリング、再押下制御、競合解決、自動再試行、再起動後のジョブ再開は実装しない。
- 同じbookmarkへの複数実行を許容し、完了順で最後の要約がExcerptを上書きする。
- Henji未導入時はボタンを表示せず、利用者向けエラーも出さない。
- ShirushiはAPIキーや設定UIを持たず、起動引数で指定されたHenji実行ファイルを引数配列で起動する。未指定時はPATHから`henji`を探索する。shell文字列を組み立てず、本文候補をstdinへ渡し、stderrを利用者へ表示しない。
- 要約は日本語5行以内とし、JSON Schemaで`summary`だけを受ける。
- provider/modelは候補比較後に利用者が選ぶ。Shirushiの設定画面、環境変数、APIキー設定は追加しない。本文要約のprovider/modelだけは起動引数`--henji-api`と`--henji-model`を対で受け付け、未指定時は比較済みの`openrouter / google/gemini-2.5-flash-lite`を使う。

## 確認済みの現状

- `Bookmark.Excerpt`は既存のメモ・抜粋欄で、新しいDBカラムは不要である。
- `PUT /api/bookmarks/{id}`は`excerpt`と`modified_at`を更新する。要約成功時もID条件付きUPDATEで同じ二項目を更新できる。
- 追加・編集モーダルは共通で、`editingId === null`が追加、数値が編集を表す。URL blurのOGP補完は追加時だけで、編集時には既存テキストを上書きしない。
- 現行URL取得はHTTP(S)検証、接続時の内部IP拒否、最大5 redirect、10秒timeout、HTML Content-Type判定、先頭1 MiB制限を持つ。
- Henji v2.1.7は指示を引数、本文をstdinとして扱う。`--json-schema`のschema不適合再試行はMVPで`--json-schema-retries 0`にする。
- このホストではPATH上の`henji`を利用できる。配布先の実行ファイル配置は環境ごとに異なるため、未指定時はPATH探索、必要時は`--henji-path`で明示指定する。

## 入力上限

| 項目 | MVP値 |
|---|---:|
| 要約用HTML読取り上限 | 2 MiB |
| 本文成立条件 | 正規化後600個以上の非空白Unicode文字、かつ3個以上の本文ブロック |
| 抽出本文の通常上限 | 40,000 Unicode文字 |
| 実際のHenji投入上限 | 選定モデルの実効`max-input-chars`から算出。最大でも40,000文字 |
| 本文切詰め配分 | 利用可能枠の先頭80%・末尾20% |
| 要約上限 | 1〜5行、空行なし、合計400 Unicode文字以内 |
| Henji stdout上限 | 16 KiB |
| Henji実行timeout | 120秒 |
| 同時処理数 | 3 |
| Shirushi側の再試行 | 0 |
| Henji schema再試行 | `--json-schema-retries 0` |

抽出本文の通常上限は40,000 Unicode文字とする。ただしHenji v2.1.7の`max-input-chars`は実装上UTF-8バイト数で結合済みpromptを切り詰める。そのためHenjiへ渡す本文は、Unicode文字数とUTF-8バイト予算の両方を満たすようにする。

`sourceByteBudget = M - P - J - F - S`

`L = min(40,000 Unicode文字, sourceByteBudgetに収まる本文のUnicode文字数)`

- `M`: 選定モデルの実効`max-input-chars`。Henji v2.1.7ではUTF-8バイト数として扱う。
- `P`: 実際に渡す固定指示文のUTF-8バイト数
- `J`: provider方言に合わせてcompact JSON化したJSON SchemaのUTF-8バイト数
- `F`: 引数とstdinの区切り、最大512行のタブ字下げ、`[中略]`を含むframing予約
- `S`: 安全余裕。`max(2,048 bytes, ceil(M × 0.05))`

`P`と`J`は実際に渡すUTF-8文字列から`len`で算出する。`F`はHenji v2.1.7のsource/testで確認した空行連結と行ごとのタブ字下げを予約する。Unicode文字数から比較用の安全上限を出すときは最悪4 bytes/文字で見積もる。`L < 600 Unicode文字`の候補はMVPから除外する。実際のadapterは本文のUTF-8バイト数を測って必要な分だけ切り詰め、本文を切り詰める場合はmarker分を先に引いて残りを先頭80%・末尾20%で保持する。本文が予算内ならmarkerを加えない。

### 2026-07-26の確認記録

- 実行Henjiは`/Users/masat/bin/henji`、versionは`v2.1.7`だった。MVPで実際に起動する固定パス`/usr/local/bin/henji`はこのホストでは未導入である。
- 実行OS userのHenji設定ではglobal `max-input-chars`は12,250であり、比較候補には個別値がある。`google / gemini-flash-lite-latest`は4,000,000、`openrouter / deepseek/deepseek-v4-flash`は4,194,304、`openai / gpt-5.6-terra`は794,000である。
- いずれも固定prompt、schema、framing、安全余裕を差し引いた後も40,000文字を上回る見込みだが、model選定後に実文字列から`P`、`J`、`F`、`S`を再計算して`L_selected`を確定する。この記録はモデル採用を意味しない。
- `SHIRUSHI_LIVE_SUMMARY_TEST=1 go test -run TestLiveExtractSummarySource_RepresentativePages -v`で、Fil-Cは2,148文字・24ブロック、Senpaiは2,259文字・56ブロック、YAMA HACKは1,750文字・37ブロックを抽出した。Sakana AIは本文候補なしだった。これは同日時点の外部ページ観測である。
- 固定promptは380 UTF-8 bytes、Google用Schemaは113 bytes、OpenAI/OpenRouter用Schemaは142 bytesとなった。最大512行の字下げ、区切り、中略marker、安全余裕を予約した本文予算は、Google 3,798,983 bytes、OpenRouter 3,983,542 bytes、OpenAI 753,254 bytesだった。最悪4 bytes/Unicode文字の保守見積りでも、3候補とも40,000 Unicode文字を投入できる。
- OpenAI strict schemaの`minLength`/`maxLength`は公式仕様で未対応のため、OpenAI/OpenRouter用Schemaから除去した。変更後のSchemaは112 bytesで、OpenRouterとOpenAIの本文予算はそれぞれ3,983,572 bytes、753,284 bytesである。第1段階ではGoogleとOpenRouterが4入力すべて成功した一方、OpenAIは合成本文で2回連続して非zero exitとなった。stderrは利用者へ表示せず、OpenAI候補を今回の比較対象から外した。

## 増分1: 安全なHTML取得と本文抽出

### 対象ファイル

- `metadata.go`: HTTPクライアント、redirect検査、Content-Type検査、上限付きHTML読取りを共通ヘルパー化する。既存`fetchMetadata`は従来どおり1 MiBで使い、OGP取得を変えない。要約取得は2 MiBで使う。
- 新規`summary.go`: 本文抽出と正規化だけを先に実装する。Henji、API、DB更新はまだ追加しない。
- 新規`summary_test.go`: 本文抽出・正規化・上限の単体テストを置く。
- 新規`summary_live_test.go`: 通常テストから除外した代表URL向けの明示実行liveテストを置く。

### 抽出規則

Go標準ライブラリだけで開始タグ、終了タグ、属性、テキストを追跡する小さなHTML scannerを実装する。正規表現だけで入れ子要素を切り出さず、壊れたHTMLでは安全に抽出失敗へ倒す。

候補を優先順に集め、同じ優先度に複数ある場合は正規化後の本文が最長の一つを選ぶ。

1. class/idの語が`readme`、`markdown-body`、`article-body`、`article-content`、`entry-content`、`post-content`に一致する領域
2. `<article>`
3. `role="main"`
4. `<main>`

`body`全文へのfallbackは行わない。候補がない、または成立条件を満たさない場合は本文不足とする。候補内でも`script`、`style`、`noscript`、`template`、`svg`、`canvas`、`nav`、`header`、`footer`、`aside`、`form`、`dialog`、`hidden`、`aria-hidden="true"`を除外する。広告、affiliate、breadcrumb、related、recommendation、share、social、sidebar、newsletter、cookie、modalを表すclass/idの領域も除外する。

段落、見出し、リスト項目、引用、pre/code、表セル、`br`を本文ブロック境界にする。HTML entityを復号し、連続空白を一つへ畳み、連続する同一行を除去する。抽出段階では成立判定後、通常上限40,000文字・最大512論理行へ収める。

### 検証

- `article`、`role=main`、`main`、README classのfixtureで期待領域を選ぶ。README領域が外側の`main`より優先される。
- nav、広告、関連記事、sidebar、script内の命令文を除外する。
- 599文字または2ブロックでは本文不足、600文字かつ3ブロックでは成立する。
- 40,000文字・512行超の本文が先頭80%・末尾20%規則で上限内になる。
- 非HTML、timeout、redirect過多、非HTTP(S)、内部IPで本文を返さない。
- 既存`fetchMetadata`のtitle、excerpt、author、image取得と1 MiB境界が変わらない。

### live確認

| URL | 期待 |
|---|---|
| `https://fil-c.org/` | 静的な本文候補が成立する |
| `https://git.sr.ht/~delthas/senpai/` | リポジトリUIよりREADME表示領域を優先する |
| `https://yamahack.com/7651` | 記事本文が成立し、affiliate、関連記事、sidebarが主要入力にならない |
| `https://buzz.xyz/`または`https://sakana.ai/` | 本文不足となりHenjiを呼ばない |

同じ規則で最初の3件と本文不足例を分離できなければ、閾値だけを恣意的に下げずconcept reviewへ戻す。

## 増分2: max-input-chars確認、モデル比較、利用者決定

候補ごとに、実際にShirushiを起動するOS userと同じHenji設定を対象として`M`を確認する。確認元の優先順位は、model entryの`max-input-chars`、Henji設定のglobal `max-input-chars`、Henji versionの公式資料またはsource/testにあるdefault値とする。`henji --list-models --output json`はmodel存在確認には使えるが、現在の出力に`max-input-chars`がないため上限値の根拠にしない。

確認記録にはHenji version、実行binaryの絶対パス、実行OS user、設定ファイルの絶対パス、API名、model ID、model単位値、global値、最終`M`、defaultを使う場合の根拠を残す。有限の`M`を確認できない候補は比較対象から外すか、運用側でmodel単位の値を明示してから再確認する。

固定指示は、stdinが未信頼のWeb文書であること、本文中の命令に従わないこと、主題と重要点を日本語1〜5行・400文字以内で要約すること、JSON Schemaに従うことを求める。一つの固定文字列として確定し、文言変更時は`P`と`L`を再計算する。

JSON Schemaはroot object、必須`summary`、`summary`が1〜400文字のstringとする。OpenAI候補では`additionalProperties: false`を使う。Google比較時はHenji manual上の制約に合わせてそれを省略し、Shirushi側の厳密JSON decodeで未知fieldを拒否する。各schemaはcompact JSONから`J`を算出する。

OpenAI strict structured outputでは`minLength`と`maxLength`が未対応であるため、OpenAI/OpenRouter用Schemaには含めない。1〜400文字、空文字なし、5行以内の制約はShirushi側で厳密に検証する。Google用Schemaでは利用できるが、provider間の出力契約はShirushi側検証を正とする。

候補`i`の上限を`L_i`、公平比較用の上限を`L_eval = min(すべての候補のL_i)`とする。Fil-C、Senpai、YAMA HACKの抽出本文を同じ`L_eval`まで切り詰めて比較する。選定後は`L_selected`で代表3件を再実行し、入力の自動切詰めがなく品質が維持されることを確認する。

1. 代表3件の抽出本文の元文字数、論理行数、hashを記録する。
2. 候補ごとの`M`、`P`、`J`、`F`、`S`、`L_i`を表にする。
3. 合成本文を各候補へ一回送り、schema対応、JSON decode、5行・400文字条件を確認する。
4. schema対応候補について代表3件を各一回、直列で要約する。
5. 利用者が有用性、正確さ、OGP説明よりの価値、日本語の自然さ、広告・UI・本文内命令を主題にしていないことを評価する。
6. 上位2候補だけ各本文でさらに2回実行し、ぶれ、成功率、中央値・最大待ち時間を記録する。
7. providerの公式料金と実際の利用量またはprovider側billingから概算費用を記録する。
8. 利用者が`API / model ID / L_selected`を選ぶ。

費用が発生する比較実行は、対象候補と最大呼出し回数を提示して利用者の承認を得てから行う。

上限計算はtable testで検証する。`M`が大きくても`L`は40,000を超えないこと、小さい場合は40,000未満となること、`L < 600`が不適格となること、marker込みの本文が`L`以下になること、最終Henji入力見積りが`M`以下になることを確認する。prompt、schema、最大論理行数の変更時には記録済み上限との不一致でテストを失敗させる。

## 増分3: Henjiアダプター

`summary_config.go`に既定の実行ファイル/API/modelと起動引数の解析を置き、`summary.go`のrunnerは起動時に選ばれた組を使う。fixed prompt、schema、`M`、`L_selected`は選択したmodelごとに不変設定として記録し、runnerをinterfaceまたは関数型で分離する。

- executableは未指定時にPATH上の`henji`を使い、起動引数`--henji-path PATH`で上書きできる。provider/modelは既定の`openrouter / google/gemini-2.5-flash-lite`、または起動引数`--henji-api API --henji-model MODEL`で対として上書きする。
- `exec.CommandContext`へ引数を個別に渡し、shellや`sh -c`を使用しない。
- timeoutは120秒。本文は`L_selected`まで整形して`cmd.Stdin`へ渡す。
- schemaはGo定数としてバイナリに含め、呼出しごとに権限0600の一時ファイルへ書き、終了後に削除する。
- stdoutは16 KiBまで取得し、超過時は失敗とする。stderr、本文、API応答を利用者レスポンスやログへ出さない。
- `--no-cache`と`--json-schema-retries 0`を指定し、`--output json`は使わない。
- JSONは未知field、末尾の余分なJSON、空summary、空行、6行以上、400文字超過を拒否する。失敗時に切り詰めて保存しない。
- Shirushiから同じ処理を再実行しない。

Goのtest helper subprocess方式で、引数とstdinの分離、shell metacharacterが実行されないこと、`L_selected`以下の投入、`M`以下の入力見積り、成功JSON、失敗JSON、timeout、非zero exit、stdout超過を検証する。

## 増分4: 非同期APIとDB更新

- `main.go`へ認証済みの`GET /api/capabilities`と`POST /api/bookmarks/{id}/summary`を追加する。
- 起動設定で選ばれたHenji実行ファイルをPATH探索または`--henji-path`で見つけられるときだけ、capabilityは`{"henji_summary":true}`とする。判定失敗時はfalseへ倒す。
- summary POSTはID不正で400、不存在で404、Henji未導入で204、開始受付で202を返す。未導入時はURL取得、ジョブ、DB更新を行わない。
- handlerは保存済みbookmarkのURLを読み、202返却前にジョブへ値コピーする。モーダルの未保存URLやExcerptは使わない。
- 容量3のプロセス内semaphoreを一つ持つ。202後のgoroutineがURL取得からDB更新まで枠を保持し、終了時に解放する。
- 成功時だけ`UPDATE bookmarks SET excerpt = ?, modified_at = CURRENT_TIMESTAMP WHERE id = ?`を実行する。0件更新なら削除済みとして静かに終了する。

versionや元Excerptを条件に加えない。重複実行は排除せず、最後に完了した要約が残る。キュー、ジョブテーブル、永続状態、再起動復元、再試行、再押下抑止、処理状態APIは追加しない。

handlerがrunner完了を待たず202を返すこと、4件開始してもrunnerが3を超えないこと、逆順完了時の上書き、手動編集の上書き、削除後に復活しないこと、失敗時のExcerpt・modified_at不変、未導入時のrunner未呼出しをテストする。

## 増分5: 編集モーダルのUI

- `static/js/app.js`でログイン後に一度capabilityを取得する。失敗時は利用不可として扱い、警告やalertを出さない。
- 保存済みbookmarkカードの編集ボタン横に、capability=trueのときだけ「AIによる要約」を置く。追加・編集モーダルには置かない。
- 押下時にカードのbookmark IDを使い、`confirm()`で「AI要約が完了すると、現在の『メモ・抜粋』を自動で上書きします。開始しますか？」と確認する。取消時はPOSTしない。承認時は`POST /api/bookmarks/{id}/summary`を一回呼ぶ。
- `GET /api/bookmarks/{id}`を追加し、`openEditModal(id)`は開く直前に最新値を読み込む。要約完了後にページを再読み込みしていなくても、古いExcerptで通常更新することを防ぐ。
- ボタンはdisableしない。処理中表示、完了通知、alert、一覧再読込、ポーリングを追加しない。編集モーダルは長いExcerptを読み書きしやすい高さにする。

未導入環境でボタンが見えないこと、導入環境で編集時だけ見えること、confirm取消時にPOSTがないこと、承認時にPOSTが一回であること、モーダルを閉じても後の再読込で結果が見えることをブラウザで確認する。

## 増分6: 文書と受入確認

`README.ja.md`、`README.md`、`docs/api.ja.md`、`docs/api.md`、`docs/e2e-test-scenarios.md`へ、Henjiが任意の外部実行時依存であること、PATH探索と`--henji-path`、未導入時の機能非表示、capability API、summary開始API、202/204、非同期・無通知・上書き仕様、投入上限の再計算条件を記載する。

検証では対象Goファイルをgofmtし、`go vet ./...`、`CGO_ENABLED=0 go test -v -cover ./...`、`CGO_ENABLED=0 go test -race ./...`、`CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/shirushi-summary .`を実行する。新しいGo依存は追加せず、`modernc.org/sqlite`以外の第三者ライブラリを導入しない。

| ケース | 受入条件 |
|---|---|
| Fil-C | 202を即時返し、完了後のExcerptが日本語1〜5行・400文字以内で主題を思い出せる |
| Senpai | READMEを要約し、リポジトリUIやnavを主題にしない |
| YAMA HACK | 記事を要約し、affiliate、商品導線、関連記事を主要点にしない |
| BuzzまたはSakana AI | 本文不足でHenji呼出し0、Excerptとmodified_at不変 |
| 長文 | 抽出本文は40,000文字以下、Henji stdinは`L_selected`以下、結合後入力見積りは`M`以下 |
| confirm取消 | POSTなし、Excerpt不変 |
| 同じbookmarkを複数実行 | 全件受理され、最後に完了した結果が残る |
| 実行中に手動編集または削除 | 成功要約は手動編集を上書き、削除済みbookmarkは復活しない |
| Henji未導入またはHenji失敗 | ボタン非表示またはExcerpt不変。利用者向けエラー、再試行、stderr表示なし |
| サーバー再起動 | 未完了ジョブを復元・再開しない |

H-001はFil-C、Senpai、YAMA HACKのうち少なくとも2件を利用者がOGP説明より有用と評価したとき合格とする。H-002はBuzzまたはSakana AIでHenji呼出し0かつExcerpt不変のとき合格とする。

## concept review request

1. resolved（2026-07-26）: revision 7のMVPとして実装へ進める。H-001/H-002を含む実Henji経由の総合受入確認は、テスト専用環境で別途行う。
2. resolved（2026-07-26）: 既定は`openrouter / google/gemini-2.5-flash-lite`とし、既定の`max-input-chars`は4,000,000 UTF-8 bytes。利用者は`--henji-api`と`--henji-model`を対で、必要なら`--henji-max-input-chars`で上書きできる。
3. 実効`max-input-chars`未確認: 有限の`M`を根拠付きで確認できない、または`L < 600`なら、Henji側でmodel単位の値を明示するか候補を除外する。Shirushi側で推測しない。
4. 抽出仮説の不成立: 代表3件を成立させつつBuzz/Sakana AIを本文不足にできない場合、対象サイトの限定、`body` fallback、第三者HTML parser例外をconcept reviewで判断する。
5. Henji内部の再試行境界: `--json-schema-retries 0`は固定する。transport retryやmodel fallbackまで禁止するかは、Henji運用設定まで含めて実装前に判断する。
