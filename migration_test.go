package main

// migration_test.go：スキーマ整合性テストです。
// 「フレッシュ作成DB」と「v1スキーマからフルマイグレーションしたDB」の
// スキーマ構造が一致することを検証します。
//
// このテストが落ちるケース:
//   - currentSchema を更新したが、runMigrationsOn に対応するマイグレーションを追加し忘れた
//   - 逆に、マイグレーションを追加したが currentSchema を更新し忘れた

import (
	"database/sql"
	"sort"
	"testing"

	_ "modernc.org/sqlite"
)

// initialSchemaV1：マイグレーション前の最小スキーマです（v1 相当）。
// 実際の初期バージョンに近い最小構成にすることで、
// すべてのマイグレーションパスを通過させます。
const initialSchemaV1 = `
CREATE TABLE bookmarks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    url         TEXT NOT NULL,
    title       TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// openMigrationTestDB：テスト用インメモリSQLiteDBを開くヘルパーです。
// インメモリDBは接続ごとに別DBになるため、MaxOpenConns(1) で固定します。
func openMigrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("テスト用DB作成エラー: %v", err)
	}
	// SQLite の ":memory:" は接続ごとに別DBになるため1接続に固定します。
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	return d
}

// columnInfo：PRAGMA table_info の1行分を表す構造体です。
type columnInfo struct {
	name    string
	colType string
	notNull int
	dflt    string
}

// getColumns：指定テーブルのカラム一覧をカラム名順で返します。
func getColumns(t *testing.T, d *sql.DB, table string) []columnInfo {
	t.Helper()
	rows, err := d.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("PRAGMA table_info(%s) エラー: %v", table, err)
	}
	defer rows.Close()

	var cols []columnInfo
	for rows.Next() {
		var cid, pk int
		var ci columnInfo
		var dflt sql.NullString
		if err := rows.Scan(&cid, &ci.name, &ci.colType, &ci.notNull, &dflt, &pk); err != nil {
			t.Fatalf("カラム情報の読み取りエラー: %v", err)
		}
		if dflt.Valid {
			ci.dflt = dflt.String
		}
		cols = append(cols, ci)
	}
	// カラム名でソートして順序依存をなくします。
	sort.Slice(cols, func(i, j int) bool { return cols[i].name < cols[j].name })
	return cols
}

// assertColumnsMatch：2つのDBの指定テーブルのカラム構造が一致することを検証します。
func assertColumnsMatch(t *testing.T, fresh, migrated *sql.DB, table string) {
	t.Helper()
	freshCols := getColumns(t, fresh, table)
	migratedCols := getColumns(t, migrated, table)

	if len(freshCols) != len(migratedCols) {
		t.Errorf("テーブル %s のカラム数が違います: fresh=%d, migrated=%d",
			table, len(freshCols), len(migratedCols))
		t.Logf("fresh:    %+v", freshCols)
		t.Logf("migrated: %+v", migratedCols)
		return
	}
	for i := range freshCols {
		f, m := freshCols[i], migratedCols[i]
		if f.name != m.name || f.colType != m.colType || f.notNull != m.notNull {
			t.Errorf("テーブル %s カラム[%d] が一致しません\nfresh:    %+v\nmigrated: %+v",
				table, i, f, m)
		}
	}
}

// TestMigrationReachesCurrentSchema：スキーマ整合性テストのメイン関数です。
//
// 2つのDBを比較します:
//  1. fresh:    currentSchema を直接適用したDB（新規インストール相当）
//  2. migrated: initialSchemaV1 から runMigrationsOn を適用したDB（既存ユーザー相当）
//
// 両者のカラム構造が一致すれば、マイグレーションが currentSchema と同期していると言えます。
func TestMigrationReachesCurrentSchema(t *testing.T) {
	// --- fresh DB: currentSchema をそのまま適用 ---
	fresh := openMigrationTestDB(t)
	for _, ddl := range currentSchema {
		if _, err := fresh.Exec(ddl); err != nil {
			t.Fatalf("currentSchema 適用エラー: %v\nSQL: %s", err, ddl)
		}
	}

	// --- migrated DB: v1スキーマ → 全マイグレーション適用 ---
	migrated := openMigrationTestDB(t)
	if _, err := migrated.Exec(initialSchemaV1); err != nil {
		t.Fatalf("v1スキーマ適用エラー: %v", err)
	}
	// runMigrationsOn は db.go で定義されており、
	// テストは同じパッケージ（package main）なのでそのまま呼べます。
	runMigrationsOn(migrated)

	// --- スキーマ比較 ---
	for _, table := range []string{"bookmarks", "tags", "bookmark_tags"} {
		t.Run(table, func(t *testing.T) {
			assertColumnsMatch(t, fresh, migrated, table)
		})
	}

	// url カラムに UNIQUE インデックスがあることも確認します。
	// currentSchema は url UNIQUE を持ち、runMigrationsOn も最終的に追加します。
	t.Run("url_unique_index", func(t *testing.T) {
		if !hasUniqueURLIndexOn(migrated) {
			t.Error("migrated DB の bookmarks.url に UNIQUE インデックスがありません")
		}
		if !hasUniqueURLIndexOn(fresh) {
			t.Error("fresh DB の bookmarks.url に UNIQUE インデックスがありません")
		}
	})
}
