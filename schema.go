package main

// schema.go：データベーススキーマの「唯一の真実」を管理するファイルです。
//
// スキーマを変更する場合は必ず以下の3ステップを守ってください:
//  1. currentSchema を更新する（新しいカラム・テーブルを追加）
//  2. db.go の runMigrationsOn に対応する ALTER TABLE 等を追加する
//  3. currentSchemaVersion をインクリメントする
//
// ステップ1だけやって2を忘れると TestMigrationReachesCurrentSchema が失敗します。
// CI がそれを検知します。

// currentSchemaVersion：スキーマの世代番号です。
// マイグレーションを追加するたびにインクリメントしてください。
const currentSchemaVersion = 2

// currentSchema：フレッシュインストール時に作成するテーブル定義です。
// マイグレーションを積み上げた結果と一致しなければならないため、
// TestMigrationReachesCurrentSchema でその整合性を自動検証します。
var currentSchema = []string{
	// ブックマークテーブル。url に UNIQUE 制約を持ちます。
	`CREATE TABLE IF NOT EXISTS bookmarks (
        id          INTEGER PRIMARY KEY AUTOINCREMENT,
        url         TEXT NOT NULL UNIQUE,
        title       TEXT NOT NULL DEFAULT '',
        excerpt     TEXT NOT NULL DEFAULT '',
        author      TEXT NOT NULL DEFAULT '',
        public      INTEGER NOT NULL DEFAULT 0,
        has_content BOOLEAN NOT NULL DEFAULT FALSE,
        image_url   TEXT NOT NULL DEFAULT '',
        created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
        modified_at DATETIME DEFAULT NULL
    )`,
	// タグテーブル。name に UNIQUE 制約を持ちます。
	`CREATE TABLE IF NOT EXISTS tags (
        id   INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL UNIQUE
    )`,
	// ブックマークとタグの中間テーブル（多対多）。
	`CREATE TABLE IF NOT EXISTS bookmark_tags (
        bookmark_id INTEGER NOT NULL,
        tag_id      INTEGER NOT NULL,
        PRIMARY KEY (bookmark_id, tag_id),
        FOREIGN KEY (bookmark_id) REFERENCES bookmarks(id) ON DELETE CASCADE,
        FOREIGN KEY (tag_id)      REFERENCES tags(id)      ON DELETE CASCADE
    )`,
}
