package main

import (
	"database/sql"
	"encoding/json" // JSON形式を扱うためのパッケージ
	"embed"         // 静的ファイルをバイナリに埋め込むためのパッケージ
	"fmt"
	"io/fs"  // ファイルシステムを抽象的に扱うためのパッケージ
	"log"
	"net/http" // Webサーバー機能を提供するパッケージ
	"strconv"  // 文字列と数値を相互変換するためのパッケージ
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
// `json:"..."` という記述（タグ）は、JSON形式にする際の名前を指定しています。
type Bookmark struct {
	ID          int       `json:"id"`
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
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
	// 3. APIのルート（住所）と、それぞれの処理（関数）を紐付けます。
	// Go 1.22からの新機能で、"GET /..." のようにHTTPメソッドを指定できます。
	http.HandleFunc("GET /api/bookmarks", handleGetBookmarks)
	http.HandleFunc("POST /api/bookmarks", handleCreateBookmark)
	http.HandleFunc("PUT /api/bookmarks/{id}", handleUpdateBookmark)
	http.HandleFunc("DELETE /api/bookmarks/{id}", handleDeleteBookmark)

	// static/ ディレクトリを埋め込みファイルシステムとして取り出します。
	// fs.Sub で "static" ディレクトリをルートとして扱えるようにします。
	// こうすることで "/static/index.html" ではなく "/" でアクセスできます。
	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal("静的ファイルの読み込みエラー:", err)
	}
	// http.FileServerFS で埋め込んだファイルをHTTPで配信します。
	http.Handle("/", http.FileServerFS(subFS))

	fmt.Println("サーバーを起動しました: http://localhost:8080")
	fmt.Println("API一覧を確認する: http://localhost:8080/api/bookmarks")
	// 4. 指定したポートでWebサーバーを起動し、待ち受け状態にします。
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal("サーバー起動エラー:", err)
	}
}

// createTable：ブックマーク保存用のテーブルを作成する関数です。
func createTable() {
	query := `
    CREATE TABLE IF NOT EXISTS bookmarks (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        url TEXT NOT NULL,
        title TEXT,
        description TEXT,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP
    );`
	_, err := db.Exec(query)
	if err != nil {
		log.Fatal("テーブル作成エラー:", err)
	}
}

// handleGetBookmarks：登録されているブックマークを一覧で返すAPIです。
func handleGetBookmarks(w http.ResponseWriter, r *http.Request) {
	// データベースから全てのデータを取得します。
	rows, err := db.Query("SELECT id, url, title, description, created_at FROM bookmarks ORDER BY id DESC")
	if err != nil {
		http.Error(w, "データベースエラー", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var bookmarks []Bookmark
	for rows.Next() {
		var b Bookmark
		// データベースの値を構造体にコピーします。
		if err := rows.Scan(&b.ID, &b.URL, &b.Title, &b.Description, &b.CreatedAt); err != nil {
			http.Error(w, "読み取りエラー", http.StatusInternalServerError)
			return
		}
		bookmarks = append(bookmarks, b)
	}
	// データをJSON形式に変換してブラウザに返します。
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bookmarks)
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
	query := `UPDATE bookmarks SET url = ?, title = ?, description = ? WHERE id = ?;`
	result, err := db.Exec(query, b.URL, b.Title, b.Description, id)
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

	// 更新後のデータをJSONで返します。
	b.ID = id
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

// handleCreateBookmark：新しいブックマークを登録するAPIです。
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
	query := `INSERT INTO bookmarks (url, title, description) VALUES (?, ?, ?);`
	result, err := db.Exec(query, b.URL, b.Title, b.Description)
	if err != nil {
		http.Error(w, "保存エラー", http.StatusInternalServerError)
		return
	}
	// 自動採番されたIDを取得してセットします。
	id, _ := result.LastInsertId()
	b.ID = int(id)
	b.CreatedAt = time.Now()
	// 成功（201 Created）を返し、登録されたデータをJSONで返信します。
	// ※ WriteHeader より前に Header().Set() を呼ぶ必要があります。
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated) // 201 Created
	json.NewEncoder(w).Encode(b)
}
