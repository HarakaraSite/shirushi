package main

// bookmark.go：ブックマークのCRUD操作・一覧取得・一括削除を担当するファイルです。
// タグとの紐付けヘルパー（syncBookmarkTags, getTagsByBookmarkID, getBookmarkByID）も含みます。

import (
	"database/sql"  // sql.ErrNoRows で未登録URLを404と区別するために使うパッケージ
	"encoding/json" // レスポンスをJSON形式で返すパッケージ
	"fmt"           // SQLのプレースホルダー生成・エラーメッセージに使うパッケージ
	"net/http"      // HTTPハンドラ・エラーレスポンスに使うパッケージ
	"strconv"       // URLパスのID文字列を整数に変換するパッケージ
	"strings"       // WHERE句・プレースホルダーの文字列組み立てに使うパッケージ
	"time"          // addOneMonth での日付計算に使うパッケージ
)

// addOneMonth："YYYY-MM" 形式の文字列に1ヶ月加算して返します。
// 日付範囲の上限計算（その月の終わりまで含める）に使います。
// 例: "2024-03" → "2024-04"
func addOneMonth(yyyyMM string) string {
	t, err := time.Parse("2006-01", yyyyMM)
	if err != nil {
		return yyyyMM
	}
	return t.AddDate(0, 1, 0).Format("2006-01")
}

