package main

// model.go：アプリ全体で使う型定義・グローバル変数・定数をまとめたファイルです。
// 他のファイルはここで定義された型や変数を共有します。

import (
	"database/sql" // *sql.DB 型を使うためのパッケージ
	"sync"         // Mutex（排他ロック）を使うためのパッケージ
	"time"         // 時刻・期間を扱うためのパッケージ
)

// Bookmark 構造体：ブックマークのデータをプログラム内で扱うための入れ物です。
// Shioriのスキーマに合わせたフィールド構成になっています。
// `json:"..."` という記述（タグ）は、JSON形式にする際の名前を指定しています。
type Bookmark struct {
	ID         int        `json:"id"`
	URL        string     `json:"url"`
	Title      string     `json:"title"`
	Excerpt    string     `json:"excerpt"`     // 本文の抜粋・メモ欄
	Author     string     `json:"author"`      // ページの著者
	Public     int        `json:"public"`      // 公開フラグ（0:非公開 1:公開）
	HasContent bool       `json:"has_content"` // 本文キャッシュの有無
	ImageURL   string     `json:"image_url"`   // サムネイル画像URL
	CreatedAt  time.Time  `json:"created_at"`
	ModifiedAt *time.Time `json:"modified_at"` // 最終更新日時（未更新の場合はnull）
	Tags       []Tag      `json:"tags"`        // 紐付いているタグの一覧
}

// Tag 構造体：タグのデータをプログラム内で扱うための入れ物です。
type Tag struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// loginAttempt：IPアドレスごとのログイン失敗状態を保存する構造体です。
type loginAttempt struct {
	Failures     int
	FirstFailure time.Time
	LockedUntil  time.Time
}

// BookmarkListResponse：ブックマーク一覧APIのレスポンス形式です。
// ページネーションのために、ブックマーク本体と総件数をまとめて返します。
type BookmarkListResponse struct {
	Bookmarks []Bookmark `json:"bookmarks"` // 現在ページのブックマーク
	Total     int        `json:"total"`     // 絞り込み条件込みの総件数
}

// Metadata 構造体：URLから取得したメタデータを表します。
type Metadata struct {
	Title    string `json:"title"`
	Excerpt  string `json:"excerpt"`
	Author   string `json:"author"`
	ImageURL string `json:"image_url"`
}

// データベースの接続を保持するグローバル変数です。
var db *sql.DB

// sessions：ログイン中のセッショントークンと有効期限を管理するマップです。
// 複数のリクエストが同時にアクセスしても安全なよう sync.Mutex で保護します。
var (
	sessions   = map[string]time.Time{} // token -> 有効期限
	sessionsMu sync.Mutex

	loginAttempts   = map[string]loginAttempt{} // client IP -> 失敗回数とロック期限
	loginAttemptsMu sync.Mutex
	nowFunc         = time.Now
)

const (
	// maxJSONBodyBytes：JSON API のリクエスト本文の最大サイズです。
	// 小さな個人用アプリでも、巨大な本文を無制限に読む必要はないため上限を設けます。
	maxJSONBodyBytes int64 = 1 << 20 // 1MB

	// maxBulkIDs：一括操作で受け付けるID配列の最大件数です。
	// SQLのプレースホルダー数やメモリ使用量が無制限に増えるのを防ぎます。
	maxBulkIDs = 1000

	// maxBulkTagPairs：一括タグ追加で実行する bookmark_id × tag_id の最大組み合わせ数です。
	maxBulkTagPairs = 5000

	// maxImportBytes：インポートHTMLの最大サイズです。
	maxImportBytes int64 = 10 << 20 // 10MB

	// maxLoginFailures：同じIPから許可する連続ログイン失敗回数です。
	maxLoginFailures = 5

	// loginFailureWindow：この時間を過ぎた古い失敗回数はリセットします。
	loginFailureWindow = 15 * time.Minute

	// loginLockoutDuration：失敗回数が上限に達したIPを一時的にロックする時間です。
	loginLockoutDuration = 15 * time.Minute
)
