# Shirushi 開発指示書

## 1. 実行環境

- 最終的にProxmox上のAlpine Linux（LXC）で稼働させる。
- `CGO_ENABLED=0`で動作する、完全な静的シングルバイナリとしてビルドできる構成を維持する。

## 2. ブランチ運用

- 新しい作業は、`feature/`、`fix/`、`chore/`などの作業ブランチで開始する。
- `main`へ直接pushしない。
- 作業完了と動作確認後に`main`へマージする。
- Forgejo CIは、`main`向けPull RequestでGoテストを実行する。通常ブランチへのpushとmainへのmerge後pushでは実行しない。
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
scripts/run-static-analysis.sh
node --check static/js/app.js
git diff --check
```

static analysis toolが未導入の場合は、先に`scripts/install-static-analysis-tools.sh`を実行する。
このinstallerはprojectのGo versionを使用し、staticcheckとchecksum検証済みgosec binaryを固定versionで導入する。

## 5. 進め方

- 作業は必要十分な小ささに分ける。小規模・局所的で方針が明確な作業は親エージェントが直接行い、それ以外は複雑さ、不確実性、専門性、委譲の便益に応じてサブエージェントへ委譲する。ファイル数や行数は判断材料に留め、固定閾値にはしない。
- サブエージェントへ委譲する場合は、事前に`docs/agent-delegation.md`を読み、task設計、agent選択、tool制約、thinking levelに応じたtimeout、async運用、親による統合・検証の指針に従う。
- 関連箇所の特定には`scout`、段取りや設計判断には`planner`、外部情報の確認には`researcher`、変更後の独立確認には`reviewer`、切り出せる実装には`worker`を優先候補とする。これらの常時利用や一律の経由は要求しない。
- 委譲時は目的、範囲、完了条件、変更可否を明示し、親エージェントが結果の検証と統合に責任を持つ。複数の変更作業を並行させる場合は、隔離したworktreeを使用する。
- 合意済みの範囲では自律的に継続する。進捗報告は確認待ちを意味せず、次の作業を継続する。
- ユーザー確認のために停止するのは、ユーザーと事前に合意したgate、計画外の問題により要件・範囲・データ・互換性などの重要な判断が必要な場合、または明示許可のない破壊的操作、外部システムの変更・公開、権限変更、機密情報の送信、課金を伴う場合に限る。
- 自明な局所修正、検証の再実行、合意範囲内の代替手段、依頼内で明示許可済みの操作では再確認しない。ただし、前提・影響・リスクが実質的に変わった場合を除く。

## 6. 過去の引き継ぎ記録

- `.handoff/handoff.md`は過去記録として固定し、更新しない。
