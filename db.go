package main

// db.go：データベースの初期化・テーブル作成・マイグレーションを担当するファイルです。
// アプリ起動時に main() から呼ばれる createTable() と runMigrations() が中心です。

import (
	"database/sql" // sql.NullString を使うためのパッケージ
	"fmt"          // マイグレーション結果のメッセージ出力に使うパッケージ
	"log"          // 致命的なエラー時に終了するためのパッケージ
)

// createTable：新規インストール時に必要なテーブルをすべて作成する関数です。
// Shioriと同等のスキーマ構成になっています。
func createTable() {
	queries := []string{
		// ブックマークテーブル
		`CREATE TABLE IF NOT EXISTS bookmarks (
            id          INTEGER PRIMARY KEY AUTOINCREMENT,
            url         TEXT NOT NULL,
            title       TEXT NOT NULL DEFAULT '',
            excerpt     TEXT NOT NULL DEFAULT '',
            author      TEXT NOT NULL DEFAULT '',
            public      INTEGER NOT NULL DEFAULT 0,
            has_content BOOLEAN NOT NULL DEFAULT FALSE,
            image_url   TEXT NOT NULL DEFAULT '',
            created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
            modified_at DATETIME DEFAULT NULL
        );`,
		// タグテーブル
		// ブックマークに付けるラベル（例: "go", "tech", "あとで読む"）を管理します。
		`CREATE TABLE IF NOT EXISTS tags (
            id   INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL UNIQUE
        );`,
		// ブックマークとタグの中間テーブル（多対多の関係）
		// 1つのブックマークに複数のタグ、1つのタグを複数のブックマークに付けられます。
		`CREATE TABLE IF NOT EXISTS bookmark_tags (
            bookmark_id INTEGER NOT NULL,
            tag_id      INTEGER NOT NULL,
            PRIMARY KEY (bookmark_id, tag_id),
            FOREIGN KEY (bookmark_id) REFERENCES bookmarks(id) ON DELETE CASCADE,
            FOREIGN KEY (tag_id)      REFERENCES tags(id)      ON DELETE CASCADE
        );`,
	}
	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			log.Fatal("テーブル作成エラー:", err)
		}
	}
}

// runMigrations：既存のデータベースに新しいカラムを追加するマイグレーション関数です。
// PRAGMA table_info で現在のカラム一覧を取得し、不足しているカラムだけを追加します。
// この方式は「冪等性（何度実行しても同じ結果になる）」があるため安全です。
func runMigrations() {
	// PRAGMA table_info はSQLiteの特殊コマンドで、テーブルのカラム情報を返します。
	rows, err := db.Query("PRAGMA table_info(bookmarks)")
	if err != nil {
		log.Fatal("マイグレーション確認エラー:", err)
	}
	defer rows.Close()

	// 現在存在するカラム名をマップに記録します。
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, pk int
		var name, colType string
		var dfltValue sql.NullString
		rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk)
		columns[name] = true
	}

	// description カラムが存在する場合は excerpt にリネームします。
	// （旧スキーマからの移行処理）
	if columns["description"] && !columns["excerpt"] {
		_, err = db.Exec("ALTER TABLE bookmarks RENAME COLUMN description TO excerpt")
		if err != nil {
			log.Fatal("カラムリネームエラー:", err)
		}
		fmt.Println("マイグレーション: description → excerpt にリネームしました")
		columns["excerpt"] = true
		delete(columns, "description")
	}

	// 不足しているカラムを定義します。
	// ALTER TABLE ADD COLUMN は既存データを保持したままカラムを追加できます。
	type migration struct {
		column string
		sql    string
	}
	migrations := []migration{
		{"excerpt", "ALTER TABLE bookmarks ADD COLUMN excerpt TEXT NOT NULL DEFAULT ''"},
		{"author", "ALTER TABLE bookmarks ADD COLUMN author TEXT NOT NULL DEFAULT ''"},
		{"public", "ALTER TABLE bookmarks ADD COLUMN public INTEGER NOT NULL DEFAULT 0"},
		{"has_content", "ALTER TABLE bookmarks ADD COLUMN has_content BOOLEAN NOT NULL DEFAULT FALSE"},
		{"image_url", "ALTER TABLE bookmarks ADD COLUMN image_url TEXT NOT NULL DEFAULT ''"},
		{"modified_at", "ALTER TABLE bookmarks ADD COLUMN modified_at DATETIME DEFAULT NULL"},
	}

	for _, m := range migrations {
		if !columns[m.column] {
			if _, err := db.Exec(m.sql); err != nil {
				log.Fatal("マイグレーションエラー:", err)
			}
			fmt.Printf("マイグレーション: %s カラムを追加しました\n", m.column)
		}
	}

	// tagsテーブルとbookmark_tagsテーブルが存在しない場合は作成します。
	// CREATE TABLE IF NOT EXISTS は冪等なので何度実行しても安全です。
	tagMigrations := []string{
		`CREATE TABLE IF NOT EXISTS tags (
            id   INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL UNIQUE
        );`,
		`CREATE TABLE IF NOT EXISTS bookmark_tags (
            bookmark_id INTEGER NOT NULL,
            tag_id      INTEGER NOT NULL,
            PRIMARY KEY (bookmark_id, tag_id),
            FOREIGN KEY (bookmark_id) REFERENCES bookmarks(id) ON DELETE CASCADE,
            FOREIGN KEY (tag_id)      REFERENCES tags(id)      ON DELETE CASCADE
        );`,
	}
	for _, q := range tagMigrations {
		if _, err := db.Exec(q); err != nil {
			log.Fatal("タグテーブル作成エラー:", err)
		}
	}
	cleanupOrphanedBookmarkTags()

	// url カラムに UNIQUE 制約が付いているか確認します。
	// PRAGMA index_list でテーブルのインデックス一覧を取得できます。
	if !hasUniqueURLIndex() {
		fmt.Println("マイグレーション: url カラムに UNIQUE 制約を追加します")
		migrateAddUniqueURL()
		fmt.Println("マイグレーション: UNIQUE 制約を追加しました")
	}
}