// handleGetBookmarks：登録されているブックマークを一覧で返すAPIです。
// クエリパラメータ:
//
//	?q=keyword         タイトル・URL・excerptで絞り込み検索
//	?tag=name          タグ名で絞り込み（"__untagged__" でタグなし絞り込み）
//	?date_from=YYYY-MM 登録日の開始月（例: 2024-01）
//	?date_to=YYYY-MM   登録日の終了月（例: 2024-03、その月末まで含む）
//	?page=N            ページ番号（デフォルト1、1始まり）
//	?limit=N           1ページあたりの件数（デフォルト50）
func handleGetBookmarks(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	dateFrom := strings.TrimSpace(r.URL.Query().Get("date_from"))
	dateTo := strings.TrimSpace(r.URL.Query().Get("date_to"))

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 1 {
		page = p
	}
	offset := (page - 1) * limit

	// "__untagged__" は「タグが1つも付いていないブックマーク」を表す特殊値です。
	const untaggedToken = "__untagged__" // #nosec G101 -- Query sentinel, not a credential.

	// ── WHERE句を動的に組み立てる ────────────────────────────────
	// 条件が増えても switch のケース数が爆発しないよう、
	// 条件文字列を slice に積み上げて最後に JOIN します。
	//
	// タグ絞り込みが必要な場合だけ JOIN を追加します。
	fromClause := "FROM bookmarks b"
	var conditions []string
	var args []interface{}

	// タグ条件
	if tag == untaggedToken {
		// NOT EXISTS：bookmark_tags に1件も紐付きがないブックマーク
		conditions = append(conditions,
			"NOT EXISTS (SELECT 1 FROM bookmark_tags bt WHERE bt.bookmark_id = b.id)")
	} else if tag != "" {
		// INNER JOIN でタグに紐付くブックマークだけに絞ります。
		// DISTINCT は JOIN で行が増えても重複カウントしないために必要です。
		fromClause += " INNER JOIN bookmark_tags bt ON b.id = bt.bookmark_id" +
			" INNER JOIN tags t ON t.id = bt.tag_id"
		conditions = append(conditions, "t.name = ?")
		args = append(args, tag)
	}

	// キーワード条件
	if q != "" {
		// SQLのLIKEでは % と _ がワイルドカードとして機能します。
		// ユーザーが "100%" や "file_name" と検索したとき、意図しない部分一致になるのを防ぐため
		// これらの文字をエスケープしてから % で囲みます。
		escaped := strings.NewReplacer(`%`, `\%`, `_`, `\_`).Replace(q)
		like := "%" + escaped + "%"

		// 日付パターン検出：
		//   6桁の数字 "202507" → created_at LIKE "2025-07%"（7月全体）
		//   4桁の数字 "2025"   → created_at LIKE "2025%"  （2025年全体）
		// strftime は保存フォーマットによって動作しないことがあるため、
		// 文字列の先頭を直接 LIKE で比較する方式にしています。
		// dateLike は純粋な数字から生成するためエスケープ不要です。
		dateLike := ""
		if _, err := strconv.Atoi(q); err == nil {
			switch len(q) {
			case 6: // YYYYMM → "YYYY-MM%"
				dateLike = q[:4] + "-" + q[4:6] + "%"
			case 4: // YYYY   → "YYYY%"
				dateLike = q + "%"
			}
		}

		// ESCAPE '\' を指定することで、\% や \_ をリテラルとして扱います。
		if dateLike != "" {
			conditions = append(conditions,
				`(b.title LIKE ? ESCAPE '\' OR b.url LIKE ? ESCAPE '\' OR b.excerpt LIKE ? ESCAPE '\' OR b.created_at LIKE ?)`)
			args = append(args, like, like, like, dateLike)
		} else {
			conditions = append(conditions,
				`(b.title LIKE ? ESCAPE '\' OR b.url LIKE ? ESCAPE '\' OR b.excerpt LIKE ? ESCAPE '\')`)
			args = append(args, like, like, like)
		}
	}

	// 日付条件（YYYY-MM → SQL の DATETIME と比較）
	// date_from: その月の1日 00:00:00 以降
	if dateFrom != "" {
		conditions = append(conditions, "b.created_at >= ?")
		args = append(args, dateFrom+"-01 00:00:00")
	}
	// date_to: 翌月の1日 00:00:00 未満（= その月末まで含む）
	if dateTo != "" {
		conditions = append(conditions, "b.created_at < ?")
		args = append(args, addOneMonth(dateTo)+"-01 00:00:00")
	}

	// WHERE句：条件がある場合のみ付けます。
	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// ── ① 総件数を取得 ──────────────────────────────────────────
	// DISTINCT b.id：タグJOINで行が増えても重複カウントを防ぎます。
	countSQL := fmt.Sprintf("SELECT COUNT(DISTINCT b.id) %s %s", fromClause, whereClause)
	var total int
	if err := db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		http.Error(w, "件数取得エラー", http.StatusInternalServerError)
		return
	}

	// ── ② ブックマークをページ単位で取得 ─────────────────────────
	// DISTINCT：タグJOINで同じブックマークが複数行になるのを防ぎます。
	dataSQL := fmt.Sprintf(`
		SELECT DISTINCT b.id, b.url, b.title, b.excerpt, b.author,
		       b.public, b.has_content, b.image_url, b.created_at, b.modified_at
		%s %s
		ORDER BY b.id DESC
		LIMIT ? OFFSET ?`, fromClause, whereClause)

	// LIMIT / OFFSET は args の後ろに追加します。
	dataArgs := append(args, limit, offset)
	rows, err := db.Query(dataSQL, dataArgs...)
	if err != nil {
		http.Error(w, "データベースエラー", http.StatusInternalServerError)
		return
	}

	// rowsが開いたまま別のクエリを実行するとSQLiteでエラーになることがあるため、
	// まず全ブックマークをスライスに読み出してからrowsを閉じます。
	var bookmarks []Bookmark
	for rows.Next() {
		var b Bookmark
		if err := rows.Scan(
			&b.ID, &b.URL, &b.Title, &b.Excerpt, &b.Author,
			&b.Public, &b.HasContent, &b.ImageURL, &b.CreatedAt, &b.ModifiedAt,
		); err != nil {
			rows.Close()
			http.Error(w, "読み取りエラー", http.StatusInternalServerError)
			return
		}
		bookmarks = append(bookmarks, b)
	}
	rows.Close()

	// ── ③ タグを2クエリ固定で一括取得 ───────────────────────────
	// N+1クエリ問題を避けるため、対象ブックマークのIDをまとめてINで渡します。
	if len(bookmarks) > 0 {
		ids := make([]int, len(bookmarks))
		for i, b := range bookmarks {
			ids[i] = b.ID
		}

		// map[bookmarkID] = bookmarksスライスの添字（O(1) で位置を引くため）
		idxByID := make(map[int]int, len(bookmarks))
		for i, b := range bookmarks {
			idxByID[b.ID] = i
		}

		placeholders := strings.Repeat("?,", len(ids))
		placeholders = placeholders[:len(placeholders)-1]

		args := make([]interface{}, len(ids))
		for i, id := range ids {
			args[i] = id
		}

		tagRows, err := db.Query(fmt.Sprintf(`
			SELECT bt.bookmark_id, t.id, t.name
			FROM tags t
			INNER JOIN bookmark_tags bt ON t.id = bt.tag_id
			WHERE bt.bookmark_id IN (%s)
			ORDER BY t.name ASC`, placeholders), args...)
		if err != nil {
			http.Error(w, "タグ取得エラー", http.StatusInternalServerError)
			return
		}
		defer tagRows.Close()

		for tagRows.Next() {
			var bookmarkID int
			var tag Tag
			if err := tagRows.Scan(&bookmarkID, &tag.ID, &tag.Name); err != nil {
				http.Error(w, "タグ読み取りエラー", http.StatusInternalServerError)
				return
			}
			if idx, ok := idxByID[bookmarkID]; ok {
				bookmarks[idx].Tags = append(bookmarks[idx].Tags, tag)
			}
		}
	}

	// 各ブックマークの Tags フィールドが nil（タグ0件）の場合も
	// JSON で null ではなく [] を返すために初期化します。
	// bookmarks スライス自体の nil ガードもあわせて行います。
	for i := range bookmarks {
		if bookmarks[i].Tags == nil {
			bookmarks[i].Tags = []Tag{}
		}
	}
	if bookmarks == nil {
		bookmarks = []Bookmark{}
	}

	// ── ④ レスポンス ─────────────────────────────────────────────
	// bookmarks（現在ページ分）と total（全件数）をまとめて返します。
	// フロントエンドはこれを使ってページ数とナビゲーションを計算します。
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(BookmarkListResponse{
		Bookmarks: bookmarks,
		Total:     total,
	})
}

