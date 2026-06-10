package main

import (
	"crypto/rand"   // 暗号学的に安全な乱数を生成するパッケージ
	"crypto/subtle" // タイミング攻撃を防ぐ一定時間比較のためのパッケージ
	"database/sql"
	"encoding/hex"  // バイト列を16進数文字列に変換するパッケージ
	"encoding/json" // JSON形式を扱うためのパッケージ
	"embed"         // 静的ファイルをバイナリに埋め込むためのパッケージ
	"errors"        // エラーの種類を判定する errors.As のためのパッケージ
	"fmt"
	"context" // 処理のキャンセルやタイムアウトを伝えるためのパッケージ
	"io"      // io.Reader を扱うためのパッケージ
	"io/fs"   // ファイルシステムを抽象的に扱うためのパッケージ
	"log"
	"net"      // IPアドレスの判定や低レベルのネットワーク接続のためのパッケージ
	"net/http" // Webサーバー機能を提供するパッケージ
	"os"       // 環境変数を読み取るためのパッケージ
	"regexp"   // 正規表現でHTMLのメタタグを抽出するためのパッケージ
	"strconv"  // 文字列と数値を相互変換するためのパッケージ
	"strings"  // 文字列操作のためのパッケージ
	"sync"     // 複数のgoroutineから安全にデータを操作するためのパッケージ
	"time"

	// modernc.org/sqlite は純粋なGo言語で実装されたSQLiteドライバです。
	// CGO（C言語との連携）が不要なため、Alpine Linuxなどの軽量環境でも
	// そのまま動くシングルバイナリが作れます。
	// エラー型（sqlite.Error）を使うため、ブランクインポート（_）ではなく
	// 名前付きでインポートします。
	sqlite "modernc.org/sqlite"
	// SQLITE_CONSTRAINT_UNIQUE などのエラーコード定数が定義されているパッケージです。
	sqlite3 "modernc.org/sqlite/lib"
)

// //go:embed ディレクティブで static ディレクトリの中身をバイナリに埋め込みます。
// これにより、実行ファイル1つでHTMLなどの静的ファイルも配信できます。
//
//go:embed static
var staticFiles embed.FS

// Bookmark 構造体：ブックマークのデータをプログラム内で扱うための入れ物です。
// Shioriのスキーマに合わせたフィールド構成になっています。
// `json:"..."` という記述（タグ）は、JSON形式にする際の名前を指定しています。
type Bookmark struct {
	ID         int       `json:"id"`
	URL        string    `json:"url"`
	Title      string    `json:"title"`
	Excerpt    string    `json:"excerpt"`    // 本文の抜粋・メモ欄
	Author     string    `json:"author"`     // ページの著者
	Public     int       `json:"public"`     // 公開フラグ（0:非公開 1:公開）
	HasContent bool      `json:"has_content"` // 本文キャッシュの有無
	ImageURL   string    `json:"image_url"`  // サムネイル画像URL
	CreatedAt  time.Time  `json:"created_at"`
	ModifiedAt *time.Time `json:"modified_at"` // 最終更新日時（未更新の場合はnull）
	Tags       []Tag      `json:"tags"`        // 紐付いているタグの一覧
}

// Tag 構造体：タグのデータをプログラム内で扱うための入れ物です。
type Tag struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// データベースの接続を保持するグローバル変数です。
var db *sql.DB

// sessions：ログイン中のセッショントークンと有効期限を管理するマップです。
// 複数のリクエストが同時にアクセスしても安全なよう sync.Mutex で保護します。
var (
	sessions   = map[string]time.Time{} // token -> 有効期限
	sessionsMu sync.Mutex
)

func main() {
	var err error
	// 1. データベースファイル（shirushi.db）を開きます。
	//
	// SQLite は外部キー制約がデフォルト無効のため、DSN（接続文字列）の
	// _pragma=foreign_keys(1) で有効化します。
	// db.Exec("PRAGMA ...") で実行する方法だと、database/sql が内部に持つ
	// コネクションプールのうち「その時使われた1本」にしか適用されません。
	// DSN で指定すれば、プールが新しいコネクションを開くたびに毎回適用されるため、
	// どのリクエストでも bookmark_tags の ON DELETE CASCADE が確実に動作します。
	db, err = sql.Open("sqlite", "./shirushi.db?_pragma=foreign_keys(1)")
	if err != nil {
		log.Fatal("データベース接続エラー:", err)
	}
	defer db.Close()

	// 2. テーブルが存在しない場合は作成します。
	createTable()
	// 3. 既存DBに新しいカラムを追加するマイグレーションを実行します。
	runMigrations()
	// 4. APIのルート（住所）と、それぞれの処理（関数）を紐付けます。
	// Go 1.22からの新機能で、"GET /..." のようにHTTPメソッドを指定できます。

	// 認証API（ミドルウェアの対象外）
	http.HandleFunc("POST /api/login", handleLogin)
	http.HandleFunc("POST /api/logout", handleLogout)

	// ブックマーク関連のAPI
	http.HandleFunc("GET /api/bookmarks", handleGetBookmarks)
	http.HandleFunc("POST /api/bookmarks", handleCreateBookmark)
	http.HandleFunc("PUT /api/bookmarks/{id}", handleUpdateBookmark)
	http.HandleFunc("DELETE /api/bookmarks/{id}", handleDeleteBookmark)
	http.HandleFunc("DELETE /api/bookmarks", handleBulkDeleteBookmarks)

	// タグ関連のAPI
	http.HandleFunc("GET /api/tags", handleGetTags)
	http.HandleFunc("POST /api/tags", handleCreateTag)
	http.HandleFunc("PUT /api/tags/{id}", handleUpdateTag)
	http.HandleFunc("DELETE /api/tags/{id}", handleDeleteTag)

	// ブックマークとタグの紐付けAPI
	http.HandleFunc("POST /api/bookmarks/{id}/tags", handleAddTagToBookmark)
	http.HandleFunc("DELETE /api/bookmarks/{id}/tags", handleRemoveTagFromBookmark)
	http.HandleFunc("POST /api/bookmarks/bulk/tags", handleBulkAddTags)
	http.HandleFunc("DELETE /api/bookmarks/bulk/tags", handleBulkRemoveTags)

	// URLからメタデータを取得するAPI
	http.HandleFunc("POST /api/fetch-metadata", handleFetchMetadata)

	// インポート・エクスポートAPI
	http.HandleFunc("GET /api/export", handleExport)
	http.HandleFunc("POST /api/import", handleImport)

	// static/ ディレクトリを埋め込みファイルシステムとして取り出します。
	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal("静的ファイルの読み込みエラー:", err)
	}
	http.Handle("/", http.FileServerFS(subFS))

	// 期限切れセッションの掃除をバックグラウンドで開始します。
	go cleanupExpiredSessions()

	fmt.Println("サーバーを起動しました: http://localhost:8181")
	fmt.Println("API一覧を確認する: http://localhost:8181/api/bookmarks")
	// 5. http.DefaultServeMux を authMiddleware でラップして全リクエストに認証を適用します。
	if err := http.ListenAndServe(":8181", authMiddleware(http.DefaultServeMux)); err != nil {
		log.Fatal("サーバー起動エラー:", err)
	}
}

