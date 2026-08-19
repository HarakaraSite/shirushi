package main

// bookmark_status.go：登録済みURLのHTTP状態を非同期で確認する処理を担当するファイルです。

import (
	"context"       // APIリクエスト終了後も続くbackground処理の起点に使います
	"encoding/json" // ジョブの状態をブラウザへJSON形式で返すために使います
	"net/http"      // 外部URLへのGETとAPIハンドラに使います
	"sync"          // ジョブ状態と並行workerを安全に共有するために使います
)

const (
	// notFoundThumbnailURL：404と判定したbookmarkへ設定する同梱画像のURLです。
	notFoundThumbnailURL = "/404.svg"
	// maxStatusCheckWorkers：同時アクセス数を抑え、相手サイトとShirushiの負荷を制限します。
	maxStatusCheckWorkers = 5
	// htmlDocumentAccept：通常のブラウザと同様にHTMLページを要求していることを接続先へ伝えます。
	// Acceptがない機械的なGETへ404を返すサイトで、存在するページを誤判定しないために使います。
	htmlDocumentAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

	bookmark404JobIdle      = "idle"
	bookmark404JobRunning   = "running"
	bookmark404JobCompleted = "completed"
	bookmark404JobFailed    = "failed"
)

// bookmarkURLCheck：DBから読み出した確認対象です。
type bookmarkURLCheck struct {
	ID  int
	URL string
}

// bookmarkURLCheckResult：1件のHTTP確認結果です。
type bookmarkURLCheckResult struct {
	ID        int
	URL       string
	NotFound  bool
	CheckFail bool
}

// Bookmark404CheckStatus：404チェックジョブの状態と集計結果です。
// checkedは処理済み件数、totalは開始時点の登録件数なので、画面で進捗を表示できます。
type Bookmark404CheckStatus struct {
	Status   string `json:"status"`
	Total    int    `json:"total"`
	Checked  int    `json:"checked"`
	NotFound int    `json:"not_found"`
	Failed   int    `json:"failed"`
	Error    string `json:"error,omitempty"`
}

// bookmark404JobState：複数のHTTPリクエストとbackground処理から共有するジョブ状態です。
// Mutexで保護し、同時に2つの全件チェックが走らないようにします。
type bookmark404JobState struct {
	mu     sync.Mutex
	status Bookmark404CheckStatus
}

var bookmark404Job = bookmark404JobState{
	status: Bookmark404CheckStatus{Status: bookmark404JobIdle},
}

// handleStartBookmark404Check：対象を確定してジョブを開始し、待たずに202を返します。
func handleStartBookmark404Check(w http.ResponseWriter, r *http.Request) {
	targets, err := loadBookmarkURLChecks()
	if err != nil {
		http.Error(w, "bookmarkの読み込みに失敗しました", http.StatusInternalServerError)
		return
	}

	status, started := bookmark404Job.start(len(targets))
	if !started {
		http.Error(w, "404チェックはすでに実行中です", http.StatusConflict)
		return
	}

	// request contextは202を返した時点で終了するため、background contextを使います。
	// URLごとの10秒timeoutはHTTPクライアント側に残るので、応答しないサイトでも停止しません。
	go runBookmark404Check(context.Background(), targets)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(status)
}

// handleGetBookmark404CheckStatus：画面のポーリング用に現在のジョブ状態を返します。
func handleGetBookmark404CheckStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bookmark404Job.snapshot())
}

