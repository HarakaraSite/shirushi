package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func summaryTestMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/capabilities", handleHenjiCapabilities)
	mux.HandleFunc("POST /api/bookmarks/{id}/summary", handleStartBookmarkSummary)
	return mux
}

func replaceSummaryJobFunctions(t *testing.T, available func(HenjiSummarySettings) bool, fetch func(string) (string, error), run func(context.Context, HenjiSummarySettings, string) (string, error)) {
	t.Helper()
	oldAvailable, oldFetch, oldRun := henjiSummaryAvailable, summarySourceFetcher, summaryJobRunner
	oldSettings := henjiSummarySettings
	henjiSummaryAvailable, summarySourceFetcher, summaryJobRunner = available, fetch, run
	t.Cleanup(func() {
		henjiSummaryAvailable, summarySourceFetcher, summaryJobRunner = oldAvailable, oldFetch, oldRun
		henjiSummarySettings = oldSettings
	})
}

func waitForSummaryExcerpt(t *testing.T, id int, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		bookmark, err := getBookmarkByID(id)
		if err == nil && bookmark.Excerpt == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	bookmark, err := getBookmarkByID(id)
	if err != nil {
		t.Fatalf("bookmarkを読めません: %v", err)
	}
	t.Fatalf("Excerptが更新されません: got=%q want=%q", bookmark.Excerpt, want)
}

func TestHandleStartBookmarkSummary_AcceptsBeforeCompletion(t *testing.T) {
	setupTestDB(t)
	henjiSummarySettings = HenjiSummarySettings{Path: "henji", API: "openrouter", Model: "test", MaxInputBytes: 4000000}
	id := createTestBookmark(t, "https://example.com/article")
	started := make(chan struct{})
	release := make(chan struct{})
	replaceSummaryJobFunctions(t,
		func(HenjiSummarySettings) bool { return true },
		func(string) (string, error) { return "本文候補", nil },
		func(context.Context, HenjiSummarySettings, string) (string, error) {
			close(started)
			<-release
			return "要約結果", nil
		},
	)

	w := httptest.NewRecorder()
	summaryTestMux().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/bookmarks/"+strconv.Itoa(id)+"/summary", nil))
	if w.Code != http.StatusAccepted {
		t.Fatalf("開始受付が202ではありません: %d", w.Code)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("非同期ジョブが開始されません")
	}
	close(release)
	waitForSummaryExcerpt(t, id, "要約結果")
}

func TestHandleStartBookmarkSummary_UnavailableDoesNothing(t *testing.T) {
	setupTestDB(t)
	henjiSummarySettings = HenjiSummarySettings{Path: "missing"}
	id := createTestBookmark(t, "https://example.com/article")
	called := false
	replaceSummaryJobFunctions(t,
		func(HenjiSummarySettings) bool { return false },
		func(string) (string, error) { called = true; return "本文", nil },
		func(context.Context, HenjiSummarySettings, string) (string, error) {
			called = true
			return "要約", nil
		},
	)

	mux := summaryTestMux()
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/bookmarks/"+strconv.Itoa(id)+"/summary", nil))
	if w.Code != http.StatusNoContent || called {
		t.Fatalf("未導入時の動作が違います: status=%d called=%t", w.Code, called)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
	var response map[string]bool
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil || response["henji_summary"] {
		t.Fatalf("capabilityが正しくありません: response=%v err=%v", response, err)
	}
}

func TestBookmarkSummaryJobs_RunAtMostThreeAtOnce(t *testing.T) {
	setupTestDB(t)
	henjiSummarySettings = HenjiSummarySettings{Path: "henji", API: "openrouter", Model: "test", MaxInputBytes: 4000000}
	var active, maxActive int32
	started := make(chan struct{}, 4)
	completed := make(chan struct{}, 4)
	release := make(chan struct{})
	replaceSummaryJobFunctions(t,
		func(HenjiSummarySettings) bool { return true },
		func(string) (string, error) { return "本文候補", nil },
		func(context.Context, HenjiSummarySettings, string) (string, error) {
			defer func() { completed <- struct{}{} }()
			current := atomic.AddInt32(&active, 1)
			for {
				seen := atomic.LoadInt32(&maxActive)
				if current <= seen || atomic.CompareAndSwapInt32(&maxActive, seen, current) {
					break
				}
			}
			started <- struct{}{}
			<-release
			atomic.AddInt32(&active, -1)
			return "要約", nil
		},
	)

	mux := summaryTestMux()
	for i := 0; i < 4; i++ {
		id := createTestBookmark(t, "https://example.com/article/"+strconv.Itoa(i))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/bookmarks/"+strconv.Itoa(id)+"/summary", nil))
		if w.Code != http.StatusAccepted {
			t.Fatalf("%d件目が202ではありません: %d", i+1, w.Code)
		}
	}
	for i := 0; i < maxConcurrentSummaryJobs; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("3件のジョブが開始されません")
		}
	}
	if got := atomic.LoadInt32(&maxActive); got != maxConcurrentSummaryJobs {
		t.Fatalf("同時実行数が違います: got=%d want=%d", got, maxConcurrentSummaryJobs)
	}
	close(release)
	for i := 0; i < 4; i++ {
		select {
		case <-completed:
		case <-time.After(time.Second):
			t.Fatal("すべての要約ジョブが終了しません")
		}
	}
}
