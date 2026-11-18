package main

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestBookmarkThumbnailReusesResizedFile(t *testing.T) {
	setupTestDB(t)
	var sourceRequests atomic.Int32
	var original bytes.Buffer
	if err := png.Encode(&original, image.NewRGBA(image.Rect(0, 0, 1280, 640))); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceRequests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(original.Bytes())
	}))
	t.Cleanup(upstream.Close)

	id := createTestBookmark(t, "https://example.test/article")
	otherID := createTestBookmark(t, "https://example.test/another")
	if _, err := db.Exec(`UPDATE bookmarks SET image_url = ?`, upstream.URL+"/image.png"); err != nil {
		t.Fatal(err)
	}

	cacheDir := t.TempDir()
	oldToken := apiToken
	apiToken = "test-token"
	t.Cleanup(func() { apiToken = oldToken })
	mux := http.NewServeMux()
	cache := newThumbnailCache(cacheDir, upstream.Client())
	mux.HandleFunc("GET /api/bookmarks/{id}/thumbnail", func(w http.ResponseWriter, r *http.Request) {
		cache.serve(w, r)
	})
	handler := authMiddleware(mux)
	request := func(bookmarkID int, token, etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/bookmarks/%d/thumbnail", bookmarkID), nil)
		r.Header.Set("Authorization", token)
		r.Header.Set("If-None-Match", etag)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := request(id, "", ""); w.Code != http.StatusUnauthorized || sourceRequests.Load() != 0 {
		t.Fatalf("未認証の応答 = %d、取得回数 = %d", w.Code, sourceRequests.Load())
	}

	const token = "Bearer test-token"
	first := request(id, token, "")
	if first.Code != http.StatusOK || first.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("初回の応答 = %d、Content-Type = %q", first.Code, first.Header().Get("Content-Type"))
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(first.Body.Bytes()))
	if err != nil || config.Width != 640 || config.Height != 320 {
		t.Fatalf("縮小後の画像 = %+v、err = %v", config, err)
	}
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("キャッシュ用の応答ヘッダーがありません: %v", first.Header())
	}

	cache = newThumbnailCache(cacheDir, upstream.Client())
	if w := request(otherID, token, etag); w.Code != http.StatusNotModified || sourceRequests.Load() != 1 {
		t.Fatalf("再起動後の応答 = %d、取得回数 = %d", w.Code, sourceRequests.Load())
	}
	if _, err := db.Exec(`UPDATE bookmarks SET image_url = ? WHERE id = ?`, upstream.URL+"/replacement.png", id); err != nil {
		t.Fatal(err)
	}
	if w := request(id, token, etag); w.Code != http.StatusOK || w.Header().Get("ETag") == etag || sourceRequests.Load() != 2 {
		t.Fatalf("画像URL変更後の応答 = %d、ETag = %q、取得回数 = %d", w.Code, w.Header().Get("ETag"), sourceRequests.Load())
	}
}

func TestBookmarkThumbnailCoalescesFailuresAndFallsBack(t *testing.T) {
	setupTestDB(t)
	var sourceRequests atomic.Int32
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceRequests.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		fmt.Fprint(w, "画像ではありません")
	}))
	t.Cleanup(upstream.Close)
	cache := newThumbnailCache(t.TempDir(), upstream.Client())
	cache.warm(upstream.URL)
	_, first := cache.prepare(upstream.URL)
	_, second := cache.prepare(upstream.URL)
	close(release)
	if first == nil || first != second {
		t.Fatal("同じ画像の生成は同じジョブで処理してください")
	}
	<-first

	id := createTestBookmark(t, "https://example.test/article")
	if _, err := db.Exec(`UPDATE bookmarks SET image_url = ? WHERE id = ?`, upstream.URL, id); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.SetPathValue("id", fmt.Sprint(id))
	w := httptest.NewRecorder()
	cache.serve(w, r)
	if w.Code != http.StatusFound || w.Header().Get("Location") != upstream.URL || sourceRequests.Load() != 1 {
		t.Fatalf("フォールバックの応答 = %d、Location = %q、取得回数 = %d", w.Code, w.Header().Get("Location"), sourceRequests.Load())
	}
}
