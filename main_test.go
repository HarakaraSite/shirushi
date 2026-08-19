package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// setupTestDB：テスト専用のインメモリDBを用意する関数です。
// ":memory:" を指定すると、ファイルを作らずメモリ上だけにDBが作られます。
// テストが終わると自動的に消えるので、本番データを汚しません。
func setupTestDB(t *testing.T) {
	// t.Helper() を呼ぶと、テスト失敗時のエラー行番号がこの関数ではなく
	// 呼び出し元のテスト関数を指すようになり、デバッグしやすくなります。
	t.Helper()

	var err error
	// 本番（main.go）と同じく DSN で外部キー制約を有効化し、
	// ON DELETE CASCADE の挙動もテストで再現できるようにします。
	db, err = sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("テスト用DB作成エラー: %v", err)
	}
	// SQLite の ":memory:" は接続ごとに別DBになります。
	// database/sql は必要に応じて複数接続を開くため、テストでは1接続に固定して
	// 「作ったテーブルが次のクエリから見えない」事故を防ぎます。
	db.SetMaxOpenConns(1)

	// テスト終了時にDBを閉じます。t.Cleanup に登録すると自動で呼ばれます。
	t.Cleanup(func() {
		db.Close()
	})

	// テーブルを作成します（本番と同じ createTable 関数を使います）。
	createTable()
}