// cleanupExpiredSessions：期限切れセッションを定期的に掃除するバックグラウンド処理です。
// 認証チェック時の削除だけでは「二度と使われないトークン」がマップに残り続けるため、
// 1時間ごとに全エントリを確認して期限切れを削除します。
// main から `go cleanupExpiredSessions()` と呼ぶことで、
// サーバー本体とは別のゴルーチン（軽量スレッド）として並行に動き続けます。
func cleanupExpiredSessions() {
	// time.Tick は指定間隔ごとに値が届くチャネルを返します。
	// for range で受け取ることで「1時間ごとに1回ループが回る」動きになります。
	for range time.Tick(1 * time.Hour) {
		now := time.Now()
		sessionsMu.Lock()
		for token, expiry := range sessions {
			if now.After(expiry) {
				delete(sessions, token)
			}
		}
		sessionsMu.Unlock()
	}
}

// authMiddleware：全リクエストに認証チェックを適用するミドルウェアです。
// ミドルウェアとは「ハンドラの前後に処理を挟む仕組み」のことです。
// http.Handler を受け取り、認証チェックを追加した新しい http.Handler を返します。
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ログイン・ログアウトAPIと静的ファイルは認証不要です。
		if r.URL.Path == "/api/login" ||
			r.URL.Path == "/api/logout" ||
			!strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		// Cookieからセッショントークンを取り出します。
		cookie, err := r.Cookie("session")
		if err != nil {
			// Cookieがない場合は401 Unauthorizedを返します。
			http.Error(w, "認証が必要です", http.StatusUnauthorized)
			return
		}

		// トークンが有効かどうかを確認します。
		sessionsMu.Lock()
		expiry, ok := sessions[cookie.Value]
		expired := ok && time.Now().After(expiry)
		if expired {
			// 期限切れのトークンは見つけた時点でマップから削除します。
			// 放置するとメモリに溜まり続けるためです。
			delete(sessions, cookie.Value)
		}
		sessionsMu.Unlock()

		if !ok || expired {
			// トークンが存在しない、または期限切れの場合は401を返します。
			http.Error(w, "セッションが無効です", http.StatusUnauthorized)
			return
		}

		// 認証OK：次のハンドラに処理を渡します。
		next.ServeHTTP(w, r)
	})
}