// hasUniqueURLIndex：bookmarks テーブルの url カラムに UNIQUE インデックスがあるか確認します。
func hasUniqueURLIndex() bool {
	// PRAGMA index_list はテーブルのインデックス一覧を返します。
	rows, err := db.Query("PRAGMA index_list(bookmarks)")
	if err != nil {
		return false
	}

	// rows を開いたまま別のクエリを実行すると、接続数が少ない環境で
	// 次のクエリが同じ接続を待ち続けることがあります。
	// そのため、まず UNIQUE インデックス名だけを読み出して rows を閉じます。
	var uniqueIndexNames []string
	for rows.Next() {
		var seq, unique int
		var name, origin string
		var partial int
		rows.Scan(&seq, &name, &unique, &origin, &partial)
		if unique == 1 {
			uniqueIndexNames = append(uniqueIndexNames, name)
		}
	}
	rows.Close()

	for _, name := range uniqueIndexNames {
		// そのインデックスが url カラムに対応するか確認します。
		infoRows, err := db.Query("PRAGMA index_info(" + name + ")")
		if err != nil {
			continue
		}
		for infoRows.Next() {
			var rank, cid int
			var colName string
			infoRows.Scan(&rank, &cid, &colName)
			if colName == "url" {
				infoRows.Close()
				return true
			}
		}
		infoRows.Close()
	}
	return false
}

// cleanupOrphanedBookmarkTags：存在しないブックマークやタグを指す古い紐付けを削除します。
// 過去に外部キー制約が無効な状態で削除されたデータがあると、bookmark_tags だけが残ることがあります。
// 本体の bookmarks / tags は消さず、「親が存在しない中間テーブルの行」だけを掃除します。
func cleanupOrphanedBookmarkTags() {
	result, err := db.Exec(`
		DELETE FROM bookmark_tags
		WHERE NOT EXISTS (
			SELECT 1 FROM bookmarks b WHERE b.id = bookmark_tags.bookmark_id
		)
		OR NOT EXISTS (
			SELECT 1 FROM tags t WHERE t.id = bookmark_tags.tag_id
		)
	`)
	if err != nil {
		log.Fatal("タグ紐付けクリーンアップエラー:", err)
	}

	deleted, err := result.RowsAffected()
	if err == nil && deleted > 0 {
		fmt.Printf("マイグレーション: 無効なタグ紐付けを %d 件削除しました\n", deleted)
	}
}

// migrateAddUniqueURL：既存データを保持しながら url カラムに UNIQUE 制約を追加します。
// SQLite では既存カラムへの制約追加ができないため、テーブルを再作成します。
// 手順: 新テーブル作成 → データコピー → 旧テーブル削除 → リネーム
func migrateAddUniqueURL() {
	// トランザクション内で行うことでエラー時にロールバックできます。
	tx, err := db.Begin()
	if err != nil {
		log.Fatal("トランザクション開始エラー:", err)
	}

	queries := []string{
		// 0. 既存のタグ紐付けを一時退避します。
		// このあと旧 bookmarks テーブルを DROP すると、外部キーの ON DELETE CASCADE により
		// bookmark_tags の行が削除される可能性があるためです。
		`CREATE TEMP TABLE bookmark_tags_backup AS
         SELECT bt.bookmark_id, bt.tag_id
         FROM bookmark_tags bt
         INNER JOIN bookmarks b ON b.id = bt.bookmark_id
         INNER JOIN tags t ON t.id = bt.tag_id`,
		// 1. UNIQUE 制約付きの新テーブルを作成します。
		`CREATE TABLE bookmarks_new (
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
		// 2. 旧テーブルからデータをコピーします。
		// URLが重複している場合は先に登録されたものを優先します（INSERT OR IGNORE）。
		`INSERT OR IGNORE INTO bookmarks_new
            (id, url, title, excerpt, author, public, has_content, image_url, created_at, modified_at)
         SELECT id, url, title, excerpt, author, public, has_content, image_url, created_at, modified_at
         FROM bookmarks`,
		// 3. 旧テーブルを削除します。
		`DROP TABLE bookmarks`,
		// 4. 新テーブルを正式な名前にリネームします。
		`ALTER TABLE bookmarks_new RENAME TO bookmarks`,
		// 5. 退避していたタグ紐付けを復元します。
		// URL重複でコピーされなかったブックマークIDは復元対象から外します。
		`INSERT OR IGNORE INTO bookmark_tags (bookmark_id, tag_id)
         SELECT backup.bookmark_id, backup.tag_id
         FROM bookmark_tags_backup backup
         INNER JOIN bookmarks b ON b.id = backup.bookmark_id
         INNER JOIN tags t ON t.id = backup.tag_id`,
		// 6. 一時テーブルを削除します。
		`DROP TABLE bookmark_tags_backup`,
	}

	for _, q := range queries {
		if _, err := tx.Exec(q); err != nil {
			tx.Rollback()
			log.Fatal("UNIQUEマイグレーションエラー:", err)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Fatal("コミットエラー:", err)
	}
}
