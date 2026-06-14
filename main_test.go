package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
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

	sessionsMu.Lock()
	_, exists := sessions[sessionCookie.Value]
	sessionsMu.Unlock()
	if !exists {
		t.Error("発行されたセッションがサーバー側に保存されていません")
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

	migrateAddUniqueURL()

	if !hasUniqueURLIndex() {
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

	cleanupOrphanedBookmarkTags()

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