// loadBookmarkURLChecks：ジョブ開始時点に登録されている全bookmarkを読み出します。
func loadBookmarkURLChecks() ([]bookmarkURLCheck, error) {
	rows, err := db.Query(`SELECT id, url FROM bookmarks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []bookmarkURLCheck
	for rows.Next() {
		var target bookmarkURLCheck
		if err := rows.Scan(&target.ID, &target.URL); err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

// runBookmark404Check：HTTPレスポンスとは独立して全URLを確認し、404画像を更新します。
func runBookmark404Check(ctx context.Context, targets []bookmarkURLCheck) {
	results := checkBookmarkURLs(ctx, targets)
	var notFoundTargets []bookmarkURLCheck
	for result := range results {
		bookmark404Job.record(result)
		if result.NotFound && !result.CheckFail {
			notFoundTargets = append(notFoundTargets, bookmarkURLCheck{ID: result.ID, URL: result.URL})
		}
	}

	// 全更新を1つのトランザクションにまとめ、途中失敗で一部だけ変わることを防ぎます。
	tx, err := db.Begin()
	if err != nil {
		bookmark404Job.fail("サムネイルの更新に失敗しました")
		return
	}
	for _, target := range notFoundTargets {
		// modified_atも更新し、404画像へ変更された時刻を通常のbookmark更新と同様に残します。
		// 非同期確認中にURLが編集された場合は、古いURLの結果で新しい画像を上書きしません。
		if _, err := tx.Exec(`UPDATE bookmarks SET image_url = ?, modified_at = CURRENT_TIMESTAMP WHERE id = ? AND url = ?`, notFoundThumbnailURL, target.ID, target.URL); err != nil {
			tx.Rollback()
			bookmark404Job.fail("サムネイルの更新に失敗しました")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		bookmark404Job.fail("サムネイルの更新に失敗しました")
		return
	}
	bookmark404Job.complete()
}

// start：実行中でなければ集計値を初期化して、新しいジョブをrunningにします。
func (j *bookmark404JobState) start(total int) (Bookmark404CheckStatus, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.status.Status == bookmark404JobRunning {
		return j.status, false
	}
	j.status = Bookmark404CheckStatus{Status: bookmark404JobRunning, Total: total}
	return j.status, true
}

// snapshot：Mutex内の値をコピーして返し、JSON変換中はロックを保持しません。
func (j *bookmark404JobState) snapshot() Bookmark404CheckStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status
}

// record：URLを1件確認するたびに進捗と集計値を更新します。
func (j *bookmark404JobState) record(result bookmarkURLCheckResult) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status.Checked++
	if result.CheckFail {
		j.status.Failed++
	} else if result.NotFound {
		j.status.NotFound++
	}
}

func (j *bookmark404JobState) complete() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status.Status = bookmark404JobCompleted
}

func (j *bookmark404JobState) fail(message string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.status.Status = bookmark404JobFailed
	j.status.Error = message
}

// checkBookmarkURLs：worker数を制限しながら、各URLの最終HTTP状態を確認します。
func checkBookmarkURLs(ctx context.Context, targets []bookmarkURLCheck) <-chan bookmarkURLCheckResult {
	results := make(chan bookmarkURLCheckResult, len(targets))
	jobs := make(chan bookmarkURLCheck, len(targets))
	for _, target := range targets {
		jobs <- target
	}
	close(jobs)

	workerCount := min(maxStatusCheckWorkers, len(targets))
	client := newSafeHTTPClient()
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for target := range jobs {
				// HEADは対応が不完全なサイトがあるためGETを使います。
				// 本文は読まずに閉じるので、404判定に不要なデータは保持しません。
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
				if err != nil {
					results <- bookmarkURLCheckResult{ID: target.ID, URL: target.URL, CheckFail: true}
					continue
				}
				req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Shirushi/1.0)")
				req.Header.Set("Accept", htmlDocumentAccept)
				resp, err := client.Do(req)
				if err != nil {
					results <- bookmarkURLCheckResult{ID: target.ID, URL: target.URL, CheckFail: true}
					continue
				}
				resp.Body.Close()
				results <- bookmarkURLCheckResult{ID: target.ID, URL: target.URL, NotFound: resp.StatusCode == http.StatusNotFound}
			}
		}()
	}

	go func() {
		workers.Wait()
		close(results)
	}()
	return results
}
