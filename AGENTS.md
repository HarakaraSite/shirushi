# Shirushi 開発指示書

## 1. 実行環境

- 最終的にProxmox上のAlpine Linux（LXC）で稼働させる。
- `CGO_ENABLED=0`で動作する、完全な静的シングルバイナリとしてビルドできる構成を維持する。

## 2. ブランチ運用

- 新しい作業は、`feature/`、`fix/`、`chore/`などの作業ブランチで開始する。
- `main`へ直接pushしない。
- 作業完了と動作確認後に`main`へマージする。
- Forgejo CIは、ブランチへのpushと`main`向けPull RequestでGoテストを実行する。
- リリースバイナリは、`v*`タグをpushしたときに生成する。

## 3. DBスキーマ変更

DBスキーマを変更する場合は、次の3ステップをすべて実施する。

1. `schema.go`の`currentSchema`を更新する。
2. `db.go`の`runMigrationsOn`に既存DB向けの移行処理を追加する。
3. `schema.go`の`currentSchemaVersion`をインクリメントする。

## 4. 検証

変更範囲に応じて必要な検証を行い、コミット前に関連する検証が成功することを確認する。

```sh
CGO_ENABLED=0 go test ./...
go vet ./...
node --check static/js/app.js
git diff --check
```

## 5. 進め方

- 一度に大量の変更を行わず、小さなステップに分割する。
- 各ステップの結果をユーザーへ提示し、確認を待ってから次へ進む。

## 6. 過去の引き継ぎ記録

- `.handoff/handoff.md`は過去記録として固定し、更新しない。