// createTestBookmark：テスト用ブックマークを1件作り、採番されたIDを返す補助関数です。
func createTestBookmark(t *testing.T, url string) int {
	t.Helper()

	result, err := db.Exec(
		`INSERT INTO bookmarks (url, title, excerpt) VALUES (?, ?, ?)`,
		url, "テスト", "",
	)
	if err != nil {
		t.Fatalf("テスト用ブックマーク作成エラー: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("ブックマークID取得エラー: %v", err)
	}
	return int(id)
}

// createTestTag：テスト用タグを1件作り、採番されたIDを返す補助関数です。
func createTestTag(t *testing.T, name string) int {
	t.Helper()

	result, err := db.Exec(`INSERT INTO tags (name) VALUES (?)`, name)
	if err != nil {
		t.Fatalf("テスト用タグ作成エラー: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("タグID取得エラー: %v", err)
	}
	return int(id)
}

// countBookmarkTags：中間テーブルの件数を数える補助関数です。
func countBookmarkTags(t *testing.T) int {
	t.Helper()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM bookmark_tags`).Scan(&count); err != nil {
		t.Fatalf("bookmark_tags 件数取得エラー: %v", err)
	}
	return count
}

// resetSessions：認証テスト同士でグローバルなセッション状態が混ざらないよう初期化します。
func resetSessions() {
	sessionsMu.Lock()
	sessions = map[string]time.Time{}
	sessionsMu.Unlock()

	loginAttemptsMu.Lock()
	loginAttempts = map[string]loginAttempt{}
	loginAttemptsMu.Unlock()

	nowFunc = time.Now
}

// newMultipartImportRequest：インポートAPI用のmultipart/form-dataリクエストを作る補助関数です。
func newMultipartImportRequest(t *testing.T, html string) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "bookmarks.html")
	if err != nil {
		t.Fatalf("multipartファイル作成エラー: %v", err)
	}
	if _, err := part.Write([]byte(html)); err != nil {
		t.Fatalf("multipart書き込みエラー: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart close エラー: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/api/import", &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	return r
}

// TestServerDisplayURL：ポート番号だけの場合とホスト指定済みの場合の表示を確認します。
func TestServerDisplayURL(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{name: "ポート番号だけ", addr: ":8181", want: "http://localhost:8181"},
		{name: "IPv4ホスト指定", addr: "127.0.0.1:18181", want: "http://127.0.0.1:18181"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serverDisplayURL(tt.addr); got != tt.want {
				t.Errorf("表示URLが違います: got %q, want %q", got, tt.want)
			}
		})
	}
}

// Test404ThumbnailIsEmbedded：404画像が正しいSVGとしてバイナリへ埋め込まれるか確認します。
func Test404ThumbnailIsEmbedded(t *testing.T) {
	data, err := staticFiles.ReadFile("static/404.svg")
	if err != nil {
		t.Fatalf("404画像を埋め込みファイルから読めません: %v", err)
	}

	var root struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(data, &root); err != nil {
		t.Fatalf("404画像が正しいXMLではありません: %v", err)
	}
	if root.XMLName.Local != "svg" {
		t.Fatalf("ルート要素がsvgではありません: %q", root.XMLName.Local)
	}
	if !bytes.Contains(data, []byte(">404</text>")) {
		t.Fatal("404画像に識別文字が含まれていません")
	}
}

// TestHandleGetBookmarks_Empty：データが0件のときに空配列が返るかテストします。
func TestHandleGetBookmarks_Empty(t *testing.T) {
	setupTestDB(t)

	// httptest.NewRecorder は、HTTPレスポンスを記録するための「ダミーの受け皿」です。
	// 実際にブラウザに送信する代わりに、このオブジェクトに書き込まれます。
	w := httptest.NewRecorder()

	// httptest.NewRequest は、テスト用のHTTPリクエストを作ります。
	// 実際にネットワーク通信は発生しません。
	r := httptest.NewRequest(http.MethodGet, "/api/bookmarks", nil)

	// テスト対象のハンドラを直接呼び出します。
	handleGetBookmarks(w, r)

	// ステータスコードが 200 OK か確認します。
	if w.Code != http.StatusOK {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusOK)
	}

	// レスポンスボディをJSONとして読み取ります。
	// GET /api/bookmarks は { "bookmarks": [...], "total": N } 形式で返します。
	var resp BookmarkListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("JSONの解析に失敗しました: %v", err)
	}

	// データが0件であることを確認します。
	if len(resp.Bookmarks) != 0 {
		t.Errorf("件数が違います: got %d, want 0", len(resp.Bookmarks))
	}
	if resp.Total != 0 {
		t.Errorf("totalが違います: got %d, want 0", resp.Total)
	}
}

// TestHandleGetBookmarks_WithData：データが登録済みのとき正しく返るかテストします。
func TestHandleGetBookmarks_WithData(t *testing.T) {
	setupTestDB(t)

	// テスト用データを直接DBに挿入します。
	_, err := db.Exec(
		`INSERT INTO bookmarks (url, title, excerpt) VALUES (?, ?, ?)`,
		"https://example.com", "テスト", "説明文",
	)
	if err != nil {
		t.Fatalf("テストデータ挿入エラー: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/bookmarks", nil)
	handleGetBookmarks(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp BookmarkListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("JSONの解析に失敗しました: %v", err)
	}

	// 1件返ってくることを確認します。
	if len(resp.Bookmarks) != 1 {
		t.Errorf("件数が違います: got %d, want 1", len(resp.Bookmarks))
	}
	if resp.Total != 1 {
		t.Errorf("totalが違います: got %d, want 1", resp.Total)
	}

	// 内容が正しいか確認します。
	if resp.Bookmarks[0].URL != "https://example.com" {
		t.Errorf("URLが違います: got %s, want https://example.com", resp.Bookmarks[0].URL)
	}
	if resp.Bookmarks[0].Title != "テスト" {
		t.Errorf("タイトルが違います: got %s, want テスト", resp.Bookmarks[0].Title)
	}
}

// TestHandleBookmark404CheckAsync：開始APIが即座に202を返し、backgroundで更新することを確認します。
func TestHandleBookmark404CheckAsync(t *testing.T) {
	setupTestDB(t)
	// httptestの接続先は127.0.0.1なので、テスト中だけprivate接続を許可します。
	t.Setenv("SHIRUSHI_ALLOW_PRIVATE_FETCH", "1")
	bookmark404Job = bookmark404JobState{status: Bookmark404CheckStatus{Status: bookmark404JobIdle}}

	releaseMissing := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing", "/stale-missing":
			// 202応答より後まで外部アクセスを止め、APIが完了を待っていないことを確かめます。
			<-releaseMissing
			http.NotFound(w, r)
		case "/server-error":
			http.Error(w, "error", http.StatusInternalServerError)
		case "/requires-html-accept":
			// crates.ioなど、HTMLを要求しないGETへ404を返すサイトの挙動を再現します。
			if !strings.Contains(r.Header.Get("Accept"), "text/html") {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	missingID := createTestBookmark(t, server.URL+"/missing")
	staleMissingID := createTestBookmark(t, server.URL+"/stale-missing")
	okID := createTestBookmark(t, server.URL+"/ok")
	errorID := createTestBookmark(t, server.URL+"/server-error")
	htmlAcceptID := createTestBookmark(t, server.URL+"/requires-html-accept")
	for _, id := range []int{missingID, staleMissingID, okID, errorID, htmlAcceptID} {
		if _, err := db.Exec(`UPDATE bookmarks SET image_url = ? WHERE id = ?`, "/original.svg", id); err != nil {
			t.Fatalf("初期サムネイル設定エラー: %v", err)
		}
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/bookmarks/check-404", nil)
	handleStartBookmark404Check(w, r)

	if w.Code != http.StatusAccepted {
		t.Fatalf("ステータスコードが違います: got %d, want %d: %s", w.Code, http.StatusAccepted, w.Body.String())
	}
	var response Bookmark404CheckStatus
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("JSONの解析に失敗しました: %v", err)
	}
	if response.Status != bookmark404JobRunning || response.Total != 5 {
		t.Fatalf("開始直後の状態が違います: %#v", response)
	}

	// 実行中の二重開始は、同じURLへ重複アクセスしないよう409で拒否します。
	w = httptest.NewRecorder()
	handleStartBookmark404Check(w, httptest.NewRequest(http.MethodPost, "/api/bookmarks/check-404", nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("二重開始のステータスが違います: got %d, want %d", w.Code, http.StatusConflict)
	}

	// 確認待ちの間にURLとサムネイルを編集し、古いURLの404結果が上書きしないことを確認します。
	if _, err := db.Exec(`UPDATE bookmarks SET url = ?, image_url = ? WHERE id = ?`, server.URL+"/now-ok", "/edited.svg", staleMissingID); err != nil {
		t.Fatalf("確認中のbookmark編集エラー: %v", err)
	}

	close(releaseMissing)
	deadline := time.Now().Add(2 * time.Second)
	for {
		w = httptest.NewRecorder()
		handleGetBookmark404CheckStatus(w, httptest.NewRequest(http.MethodGet, "/api/bookmarks/check-404", nil))
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("状態JSONの解析に失敗しました: %v", err)
		}
		if response.Status == bookmark404JobCompleted {
			break
		}
		if response.Status == bookmark404JobFailed {
			t.Fatalf("backgroundジョブが失敗しました: %#v", response)
		}
		if time.Now().After(deadline) {
			t.Fatalf("backgroundジョブが完了しません: %#v", response)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if response.Total != 5 || response.Checked != 5 || response.NotFound != 2 || response.Failed != 0 {
		t.Fatalf("完了時の集計結果が違います: %#v", response)
	}

	for _, tt := range []struct {
		id           int
		wantImage    string
		wantModified bool
	}{
		{id: missingID, wantImage: notFoundThumbnailURL, wantModified: true},
		{id: staleMissingID, wantImage: "/edited.svg", wantModified: false},
		{id: okID, wantImage: "/original.svg", wantModified: false},
		{id: errorID, wantImage: "/original.svg", wantModified: false},
		{id: htmlAcceptID, wantImage: "/original.svg", wantModified: false},
	} {
		var gotImage string
		var gotModified sql.NullTime
		if err := db.QueryRow(`SELECT image_url, modified_at FROM bookmarks WHERE id = ?`, tt.id).Scan(&gotImage, &gotModified); err != nil {
			t.Fatalf("サムネイル・更新日時取得エラー: %v", err)
		}
		if gotImage != tt.wantImage {
			t.Errorf("bookmark %d のサムネイルが違います: got %q, want %q", tt.id, gotImage, tt.wantImage)
		}
		if gotModified.Valid != tt.wantModified {
			t.Errorf("bookmark %d のmodified_at有無が違います: got %v, want %v", tt.id, gotModified.Valid, tt.wantModified)
		}
	}
}

// TestHandleDeleteBookmark_Success：存在するIDを削除すると204が返るかテストします。
func TestHandleDeleteBookmark_Success(t *testing.T) {
	setupTestDB(t)

	// 削除対象のデータを1件挿入して、採番されたIDを取得します。
	result, err := db.Exec(
		`INSERT INTO bookmarks (url, title, excerpt) VALUES (?, ?, ?)`,
		"https://example.com", "削除テスト", "",
	)
	if err != nil {
		t.Fatalf("テストデータ挿入エラー: %v", err)
	}
	id, _ := result.LastInsertId()

	w := httptest.NewRecorder()
	// fmt.Sprintf でURLにIDを埋め込みます。
	r := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/bookmarks/%d", id), nil)
	// r.SetPathValue で "{id}" に対応する値をセットします。
	// httptest では PathValue を自動で解析しないので、手動で設定が必要です。
	r.SetPathValue("id", fmt.Sprintf("%d", id))

	handleDeleteBookmark(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusNoContent)
	}
}

// TestHandleDeleteBookmark_NotFound：存在しないIDを削除すると404が返るかテストします。
func TestHandleDeleteBookmark_NotFound(t *testing.T) {
	setupTestDB(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/bookmarks/999", nil)
	// DBに何も入っていないので、どのIDを指定しても404になります。
	r.SetPathValue("id", "999")

	handleDeleteBookmark(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestHandleCreateBookmark_Success：正しいJSONを送ると201とデータが返るかテストします。
func TestHandleCreateBookmark_Success(t *testing.T) {
	setupTestDB(t)

	// strings.NewReader でJSON文字列をリクエストボディとして渡します。
	body := strings.NewReader(`{"url":"https://example.com","title":"テスト","excerpt":"説明"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/bookmarks", body)

	handleCreateBookmark(w, r)

	// 新規作成は 201 Created が正しいステータスコードです。
	if w.Code != http.StatusCreated {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusCreated)
	}

	// レスポンスのJSONに登録したデータが含まれているか確認します。
	var b Bookmark
	if err := json.NewDecoder(w.Body).Decode(&b); err != nil {
		t.Fatalf("JSONの解析に失敗しました: %v", err)
	}
	if b.URL != "https://example.com" {
		t.Errorf("URLが違います: got %s, want https://example.com", b.URL)
	}
	// IDが採番されて 0 より大きい値になっているか確認します。
	if b.ID == 0 {
		t.Errorf("IDが採番されていません: got %d", b.ID)
	}
}

// TestHandleCreateBookmark_MissingURL：URLが空のとき400が返るかテストします。
func TestHandleCreateBookmark_MissingURL(t *testing.T) {
	setupTestDB(t)

	body := strings.NewReader(`{"url":"","title":"タイトルだけ"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/bookmarks", body)

	handleCreateBookmark(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestHandleCreateBookmark_InvalidURL：http/https以外のURLを拒否するかテストします。
func TestHandleCreateBookmark_InvalidURL(t *testing.T) {
	setupTestDB(t)

	body := strings.NewReader(`{"url":"javascript:alert(1)","title":"危険なURL"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/bookmarks", body)

	handleCreateBookmark(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestHandleCreateBookmark_TooLargeJSON：JSON本文が大きすぎる場合に拒否するかテストします。
func TestHandleCreateBookmark_TooLargeJSON(t *testing.T) {
	setupTestDB(t)

	largeTitle := strings.Repeat("a", int(maxJSONBodyBytes)+1)
	body := strings.NewReader(fmt.Sprintf(`{"url":"https://example.com","title":"%s"}`, largeTitle))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/bookmarks", body)

	handleCreateBookmark(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestHandleGetBookmark_ReturnsCurrentExcerpt：編集を開く直前に、DBの最新Excerptを1件取得できるか確認します。
func TestHandleGetBookmark_ReturnsCurrentExcerpt(t *testing.T) {
	setupTestDB(t)

	result, err := db.Exec(`INSERT INTO bookmarks (url, title, excerpt) VALUES (?, ?, ?)`,
		"https://example.com/latest", "最新値", "Henjiが更新した要約")
	if err != nil {
		t.Fatalf("テストデータ挿入エラー: %v", err)
	}
	id, _ := result.LastInsertId()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/bookmarks/%d", id), nil)
	r.SetPathValue("id", fmt.Sprintf("%d", id))
	handleGetBookmark(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusOK)
	}
	var got Bookmark
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("JSONの解析に失敗しました: %v", err)
	}
	if got.ID != int(id) || got.Excerpt != "Henjiが更新した要約" {
		t.Fatalf("最新bookmarkが返っていません: %#v", got)
	}
}

func TestHandleGetBookmark_InvalidOrMissing(t *testing.T) {
	setupTestDB(t)

	for _, tt := range []struct {
		name string
		id   string
		want int
	}{
		{name: "invalid", id: "not-a-number", want: http.StatusBadRequest},
		{name: "missing", id: "999", want: http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/api/bookmarks/"+tt.id, nil)
			r.SetPathValue("id", tt.id)
			handleGetBookmark(w, r)
			if w.Code != tt.want {
				t.Fatalf("ステータスコードが違います: got %d, want %d", w.Code, tt.want)
			}
		})
	}
}

// TestHandleGetBookmarkByURL：拡張向けのURL完全一致検索が、登録済みbookmarkとタグを返すか確認します。
func TestHandleGetBookmarkByURL(t *testing.T) {
	setupTestDB(t)

	bookmarkID := createTestBookmark(t, "https://example.com/article")
	tagID := createTestTag(t, "go")
	if _, err := db.Exec(`INSERT INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)`, bookmarkID, tagID); err != nil {
		t.Fatalf("テスト用タグ紐付け作成エラー: %v", err)
	}
	// URLだけが含まれる別bookmarkを作り、部分一致検索へ後退していないことも確認します。
	createTestBookmark(t, "https://example.com/other?ref=https://example.com/article")

	w := httptest.NewRecorder()
	// 大文字ホストと既定HTTPSポートを、保存時と同じ正規化で一致させます。
	r := httptest.NewRequest(http.MethodGet, "/api/bookmarks/by-url?url=https%3A%2F%2FEXAMPLE.com%3A443%2Farticle", nil)
	handleGetBookmarkByURL(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusOK)
	}
	var got Bookmark
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("JSONの解析に失敗しました: %v", err)
	}
	if got.ID != bookmarkID || got.URL != "https://example.com/article" {
		t.Fatalf("完全一致のbookmarkが返っていません: %#v", got)
	}
	if len(got.Tags) != 1 || got.Tags[0].Name != "go" {
		t.Fatalf("タグ込みのbookmarkが返っていません: %#v", got.Tags)
	}
}

// TestHandleGetBookmarkByURL_InvalidOrMissing：入力不正は400、未登録URLは404で返すか確認します。
func TestHandleGetBookmarkByURL_InvalidOrMissing(t *testing.T) {
	setupTestDB(t)

	for _, tt := range []struct {
		name string
		path string
		want int
	}{
		{name: "URL未指定", path: "/api/bookmarks/by-url", want: http.StatusBadRequest},
		{name: "非HTTPURL", path: "/api/bookmarks/by-url?url=ftp%3A%2F%2Fexample.com", want: http.StatusBadRequest},
		{name: "未登録", path: "/api/bookmarks/by-url?url=https%3A%2F%2Fexample.com%2Fnot-saved", want: http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			handleGetBookmarkByURL(w, r)
			if w.Code != tt.want {
				t.Fatalf("ステータスコードが違います: got %d, want %d", w.Code, tt.want)
			}
		})
	}
}

// TestBookmarkByURLRouteAndBearerAuth：固定パスが {id} へ誤ルーティングせず、
// 既存の認証ミドルウェアを通じてBearer認証が使えることを確認します。
func TestBookmarkByURLRouteAndBearerAuth(t *testing.T) {
	setupTestDB(t)
	createTestBookmark(t, "https://example.com/article")

	// main.go と同じ2ルートを小さなServeMuxへ登録します。
	// /by-url が {id} より具体的な固定パスとして選ばれることをHTTP経由で確認します。
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/bookmarks/by-url", handleGetBookmarkByURL)
	mux.HandleFunc("GET /api/bookmarks/{id}", handleGetBookmark)
	handler := authMiddleware(mux)

	oldAPIToken := apiToken
	apiToken = "test-api-token"
	t.Cleanup(func() { apiToken = oldAPIToken })

	path := "/api/bookmarks/by-url?url=https%3A%2F%2Fexample.com%2Farticle"

	// 認証なしでは、ハンドラへ届く前に401になります。
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("未認証のステータスコードが違います: got %d, want %d", w.Code, http.StatusUnauthorized)
	}

	// 正しいBearerトークンなら、固定パスのハンドラが200を返します。
	w = httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Authorization", "Bearer test-api-token")
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("Bearer認証時のステータスコードが違います: got %d, want %d", w.Code, http.StatusOK)
	}
}