// handleGetBookmark：指定IDのbookmarkを、タグを含めて1件だけ返します。
// 編集モーダルを開く直前にDBの最新値を読み、非同期要約後のExcerptを古い画面内データで
// 上書きしないために使います。
func handleGetBookmark(w http.ResponseWriter, r *http.Request) {
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bookmark)
}

// handleGetBookmarkByURL：登録済みURLを完全一致で検索し、タグ込みのbookmarkを1件返します。
// 拡張が現在開いているページの登録状態を確認するために使います。部分一致検索ではなく、
// 登録・更新と同じ validateHTTPURL による正規化後のURLだけを比較します。
func handleGetBookmarkByURL(w http.ResponseWriter, r *http.Request) {
	validatedURL, err := validateHTTPURL(r.URL.Query().Get("url"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// URL列はUNIQUE制約なので、IDは最大1件だけ取得できます。
	var id int
	err = db.QueryRow(`SELECT id FROM bookmarks WHERE url = ?`, validatedURL).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "指定されたURLのbookmarkが見つかりません", http.StatusNotFound)
			return
		}
		http.Error(w, "データベースエラー", http.StatusInternalServerError)
		return
	}

	// 既存の1件取得ヘルパーを使うことで、ID指定APIと同じBookmark JSON（タグを含む）を返します。
	bookmark, err := getBookmarkByID(id)
	if err != nil {
		http.Error(w, "データベースエラー", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bookmark)
}

// syncBookmarkTags：ブックマークのタグ紐付けを同期するヘルパー関数です。
// 既存の紐付けを全削除してから、新しいタグを挿入します（置き換え方式）。
// タグが空リストの場合は全削除のみ行います。
//
// DELETE と INSERT をトランザクションにまとめる理由:
// トランザクションなしだと、削除が成功したあと INSERT の途中でエラーが起きた場合に
// 「タグが一部だけ消えた」中途半端な状態がDBに残ってしまいます。
// トランザクションにすれば、全部成功か全部なかったことか、どちらかに必ずなります。
func syncBookmarkTags(bookmarkID int, tags []Tag) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // Commit が呼ばれる前に return した場合の安全策です。

	// 既存の紐付けをすべて削除します。
	if _, err := tx.Exec("DELETE FROM bookmark_tags WHERE bookmark_id = ?", bookmarkID); err != nil {
		return err
	}
	// 新しいタグを挿入します。
	for _, t := range tags {
		if t.ID == 0 {
			continue // IDが指定されていないタグはスキップします。
		}
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)",
			bookmarkID, t.ID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// getTagsByBookmarkID：指定したブックマークIDに紐付いたタグを取得するヘルパー関数です。
