package main

import (
	"crypto/rand" // 暗号学的に安全な乱数を生成するパッケージ
	"database/sql"
	"encoding/hex"  // バイト列を16進数文字列に変換するパッケージ
	"encoding/json" // JSON形式を扱うためのパッケージ
	"embed"         // 静的ファイルをバイナリに埋め込むためのパッケージ
	"fmt"
	"io"     // io.Reader を扱うためのパッケージ
	"io/fs"  // ファイルシステムを抽象的に扱うためのパッケージ
	"log"
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
	_ "modernc.org/sqlite"
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
	db, err = sql.Open("sqlite", "./shirushi.db")
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

	// タグ関連のAPI
	http.HandleFunc("GET /api/tags", handleGetTags)
	http.HandleFunc("POST /api/tags", handleCreateTag)
	http.HandleFunc("PUT /api/tags/{id}", handleUpdateTag)
	http.HandleFunc("DELETE /api/tags/{id}", handleDeleteTag)

	// ブックマークとタグの紐付けAPI
	http.HandleFunc("POST /api/bookmarks/{id}/tags", handleAddTagToBookmark)
	http.HandleFunc("DELETE /api/bookmarks/{id}/tags", handleRemoveTagFromBookmark)

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

	fmt.Println("サーバーを起動しました: http://localhost:8181")
	fmt.Println("API一覧を確認する: http://localhost:8181/api/bookmarks")
	// 5. http.DefaultServeMux を authMiddleware でラップして全リクエストに認証を適用します。
	if err := http.ListenAndServe(":8181", authMiddleware(http.DefaultServeMux)); err != nil {
		log.Fatal("サーバー起動エラー:", err)
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
		sessionsMu.Unlock()

		if !ok || time.Now().After(expiry) {
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
	if correctPassword == "" || body.Password != correctPassword {
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

// handleGetBookmarks：登録されているブックマークを一覧で返すAPIです。
// クエリパラメータ:
//   ?q=keyword  タイトル・URL・excerptで絞り込み検索
//   ?tag=name   タグ名で絞り込み
//   ?limit=N    取得件数上限（デフォルト100、最大500）
func handleGetBookmarks(w http.ResponseWriter, r *http.Request) {
	q   := strings.TrimSpace(r.URL.Query().Get("q"))
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))

	// 件数上限を取得します。指定なし・不正値はデフォルト100件にします。
	limit := 100
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 500 {
		limit = l
	}

	var rows *sql.Rows
	var err error

	// q（キーワード）と tag（タグ名）の組み合わせで4パターンに分岐します。
	// タグ絞り込みがある場合は bookmark_tags・tags テーブルと JOIN します。
	switch {
	case q == "" && tag == "":
		// 絞り込みなし：新しい順に全件取得します。
		rows, err = db.Query(`
			SELECT id, url, title, excerpt, author, public, has_content, image_url, created_at, modified_at
			FROM bookmarks
			ORDER BY id DESC
			LIMIT ?`, limit)

	case q != "" && tag == "":
		// キーワード検索のみ：LIKE で部分一致します。
		like := "%" + q + "%"
		rows, err = db.Query(`
			SELECT id, url, title, excerpt, author, public, has_content, image_url, created_at, modified_at
			FROM bookmarks
			WHERE title LIKE ? OR url LIKE ? OR excerpt LIKE ?
			ORDER BY id DESC
			LIMIT ?`,
			like, like, like, limit)

	case q == "" && tag != "":
		// タグ絞り込みのみ：INNER JOIN でタグに紐付くブックマークだけ取得します。
		rows, err = db.Query(`
			SELECT b.id, b.url, b.title, b.excerpt, b.author, b.public, b.has_content, b.image_url, b.created_at, b.modified_at
			FROM bookmarks b
			INNER JOIN bookmark_tags bt ON b.id = bt.bookmark_id
			INNER JOIN tags t ON t.id = bt.tag_id
			WHERE t.name = ?
			ORDER BY b.id DESC
			LIMIT ?`,
			tag, limit)

	default:
		// キーワード＋タグ絞り込み：両方の条件を AND で組み合わせます。
		like := "%" + q + "%"
		rows, err = db.Query(`
			SELECT b.id, b.url, b.title, b.excerpt, b.author, b.public, b.has_content, b.image_url, b.created_at, b.modified_at
			FROM bookmarks b
			INNER JOIN bookmark_tags bt ON b.id = bt.bookmark_id
			INNER JOIN tags t ON t.id = bt.tag_id
			WHERE t.name = ?
			  AND (b.title LIKE ? OR b.url LIKE ? OR b.excerpt LIKE ?)
			ORDER BY b.id DESC
			LIMIT ?`,
			tag, like, like, like, limit)
	}
	if err != nil {
		http.Error(w, "データベースエラー", http.StatusInternalServerError)
		return
	}

	// まず全ブックマークを取得してrowsを閉じます。
	// rowsが開いたまま別のクエリを実行するとSQLiteでエラーになることがあるためです。
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
	rows.Close() // タグ取得の前に明示的に閉じます。

	// N+1クエリ問題を避けるため、タグを1回のSQLで一括取得します。
	// NG例（N+1）: ブックマークが1000件あると1001回クエリが走る
	//   for i := range bookmarks { bookmarks[i].Tags = getTagsByBookmarkID(...) }
	// OK例（2クエリ固定）: 全ブックマークIDをまとめてINで渡す
	if len(bookmarks) > 0 {
		// 取得したブックマークのIDをスライスにまとめます。
		ids := make([]int, len(bookmarks))
		for i, b := range bookmarks {
			ids[i] = b.ID
		}

		// IDをマップのキーとして、ブックマークの添字を素早く引けるようにします。
		// map[bookmarkID] = bookmarksスライスの添字
		idxByID := make(map[int]int, len(bookmarks))
		for i, b := range bookmarks {
			idxByID[b.ID] = i
		}

		// SQLの IN句 に渡すプレースホルダー（?,?,?...）を動的に生成します。
		placeholders := strings.Repeat("?,", len(ids))
		placeholders = placeholders[:len(placeholders)-1] // 末尾のカンマを除去

		// args は interface{} のスライスとして各IDを渡す必要があります。
		args := make([]interface{}, len(ids))
		for i, id := range ids {
			args[i] = id
		}

		// 対象ブックマークのタグを1回のJOINクエリでまとめて取得します。
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

		// 取得したタグ行を対応するブックマークに割り当てます。
		for tagRows.Next() {
			var bookmarkID int
			var tag Tag
			if err := tagRows.Scan(&bookmarkID, &tag.ID, &tag.Name); err != nil {
				http.Error(w, "タグ読み取りエラー", http.StatusInternalServerError)
				return
			}
			// idxByID でブックマークの位置をO(1)で特定してタグを追加します。
			if idx, ok := idxByID[bookmarkID]; ok {
				bookmarks[idx].Tags = append(bookmarks[idx].Tags, tag)
			}
		}
	}

	// nilスライスをそのままJSONにすると null になるため、空スライスで初期化します。
	if bookmarks == nil {
		bookmarks = []Bookmark{}
	}

	// データをJSON形式に変換してブラウザに返します。
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bookmarks)
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
	// レスポンス用にDBからタグ名を取り直します（リクエストにはIDしかないため）。
	b.Tags, err = getTagsByBookmarkID(b.ID)
	if err != nil {
		http.Error(w, "タグ取得エラー", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	b.CreatedAt = now
	b.ModifiedAt = &now
	// 成功（201 Created）を返し、登録されたデータをJSONで返信します。
	// ※ WriteHeader より前に Header().Set() を呼ぶ必要があります。
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated) // 201 Created
	json.NewEncoder(w).Encode(b)
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

	// タグが指定されていれば既存の紐付けを置き換えます。
	if err := syncBookmarkTags(id, b.Tags); err != nil {
		http.Error(w, "タグ保存エラー", http.StatusInternalServerError)
		return
	}
	// レスポンス用にDBからタグ名を取り直します。
	b.Tags, err = getTagsByBookmarkID(id)
	if err != nil {
		http.Error(w, "タグ取得エラー", http.StatusInternalServerError)
		return
	}

	// 更新後のデータをJSONで返します。
	b.ID = id
	now := time.Now()
	b.ModifiedAt = &now
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(b)
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

// fetchMetadata：URLにHTTPアクセスしてHTMLからメタデータを抽出する関数です。
func fetchMetadata(url string) (*Metadata, error) {
	// 10秒でタイムアウトするHTTPクライアントを作ります。
	// デフォルトのクライアントはタイムアウトがないため、自前で設定するのが定石です。
	client := &http.Client{Timeout: 10 * time.Second}

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

// handleGetTags：登録されているタグを一覧で返すAPIです。
func handleGetTags(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, name FROM tags ORDER BY name ASC")
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

	result, err := db.Exec("INSERT INTO tags (name) VALUES (?)", t.Name)
	if err != nil {
		// UNIQUE制約違反の場合は409 Conflictを返します。
		http.Error(w, "同じ名前のタグが既に存在します", http.StatusConflict)
		return
	}

	id, _ := result.LastInsertId()
	t.ID = int(id)

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