// TestHandleUpdateBookmark_Success：存在するIDを正しいJSONで更新すると200が返るかテストします。
func TestHandleUpdateBookmark_Success(t *testing.T) {
	setupTestDB(t)

	// 更新対象のデータを1件挿入します。
	result, err := db.Exec(
		`INSERT INTO bookmarks (url, title, excerpt) VALUES (?, ?, ?)`,
		"https://before.com", "更新前", "",
	)
	if err != nil {
		t.Fatalf("テストデータ挿入エラー: %v", err)
	}
	id, _ := result.LastInsertId()

	body := strings.NewReader(`{"url":"https://after.com","title":"更新後","excerpt":"メモ"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/bookmarks/%d", id), body)
	r.SetPathValue("id", fmt.Sprintf("%d", id))

	handleUpdateBookmark(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusOK)
	}

	// レスポンスのJSONに更新後のデータが含まれているか確認します。
	var b Bookmark
	if err := json.NewDecoder(w.Body).Decode(&b); err != nil {
		t.Fatalf("JSONの解析に失敗しました: %v", err)
	}
	if b.URL != "https://after.com" {
		t.Errorf("URLが違います: got %s, want https://after.com", b.URL)
	}
}

// TestHandleUpdateBookmark_NotFound：存在しないIDを更新すると404が返るかテストします。
func TestHandleUpdateBookmark_NotFound(t *testing.T) {
	setupTestDB(t)

	body := strings.NewReader(`{"url":"https://example.com","title":"タイトル"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/api/bookmarks/999", body)
	r.SetPathValue("id", "999")

	handleUpdateBookmark(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestHandleCreateTag_ReturnsExistingTag：同名タグ作成時に既存タグが返るかテストします。
func TestHandleCreateTag_ReturnsExistingTag(t *testing.T) {
	setupTestDB(t)

	originalID := createTestTag(t, "go")

	body := strings.NewReader(`{"name":"go"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/tags", body)

	handleCreateTag(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusCreated)
	}

	var tag Tag
	if err := json.NewDecoder(w.Body).Decode(&tag); err != nil {
		t.Fatalf("JSONの解析に失敗しました: %v", err)
	}
	if tag.ID != originalID {
		t.Errorf("既存タグIDが返っていません: got %d, want %d", tag.ID, originalID)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tags WHERE name = ?`, "go").Scan(&count); err != nil {
		t.Fatalf("タグ件数取得エラー: %v", err)
	}
	if count != 1 {
		t.Errorf("同名タグが重複作成されています: got %d, want 1", count)
	}
}

// TestHandleGetTags_DefaultOnlyUsedAndAllIncludesUnused：通常は使用中タグのみ、all=1で全タグが返るかテストします。
func TestHandleGetTags_DefaultOnlyUsedAndAllIncludesUnused(t *testing.T) {
	setupTestDB(t)

	bookmarkID := createTestBookmark(t, "https://example.com")
	usedTagID := createTestTag(t, "used")
	createTestTag(t, "unused")
	if _, err := db.Exec(
		`INSERT INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)`,
		bookmarkID, usedTagID,
	); err != nil {
		t.Fatalf("タグ紐付けエラー: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	handleGetTags(w, r)

	var usedTags []Tag
	if err := json.NewDecoder(w.Body).Decode(&usedTags); err != nil {
		t.Fatalf("使用中タグJSONの解析に失敗しました: %v", err)
	}
	if len(usedTags) != 1 || usedTags[0].Name != "used" {
		t.Fatalf("使用中タグだけが返っていません: got %+v", usedTags)
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/api/tags?all=1", nil)
	handleGetTags(w, r)

	var allTags []Tag
	if err := json.NewDecoder(w.Body).Decode(&allTags); err != nil {
		t.Fatalf("全タグJSONの解析に失敗しました: %v", err)
	}
	if len(allTags) != 2 {
		t.Errorf("全タグ数が違います: got %d, want 2", len(allTags))
	}
}

// TestHandleUpdateTag_Success：タグ名を更新できるかテストします。
func TestHandleUpdateTag_Success(t *testing.T) {
	setupTestDB(t)

	tagID := createTestTag(t, "before")

	body := strings.NewReader(`{"name":"after"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/tags/%d", tagID), body)
	r.SetPathValue("id", fmt.Sprintf("%d", tagID))

	handleUpdateTag(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusOK)
	}

	var tag Tag
	if err := json.NewDecoder(w.Body).Decode(&tag); err != nil {
		t.Fatalf("JSONの解析に失敗しました: %v", err)
	}
	if tag.Name != "after" {
		t.Errorf("タグ名が更新されていません: got %s, want after", tag.Name)
	}
}

// TestHandleDeleteTag_CascadesBookmarkTags：タグ削除時に紐付けも削除されるかテストします。
func TestHandleDeleteTag_CascadesBookmarkTags(t *testing.T) {
	setupTestDB(t)

	bookmarkID := createTestBookmark(t, "https://example.com")
	tagID := createTestTag(t, "go")
	if _, err := db.Exec(
		`INSERT INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)`,
		bookmarkID, tagID,
	); err != nil {
		t.Fatalf("タグ紐付けエラー: %v", err)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/tags/%d", tagID), nil)
	r.SetPathValue("id", fmt.Sprintf("%d", tagID))

	handleDeleteTag(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := countBookmarkTags(t); got != 0 {
		t.Errorf("タグ紐付けが残っています: got %d, want 0", got)
	}
}

// TestHandleAddAndRemoveTagToBookmark：個別のタグ追加・削除APIをテストします。
func TestHandleAddAndRemoveTagToBookmark(t *testing.T) {
	setupTestDB(t)

	bookmarkID := createTestBookmark(t, "https://example.com")
	tagID := createTestTag(t, "go")

	body := strings.NewReader(fmt.Sprintf(`{"tag_id":%d}`, tagID))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/bookmarks/%d/tags", bookmarkID), body)
	r.SetPathValue("id", fmt.Sprintf("%d", bookmarkID))

	handleAddTagToBookmark(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("追加ステータスコードが違います: got %d, want %d", w.Code, http.StatusNoContent)
	}

	// 同じタグをもう一度追加しても INSERT OR IGNORE により重複しないことを確認します。
	body = strings.NewReader(fmt.Sprintf(`{"tag_id":%d}`, tagID))
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/bookmarks/%d/tags", bookmarkID), body)
	r.SetPathValue("id", fmt.Sprintf("%d", bookmarkID))
	handleAddTagToBookmark(w, r)

	if got := countBookmarkTags(t); got != 1 {
		t.Errorf("タグ紐付けが重複しています: got %d, want 1", got)
	}

	body = strings.NewReader(fmt.Sprintf(`{"tag_id":%d}`, tagID))
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/bookmarks/%d/tags", bookmarkID), body)
	r.SetPathValue("id", fmt.Sprintf("%d", bookmarkID))

	handleRemoveTagFromBookmark(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("削除ステータスコードが違います: got %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := countBookmarkTags(t); got != 0 {
		t.Errorf("タグ紐付けが削除されていません: got %d, want 0", got)
	}
}

// TestHandleAddTagToBookmark_MissingTag：存在しないタグIDを指定すると404が返るかテストします。
func TestHandleAddTagToBookmark_MissingTag(t *testing.T) {
	setupTestDB(t)

	bookmarkID := createTestBookmark(t, "https://example.com")

	body := strings.NewReader(`{"tag_id":999}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/bookmarks/%d/tags", bookmarkID), body)
	r.SetPathValue("id", fmt.Sprintf("%d", bookmarkID))

	handleAddTagToBookmark(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestHandleBulkAddAndRemoveTags：一括タグ追加・削除APIをテストします。
func TestHandleBulkAddAndRemoveTags(t *testing.T) {
	setupTestDB(t)

	bookmarkID1 := createTestBookmark(t, "https://example.com/1")
	bookmarkID2 := createTestBookmark(t, "https://example.com/2")
	tagID1 := createTestTag(t, "go")
	tagID2 := createTestTag(t, "sqlite")

	body := strings.NewReader(fmt.Sprintf(
		`{"bookmark_ids":[%d,%d],"tag_ids":[%d,%d]}`,
		bookmarkID1, bookmarkID2, tagID1, tagID2,
	))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/bookmarks/bulk/tags", body)

	handleBulkAddTags(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("一括追加ステータスコードが違います: got %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := countBookmarkTags(t); got != 4 {
		t.Fatalf("一括追加件数が違います: got %d, want 4", got)
	}

	// 同じリクエストを再送しても重複しないことを確認します。
	body = strings.NewReader(fmt.Sprintf(
		`{"bookmark_ids":[%d,%d],"tag_ids":[%d,%d]}`,
		bookmarkID1, bookmarkID2, tagID1, tagID2,
	))
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/bookmarks/bulk/tags", body)
	handleBulkAddTags(w, r)

	if got := countBookmarkTags(t); got != 4 {
		t.Fatalf("一括追加が重複しています: got %d, want 4", got)
	}

	body = strings.NewReader(fmt.Sprintf(
		`{"bookmark_ids":[%d,%d],"tag_ids":[%d]}`,
		bookmarkID1, bookmarkID2, tagID1,
	))
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodDelete, "/api/bookmarks/bulk/tags", body)

	handleBulkRemoveTags(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("一括削除ステータスコードが違います: got %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := countBookmarkTags(t); got != 2 {
		t.Errorf("一括削除後の件数が違います: got %d, want 2", got)
	}
}

// TestHandleBulkDeleteBookmarks_CascadesTags：一括削除でブックマークとタグ紐付けが消えるかテストします。
func TestHandleBulkDeleteBookmarks_CascadesTags(t *testing.T) {
	setupTestDB(t)

	bookmarkID1 := createTestBookmark(t, "https://example.com/1")
	bookmarkID2 := createTestBookmark(t, "https://example.com/2")
	tagID := createTestTag(t, "go")
	for _, bookmarkID := range []int{bookmarkID1, bookmarkID2} {
		if _, err := db.Exec(
			`INSERT INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)`,
			bookmarkID, tagID,
		); err != nil {
			t.Fatalf("タグ紐付けエラー: %v", err)
		}
	}

	body := strings.NewReader(fmt.Sprintf(`{"ids":[%d,%d]}`, bookmarkID1, bookmarkID2))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/bookmarks", body)

	handleBulkDeleteBookmarks(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]int
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("JSONの解析に失敗しました: %v", err)
	}
	if resp["deleted"] != 2 {
		t.Errorf("削除件数が違います: got %d, want 2", resp["deleted"])
	}
	if got := countBookmarkTags(t); got != 0 {
		t.Errorf("ブックマーク削除後にタグ紐付けが残っています: got %d, want 0", got)
	}
}

// TestHandleBulkDeleteBookmarks_TooManyIDs：一括削除のID数上限を超えると拒否されるかテストします。
func TestHandleBulkDeleteBookmarks_TooManyIDs(t *testing.T) {
	setupTestDB(t)

	ids := make([]string, maxBulkIDs+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("%d", i+1)
	}
	body := strings.NewReader(fmt.Sprintf(`{"ids":[%s]}`, strings.Join(ids, ",")))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/api/bookmarks", body)

	handleBulkDeleteBookmarks(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestAuthMiddleware_RequiresSessionForAPI：APIには有効なセッションが必要なことをテストします。
func TestAuthMiddleware_RequiresSessionForAPI(t *testing.T) {
	resetSessions()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := authMiddleware(next)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/bookmarks", nil)

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if called {
		t.Error("認証なしのAPIリクエストで次のハンドラが呼ばれています")
	}
}

// TestAuthMiddleware_AllowsValidSession：有効なセッションCookieならAPIを通すことをテストします。
func TestAuthMiddleware_AllowsValidSession(t *testing.T) {
	resetSessions()

	sessionsMu.Lock()
	sessions["valid-token"] = time.Now().Add(time.Hour)
	sessionsMu.Unlock()

	handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/bookmarks", nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: "valid-token"})

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusOK)
	}
}

// TestAuthMiddleware_RemovesExpiredSession：期限切れセッションを拒否し、マップから削除することをテストします。
func TestAuthMiddleware_RemovesExpiredSession(t *testing.T) {
	resetSessions()

	sessionsMu.Lock()
	sessions["expired-token"] = time.Now().Add(-time.Hour)
	sessionsMu.Unlock()

	handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/bookmarks", nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: "expired-token"})

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusUnauthorized)
	}

	sessionsMu.Lock()
	_, exists := sessions["expired-token"]
	sessionsMu.Unlock()
	if exists {
		t.Error("期限切れセッションが削除されていません")
	}
}

// TestHandleLogin_SuccessCreatesSessionCookie：正しいパスワードでCookieとセッションが作られるかテストします。
func TestHandleLogin_SuccessCreatesSessionCookie(t *testing.T) {
	resetSessions()
	t.Setenv("SHIRUSHI_PASSWORD", "secret")

	body := strings.NewReader(`{"password":"secret"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/login", body)

	handleLogin(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusOK)
	}

	result := w.Result()
	defer result.Body.Close()

	cookies := result.Cookies()
	if len(cookies) == 0 {
		t.Fatal("セッションCookieがセットされていません")
	}

	var sessionCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == "session" {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("session Cookieが見つかりません")
	}
	if !sessionCookie.HttpOnly {
		t.Error("session Cookie に HttpOnly が付いていません")
	}
	if sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSiteが違います: got %v, want Strict", sessionCookie.SameSite)
	}
	if sessionCookie.Secure {
		t.Error("SHIRUSHI_COOKIE_SECURE 未設定なのに Secure が付いています")
	}

	sessionsMu.Lock()
	_, exists := sessions[sessionCookie.Value]
	sessionsMu.Unlock()
	if !exists {
		t.Error("発行されたセッションがサーバー側に保存されていません")
	}
}

// TestHandleLogin_SecureCookieWhenEnabled：本番HTTPS向け設定でSecure Cookieになるかテストします。
func TestHandleLogin_SecureCookieWhenEnabled(t *testing.T) {
	resetSessions()
	t.Setenv("SHIRUSHI_PASSWORD", "secret")
	t.Setenv("SHIRUSHI_COOKIE_SECURE", "1")

	body := strings.NewReader(`{"password":"secret"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/login", body)

	handleLogin(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusOK)
	}

	result := w.Result()
	defer result.Body.Close()

	var sessionCookie *http.Cookie
	for _, cookie := range result.Cookies() {
		if cookie.Name == "session" {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("session Cookieが見つかりません")
	}
	if !sessionCookie.Secure {
		t.Error("SHIRUSHI_COOKIE_SECURE=1 なのに Secure が付いていません")
	}
}

// TestHandleLogin_WrongPassword：誤ったパスワードではログインできないことをテストします。
func TestHandleLogin_WrongPassword(t *testing.T) {
	resetSessions()
	t.Setenv("SHIRUSHI_PASSWORD", "secret")

	body := strings.NewReader(`{"password":"wrong"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/login", body)

	handleLogin(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusUnauthorized)
	}

	sessionsMu.Lock()
	count := len(sessions)
	sessionsMu.Unlock()
	if count != 0 {
		t.Errorf("失敗ログインでセッションが作られています: got %d, want 0", count)
	}
}

// TestHandleLogin_LocksAfterRepeatedFailures：同じIPからの連続失敗で一時ロックされるかテストします。
func TestHandleLogin_LocksAfterRepeatedFailures(t *testing.T) {
	resetSessions()
	t.Setenv("SHIRUSHI_PASSWORD", "secret")

	baseTime := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return baseTime }
	t.Cleanup(func() { nowFunc = time.Now })

	for i := 0; i < maxLoginFailures-1; i++ {
		body := strings.NewReader(`{"password":"wrong"}`)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/login", body)
		r.RemoteAddr = "203.0.113.10:12345"

		handleLogin(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%d回目の失敗ステータスが違います: got %d, want %d", i+1, w.Code, http.StatusUnauthorized)
		}
	}

	body := strings.NewReader(`{"password":"wrong"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/login", body)
	r.RemoteAddr = "203.0.113.10:12345"

	handleLogin(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("上限到達時のステータスが違います: got %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	body = strings.NewReader(`{"password":"secret"}`)
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/api/login", body)
	r.RemoteAddr = "203.0.113.10:12345"

	handleLogin(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("ロック中の正しいパスワードが拒否されていません: got %d, want %d", w.Code, http.StatusTooManyRequests)
	}
}

// TestHandleLogin_AllowsAfterLockoutExpires：ロック時間が過ぎたらログインできるかテストします。
func TestHandleLogin_AllowsAfterLockoutExpires(t *testing.T) {
	resetSessions()
	t.Setenv("SHIRUSHI_PASSWORD", "secret")

	currentTime := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	nowFunc = func() time.Time { return currentTime }
	t.Cleanup(func() { nowFunc = time.Now })

	for i := 0; i < maxLoginFailures; i++ {
		body := strings.NewReader(`{"password":"wrong"}`)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/login", body)
		r.RemoteAddr = "203.0.113.20:12345"

		handleLogin(w, r)
	}

	currentTime = currentTime.Add(loginLockoutDuration + time.Second)

	body := strings.NewReader(`{"password":"secret"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/login", body)
	r.RemoteAddr = "203.0.113.20:12345"

	handleLogin(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("ロック期限後のログインステータスが違います: got %d, want %d", w.Code, http.StatusOK)
	}
}

// TestGetClientIP_UsesForwardedHeaderFromLocalProxy：ローカルプロキシ経由では転送元IPを使うかテストします。
// Caddy は X-Forwarded-For に実際の接続元IPを末尾に追記するため、末尾を採用します。
// シナリオ: 攻撃者(203.0.113.30)が X-Forwarded-For: 10.0.0.1 を偽装して送信 →
//
//	Caddy が本物の接続元 203.0.113.30 を末尾に追記 →
//	Shirushi は末尾を採用して攻撃者の本物IPを取得します。
func TestGetClientIP_UsesForwardedHeaderFromLocalProxy(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	// 先頭が攻撃者による偽装IP、末尾がCaddyの付加した本物IP
	r.Header.Set("X-Forwarded-For", "10.0.0.1, 203.0.113.30")

	if got := getClientIP(r); got != "203.0.113.30" {
		t.Fatalf("クライアントIPが違います: got %q, want %q", got, "203.0.113.30")
	}
}

// TestGetClientIP_IgnoresForwardedHeaderFromUntrustedRemote：直接接続時は偽装ヘッダーを無視するかテストします。
func TestGetClientIP_IgnoresForwardedHeaderFromUntrustedRemote(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	r.RemoteAddr = "198.51.100.10:12345"
	r.Header.Set("X-Forwarded-For", "203.0.113.30")

	if got := getClientIP(r); got != "198.51.100.10" {
		t.Fatalf("クライアントIPが違います: got %q, want %q", got, "198.51.100.10")
	}
}

// TestHandleLogout_RemovesSessionAndCookie：ログアウトでセッション削除とCookie失効が行われるかテストします。
func TestHandleLogout_RemovesSessionAndCookie(t *testing.T) {
	resetSessions()

	sessionsMu.Lock()
	sessions["logout-token"] = time.Now().Add(time.Hour)
	sessionsMu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: "logout-token"})

	handleLogout(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusOK)
	}

	sessionsMu.Lock()
	_, exists := sessions["logout-token"]
	sessionsMu.Unlock()
	if exists {
		t.Error("ログアウト後もセッションが残っています")
	}

	result := w.Result()
	defer result.Body.Close()

	var expiredCookie *http.Cookie
	for _, cookie := range result.Cookies() {
		if cookie.Name == "session" {
			expiredCookie = cookie
			break
		}
	}
	if expiredCookie == nil {
		t.Fatal("削除用のsession Cookieが返っていません")
	}
	if expiredCookie.MaxAge != -1 {
		t.Errorf("Cookieが失効設定になっていません: got %d, want -1", expiredCookie.MaxAge)
	}
	if !expiredCookie.HttpOnly {
		t.Error("削除用session Cookie に HttpOnly が付いていません")
	}
	if expiredCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("削除用CookieのSameSiteが違います: got %v, want Strict", expiredCookie.SameSite)
	}
	if expiredCookie.Secure {
		t.Error("SHIRUSHI_COOKIE_SECURE 未設定なのに削除用Cookieへ Secure が付いています")
	}
}

// TestHandleLogout_SecureCookieWhenEnabled：Secure Cookie利用時は削除CookieにもSecureが付くかテストします。
func TestHandleLogout_SecureCookieWhenEnabled(t *testing.T) {
	resetSessions()
	t.Setenv("SHIRUSHI_COOKIE_SECURE", "1")

	sessionsMu.Lock()
	sessions["logout-token"] = time.Now().Add(time.Hour)
	sessionsMu.Unlock()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	r.AddCookie(&http.Cookie{Name: "session", Value: "logout-token"})

	handleLogout(w, r)

	result := w.Result()
	defer result.Body.Close()

	var expiredCookie *http.Cookie
	for _, cookie := range result.Cookies() {
		if cookie.Name == "session" {
			expiredCookie = cookie
			break
		}
	}
	if expiredCookie == nil {
		t.Fatal("削除用のsession Cookieが返っていません")
	}
	if !expiredCookie.Secure {
		t.Error("SHIRUSHI_COOKIE_SECURE=1 なのに削除用Cookieへ Secure が付いていません")
	}
}

// TestHandleFetchMetadata_InvalidURL：メタデータ取得でもhttp/https以外のURLを拒否するかテストします。
func TestHandleFetchMetadata_InvalidURL(t *testing.T) {
	body := strings.NewReader(`{"url":"file:///etc/passwd"}`)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/fetch-metadata", body)

	handleFetchMetadata(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestFetchMetadata_RejectsNonHTML：HTMLではないレスポンスをメタデータ取得対象から外すかテストします。
func TestFetchMetadata_RejectsNonHTML(t *testing.T) {
	t.Setenv("SHIRUSHI_ALLOW_PRIVATE_FETCH", "1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"title":"not html"}`)
	}))
	defer server.Close()

	if _, err := fetchMetadata(server.URL); err == nil {
		t.Fatal("HTMLではないレスポンスなのにエラーになっていません")
	}
}

// TestMigrateAddUniqueURL_PreservesBookmarkTags：UNIQUE制約追加マイグレーションでタグ紐付けが残るかテストします。
func TestMigrateAddUniqueURL_PreservesBookmarkTags(t *testing.T) {
	setupTestDB(t)

	bookmarkID := createTestBookmark(t, "https://example.com")
	tagID := createTestTag(t, "go")
	if _, err := db.Exec(
		`INSERT INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)`,
		bookmarkID, tagID,
	); err != nil {
		t.Fatalf("タグ紐付けエラー: %v", err)
	}

	migrateAddUniqueURLOn(db)

	if !hasUniqueURLIndexOn(db) {
		t.Fatal("url カラムに UNIQUE 制約が追加されていません")
	}
	if got := countBookmarkTags(t); got != 1 {
		t.Errorf("マイグレーション後のタグ紐付け件数が違います: got %d, want 1", got)
	}

	tags, err := getTagsByBookmarkID(bookmarkID)
	if err != nil {
		t.Fatalf("タグ取得エラー: %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "go" {
		t.Errorf("マイグレーション後のタグ紐付け内容が違います: got %+v", tags)
	}
}

// TestCleanupOrphanedBookmarkTags：親が存在しない古いタグ紐付けだけ削除されるかテストします。
func TestCleanupOrphanedBookmarkTags(t *testing.T) {
	setupTestDB(t)

	bookmarkID := createTestBookmark(t, "https://example.com")
	validTagID := createTestTag(t, "go")
	orphanTagID := createTestTag(t, "github")

	if _, err := db.Exec(
		`INSERT INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)`,
		bookmarkID, validTagID,
	); err != nil {
		t.Fatalf("有効なタグ紐付け作成エラー: %v", err)
	}

	// 実DBに残っていた古い不整合を再現するため、この挿入時だけ外部キー制約を無効化します。
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatalf("外部キー制約OFFエラー: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)`,
		9999, orphanTagID,
	); err != nil {
		t.Fatalf("孤児タグ紐付け作成エラー: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)`,
		bookmarkID, 9999,
	); err != nil {
		t.Fatalf("存在しないタグへの紐付け作成エラー: %v", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("外部キー制約ONエラー: %v", err)
	}

	cleanupOrphanedBookmarkTagsOn(db)

	if got := countBookmarkTags(t); got != 1 {
		t.Fatalf("クリーンアップ後のタグ紐付け件数が違います: got %d, want 1", got)
	}

	tags, err := getTagsByBookmarkID(bookmarkID)
	if err != nil {
		t.Fatalf("タグ取得エラー: %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "go" {
		t.Errorf("有効なタグ紐付けが残っていません: got %+v", tags)
	}
}

// TestHandleImport_DDDoesNotLeakToPreviousBookmark：<DD> が次のブックマークから漏れないかテストします。
func TestHandleImport_DDDoesNotLeakToPreviousBookmark(t *testing.T) {
	setupTestDB(t)

	doc := `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<DL><p>
    <DT><A HREF="https://first.example" ADD_DATE="1717200000">First</A>
    <DT><A HREF="https://second.example" ADD_DATE="1717200001">Second</A>
    <DD>Second description
</DL><p>`

	w := httptest.NewRecorder()
	r := newMultipartImportRequest(t, doc)

	handleImport(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("ステータスコードが違います: got %d, want %d", w.Code, http.StatusOK)
	}

	var firstExcerpt string
	if err := db.QueryRow(`SELECT excerpt FROM bookmarks WHERE url = ?`, "https://first.example").Scan(&firstExcerpt); err != nil {
		t.Fatalf("1件目の抜粋取得エラー: %v", err)
	}
	if firstExcerpt != "" {
		t.Errorf("1件目に2件目のDDが混ざっています: got %q, want empty", firstExcerpt)
	}

	var secondExcerpt string
	if err := db.QueryRow(`SELECT excerpt FROM bookmarks WHERE url = ?`, "https://second.example").Scan(&secondExcerpt); err != nil {
		t.Fatalf("2件目の抜粋取得エラー: %v", err)
	}
	if secondExcerpt != "Second description" {
		t.Errorf("2件目の抜粋が違います: got %q, want %q", secondExcerpt, "Second description")
	}
}
