package main

// util.go：アプリ全体から呼ばれる共通ユーティリティ関数をまとめたファイルです。
// DB確認・URL検証・JSONデコード・エラー判定など、複数のハンドラで使い回す処理を置きます。

import (
	"encoding/json" // decodeJSONBody で使うパッケージ
	"errors"        // isUniqueConstraintError で errors.As を使うパッケージ
	"fmt"           // checkAllExist / validateBulkIDs でエラーメッセージを組み立てるパッケージ
	"net/http"      // decodeJSONBody で http.MaxBytesReader を使うパッケージ
	"net/url"       // validateHTTPURL で URL を構文解析するパッケージ
	"strings"       // validateHTTPURL / checkAllExist で文字列操作するパッケージ

	// modernc.org/sqlite は純粋なGo言語で実装されたSQLiteドライバです。
	// CGO（C言語との連携）が不要なため、Alpine Linuxなどの軽量環境でも
	// そのまま動くシングルバイナリが作れます。
	// エラー型（sqlite.Error）を使うため、ブランクインポート（_）ではなく
	// 名前付きでインポートします。このインポートによりSQLiteドライバの登録も行われます。
	sqlite "modernc.org/sqlite"
	// SQLITE_CONSTRAINT_UNIQUE などのエラーコード定数が定義されているパッケージです。
	sqlite3 "modernc.org/sqlite/lib"
)

// recordExists：指定テーブルにIDの行が存在するか確認します。
// table はプログラム内の固定文字列だけを渡す前提で使います。
func recordExists(table string, id int) (bool, error) {
	var exists bool
	err := db.QueryRow(fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE id = ?)", table), id).Scan(&exists)
	return exists, err
}

// checkAllExist：指定テーブルに ids の全IDが存在するか1クエリで確認します。
// 1件でも存在しないIDがあればエラーを返します。
// table はプログラム内の固定文字列だけを渡す前提で使います（SQL インジェクション防止）。
func checkAllExist(table string, ids []int) error {
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var found int
	err := db.QueryRow(
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE id IN (%s)", table, placeholders),
		args...,
	).Scan(&found)
	if err != nil {
		return fmt.Errorf("%s の確認エラー: %w", table, err)
	}
	if found != len(ids) {
		return fmt.Errorf("指定された %s の一部が見つかりません", table)
	}
	return nil
}

// isUniqueConstraintError：エラーが UNIQUE 制約違反かどうかを判定するヘルパー関数です。
// errors.As は、エラーが特定の型（ここでは *sqlite.Error）かどうかを調べ、
// そうであれば中身を取り出してくれる標準の仕組みです。
// SQLite はエラーの種類を数値コードで表し、UNIQUE 制約違反は
// SQLITE_CONSTRAINT_UNIQUE（2067）という拡張コードになります。
func isUniqueConstraintError(err error) bool {
	var serr *sqlite.Error
	if errors.As(err, &serr) {
		return serr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
	}
	return false
}

// validateHTTPURL：保存・取得対象として扱ってよいURLか確認します。
// ブラウザ側でも javascript: などを無害化していますが、CLIや別クライアントが
// APIを直接使う可能性もあるため、サーバー側でも同じ入口で防ぎます。
func validateHTTPURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("URLは必須です")
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("URLは http または https で始まる必要があります")
	}
	if u.Host == "" {
		return "", errors.New("URLにはホスト名が必要です")
	}
	return trimmed, nil
}

// decodeJSONBody：JSONリクエスト本文をサイズ制限付きで読み取ります。
// json.NewDecoder に r.Body を直接渡すと、巨大な本文を送られたときに
// 必要以上にメモリや処理時間を使うため、http.MaxBytesReader で上限をかけます。
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	return json.NewDecoder(r.Body).Decode(dst)
}

// validateBulkIDs：一括操作のID配列が空でなく、上限以内か確認します。
func validateBulkIDs(ids []int, name string) error {
	if len(ids) == 0 {
		return fmt.Errorf("%s が必要です", name)
	}
	if len(ids) > maxBulkIDs {
		return fmt.Errorf("%s は一度に%d件までです", name, maxBulkIDs)
	}
	return nil
}
