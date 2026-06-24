package main

// tag.go：タグのCRUD操作・ブックマークとタグの紐付け操作を担当するファイルです。
// 単体の紐付け（handleAddTagToBookmark）と一括操作（handleBulkAddTags）の両方を含みます。

import (
	"database/sql"  // handleGetTags で *sql.Rows を明示的に使うパッケージ
	"encoding/json" // レスポンスをJSON形式で返すパッケージ
	"fmt"           // IN句のプレースホルダー生成・エラーメッセージに使うパッケージ
	"net/http"      // HTTPハンドラ・エラーレスポンスに使うパッケージ
	"strconv"       // URLパスのID文字列を整数に変換するパッケージ
	"strings"       // IN句のプレースホルダー文字列組み立てに使うパッケージ
)

// handleBulkAddTags：複数のブックマークに複数のタグをまとめて付与するAPIです。
// リクエストボディ: {"bookmark_ids": [1,2,3], "tag_ids": [10,11]}
// すでに紐付いているものは INSERT OR IGNORE でスキップします。
func handleBulkAddTags(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookmarkIDs []int `json:"bookmark_ids"`
		TagIDs      []int `json:"tag_ids"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "bookmark_ids と tag_ids が必要です", http.StatusBadRequest)
		return
	}
	if err := validateBulkIDs(req.BookmarkIDs, "bookmark_ids"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateBulkIDs(req.TagIDs, "tag_ids"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.BookmarkIDs)*len(req.TagIDs) > maxBulkTagPairs {
		http.Error(w, fmt.Sprintf("タグ一括追加は一度に%d組み合わせまでです", maxBulkTagPairs), http.StatusBadRequest)
		return
	}

	// 存在チェック：送られてきた bookmark_id / tag_id が全件DBに存在するか確認します。
	// INSERT OR IGNORE は FK 制約違反でエラーになる（IGNORE は UNIQUE/CHECK/NOTNULL のみ対象）ため、
	// 不正IDが混じると操作全体が 500 になります。単体 API（handleAddTagToBookmark）が 404 を返すのと
	// 挙動を揃えるために、事前に1クエリで確認して不足があれば 404 を返します。
	if err := checkAllExist("bookmarks", req.BookmarkIDs); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := checkAllExist("tags", req.TagIDs); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// トランザクション：全ての挿入を1まとめにします。
	// 途中でエラーが起きた場合は全件ロールバックされます。
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "トランザクション開始エラー", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback() // commit前にreturnした場合の安全策

	// ブックマークID × タグID の全組み合わせを挿入します。
	// INSERT OR IGNORE はすでに存在するペアを静かにスキップします。
	stmt, err := tx.Prepare("INSERT OR IGNORE INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)")
	if err != nil {
		http.Error(w, "クエリ準備エラー", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	for _, bID := range req.BookmarkIDs {
		for _, tID := range req.TagIDs {
			if _, err := stmt.Exec(bID, tID); err != nil {
				http.Error(w, "タグ追加エラー", http.StatusInternalServerError)
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "コミットエラー", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleBulkRemoveTags：複数のブックマークから複数のタグをまとめて削除するAPIです。
// リクエストボディ: {"bookmark_ids": [1,2,3], "tag_ids": [10,11]}
func handleBulkRemoveTags(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookmarkIDs []int `json:"bookmark_ids"`
		TagIDs      []int `json:"tag_ids"`
	}
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "bookmark_ids と tag_ids が必要です", http.StatusBadRequest)
		return
	}
	if err := validateBulkIDs(req.BookmarkIDs, "bookmark_ids"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateBulkIDs(req.TagIDs, "tag_ids"); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// IN句のプレースホルダーを bookmark_ids 分・tag_ids 分それぞれ生成します。
	bPlaceholders := strings.Repeat("?,", len(req.BookmarkIDs))
	bPlaceholders = bPlaceholders[:len(bPlaceholders)-1]
	tPlaceholders := strings.Repeat("?,", len(req.TagIDs))
	tPlaceholders = tPlaceholders[:len(tPlaceholders)-1]

	// []int を []interface{} に変換してクエリ引数としてまとめます。
	args := make([]interface{}, 0, len(req.BookmarkIDs)+len(req.TagIDs))
	for _, id := range req.BookmarkIDs {
		args = append(args, id)
	}
	for _, id := range req.TagIDs {
		args = append(args, id)
	}

	// bookmark_id と tag_id の両方が一致する行をまとめて削除します。
	_, err := db.Exec(
		fmt.Sprintf(
			"DELETE FROM bookmark_tags WHERE bookmark_id IN (%s) AND tag_id IN (%s)",
			bPlaceholders, tPlaceholders,
		),
		args...,
	)
	if err != nil {
		http.Error(w, "タグ削除エラー", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleAddTagToBookmark：ブックマークにタグを紐付けるAPIです。
// リクエストボディ: {"tag_id": 1}
func handleAddTagToBookmark(w http.ResponseWriter, r *http.Request) {
	bookmarkID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "IDが不正です", http.StatusBadRequest)
		return
	}

	// リクエストボディからtag_idを取り出します。
	var body struct {
		TagID int `json:"tag_id"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil || body.TagID == 0 {
		http.Error(w, "tag_idは必須です", http.StatusBadRequest)
		return
	}
	bookmarkExists, err := recordExists("bookmarks", bookmarkID)
	if err != nil {
		http.Error(w, "ブックマーク確認エラー", http.StatusInternalServerError)
		return
	}
	if !bookmarkExists {
		http.Error(w, "指定されたブックマークが見つかりません", http.StatusNotFound)
		return
	}
	tagExists, err := recordExists("tags", body.TagID)
	if err != nil {
		http.Error(w, "タグ確認エラー", http.StatusInternalServerError)
		return
	}
	if !tagExists {
		http.Error(w, "指定されたタグが見つかりません", http.StatusNotFound)
		return
	}

	// INSERT OR IGNORE は同じ組み合わせが既に存在する場合は何もしません。
	// これにより「同じタグを二重に付ける」エラーを防げます。
	_, err = db.Exec(
		"INSERT OR IGNORE INTO bookmark_tags (bookmark_id, tag_id) VALUES (?, ?)",
		bookmarkID, body.TagID,
	)
	if err != nil {
		http.Error(w, "タグ追加エラー", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleRemoveTagFromBookmark：ブックマークからタグの紐付けを外すAPIです。
// リクエストボディ: {"tag_id": 1}
func handleRemoveTagFromBookmark(w http.ResponseWriter, r *http.Request) {
	bookmarkID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "IDが不正です", http.StatusBadRequest)
		return
	}

	var body struct {
		TagID int `json:"tag_id"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil || body.TagID == 0 {
		http.Error(w, "tag_idは必須です", http.StatusBadRequest)
		return
	}

	_, err = db.Exec(
		"DELETE FROM bookmark_tags WHERE bookmark_id = ? AND tag_id = ?",
		bookmarkID, body.TagID,
	)
	if err != nil {
		http.Error(w, "タグ削除エラー", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleGetTags：タグ一覧を返すAPIです。
// デフォルトはブックマークに使われているタグのみ返します。
// ?all=1 を付けると未使用タグも含めた全タグを返します（タグ管理画面用）。
func handleGetTags(w http.ResponseWriter, r *http.Request) {
	var rows *sql.Rows
	var err error

	if r.URL.Query().Get("all") == "1" {
		// 全タグ（未使用含む）を返します。
		rows, err = db.Query(`SELECT id, name FROM tags ORDER BY name ASC`)
	} else {
		// 使用中のタグのみ返します（フィルターチップ・オートコンプリート用）。
		rows, err = db.Query(`
			SELECT DISTINCT t.id, t.name
			FROM tags t
			INNER JOIN bookmark_tags bt ON t.id = bt.tag_id
			ORDER BY t.name ASC`)
	}
	if err != nil {
		http.Error(w, "データベースエラー", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// var tags []Tag だと0件のとき nil のままJSONが "null" になるため、
	// 空スライスで初期化して必ず "[]" が返るようにします。
	// （フロント側で tags.forEach などがエラーになるのを防ぐ）
	tags := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			http.Error(w, "読み取りエラー", http.StatusInternalServerError)
			return
		}
		tags = append(tags, t)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tags)
}

// handleCreateTag：新しいタグを登録するAPIです。
// 同名タグが既に存在する場合は新規作成せず、既存タグをそのまま返します。
// これにより、未使用タグ（フィルター候補に出ない）と同名のタグを
// 追加しようとしても競合エラーにならず、正しいIDが取得できます。
func handleCreateTag(w http.ResponseWriter, r *http.Request) {
	var t Tag
	if err := decodeJSONBody(w, r, &t); err != nil {
		http.Error(w, "リクエスト解析エラー", http.StatusBadRequest)
		return
	}
	if t.Name == "" {
		http.Error(w, "タグ名は必須です", http.StatusBadRequest)
		return
	}

	// INSERT OR IGNORE：同名タグが既にあれば何もしない（エラーにしない）。
	// その後 SELECT で必ず正しい ID を取得します。
	// 新規作成でも既存タグの再利用でも、常に同じ処理で対応できます。
	if _, err := db.Exec("INSERT OR IGNORE INTO tags (name) VALUES (?)", t.Name); err != nil {
		http.Error(w, "タグ保存エラー", http.StatusInternalServerError)
		return
	}
	if err := db.QueryRow("SELECT id, name FROM tags WHERE name = ?", t.Name).Scan(&t.ID, &t.Name); err != nil {
		http.Error(w, "タグ取得エラー", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

// handleUpdateTag：既存のタグ名を更新するAPIです。
func handleUpdateTag(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "IDが不正です", http.StatusBadRequest)
		return
	}

	var t Tag
	if err := decodeJSONBody(w, r, &t); err != nil {
		http.Error(w, "リクエスト解析エラー", http.StatusBadRequest)
		return
	}
	if t.Name == "" {
		http.Error(w, "タグ名は必須です", http.StatusBadRequest)
		return
	}

	result, err := db.Exec("UPDATE tags SET name = ? WHERE id = ?", t.Name, id)
	if err != nil {
		http.Error(w, "更新エラー", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "指定されたIDが見つかりません", http.StatusNotFound)
		return
	}

	t.ID = id
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

// handleDeleteTag：指定されたIDのタグを削除するAPIです。
// bookmark_tags テーブルの関連レコードはFOREIGN KEY ON DELETE CASCADEで自動削除されます。
func handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Error(w, "IDが不正です", http.StatusBadRequest)
		return
	}

	result, err := db.Exec("DELETE FROM tags WHERE id = ?", id)
	if err != nil {
		http.Error(w, "削除エラー", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		http.Error(w, "指定されたIDが見つかりません", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
