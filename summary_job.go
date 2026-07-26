package main

// summary_job.go：保存済みbookmarkの本文を非同期で要約し、成功時だけExcerptを更新します。

import (
	"context"       // Henjiアダプターへキャンセル可能な実行文脈を渡すために使うパッケージ
	"encoding/json" // capabilityレスポンスをJSONにするために使うパッケージ
	"net/http"      // 要約開始とcapabilityのAPIを実装するために使うパッケージ
	"os/exec"       // PATHまたは明示パスにHenjiがあるか確認するために使うパッケージ
	"strconv"       // URLのbookmark IDを整数へ変換するために使うパッケージ
)

const maxConcurrentSummaryJobs = 3

// summaryJobSemaphore：URL取得からDB更新までを3件に制限します。
// 同じbookmarkの重複実行は意図的に排除しません。
var summaryJobSemaphore = make(chan struct{}, maxConcurrentSummaryJobs)

// テストでは実Henjiや外部HTTPを呼ばずに境界を確認できるよう、処理を関数として差し替えます。
var (
	henjiSummaryAvailable = func(settings HenjiSummarySettings) bool {
		_, err := exec.LookPath(settings.Path)
		return err == nil
	}
	summarySourceFetcher = func(rawURL string) (string, error) {
		html, _, err := fetchHTML(rawURL, 2*1024*1024)
		if err != nil {
			return "", err
		}
		return extractSummarySource(html), nil
	}
	summaryJobRunner = runHenjiSummary
)

// handleHenjiCapabilities：Henjiがこの起動環境で使えるかだけを返します。
func handleHenjiCapabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"henji_summary": henjiSummaryAvailable(henjiSummarySettings)})
}

// handleStartBookmarkSummary：保存済みbookmark一件の要約を受け付けます。
// 成功・失敗の結果を待たずに202を返すため、ブラウザ操作を長時間止めません。
func handleStartBookmarkSummary(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "IDが不正です", http.StatusBadRequest)
		return
	}
	bookmark, err := getBookmarkByID(id)
	if err != nil {
		http.Error(w, "指定されたIDが見つかりません", http.StatusNotFound)
		return
	}
	if !henjiSummaryAvailable(henjiSummarySettings) {
		// 未導入時はジョブを作らず、UIもボタンを出さない契約です。
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// モーダル上の未保存値ではなく、受付時点でDBに保存済みのURLだけをコピーします。
	bookmarkID, bookmarkURL := bookmark.ID, bookmark.URL
	go runBookmarkSummaryJob(bookmarkID, bookmarkURL)
	w.WriteHeader(http.StatusAccepted)
}

// runBookmarkSummaryJob：1ジョブを最後まで実行します。途中失敗は静かに終えます。
func runBookmarkSummaryJob(bookmarkID int, bookmarkURL string) {
	summaryJobSemaphore <- struct{}{}
	defer func() { <-summaryJobSemaphore }()

	source, err := summarySourceFetcher(bookmarkURL)
	if err != nil || source == "" {
		return
	}
	summary, err := summaryJobRunner(context.Background(), henjiSummarySettings, source)
	if err != nil {
		return
	}
	// 条件を追加しないため、複数ジョブは完了順で最後の要約を残します。
	// 削除済みなら0件更新となり、bookmarkを復活させません。
	_, _ = db.Exec(`UPDATE bookmarks SET excerpt = ?, modified_at = CURRENT_TIMESTAMP WHERE id = ?`, summary, bookmarkID)
}
