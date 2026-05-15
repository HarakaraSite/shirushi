# ブックマークアプリ (Shioriクローン) 開発要件定義書

## 1. 目的
- Go言語の学習と、自身が本当に欲しい実用的なアプリの作成を兼ねる。
- 既存のブックマークアプリ「Shiori」のようなブックマーク管理アプリの基本機能（URL保存、一覧表示など）をベースに、自分好みにカスタマイズしていく。

## 2. 開発方針・アプローチ
- **シンプル第一:** 最初は複雑な機能（メタデータの自動取得など）を省き、「MVP（必要最小限の動くプロダクト）」を最優先で完成させる。拡張は後から行う。
- **APIファースト:** 将来的なCLIツールからの利用や拡張を見据え、バックエンドは純粋なJSON APIとして設計する。
- **シングルバイナリ化:** 自宅のProxmox/Alpine Linux環境で手軽に運用できるよう、バックエンドとフロントエンドを1つの実行ファイルにまとめる。

## 3. 機能要件 (MVP段階)
### 3.1 データモデル
以下の項目を保存・管理する。
- `ID` (Integer, Primary Key)
- `URL` (String, Required): ブックマークする対象のURL
- `Title` (String): ページのタイトル（初期は手入力）
- `Description` (String): 説明・メモ欄
- `CreatedAt` (Datetime): 登録日時

### 3.2 APIエンドポイント (JSONベース)
- `GET /api/bookmarks` : ブックマーク一覧の取得
- `POST /api/bookmarks` : 新規ブックマークの登録
- `PUT /api/bookmarks/:id` : 既存ブックマークの更新（メモの追記など）
- `DELETE /api/bookmarks/:id` : ブックマークの削除

### 3.3 ユーザーインターフェース (UI)
- APIと通信してデータをやり取りするシンプルなSPA（Single Page Application）。
- URLとメモを入力して送信する「登録フォーム」。
- 登録済みのデータを表示する「一覧表示エリア」。

## 4. 技術要件
### 4.1 バックエンド (サーバー側)
- **言語:** Go (Golang)
- **ルーティング:** Go標準パッケージ `net/http` （Go 1.22以降の拡張ルーティング機能を活用し、外部フレームワークは不使用）

### 4.2 データベース
- **DB:** SQLite
- **ドライバ:** `modernc.org/sqlite`
  - 選定理由: CGO不要のピュアGo実装であるため、Alpine Linux等でのクロスコンパイルや環境構築が極めて容易なため。

### 4.3 フロントエンド (ブラウザ側)
- **言語:** HTML + Vanilla JavaScript (ピュアなJS)
- **通信:** 標準の `fetch` APIを使用してバックエンドのJSON APIと通信し、DOMを動的に書き換える。
- **※備考:** htmxはサーバー側の役割が複雑化（JSONとHTMLパーツの両方の生成が必要）するため、今回は不採用とする。

### 4.4 アプリケーションの組み込み・配信
- **機能:** Go標準の `go:embed`
- **用途:** 作成したHTML/JS/CSSなどの静的ファイルをGoのバイナリに埋め込み、1つの実行ファイルとしてWebサーバー機能とUIを同時に提供する。

## 5. 参考ドキュメント
- Go公式 `net/http`: https://go.dev/doc/go1.22#net/http
- `modernc.org/sqlite`: https://pkg.go.dev/modernc.org/sqlite
- Go公式 `go:embed`: https://pkg.go.dev/embed
- gemini-cli リポジトリ: https://github.com/google-gemini/gemini-cli
- gemini-cli 導入記事: https://cloud.google.com/blog/ja/topics/developers-practitioners/introducing-gemini-cli/