// handleLogin：パスワードを受け取り、正しければセッショントークンを発行するAPIです。
func handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "リクエスト解析エラー", http.StatusBadRequest)
		return
	}

	// 環境変数からパスワードを取得して照合します。
	// パスワードをコードに直接書かず環境変数にする理由は、
	// ソースコードをgitで管理しても漏洩しないようにするためです。
	correctPassword := os.Getenv("SHIRUSHI_PASSWORD")
	// パスワードの比較には subtle.ConstantTimeCompare を使います。
	// 通常の == や != は「先頭から比較して違いが見つかった時点で終了」するため、
	// 応答時間のわずかな差から正解のパスワードを1文字ずつ推測される
	// 「タイミング攻撃」の余地があります。この関数は内容に関わらず
	// 常に同じ時間で比較するため、その手がかりを与えません（一致すると1を返します）。
	if correctPassword == "" ||
		subtle.ConstantTimeCompare([]byte(body.Password), []byte(correctPassword)) != 1 {
		http.Error(w, "パスワードが違います", http.StatusUnauthorized)
		return
	}

	// crypto/rand で暗号学的に安全なランダムトークンを生成します。
	// math/rand と違い、予測不可能な値が生成されます。
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		http.Error(w, "トークン生成エラー", http.StatusInternalServerError)
		return
	}
	token := hex.EncodeToString(tokenBytes) // バイト列を16進数文字列に変換します。

	// セッションを保存します（有効期限は24時間）。
	sessionsMu.Lock()
	sessions[token] = time.Now().Add(24 * time.Hour)
	sessionsMu.Unlock()

	// Cookieにトークンをセットします。
	// HttpOnly: JavaScriptからCookieを読めなくする（XSS対策）
	// SameSite: 別サイトからのリクエストにCookieを送らない（CSRF対策）
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400, // 24時間（秒）
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleLogout：セッションを削除してログアウトするAPIです。
func handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		// セッションマップからトークンを削除します。
		sessionsMu.Lock()
		delete(sessions, cookie.Value)
		sessionsMu.Unlock()
	}

	// Cookieを即座に無効化します（MaxAge=-1 で削除）。
	http.SetCookie(w, &http.Cookie{
		Name:   "session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

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
	defer rows.Close()

	for rows.Next() {
		var seq, unique int
		var name, origin string
		var partial int
		rows.Scan(&seq, &name, &unique, &origin, &partial)
		// unique=1 かつ url カラムのインデックスを探します。
		if unique == 1 {
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
	}
	return false
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

// BookmarkListResponse：ブックマーク一覧APIのレスポンス形式です。
// ページネーションのために、ブックマーク本体と総件数をまとめて返します。
type BookmarkListResponse struct {
	Bookmarks []Bookmark `json:"bookmarks"` // 現在ページのブックマーク
	Total     int        `json:"total"`     // 絞り込み条件込みの総件数
}

// addOneMonth："YYYY-MM" 形式の文字列に1ヶ月加算して返します。
// 日付範囲の上限計算（その月の終わりまで含める）に使います。
// 例: "2024-03" → "2024-04"
func addOneMonth(yyyyMM string) string {
	t, err := time.Parse("2006-01", yyyyMM)
	if err != nil {
		return yyyyMM
	}
	return t.AddDate(0, 1, 0).Format("2006-01")
}

// handleGetBookmarks：登録されているブックマークを一覧で返すAPIです。
// クエリパラメータ:
//
//	?q=keyword         タイトル・URL・excerptで絞り込み検索
//	?tag=name          タグ名で絞り込み（"__untagged__" でタグなし絞り込み）
//	?date_from=YYYY-MM 登録日の開始月（例: 2024-01）
//	?date_to=YYYY-MM   登録日の終了月（例: 2024-03、その月末まで含む）
//	?page=N            ページ番号（デフォルト1、1始まり）
//	?limit=N           1ページあたりの件数（デフォルト50）
func handleGetBookmarks(w http.ResponseWriter, r *http.Request) {
	q        := strings.TrimSpace(r.URL.Query().Get("q"))
	tag      := strings.TrimSpace(r.URL.Query().Get("tag"))
	dateFrom := strings.TrimSpace(r.URL.Query().Get("date_from"))
	dateTo   := strings.TrimSpace(r.URL.Query().Get("date_to"))

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 1 {
		page = p
	}
	offset := (page - 1) * limit

	// "__untagged__" は「タグが1つも付いていないブックマーク」を表す特殊値です。
	const untaggedToken = "__untagged__"

	// ── WHERE句を動的に組み立てる ────────────────────────────────
	// 条件が増えても switch のケース数が爆発しないよう、
	// 条件文字列を slice に積み上げて最後に JOIN します。
	//
	// タグ絞り込みが必要な場合だけ JOIN を追加します。
	fromClause := "FROM bookmarks b"
	var conditions []string
	var args []interface{}

	// タグ条件
	if tag == untaggedToken {
		// NOT EXISTS：bookmark_tags に1件も紐付きがないブックマーク
		conditions = append(conditions,
			"NOT EXISTS (SELECT 1 FROM bookmark_tags bt WHERE bt.bookmark_id = b.id)")
	} else if tag != "" {
		// INNER JOIN でタグに紐付くブックマークだけに絞ります。
		// DISTINCT は JOIN で行が増えても重複カウントしないために必要です。
		fromClause += " INNER JOIN bookmark_tags bt ON b.id = bt.bookmark_id" +
			" INNER JOIN tags t ON t.id = bt.tag_id"
		conditions = append(conditions, "t.name = ?")
		args = append(args, tag)
	}

	// キーワード条件
	if q != "" {
		like := "%" + q + "%"

		// 日付パターン検出：
		//   6桁の数字 "202507" → created_at LIKE "2025-07%"（7月全体）
		//   4桁の数字 "2025"   → created_at LIKE "2025%"  （2025年全体）
		// strftime は保存フォーマットによって動作しないことがあるため、
		// 文字列の先頭を直接 LIKE で比較する方式にしています。
		dateLike := ""
		if _, err := strconv.Atoi(q); err == nil {
			switch len(q) {
			case 6: // YYYYMM → "YYYY-MM%"
				dateLike = q[:4] + "-" + q[4:6] + "%"
			case 4: // YYYY   → "YYYY%"
				dateLike = q + "%"
			}
		}

		if dateLike != "" {
			conditions = append(conditions,
				"(b.title LIKE ? OR b.url LIKE ? OR b.excerpt LIKE ? OR b.created_at LIKE ?)")
			args = append(args, like, like, like, dateLike)
		} else {
			conditions = append(conditions,
				"(b.title LIKE ? OR b.url LIKE ? OR b.excerpt LIKE ?)")
			args = append(args, like, like, like)
		}
	}

	// 日付条件（YYYY-MM → SQL の DATETIME と比較）
	// date_from: その月の1日 00:00:00 以降
	if dateFrom != "" {
		conditions = append(conditions, "b.created_at >= ?")
		args = append(args, dateFrom+"-01 00:00:00")
	}
	// date_to: 翌月の1日 00:00:00 未満（= その月末まで含む）
	if dateTo != "" {
		conditions = append(conditions, "b.created_at < ?")
		args = append(args, addOneMonth(dateTo)+"-01 00:00:00")
	}

	// WHERE句：条件がある場合のみ付けます。
	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// ── ① 総件数を取得 ──────────────────────────────────────────
	// DISTINCT b.id：タグJOINで行が増えても重複カウントを防ぎます。
	countSQL := fmt.Sprintf("SELECT COUNT(DISTINCT b.id) %s %s", fromClause, whereClause)
	var total int
	if err := db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		http.Error(w, "件数取得エラー", http.StatusInternalServerError)
		return
	}

	// ── ② ブックマークをページ単位で取得 ─────────────────────────
	// DISTINCT：タグJOINで同じブックマークが複数行になるのを防ぎます。
	dataSQL := fmt.Sprintf(`
		SELECT DISTINCT b.id, b.url, b.title, b.excerpt, b.author,
		       b.public, b.has_content, b.image_url, b.created_at, b.modified_at
		%s %s
		ORDER BY b.id DESC
		LIMIT ? OFFSET ?`, fromClause, whereClause)

	// LIMIT / OFFSET は args の後ろに追加します。
	dataArgs := append(args, limit, offset)
	rows, err := db.Query(dataSQL, dataArgs...)
	if err != nil {
		http.Error(w, "データベースエラー", http.StatusInternalServerError)
		return
	}

	// rowsが開いたまま別のクエリを実行するとSQLiteでエラーになることがあるため、
	// まず全ブックマークをスライスに読み出してからrowsを閉じます。
	var bookmarks []Bookmark
	for rows.Next() {
		var b Bookmark
		if err := rows.Scan(
			&b.ID, &b.URL, &b.Title, &b.Excerpt, &b.Author,
			&b.Public, &b.HasContent, &b.ImageURL, &b.CreatedAt, &b.ModifiedAt,
		); err != nil {
			rows.Close()
			http.Error(w, "読み取りエラー", http.StatusInternalServerError)
			return
		}
		bookmarks = append(bookmarks, b)
	}
	rows.Close()

	// ── ③ タグを2クエリ固定で一括取得 ───────────────────────────
	// N+1クエリ問題を避けるため、対象ブックマークのIDをまとめてINで渡します。
	if len(bookmarks) > 0 {
		ids := make([]int, len(bookmarks))
		for i, b := range bookmarks {
			ids[i] = b.ID
		}

		// map[bookmarkID] = bookmarksスライスの添字（O(1) で位置を引くため）
		idxByID := make(map[int]int, len(bookmarks))
		for i, b := range bookmarks {
			idxByID[b.ID] = i
		}

		placeholders := strings.Repeat("?,", len(ids))
		placeholders = placeholders[:len(placeholders)-1]

		args := make([]interface{}, len(ids))
		for i, id := range ids {
			args[i] = id
		}

		tagRows, err := db.Query(fmt.Sprintf(`
			SELECT bt.bookmark_id, t.id, t.name
			FROM tags t
			INNER JOIN bookmark_tags bt ON t.id = bt.tag_id
			WHERE bt.bookmark_id IN (%s)
			ORDER BY t.name ASC`, placeholders), args...)
		if err != nil {
			http.Error(w, "タグ取得エラー", http.StatusInternalServerError)
			return
		}
		defer tagRows.Close()

		for tagRows.Next() {
			var bookmarkID int
			var tag Tag
			if err := tagRows.Scan(&bookmarkID, &tag.ID, &tag.Name); err != nil {
				http.Error(w, "タグ読み取りエラー", http.StatusInternalServerError)
				return
			}
			if idx, ok := idxByID[bookmarkID]; ok {
				bookmarks[idx].Tags = append(bookmarks[idx].Tags, tag)
			}
		}
	}

	// nil スライスをそのままJSONにすると null になるため、空スライスで初期化します。
	if bookmarks == nil {
		bookmarks = []Bookmark{}
	}

	// ── ④ レスポンス ─────────────────────────────────────────────
	// bookmarks（現在ページ分）と total（全件数）をまとめて返します。
	// フロントエンドはこれを使ってページ数とナビゲーションを計算します。
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(BookmarkListResponse{
		Bookmarks: bookmarks,
		Total:     total,
	})
}

// syncBookmarkTags：ブックマークのタグ紐付けを同期するヘルパー関数です。
// 既存の紐付けを全削除してから、新しいタグを挿入します（置き換え方式）。
// タグが空リストの場合は全削除のみ行います。
func syncBookmarkTags(bookmarkID int, tags []Tag) error {
	// 既存の紐付けをすべて削除します。
	if _, err := db.Exec("DELETE FROM bookmark_tags WHERE bookmark_id = ?", bookmarkID); err != nil {
		return err
	}
	// 新しいタグを挿入します。
	for _, t := range tags {
		if t.ID == 0 {
			continue // IDが指定されていないタグはスキップします。
		}
		_, err := db.Exec(
			"INSERT OR IGNORE INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)",
			bookmarkID, t.ID,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// getTagsByBookmarkID：指定したブックマークIDに紐付いたタグを取得するヘルパー関数です。
// 複数のハンドラから使い回せるように独立した関数にしています。
func getTagsByBookmarkID(bookmarkID int) ([]Tag, error) {
	rows, err := db.Query(`
		SELECT t.id, t.name
		FROM tags t
		INNER JOIN bookmark_tags bt ON t.id = bt.tag_id
		WHERE bt.bookmark_id = ?
		ORDER BY t.name ASC`, bookmarkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// タグが0件の場合も空配列（nullではなく[]）で返すために初期化します。
	tags := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, nil
}

// getBookmarkByID：指定したIDのブックマークをタグ込みでDBから取得するヘルパー関数です。
// 作成・更新APIのレスポンスに使います。INSERT/UPDATE のあとDBから読み直すことで、
// created_at / modified_at に「DBに実際に保存された値」を正確に返せます。
// （Go側で time.Now() をセットすると、DBの CURRENT_TIMESTAMP（UTC）と
// 　タイムゾーンや秒数がズレた「嘘の値」を返してしまうため）
func getBookmarkByID(id int) (*Bookmark, error) {
	var b Bookmark
	err := db.QueryRow(`
		SELECT id, url, title, excerpt, author,
		       public, has_content, image_url, created_at, modified_at
		FROM bookmarks WHERE id = ?`, id).Scan(
		&b.ID, &b.URL, &b.Title, &b.Excerpt, &b.Author,
		&b.Public, &b.HasContent, &b.ImageURL, &b.CreatedAt, &b.ModifiedAt,
	)
	if err != nil {
		return nil, err
	}
	// タグも一緒に取得してセットします。
	b.Tags, err = getTagsByBookmarkID(id)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// isUniqueConstraintError：エラーが UNIQUE 制約違反かどうかを判定するヘルパー関数です。
// errors.As は、エラーが特定の型（ここでは *sqlite.Error）かどうかを調べ、
// そうであれば中身を取り出してくれる標準の仕組みです。
// SQLite はエラーの種類を数値コードで表し、UNIQUE 制約違反は
// SQLITE_CONSTRAINT_UNIQUE（2067）という拡張コードになります。
func isUniqueConstraintError(err error) bool {
	var serr *sqlite.Error
	if errors.As(err, &serr) {
		return serr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
	}
	return false
}

// handleCreateBookmark：新しいブックマークを登録するAPIです。
// リクエストボディの tags フィールドにタグIDのリストを含めると紐付けも行います。
// 例: {"url":"...","title":"...","tags":[{"id":1},{"id":2}]}
func handleCreateBookmark(w http.ResponseWriter, r *http.Request) {
	var b Bookmark
	// ブラウザから送られてきたJSONを読み取り、構造体に変換します。
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "リクエスト解析エラー", http.StatusBadRequest)
		return
	}
	// URLが空の場合はエラーにします。
	if b.URL == "" {
		http.Error(w, "URLは必須です", http.StatusBadRequest)
		return
	}
	// データベースに保存します。
	query := `INSERT INTO bookmarks (url, title, excerpt, author, image_url) VALUES (?, ?, ?, ?, ?);`
	result, err := db.Exec(query, b.URL, b.Title, b.Excerpt, b.Author, b.ImageURL)
	if err != nil {
		// url カラムには UNIQUE 制約があるため、登録済みのURLを再登録しようとすると
		// 制約違反エラーになります。これはサーバーの障害ではなく「リクエスト内容の競合」
		// なので、409 Conflict と分かりやすいメッセージを返します。
		if isUniqueConstraintError(err) {
			http.Error(w, "このURLは既に登録されています", http.StatusConflict)
			return
		}
		http.Error(w, "保存エラー", http.StatusInternalServerError)
		return
	}
	// 自動採番されたIDを取得してセットします。
	id, _ := result.LastInsertId()
	b.ID = int(id)

	// タグが指定されていれば bookmark_tags に紐付けを保存します。
	if err := syncBookmarkTags(b.ID, b.Tags); err != nil {
		http.Error(w, "タグ保存エラー", http.StatusInternalServerError)
		return
	}

	// DBから読み直して「実際に保存された値」をレスポンスとして返します。
	// created_at はDBの CURRENT_TIMESTAMP が入り、modified_at は未更新なので
	// NULL（JSONでは null）になります。
	created, err := getBookmarkByID(b.ID)
	if err != nil {
		http.Error(w, "登録データ取得エラー", http.StatusInternalServerError)
		return
	}

	// 成功（201 Created）を返し、登録されたデータをJSONで返信します。
	// ※ WriteHeader より前に Header().Set() を呼ぶ必要があります。
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated) // 201 Created
	json.NewEncoder(w).Encode(created)
}

// handleUpdateBookmark：既存のブックマークを更新するAPIです。
func handleUpdateBookmark(w http.ResponseWriter, r *http.Request) {
	// r.PathValue は Go 1.22 の新機能です。
	// URLパターン "{id}" に対応する部分の文字列を取り出します。
	idStr := r.PathValue("id")

	// URLから取得した文字列は "123" のような文字列なので、
	// strconv.Atoi で整数に変換します。変換できなければ不正なリクエストです。
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "IDが不正です", http.StatusBadRequest)
		return
	}

	// リクエストボディのJSONを読み取り、構造体に変換します。
	var b Bookmark
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "リクエスト解析エラー", http.StatusBadRequest)
		return
	}
	if b.URL == "" {
		http.Error(w, "URLは必須です", http.StatusBadRequest)
		return
	}

	// UPDATE文で該当IDのレコードを更新します。
	// modified_at は更新のたびに現在時刻をセットします。
	query := `
		UPDATE bookmarks
		SET url = ?, title = ?, excerpt = ?, author = ?, image_url = ?,
		    modified_at = CURRENT_TIMESTAMP
		WHERE id = ?;`
	result, err := db.Exec(query, b.URL, b.Title, b.Excerpt, b.Author, b.ImageURL, id)
	if err != nil {
		// 更新でも、URLを「別のブックマークが既に使っているURL」に変更しようとすると
		// UNIQUE 制約違反になるため、作成時と同じく 409 Conflict を返します。
		if isUniqueConstraintError(err) {
			http.Error(w, "このURLは既に別のブックマークで登録されています", http.StatusConflict)
			return
		}
		http.Error(w, "更新エラー", http.StatusInternalServerError)
		return
	}

	// RowsAffected で実際に更新された行数を確認します。
	// 0 件の場合は、指定したIDが存在しないことを意味します。
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "指定されたIDが見つかりません", http.StatusNotFound)
		return
	}

	// タグの更新は tags フィールドが送られてきた場合のみ行います。
	// Goの encoding/json では、JSONにキーが存在しない場合スライスは nil のまま、
	// "tags": [] と明示された場合は「空のスライス」になります。
	// この違いを利用して「省略＝変更しない」「空配列＝全削除」を区別します。
	// （CLIなど将来のクライアントがタイトルだけ更新したいとき、
	// 　tags を送り忘れてタグが全消えする事故を防ぎます）
	if b.Tags != nil {
		if err := syncBookmarkTags(id, b.Tags); err != nil {
			http.Error(w, "タグ保存エラー", http.StatusInternalServerError)
			return
		}
	}

	// DBから読み直して「実際に保存された値」をレスポンスとして返します。
	// これにより created_at（リクエストには含まれないためゼロ値だった）と
	// modified_at（DBの CURRENT_TIMESTAMP）の両方が正確な値になります。
	updated, err := getBookmarkByID(id)
	if err != nil {
		http.Error(w, "更新データ取得エラー", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
}

// handleDeleteBookmark：指定されたIDのブックマークを削除するAPIです。
func handleDeleteBookmark(w http.ResponseWriter, r *http.Request) {
	// URLから "{id}" の部分を文字列として取り出します。
	idStr := r.PathValue("id")

	// 文字列を整数に変換します。変換できなければ不正なリクエストです。
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "IDが不正です", http.StatusBadRequest)
		return
	}

	// DELETE文で該当IDのレコードを削除します。
	result, err := db.Exec("DELETE FROM bookmarks WHERE id = ?", id)
	if err != nil {
		http.Error(w, "削除エラー", http.StatusInternalServerError)
		return
	}

	// 実際に削除された行数を確認します。0件なら指定IDが存在しません。
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "指定されたIDが見つかりません", http.StatusNotFound)
		return
	}

	// 削除成功は204 No Content（返すデータなし）が REST の慣習です。
	w.WriteHeader(http.StatusNoContent)
}