// 複数のハンドラから使い回せるように独立した関数にしています。
func getTagsByBookmarkID(bookmarkID int) ([]Tag, error) {
	rows, err := db.Query(`
		SELECT t.id, t.name
		FROM tags t
		INNER JOIN bookmark_tags bt ON t.id = bt.tag_id
		WHERE bt.bookmark_id = ?
		ORDER BY t.name ASC`, bookmarkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// タグが0件の場合も空配列（nullではなく[]）で返すために初期化します。
	tags := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, nil
}

// getBookmarkByID：指定したIDのブックマークをタグ込みでDBから取得するヘルパー関数です。
// 作成・更新APIのレスポンスに使います。INSERT/UPDATE のあとDBから読み直すことで、
// created_at / modified_at に「DBに実際に保存された値」を正確に返せます。
// （Go側で time.Now() をセットすると、DBの CURRENT_TIMESTAMP（UTC）と
// 　タイムゾーンや秒数がズレた「嘘の値」を返してしまうため）
func getBookmarkByID(id int) (*Bookmark, error) {
	var b Bookmark
	err := db.QueryRow(`
		SELECT id, url, title, excerpt, author,
		       public, has_content, image_url, created_at, modified_at
		FROM bookmarks WHERE id = ?`, id).Scan(
		&b.ID, &b.URL, &b.Title, &b.Excerpt, &b.Author,
		&b.Public, &b.HasContent, &b.ImageURL, &b.CreatedAt, &b.ModifiedAt,
	)
	if err != nil {
		return nil, err
	}
	// タグも一緒に取得してセットします。
	b.Tags, err = getTagsByBookmarkID(id)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// handleCreateBookmark：新しいブックマークを登録するAPIです。
// リクエストボディの tags フィールドにタグIDのリストを含めると紐付けも行います。
// 例: {"url":"...","title":"...","tags":[{"id":1},{"id":2}]}
func handleCreateBookmark(w http.ResponseWriter, r *http.Request) {
	var b Bookmark
	// ブラウザから送られてきたJSONを読み取り、構造体に変換します。
	if err := decodeJSONBody(w, r, &b); err != nil {
		http.Error(w, "リクエスト解析エラー", http.StatusBadRequest)
		return
	}
	validatedURL, err := validateHTTPURL(b.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.URL = validatedURL
	// データベースに保存します。
	query := `INSERT INTO bookmarks (url, title, excerpt, author, image_url) VALUES (?, ?, ?, ?, ?);`
	result, err := db.Exec(query, b.URL, b.Title, b.Excerpt, b.Author, b.ImageURL)
	if err != nil {
		// url カラムには UNIQUE 制約があるため、登録済みのURLを再登録しようとすると
		// 制約違反エラーになります。これはサーバーの障害ではなく「リクエスト内容の競合」
		// なので、409 Conflict と分かりやすいメッセージを返します。
		if isUniqueConstraintError(err) {
			http.Error(w, "このURLは既に登録されています", http.StatusConflict)
			return
		}
		http.Error(w, "保存エラー", http.StatusInternalServerError)
		return
	}
	// 自動採番されたIDを取得してセットします。
	id, _ := result.LastInsertId()
	b.ID = int(id)

	// タグが指定されていれば bookmark_tags に紐付けを保存します。
	if err := syncBookmarkTags(b.ID, b.Tags); err != nil {
		http.Error(w, "タグ保存エラー", http.StatusInternalServerError)
		return
	}

	// DBから読み直して「実際に保存された値」をレスポンスとして返します。
	// created_at はDBの CURRENT_TIMESTAMP が入り、modified_at は未更新なので
	// NULL（JSONでは null）になります。
	created, err := getBookmarkByID(b.ID)
	if err != nil {
		http.Error(w, "登録データ取得エラー", http.StatusInternalServerError)
		return
	}

	// 成功（201 Created）を返し、登録されたデータをJSONで返信します。
	// ※ WriteHeader より前に Header().Set() を呼ぶ必要があります。
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated) // 201 Created
	json.NewEncoder(w).Encode(created)
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
	if err := decodeJSONBody(w, r, &b); err != nil {
		http.Error(w, "リクエスト解析エラー", http.StatusBadRequest)
		return
	}
	validatedURL, err := validateHTTPURL(b.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.URL = validatedURL

	// UPDATE文で該当IDのレコードを更新します。
	// modified_at は更新のたびに現在時刻をセットします。
	query := `
		UPDATE bookmarks
		SET url = ?, title = ?, excerpt = ?, author = ?, image_url = ?,
		    modified_at = CURRENT_TIMESTAMP
		WHERE id = ?;`
	result, err := db.Exec(query, b.URL, b.Title, b.Excerpt, b.Author, b.ImageURL, id)
	if err != nil {
		// 更新でも、URLを「別のブックマークが既に使っているURL」に変更しようとすると
		// UNIQUE 制約違反になるため、作成時と同じく 409 Conflict を返します。
		if isUniqueConstraintError(err) {
			http.Error(w, "このURLは既に別のブックマークで登録されています", http.StatusConflict)
			return
		}
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

	// タグの更新は tags フィールドが送られてきた場合のみ行います。
	// Goの encoding/json では、JSONにキーが存在しない場合スライスは nil のまま、
	// "tags": [] と明示された場合は「空のスライス」になります。
	// この違いを利用して「省略＝変更しない」「空配列＝全削除」を区別します。
	// （CLIなど将来のクライアントがタイトルだけ更新したいとき、
	// 　tags を送り忘れてタグが全消えする事故を防ぎます）
	if b.Tags != nil {
		if err := syncBookmarkTags(id, b.Tags); err != nil {
			http.Error(w, "タグ保存エラー", http.StatusInternalServerError)
			return
		}
	}

	// DBから読み直して「実際に保存された値」をレスポンスとして返します。
	// これにより created_at（リクエストには含まれないためゼロ値だった）と
	// modified_at（DBの CURRENT_TIMESTAMP）の両方が正確な値になります。
	updated, err := getBookmarkByID(id)
	if err != nil {
		http.Error(w, "更新データ取得エラー", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(updated)
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

// handleBulkDeleteBookmarks：複数のブックマークをまとめて削除するAPIです。
// リクエストボディ: {"ids": [1, 2, 3]}
func handleBulkDeleteBookmarks(w http.ResponseWriter, r *http.Request) {
	// リクエストボディを構造体に読み込みます。
	var req struct {
		IDs []int `json:"ids"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "IDリストが不正です", http.StatusBadRequest)
		return
	}
	if err := validateBulkIDs(req.IDs, "ids"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// IN句のプレースホルダーを動的に生成します。
	// 例: IDs=[1,2,3] → "?,?,?"
	placeholders := strings.Repeat("?,", len(req.IDs))
	placeholders = placeholders[:len(placeholders)-1]

	// []int を []interface{} に変換してクエリに渡します。
	// database/sql はインターフェース型のスライスを要求するためです。
	args := make([]interface{}, len(req.IDs))
	for i, id := range req.IDs {
		args[i] = id
	}

	// 1回のDELETE文でまとめて削除します。
	// bookmark_tagsの紐付けはON DELETE CASCADEで自動削除されます。
	result, err := db.Exec(
		fmt.Sprintf("DELETE FROM bookmarks WHERE id IN (%s)", placeholders),
		args...,
	)
	if err != nil {
		http.Error(w, "削除エラー", http.StatusInternalServerError)
		return
	}

	deleted, _ := result.RowsAffected()
	// 削除件数をJSONで返します。
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"deleted": int(deleted)})
}
