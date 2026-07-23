package main

// main.go：エントリーポイントです。
// DBの接続・ルーティングの登録・HTTPサーバーの起動だけを行います。
// 各機能の実装は model.go / db.go / auth.go / bookmark.go / tag.go /
// metadata.go / importexport.go / util.go に分かれています。

import (
	"database/sql" // sql.Open でDB接続を開くパッケージ
	"embed"        // 静的ファイルをバイナリに埋め込むためのパッケージ
	"fmt"          // 起動メッセージの出力に使うパッケージ
	"io/fs"        // 埋め込みファイルシステムのサブディレクトリ取得に使うパッケージ
	"log"          // 致命的なエラー時にサーバーを終了するパッケージ
	"net/http"     // Webサーバー機能を提供するパッケージ
	"os"           // 環境変数を読み取るパッケージ
	"strings"      // 待ち受けアドレスがポート番号だけか判定するパッケージ
	"time"         // サーバーのタイムアウト設定に使うパッケージ
)

// //go:embed ディレクティブで static ディレクトリの中身をバイナリに埋め込みます。
// これにより、実行ファイル1つでHTMLなどの静的ファイルも配信できます。
//
//go:embed static
var staticFiles embed.FS

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
	//
	// あわせて、複数リクエストの同時アクセスに備えた設定も DSN に入れます。
	// database/sql は内部で複数のコネクションを開くため、SQLite のように
	// 「書き込みは一度に1つ」という制約があるDBでは、同時アクセス時に
	// 「database is locked」エラーが起きやすくなります。これを防ぐために:
	//
	//   - busy_timeout(5000): 書き込みロックが取れないとき、即エラーにせず
	//     最大5000ミリ秒（5秒）まで待って自動でリトライします。
	//     例えば「一括タグ付けの最中に一覧を再読み込み」しても、
	//     少し待てば成功するようになり、ロックエラーをほぼ回避できます。
	//   - journal_mode(WAL): Write-Ahead Logging モードにします。
	//     読み取りと書き込みを並行できるようになり、ロックの競合自体が減ります。
	//     WAL はファイルに記録されるDB自体の属性なので、一度設定すれば以後も維持されます。
	db, err = sql.Open("sqlite",
		"./shirushi.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		log.Fatal("データベース接続エラー:", err)
	}
	defer db.Close()

	// 2. SHIRUSHI_PASSWORD が未設定のまま起動するとログインが一切できません。
	// 設定し忘れに早い段階で気づけるよう、起動直後に確認して止めます。
	if os.Getenv("SHIRUSHI_PASSWORD") == "" {
		log.Fatal("SHIRUSHI_PASSWORD が設定されていません。環境変数にパスワードを指定してから起動してください。\n例: SHIRUSHI_PASSWORD='yourpassword' ./shirushi")
	}

	// 3. Bearer トークンを起動時に1回だけ読んでグローバル変数に保持します。
	// リクエストごとに os.Getenv を呼ぶのと違い、「未設定なら Bearer 無効」という
	// フラグ的な役割も起動時1回の読み込みで自然に表現できます。
	apiToken = os.Getenv("SHIRUSHI_API_TOKEN")
	if apiToken == "" {
		log.Println("警告: SHIRUSHI_API_TOKEN が設定されていません。Bearer 認証は無効です（Cookie 認証のみで動作します）。")
	}

	// 4. テーブルが存在しない場合は作成します。
	createTable()
	// 4. 既存DBに新しいカラムを追加するマイグレーションを実行します。
	runMigrations()
	// 5. APIのルート（住所）と、それぞれの処理（関数）を紐付けます。
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

	// 5. 待ち受けアドレスを環境変数 SHIRUSHI_ADDR で設定可能にします。
	// デフォルトは ":8181"（全インターフェース）です。
	// Caddy を同一ホストで動かす場合は "127.0.0.1:8181" を推奨します。
	// 例: SHIRUSHI_ADDR=127.0.0.1:8181 SHIRUSHI_PASSWORD='...' ./shirushi
	addr := os.Getenv("SHIRUSHI_ADDR")
	if addr == "" {
		addr = ":8181"
	}
	// ":8181" のようにポート番号だけ指定された場合は、ブラウザで開けるよう
	// 表示用URLに localhost を補います。"127.0.0.1:18181" のようにホストが
	// 含まれている場合は、その前に localhost を重ねず、そのまま表示します。
	fmt.Println("サーバーを起動しました: " + serverDisplayURL(addr))
	// 6. http.DefaultServeMux を authMiddleware でラップして全リクエストに認証を適用します。
	server := &http.Server{
		Addr:              addr,
		Handler:           authMiddleware(http.DefaultServeMux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal("サーバー起動エラー:", err)
	}
}

// serverDisplayURL：net/httpの待ち受けアドレスを、ブラウザで開けるURLへ変換します。
// サーバーが実際に待ち受ける値は変更せず、起動メッセージの表示だけを整えます。
func serverDisplayURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		addr = "localhost" + addr
	}
	return "http://" + addr
}
