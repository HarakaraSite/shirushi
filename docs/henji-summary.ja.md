# Henji本文要約

English version: [henji-summary.md](henji-summary.md)

Shirushiは、保存済みブックマークの静的HTMLを[Henji](https://forge.harakara.site/littleisland/henji)を通じて生成AIモデルで日本語要約できます。この機能は任意です。Henjiがなくても通常のブックマーク機能は使えます。

要約は常に成功するものではありません。本文候補が不足する場合、ページ取得やHenji実行に失敗する場合、出力が要件を満たさない場合は要約せず、既存Excerptを変えません。

## 事前準備

Henjiの導入と、選択するproviderに必要な認証情報の設定はHenji側で行います。Shirushiはprovider APIキーを読まず、保存せず、画面やAPIで公開しません。Shirushiは外部コマンドとしてHenjiを起動し、抽出した本文候補を標準入力へ渡します。

実装はHenji v2.1.7で確認しています。API/model選択とJSON Schema出力に対応する互換Henjiコマンドが必要です。

## Henjiを使って起動する

既定ではPATH上の`henji`を探索し、`openrouter / google/gemini-2.5-flash-lite`を使います。

```bash
SHIRUSHI_PASSWORD='yourpassword' ./shirushi
```

実行ファイルの場所を明示する場合:

```bash
SHIRUSHI_PASSWORD='yourpassword' ./shirushi \
  --henji-path /opt/bin/henji
```

providerとmodelを対で上書きする場合:

```bash
SHIRUSHI_PASSWORD='yourpassword' ./shirushi \
  --henji-api openrouter \
  --henji-model example/model \
  --henji-max-input-chars 4000000
```

`--henji-api`と`--henji-model`は必ず対で指定します。既知のAPI/model組には入力上限を内蔵しています。未知の組では、model上限を推測しないため、正の`--henji-max-input-chars`をUTF-8バイト数で指定してください。

別のmodelを使う場合は、利用するproviderの認証情報やmodelの利用設定を先にHenji側で済ませてください。そのうえでShirushiの起動引数にAPI/modelを指定します。要約は待ち時間がそのまま操作の待ち時間になるため、要約品質を満たす範囲では高速に応答するmodelを推奨します。

## Web UIでの使い方

Henjiが利用できると、保存済みブックマークカードにだけ「AI」ボタンが表示されます。新規登録・編集モーダルには表示しません。

確認ダイアログで承認すると、Shirushiは要約ジョブをすぐ受け付け、バックグラウンドで実行します。成功時だけExcerptを、日本語の空行なし1〜5行・400 Unicode文字以内の要約で上書きします。

処理中表示、完了通知、ポーリング、自動再試行、サーバー再起動後のジョブ再開はありません。後で一覧を再読み込みすると更新済みカードを確認できます。編集モーダルを開くときは常にサーバーから最新のbookmarkを読み込むため、ページ未再読み込みでも完了済み要約をフォームに表示します。

同じブックマークへの複数開始は許可され、最後に完了した要約が残ります。

## Henjiへ渡す内容

Shirushiは、保存済みURLを既存のSSRF防御付きで取得します。JavaScript実行、headless browser、認証済みページの取得は行いません。

静的HTMLからREADME・article・mainを優先して候補を抽出し、ナビゲーション、header、footer、sidebar、広告、SNS操作、script/styleの内容を除外します。正規化後に非空白Unicode文字が600文字以上かつ本文ブロックが3個以上なければ、Henjiを呼ばずExcerptを変えません。

通常の抽出上限は40,000 Unicode文字です。実際の標準入力は、選択modelの実効`max-input-chars`から固定指示文、JSON Schema、framing、安全余裕を引いた値と40,000文字の小さい方へ収めます。長文では本文候補の先頭と末尾を残します。

Henjiへは固定の日本語指示と、`summary`だけを受けるJSON Schemaを渡します。Shirushi側でもJSONを検証し、空でない要約と行数・文字数上限だけを受け入れます。

## 失敗時と安全性

Henjiが見つからない場合、Web UIは「AI」ボタンを隠します。`GET /api/capabilities`は`{"henji_summary":false}`を返し、要約開始APIはURL取得やDB更新を行わず`204 No Content`を返します。

本文不足、ネットワーク失敗、Henji失敗、timeout、不正な出力では、既存Excerptを変えません。Henjiのstderr、本文、provider応答、provider設定は利用者に表示しません。

Shirushiはshell文字列を組み立てず、引数配列でHenjiを起動します。本文候補は標準入力で渡し、timeoutは120秒、stdout上限は16 KiB、同時要約ジョブは最大3件です。

エンドポイントの詳細は[APIリファレンス](api.ja.md)を参照してください。
