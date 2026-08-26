package main

// importexport.go：Netscape Bookmark形式（.html）のインポート・エクスポートと、
// インポート後のバックグラウンドサムネイル取得を担当するファイルです。

import (
	"encoding/json" // レスポンスをJSON形式で返すパッケージ
	"fmt"           // エクスポートHTML生成（fmt.Fprintln, fmt.Fprintf）に使うパッケージ
	"html"          // HTML特殊文字のエスケープ・アンエスケープに使うパッケージ
	"io"            // ファイル本文の読み取りに使うパッケージ
	"log"           // サムネイル保存エラーのログ出力に使うパッケージ
	"net/http"      // HTTPハンドラ・エラーレスポンスに使うパッケージ
	"regexp"        // インポートHTMLからブックマークを抽出する正規表現パッケージ
	"strconv"       // ADD_DATE（Unixタイムスタンプ）を整数に変換するパッケージ
	"strings"       // タグ名の分割・トリムに使うパッケージ
	"time"          // ADD_DATE から time.Time への変換・スリープに使うパッケージ
)

// handleExport：全ブックマークをNetscape Bookmark形式のHTMLとして返すAPIです。
// ブラウザの「ブックマークをエクスポート」と同じ形式なので、他のアプリにインポートできます。
func handleExport(w http.ResponseWriter, r *http.Request) {
	// タグを含む全ブックマークを取得します。
	rows, err := db.Query(`
		SELECT id, url, title, excerpt, created_at
		FROM bookmarks
		ORDER BY id ASC`)
	if err != nil {
		http.Error(w, "データベースエラー", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type exportItem struct {
		ID        int
		URL       string
		Title     string
		Excerpt   string
		CreatedAt time.Time
		Tags      []Tag
	}

	var items []exportItem
	for rows.Next() {
		var item exportItem
		if err := rows.Scan(&item.ID, &item.URL, &item.Title, &item.Excerpt, &item.CreatedAt); err != nil {
			http.Error(w, "読み取りエラー", http.StatusInternalServerError)
			return
		}
		items = append(items, item)
	}
	rows.Close()

	// タグを取得します。
	for i := range items {
		items[i].Tags, err = getTagsByBookmarkID(items[i].ID)
		if err != nil {
			http.Error(w, "タグ取得エラー", http.StatusInternalServerError)
			return
		}
	}

	// Netscape Bookmark形式のHTMLを生成します。
	// fmt.Fprintf でレスポンスに直接書き込むことで、メモリ効率よく処理できます。
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	// Content-Disposition でブラウザにファイルとしてダウンロードさせます。
	w.Header().Set("Content-Disposition", `attachment; filename="shirushi-bookmarks.html"`)

	fmt.Fprintln(w, `<!DOCTYPE NETSCAPE-Bookmark-file-1>`)
	fmt.Fprintln(w, `<!-- Shirushi によってエクスポートされたブックマーク -->`)
	fmt.Fprintln(w, `<META HTTP-EQUIV="Content-Type" CONTENT="text/html; charset=UTF-8">`)
	fmt.Fprintln(w, `<TITLE>Bookmarks</TITLE>`)
	fmt.Fprintln(w, `<H1>Bookmarks</H1>`)
	fmt.Fprintln(w, `<DL><p>`)

	for _, item := range items {
		// タグをカンマ区切りの文字列に変換します。
		tagNames := make([]string, len(item.Tags))
		for i, t := range item.Tags {
			tagNames[i] = t.Name
		}
		tags := strings.Join(tagNames, ",")

		// ADD_DATE は Unix タイムスタンプ（秒）です。
		addDate := item.CreatedAt.Unix()

		// タイトルやURLに < > & " などのHTML特殊文字が含まれていると
		// 生成されるHTMLの構造が壊れてしまうため（例: タイトルが A<B>C のページ）、
		// html.EscapeString で安全な表記（&lt; など）に変換してから埋め込みます。
		fmt.Fprintf(w, `    <DT><A HREF="%s" ADD_DATE="%d" TAGS="%s">%s</A>`+"\n",
			html.EscapeString(item.URL), addDate,
			html.EscapeString(tags), html.EscapeString(item.Title))

		// excerptがある場合は <DD> タグで説明文を追加します。
		if item.Excerpt != "" {
			fmt.Fprintf(w, `    <DD>%s`+"\n", html.EscapeString(item.Excerpt))
		}
	}

	fmt.Fprintln(w, `</DL><p>`)
}

// handleImport：Netscape Bookmark形式のHTMLファイルを読み込んでDBに登録するAPIです。
func handleImport(w http.ResponseWriter, r *http.Request) {
	// multipart/form-data 形式でファイルを受け取ります。
	// ParseMultipartForm の引数は「メモリ上に保持する最大量」であって、
	// アップロード全体の上限ではないため、MaxBytesReader でも制限します。
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes)
	// ParseMultipartForm の引数は最大メモリ使用量（バイト）です。
	// #nosec G120 -- MaxBytesReader immediately above bounds the complete request body.
	if err := r.ParseMultipartForm(maxImportBytes); err != nil {
		http.Error(w, "ファイルの解析に失敗しました", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "ファイルが見つかりません", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// ファイルの内容をすべて読み込みます。
	content, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "ファイルの読み込みに失敗しました", http.StatusInternalServerError)
		return
	}
	// 変数名を doc にしているのは、エスケープ処理で使う標準パッケージ html と
	// 名前が衝突（シャドーイング）しないようにするためです。
	doc := string(content)

	// <DT><A ...> のパターンでブックマークを抽出します。
	// (?i) は大文字小文字を区別しないオプションです。
	// [^>]+ や [^<]* は改行を含む任意の文字にマッチするため、(?s) は不要です。
	reBookmark := regexp.MustCompile(`(?i)<DT><A\s([^>]+)>([^<]*)</A>`)
	reHref := regexp.MustCompile(`(?i)HREF="([^"]+)"`)
	reAddDate := regexp.MustCompile(`(?i)ADD_DATE="([^"]+)"`)
	reTags := regexp.MustCompile(`(?i)TAGS="([^"]*)"`)
	// <DD> タグで説明文を取得します。
	reDD := regexp.MustCompile(`(?i)<DD>([^\n<]+)`)

	matches := reBookmark.FindAllStringSubmatchIndex(doc, -1)

	imported := 0
	skipped := 0
	// バックグラウンドでサムネイルを取得するために、新規登録した ID を収集します。
	var importedIDs []int64

	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "トランザクション開始エラー", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	for i, matchIdx := range matches {
		// matchIdx[2],matchIdx[3] が属性部分、matchIdx[4],matchIdx[5] がタイトルです。
		attrs := doc[matchIdx[2]:matchIdx[3]]
		// エクスポート時に &lt; などへエスケープされた特殊文字を元に戻します。
		// （ブラウザがエクスポートしたファイルも同様にエスケープされています）
		title := html.UnescapeString(strings.TrimSpace(doc[matchIdx[4]:matchIdx[5]]))

		hrefMatch := reHref.FindStringSubmatch(attrs)
		if len(hrefMatch) < 2 {
			continue
		}
		url := html.UnescapeString(hrefMatch[1])
		validatedURL, err := validateHTTPURL(url)
		if err != nil {
			skipped++
			continue
		}
		url = validatedURL

		// ADD_DATE（Unixタイムスタンプ）を time.Time に変換します。
		var createdAt time.Time
		if m := reAddDate.FindStringSubmatch(attrs); len(m) > 1 {
			if ts, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				createdAt = time.Unix(ts, 0)
			}
		}
		if createdAt.IsZero() {
			createdAt = time.Now()
		}

		// タグ名をカンマで分割します（エスケープも元に戻します）。
		var tagNames []string
		if m := reTags.FindStringSubmatch(attrs); len(m) > 1 && m[1] != "" {
			for _, t := range strings.Split(html.UnescapeString(m[1]), ",") {
				if name := strings.TrimSpace(t); name != "" {
					tagNames = append(tagNames, name)
				}
			}
		}

		// <DD> タグの説明文を取得します（<A>タグの直後を探します）。
		excerpt := ""
		// 次の <DT><A> までを現在のブックマークの範囲として扱います。
		// 範囲を区切らないと、現在のブックマークに <DD> がない場合に
		// 次のブックマークの説明文を誤って拾うことがあります。
		nextBookmarkStart := len(doc)
		if i+1 < len(matches) {
			nextBookmarkStart = matches[i+1][0]
		}
		afterA := doc[matchIdx[1]:nextBookmarkStart]
		if m := reDD.FindStringSubmatch(afterA); len(m) > 1 {
			excerpt = html.UnescapeString(strings.TrimSpace(m[1]))
		}

		// URLが重複している場合はスキップします（INSERT OR IGNORE）。
		result, err := tx.Exec(
			`INSERT OR IGNORE INTO bookmarks (url, title, excerpt, created_at) VALUES (?, ?, ?, ?)`,
			url, title, excerpt, createdAt,
		)
		if err != nil {
			http.Error(w, "保存エラー: "+err.Error(), http.StatusInternalServerError)
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			// 既にURLが存在していた場合はスキップします。
			skipped++
			continue
		}

		bookmarkID, _ := result.LastInsertId()
		importedIDs = append(importedIDs, bookmarkID)

		// タグを処理します。存在しないタグは新規作成します。
		for _, name := range tagNames {
			// INSERT OR IGNORE でタグが存在しなければ作成します。
			if _, err := tx.Exec(`INSERT OR IGNORE INTO tags (name) VALUES (?)`, name); err != nil {
				http.Error(w, "タグ保存エラー: "+err.Error(), http.StatusInternalServerError)
				return
			}

			var tagID int
			if err := tx.QueryRow(`SELECT id FROM tags WHERE name = ?`, name).Scan(&tagID); err != nil {
				http.Error(w, "タグ取得エラー: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if tagID > 0 {
				if _, err := tx.Exec(
					`INSERT OR IGNORE INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)`,
					bookmarkID, tagID,
				); err != nil {
					http.Error(w, "タグ紐付けエラー: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}
		}

		imported++
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "コミットエラー: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 新規登録したブックマークのサムネイルをバックグラウンドで取得します。
	// インポートのレスポンスはここで即座に返し、取得処理は非同期で行います。
	if len(importedIDs) > 0 {
		go batchFetchThumbnails(importedIDs)
	}

	// 結果をJSONで返します。
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{
		"imported": imported,
		"skipped":  skipped,
	})
}

// batchFetchThumbnails：インポート後にバックグラウンドでサムネイルを取得します。
// image_url が空のブックマークだけを対象に、1件ずつ順番に取得します。
// 各リクエストの間に500msのウェイトを入れ、外部サーバーへの負荷を抑えます。
func batchFetchThumbnails(ids []int64) {
	for _, id := range ids {
		// image_url がすでに入っているものはスキップします。
		//（インポートファイルに image_url が含まれていた場合など）
		var rawURL string
		err := db.QueryRow(
			`SELECT url FROM bookmarks WHERE id = ? AND (image_url IS NULL OR image_url = '')`,
			id,
		).Scan(&rawURL)
		if err != nil {
			// 該当なし（image_url 済み or 削除済み）はスキップ
			continue
		}

		meta, err := fetchMetadata(rawURL)
		if err != nil || meta.ImageURL == "" {
			// 取得失敗・画像なしはスキップ（エラーにはしない）
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if _, err := db.Exec(
			`UPDATE bookmarks SET image_url = ? WHERE id = ? AND (image_url IS NULL OR image_url = '')`,
			meta.ImageURL, id,
		); err != nil {
			log.Printf("サムネイル保存エラー (id=%d): %v", id, err)
		}

		// 外部サーバーへの連続アクセスを避けるためのウェイトです。
		time.Sleep(500 * time.Millisecond)
	}
}
