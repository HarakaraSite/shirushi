package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	db, err = sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("テスト用DB作成エラー: %v", err)
	}

	// テスト終了時にDBを閉じます。t.Cleanup に登録すると自動で呼ばれます。
	t.Cleanup(func() {
		db.Close()
	})

	// テーブルを作成します（本番と同じ createTable 関数を使います）。
	createTable()
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
