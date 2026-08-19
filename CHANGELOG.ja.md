# 変更履歴

English version: [CHANGELOG.md](CHANGELOG.md)

## 1.2.0

### 追加

- 保存済みbookmarkの全URLを並行数制限付きで確認し、HTTP 404のサムネイルを同梱404画像へ変更する、非同期の「404チェック」操作。
- 404チェックの進捗ポーリングと、通信失敗件数を含む完了結果表示。

### 変更

- マイグレーション完了後、SQLiteの`PRAGMA user_version`へスキーマ世代を記録。
- Forgejoのtag release workflowでtest・vet、CGOなし4バイナリbuild、`SHA256SUMS`公開を実施。

### 注意事項

- 404判定時は`image_url`と`modified_at`を更新。確認中にURLが編集された場合、古い結果で編集後のbookmarkを上書きしない。
- 404ジョブの進捗はメモリ内だけに保持し、Shirushi再起動後は復元しない。

## 1.1.0

### 追加

- ブラウザ拡張などのAPIクライアントが、正規化後のURL完全一致で保存済みbookmarkを取得できる`GET /api/bookmarks/by-url`。

## 1.0.0

### 追加

- [Henji](https://forge.harakara.site/littleisland/henji)を使った、保存済みブックマーク本文の任意の日本語要約。
- 編集前に最新bookmarkを取得する`GET /api/bookmarks/{id}`。
- Henji実行ファイル、provider、model、入力上限を起動引数で指定する設定。

### 変更

- Henjiが利用可能なとき、bookmarkカードに「AI」操作を表示。
- 編集モーダルを開く前にサーバーの最新bookmarkを読むことで、完了済み要約を古い画面データで上書きしないよう改善。
- 長いExcerptを読み書きしやすいよう、編集モーダルを拡大。

### 注意事項

- Henjiは任意の外部コマンドで、設定は別に行います。Shirushiはprovider APIキーを管理しません。
- 要約は非同期です。進捗表示、完了通知、ポーリング、自動再試行、再起動後の再開は意図的に提供しません。