// handleBulkAddTags：複数のブックマークに複数のタグをまとめて付与するAPIです。
// リクエストボディ: {"bookmark_ids": [1,2,3], "tag_ids": [10,11]}
// すでに紐付いているものは INSERT OR IGNORE でスキップします。
func handleBulkAddTags(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookmarkIDs []int `json:"bookmark_ids"`
		TagIDs      []int `json:"tag_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		len(req.BookmarkIDs) == 0 || len(req.TagIDs) == 0 {
		http.Error(w, "bookmark_ids と tag_ids が必要です", http.StatusBadRequest)
		return
	}

	// トランザクション：全ての挿入を1まとめにします。
	// 途中でエラーが起きた場合は全件ロールバックされます。
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "トランザクション開始エラー", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback() // commit前にreturnした場合の安全策

	// ブックマークID × タグID の全組み合わせを挿入します。
	// INSERT OR IGNORE はすでに存在するペアを静かにスキップします。
	stmt, err := tx.Prepare("INSERT OR IGNORE INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)")
	if err != nil {
		http.Error(w, "クエリ準備エラー", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	for _, bID := range req.BookmarkIDs {
		for _, tID := range req.TagIDs {
			if _, err := stmt.Exec(bID, tID); err != nil {
				http.Error(w, "タグ追加エラー", http.StatusInternalServerError)
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "コミットエラー", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleBulkRemoveTags：複数のブックマークから複数のタグをまとめて削除するAPIです。
// リクエストボディ: {"bookmark_ids": [1,2,3], "tag_ids": [10,11]}
func handleBulkRemoveTags(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookmarkIDs []int `json:"bookmark_ids"`
		TagIDs      []int `json:"tag_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		len(req.BookmarkIDs) == 0 || len(req.TagIDs) == 0 {
		http.Error(w, "bookmark_ids と tag_ids が必要です", http.StatusBadRequest)
		return
	}

	// IN句のプレースホルダーを bookmark_ids 分・tag_ids 分それぞれ生成します。
	bPlaceholders := strings.Repeat("?,", len(req.BookmarkIDs))
	bPlaceholders = bPlaceholders[:len(bPlaceholders)-1]
	tPlaceholders := strings.Repeat("?,", len(req.TagIDs))
	tPlaceholders = tPlaceholders[:len(tPlaceholders)-1]

	// []int を []interface{} に変換してクエリ引数としてまとめます。
	args := make([]interface{}, 0, len(req.BookmarkIDs)+len(req.TagIDs))
	for _, id := range req.BookmarkIDs {
		args = append(args, id)
	}
	for _, id := range req.TagIDs {
		args = append(args, id)
	}

	// bookmark_id と tag_id の両方が一致する行をまとめて削除します。
	_, err := db.Exec(
		fmt.Sprintf(
			"DELETE FROM bookmark_tags WHERE bookmark_id IN (%s) AND tag_id IN (%s)",
			bPlaceholders, tPlaceholders,
		),
		args...,
	)
	if err != nil {
		http.Error(w, "タグ削除エラー", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleBulkDeleteBookmarks：複数のブックマークをまとめて削除するAPIです。
// リクエストボディ: {"ids": [1, 2, 3]}
func handleBulkDeleteBookmarks(w http.ResponseWriter, r *http.Request) {
	// リクエストボディを構造体に読み込みます。
	var req struct {
		IDs []int `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		http.Error(w, "IDリストが不正です", http.StatusBadRequest)
		return
	}

	// IN句のプレースホルダーを動的に生成します。
	// 例: IDs=[1,2,3] → "?,?,?"
	placeholders := strings.Repeat("?,", len(req.IDs))
	placeholders = placeholders[:len(placeholders)-1]

	// []int を []interface{} に変換してクエリに渡します。
	// database/sql はインターフェース型のスライスを要求するためです。
	args := make([]interface{}, len(req.IDs))
	for i, id := range req.IDs {
		args[i] = id
	}

	// 1回のDELETE文でまとめて削除します。
	// bookmark_tagsの紐付けはON DELETE CASCADEで自動削除されます。
	result, err := db.Exec(
		fmt.Sprintf("DELETE FROM bookmarks WHERE id IN (%s)", placeholders),
		args...,
	)
	if err != nil {
		http.Error(w, "削除エラー", http.StatusInternalServerError)
		return
	}

	deleted, _ := result.RowsAffected()
	// 削除件数をJSONで返します。
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"deleted": int(deleted)})
}

// Metadata 構造体：URLから取得したメタデータを表します。
type Metadata struct {
	Title    string `json:"title"`
	Excerpt  string `json:"excerpt"`
	Author   string `json:"author"`
	ImageURL string `json:"image_url"`
}

// handleFetchMetadata：指定されたURLにアクセスしてメタデータを返すAPIです。
func handleFetchMetadata(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		http.Error(w, "URLは必須です", http.StatusBadRequest)
		return
	}

	meta, err := fetchMetadata(body.URL)
	if err != nil {
		http.Error(w, "メタデータの取得に失敗しました", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(meta)
}

// isPrivateIP：プライベート・内部ネットワーク向けのIPアドレスかどうかを判定します。
// SSRF（Server-Side Request Forgery）対策に使います。
// SSRFとは「サーバーに内部ネットワークへのアクセスを代行させる攻撃」のことで、
// 例えば http://192.168.1.1/ を渡してルーターの管理画面を取得させる、といった悪用です。
func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || // 127.0.0.1 など自分自身
		ip.IsPrivate() || // 10.x / 172.16-31.x / 192.168.x のLAN内アドレス
		ip.IsLinkLocalUnicast() || // 169.254.x（クラウドのメタデータAPIで悪用されがち）
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() // 0.0.0.0
}

// safeDialContext：接続先のIPを検査してから接続する、安全なダイヤル関数です。
// URLの文字列ではなく「実際に接続する瞬間のIP」を検査するのがポイントで、
// これによりリダイレクトやDNSの再解決を使ったすり抜けも防げます。
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	// addr は "example.com:443" のような形式なので、ホスト名とポートに分けます。
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	// ホスト名をIPアドレスに解決します（既にIPならそのまま返ります）。
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return nil, errors.New("内部ネットワークへのアクセスは禁止されています: " + ip.String())
		}
	}

	// 検査をパスした解決済みIPに対して直接接続します。
	// ホスト名のまま接続すると、接続時にDNSが再解決されて
	// 別の（内部の）IPに繋がる恐れがあるためです。
	var d net.Dialer
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

// fetchMetadata：URLにHTTPアクセスしてHTMLからメタデータを抽出する関数です。
func fetchMetadata(url string) (*Metadata, error) {
	// 10秒でタイムアウトするHTTPクライアントを作ります。
	// デフォルトのクライアントはタイムアウトがないため、自前で設定するのが定石です。
	client := &http.Client{Timeout: 10 * time.Second}

	// SSRF対策：接続先のIPを検査するダイヤル関数を組み込みます。
	// 自宅LAN内のサーバーのメタデータを取得したい場合は、
	// 環境変数 SHIRUSHI_ALLOW_PRIVATE_FETCH=1 を設定すると検査を無効化できます。
	if os.Getenv("SHIRUSHI_ALLOW_PRIVATE_FETCH") != "1" {
		client.Transport = &http.Transport{DialContext: safeDialContext}
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	// User-Agentを設定しないとアクセスを弾くサイトがあるため設定します。
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Shirushi/1.0)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// HTMLが大きいサイトでも安全に処理できるよう、最大1MBだけ読み込みます。
	// メタタグは通常 <head> 内にあるので先頭部分で十分です。
	limitedBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return nil, err
	}
	html := string(limitedBody)

	meta := &Metadata{}
	// OGタグを優先し、なければ通常のメタタグ・titleタグを使います。
	meta.Title    = firstNonEmpty(extractOGTag(html, "og:title"),    extractTitle(html))
	meta.Excerpt  = firstNonEmpty(extractOGTag(html, "og:description"), extractMetaTag(html, "description"))
	meta.Author   = firstNonEmpty(extractOGTag(html, "og:author"),   extractMetaTag(html, "author"))
	meta.ImageURL = extractOGTag(html, "og:image")

	return meta, nil
}

// extractTitle：HTMLの <title> タグからテキストを取り出します。
func extractTitle(html string) string {
	// (?i) は大文字小文字を区別しないオプションです。
	re := regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
	if m := re.FindStringSubmatch(html); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// extractOGTag：Open Graph プロトコルのメタタグから content を取り出します。
// <meta property="og:title" content="..."> のような形式を対象にします。
func extractOGTag(html, property string) string {
	// property と content の順序が逆でも対応できるよう2パターン用意します。
	patterns := []string{
		`(?i)<meta[^>]*property=["\']` + regexp.QuoteMeta(property) + `["\'][^>]*content=["\']([^"\']+)["\']`,
		`(?i)<meta[^>]*content=["\']([^"\']+)["\'][^>]*property=["\']` + regexp.QuoteMeta(property) + `["\']`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// extractMetaTag：通常の <meta name="..." content="..."> からcontent を取り出します。
func extractMetaTag(html, name string) string {
	patterns := []string{
		`(?i)<meta[^>]*name=["\']` + regexp.QuoteMeta(name) + `["\'][^>]*content=["\']([^"\']+)["\']`,
		`(?i)<meta[^>]*content=["\']([^"\']+)["\'][^>]*name=["\']` + regexp.QuoteMeta(name) + `["\']`,
	}
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		if m := re.FindStringSubmatch(html); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}

// firstNonEmpty：引数の中で最初の空でない文字列を返します。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// handleAddTagToBookmark：ブックマークにタグを紐付けるAPIです。
// リクエストボディ: {"tag_id": 1}
func handleAddTagToBookmark(w http.ResponseWriter, r *http.Request) {
	bookmarkID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "IDが不正です", http.StatusBadRequest)
		return
	}

	// リクエストボディからtag_idを取り出します。
	var body struct {
		TagID int `json:"tag_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TagID == 0 {
		http.Error(w, "tag_idは必須です", http.StatusBadRequest)
		return
	}

	// INSERT OR IGNORE は同じ組み合わせが既に存在する場合は何もしません。
	// これにより「同じタグを二重に付ける」エラーを防げます。
	_, err = db.Exec(
		"INSERT OR IGNORE INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)",
		bookmarkID, body.TagID,
	)
	if err != nil {
		http.Error(w, "タグ追加エラー", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRemoveTagFromBookmark：ブックマークからタグの紐付けを外すAPIです。
// リクエストボディ: {"tag_id": 1}
func handleRemoveTagFromBookmark(w http.ResponseWriter, r *http.Request) {
	bookmarkID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "IDが不正です", http.StatusBadRequest)
		return
	}

	var body struct {
		TagID int `json:"tag_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TagID == 0 {
		http.Error(w, "tag_idは必須です", http.StatusBadRequest)
		return
	}

	_, err = db.Exec(
		"DELETE FROM bookmark_tags WHERE bookmark_id = ? AND tag_id = ?",
		bookmarkID, body.TagID,
	)
	if err != nil {
		http.Error(w, "タグ削除エラー", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleGetTags：タグ一覧を返すAPIです。
// デフォルトはブックマークに使われているタグのみ返します。
// ?all=1 を付けると未使用タグも含めた全タグを返します（タグ管理画面用）。
func handleGetTags(w http.ResponseWriter, r *http.Request) {
	var rows *sql.Rows
	var err error

	if r.URL.Query().Get("all") == "1" {
		// 全タグ（未使用含む）を返します。
		rows, err = db.Query(`SELECT id, name FROM tags ORDER BY name ASC`)
	} else {
		// 使用中のタグのみ返します（フィルターチップ・オートコンプリート用）。
		rows, err = db.Query(`
			SELECT DISTINCT t.id, t.name
			FROM tags t
			INNER JOIN bookmark_tags bt ON t.id = bt.tag_id
			ORDER BY t.name ASC`)
	}
	if err != nil {
		http.Error(w, "データベースエラー", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			http.Error(w, "読み取りエラー", http.StatusInternalServerError)
			return
		}
		tags = append(tags, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tags)
}

// handleCreateTag：新しいタグを登録するAPIです。
// 同名タグが既に存在する場合は新規作成せず、既存タグをそのまま返します。
// これにより、未使用タグ（フィルター候補に出ない）と同名のタグを
// 追加しようとしても競合エラーにならず、正しいIDが取得できます。
func handleCreateTag(w http.ResponseWriter, r *http.Request) {
	var t Tag
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		http.Error(w, "リクエスト解析エラー", http.StatusBadRequest)
		return
	}
	if t.Name == "" {
		http.Error(w, "タグ名は必須です", http.StatusBadRequest)
		return
	}

	// INSERT OR IGNORE：同名タグが既にあれば何もしない（エラーにしない）。
	// その後 SELECT で必ず正しい ID を取得します。
	// 新規作成でも既存タグの再利用でも、常に同じ処理で対応できます。
	if _, err := db.Exec("INSERT OR IGNORE INTO tags (name) VALUES (?)", t.Name); err != nil {
		http.Error(w, "タグ保存エラー", http.StatusInternalServerError)
		return
	}
	if err := db.QueryRow("SELECT id, name FROM tags WHERE name = ?", t.Name).Scan(&t.ID, &t.Name); err != nil {
		http.Error(w, "タグ取得エラー", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

// handleUpdateTag：既存のタグ名を更新するAPIです。
func handleUpdateTag(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "IDが不正です", http.StatusBadRequest)
		return
	}

	var t Tag
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		http.Error(w, "リクエスト解析エラー", http.StatusBadRequest)
		return
	}
	if t.Name == "" {
		http.Error(w, "タグ名は必須です", http.StatusBadRequest)
		return
	}

	result, err := db.Exec("UPDATE tags SET name = ? WHERE id = ?", t.Name, id)
	if err != nil {
		http.Error(w, "更新エラー", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "指定されたIDが見つかりません", http.StatusNotFound)
		return
	}

	t.ID = id
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

// handleDeleteTag：指定されたIDのタグを削除するAPIです。
// bookmark_tags テーブルの関連レコードはFOREIGN KEY ON DELETE CASCADEで自動削除されます。
func handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "IDが不正です", http.StatusBadRequest)
		return
	}

	result, err := db.Exec("DELETE FROM tags WHERE id = ?", id)
	if err != nil {
		http.Error(w, "削除エラー", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "指定されたIDが見つかりません", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleExport：全ブックマークをNetscape Bookmark形式のHTMLとして返すAPIです。
// ブラウザの「ブックマークをエクスポート」と同じ形式なので、他のアプリにインポートできます。
func handleExport(w http.ResponseWriter, r *http.Request) {
	// タグを含む全ブックマークを取得します。
	rows, err := db.Query(`
		SELECT id, url, title, excerpt, created_at
		FROM bookmarks
		ORDER BY id ASC`)
	if err != nil {
		http.Error(w, "データベースエラー", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type exportItem struct {
		ID        int
		URL       string
		Title     string
		Excerpt   string
		CreatedAt time.Time
		Tags      []Tag
	}

	var items []exportItem
	for rows.Next() {
		var item exportItem
		if err := rows.Scan(&item.ID, &item.URL, &item.Title, &item.Excerpt, &item.CreatedAt); err != nil {
			http.Error(w, "読み取りエラー", http.StatusInternalServerError)
			return
		}
		items = append(items, item)
	}
	rows.Close()

	// タグを取得します。
	for i := range items {
		items[i].Tags, err = getTagsByBookmarkID(items[i].ID)
		if err != nil {
			http.Error(w, "タグ取得エラー", http.StatusInternalServerError)
			return
		}
	}

	// Netscape Bookmark形式のHTMLを生成します。
	// fmt.Fprintf でレスポンスに直接書き込むことで、メモリ効率よく処理できます。
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	// Content-Disposition でブラウザにファイルとしてダウンロードさせます。
	w.Header().Set("Content-Disposition", `attachment; filename="shirushi-bookmarks.html"`)

	fmt.Fprintln(w, `<!DOCTYPE NETSCAPE-Bookmark-file-1>`)
	fmt.Fprintln(w, `<!-- Shirushi によってエクスポートされたブックマーク -->`)
	fmt.Fprintln(w, `<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">`)
	fmt.Fprintln(w, `<TITLE>Bookmarks</TITLE>`)
	fmt.Fprintln(w, `<H1>Bookmarks</H1>`)
	fmt.Fprintln(w, `<DL><p>`)

	for _, item := range items {
		// タグをカンマ区切りの文字列に変換します。
		tagNames := make([]string, len(item.Tags))
		for i, t := range item.Tags {
			tagNames[i] = t.Name
		}
		tags := strings.Join(tagNames, ",")

		// ADD_DATE は Unix タイムスタンプ（秒）です。
		addDate := item.CreatedAt.Unix()

		fmt.Fprintf(w, `    <DT><A HREF="%s" ADD_DATE="%d" TAGS="%s">%s</A>`+"\n",
			item.URL, addDate, tags, item.Title)

		// excerptがある場合は <DD> タグで説明文を追加します。
		if item.Excerpt != "" {
			fmt.Fprintf(w, `    <DD>%s`+"\n", item.Excerpt)
		}
	}

	fmt.Fprintln(w, `</DL><p>`)
}

// handleImport：Netscape Bookmark形式のHTMLファイルを読み込んでDBに登録するAPIです。
func handleImport(w http.ResponseWriter, r *http.Request) {
	// multipart/form-data 形式でファイルを受け取ります。
	// ParseMultipartForm の引数は最大メモリ使用量（バイト）です。
	if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB
		http.Error(w, "ファイルの解析に失敗しました", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "ファイルが見つかりません", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// ファイルの内容をすべて読み込みます。
	content, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "ファイルの読み込みに失敗しました", http.StatusInternalServerError)
		return
	}
	html := string(content)

	// <DT><A ...> のパターンでブックマークを抽出します。
	// (?s) は . が改行にもマッチするオプションです。
	reBookmark := regexp.MustCompile(`(?i)<DT><A\s([^>]+)>([^<]*)</A>`)
	reHref    := regexp.MustCompile(`(?i)HREF="([^"]+)"`)
	reAddDate := regexp.MustCompile(`(?i)ADD_DATE="([^"]+)"`)
	reTags    := regexp.MustCompile(`(?i)TAGS="([^"]*)"`)
	// <DD> タグで説明文を取得します。
	reDD := regexp.MustCompile(`(?i)<DD>([^\n<]+)`)

	matches := reBookmark.FindAllStringSubmatchIndex(html, -1)

	imported := 0
	skipped  := 0

	for _, matchIdx := range matches {
		// matchIdx[2],matchIdx[3] が属性部分、matchIdx[4],matchIdx[5] がタイトルです。
		attrs := html[matchIdx[2]:matchIdx[3]]
		title := strings.TrimSpace(html[matchIdx[4]:matchIdx[5]])

		hrefMatch := reHref.FindStringSubmatch(attrs)
		if len(hrefMatch) < 2 {
			continue
		}
		url := hrefMatch[1]

		// ADD_DATE（Unixタイムスタンプ）を time.Time に変換します。
		var createdAt time.Time
		if m := reAddDate.FindStringSubmatch(attrs); len(m) > 1 {
			if ts, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				createdAt = time.Unix(ts, 0)
			}
		}
		if createdAt.IsZero() {
			createdAt = time.Now()
		}

		// タグ名をカンマで分割します。
		var tagNames []string
		if m := reTags.FindStringSubmatch(attrs); len(m) > 1 && m[1] != "" {
			for _, t := range strings.Split(m[1], ",") {
				if name := strings.TrimSpace(t); name != "" {
					tagNames = append(tagNames, name)
				}
			}
		}

		// <DD> タグの説明文を取得します（<A>タグの直後を探します）。
		excerpt := ""
		afterA := html[matchIdx[1]:]
		if m := reDD.FindStringSubmatch(afterA); len(m) > 1 {
			excerpt = strings.TrimSpace(m[1])
		}

		// URLが重複している場合はスキップします（INSERT OR IGNORE）。
		result, err := db.Exec(
			`INSERT OR IGNORE INTO bookmarks (url, title, excerpt, created_at) VALUES (?, ?, ?, ?)`,
			url, title, excerpt, createdAt,
		)
		if err != nil {
			http.Error(w, "保存エラー: "+err.Error(), http.StatusInternalServerError)
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			// 既にURLが存在していた場合はスキップします。
			skipped++
			continue
		}

		bookmarkID, _ := result.LastInsertId()

		// タグを処理します。存在しないタグは新規作成します。
		for _, name := range tagNames {
			// INSERT OR IGNORE でタグが存在しなければ作成します。
			db.Exec(`INSERT OR IGNORE INTO tags (name) VALUES (?)`, name)

			var tagID int
			db.QueryRow(`SELECT id FROM tags WHERE name = ?`, name).Scan(&tagID)
			if tagID > 0 {
				db.Exec(`INSERT OR IGNORE INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)`,
					bookmarkID, tagID)
			}
		}

		imported++
	}

	// 結果をJSONで返します。
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{
		"imported": imported,
		"skipped":  skipped,
	})
}
