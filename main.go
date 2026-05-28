package main

import (
	"database/sql"
	"encoding/json" // JSON形式を扱うためのパッケージ
	"embed"         // 静的ファイルをバイナリに埋め込むためのパッケージ
	"fmt"
	"io"     // io.Reader を扱うためのパッケージ
	"io/fs"  // ファイルシステムを抽象的に扱うためのパッケージ
	"log"
	"net/http" // Webサーバー機能を提供するパッケージ
	"regexp"   // 正規表現でHTMLのメタタグを抽出するためのパッケージ
	"strconv"  // 文字列と数値を相互変換するためのパッケージ
	"strings"  // 文字列操作のためのパッケージ
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

	// static/ ディレクトリを埋め込みファイルシステムとして取り出します。
	// fs.Sub で "static" ディレクトリをルートとして扱えるようにします。
	// こうすることで "/static/index.html" ではなく "/" でアクセスできます。
	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal("静的ファイルの読み込みエラー:", err)
	}
	// http.FileServerFS で埋め込んだファイルをHTTPで配信します。
	http.Handle("/", http.FileServerFS(subFS))

	fmt.Println("サーバーを起動しました: http://localhost:8181")
	fmt.Println("API一覧を確認する: http://localhost:8181/api/bookmarks")
	// 5. 指定したポートでWebサーバーを起動し、待ち受け状態にします。
	if err := http.ListenAndServe(":8181", nil); err != nil {
		log.Fatal("サーバー起動エラー:", err)
	}
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
}

// handleGetBookmarks：登録されているブックマークを一覧で返すAPIです。
// 各ブックマークに紐付いたタグも一緒に返します。
func handleGetBookmarks(w http.ResponseWriter, r *http.Request) {
	// データベースから全てのデータを取得します。
	rows, err := db.Query(`
		SELECT id, url, title, excerpt, author, public, has_content, image_url, created_at, modified_at
		FROM bookmarks
		ORDER BY id DESC`)
	if err != nil {
		http.Error(w, "データベースエラー", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// まず全ブックマークを取得してrowsを閉じます。
	// rowsが開いたまま別のクエリを実行するとSQLiteでエラーになることがあるためです。
	var bookmarks []Bookmark
	for rows.Next() {
		var b Bookmark
		if err := rows.Scan(
			&b.ID, &b.URL, &b.Title, &b.Excerpt, &b.Author,
			&b.Public, &b.HasContent, &b.ImageURL, &b.CreatedAt, &b.ModifiedAt,
		); err != nil {
			http.Error(w, "読み取りエラー", http.StatusInternalServerError)
			return
		}
		bookmarks = append(bookmarks, b)
	}
	rows.Close() // タグ取得の前に明示的に閉じます。

	// rowsを閉じてから各ブックマークのタグを取得します。
	for i := range bookmarks {
		bookmarks[i].Tags, err = getTagsByBookmarkID(bookmarks[i].ID)
		if err != nil {
			http.Error(w, "タグ取得エラー", http.StatusInternalServerError)
			return
		}
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
