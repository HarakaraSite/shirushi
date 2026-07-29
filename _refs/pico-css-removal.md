# Pico CSS 廃止の将来対応メモ

## 目的

Pico CSSを廃止し、Shirushiの画面を独自CSSだけで表示する。既存のダークテーマ、操作、レスポンシブ表示、アクセシビリティを維持しつつ、Picoの詳細度や暗黙の既定スタイルに依存しない構成にする。

これは将来対応であり、現時点では実装しない。

## 調査結果（2026-07-27）

- `static/index.html` はPico CSS v2.1.1の`static/css/pico.min.css`を読み込んでから、`static/css/style.css`で上書きしている。
- Pico CSSは71,040 bytes、Shirushi独自CSSは18,947 bytes・874行である。
- 独自CSSは`--pico-*`変数を19種類上書きし、Picoとの詳細度競合を避ける`!important`も19か所使っている。
- 画面はすでにカード、ヘッダー、タグ操作、バルク操作、モーダル、タグ管理などを独自class/idでレイアウトしている。そのため、バックエンドやJavaScriptの再設計は不要で、主作業はCSS基盤の置換になる。
- Picoが現在担う主な範囲は、基本リセット、文字組み、button/input/select/textarea/checkboxの既定スタイルとfocus状態、dialog、nav、モバイル時の既定調整である。

## 規模感

既存デザインを保つ前提なら、中規模のフロントエンド作業である。

- 置換実装: 半日から1日
- PC・モバイル表示、フォーム、モーダル、キーボード操作の確認と調整: 半日から1日
- 合計の目安: 1から2日

デザイン刷新を同時に行う場合は、比較基準がなくなるため別プロジェクトとして扱う。

## 実施順序

1. `style.css`のShirushi独自トークンを唯一の色・余白・角丸・フォント定義に整理する。
2. 最小限のbox sizing・body・見出し・リンクのリセットを独自CSSで定義する。
3. button、input、select、textarea、checkboxの通常、hover、focus-visible、disabled、autofillを自前化する。
4. dialogのオーバーレイ、スクロール、編集モーダルの高さ、小画面表示を自前化する。
5. ヘッダー、カードグリッド、タグ候補、バルク操作、タグ管理をPC・モバイル幅で確認する。
6. `pico.min.css`の`link`を外し、不要になったPico変数と`!important`を削除する。

## 受入条件

- `pico.min.css`を読み込まない。
- ログイン、登録・編集モーダル、タグ追加・削除、タグ管理、検索、ページネーション、インポート・エクスポート、AIボタンが現行と同じ操作で使える。
- マウス操作だけでなく、Tab移動時のfocus-visibleが明確に見える。
- button、input、select、textarea、checkbox、dialogがChrome/FirefoxのPC表示とモバイル幅で崩れない。
- `node --check static/js/app.js`、`go test ./...`、既存E2E手順が成功する。
- スクリーンショット比較で、意図しない配色・余白・フォントサイズ・モーダル表示の差分がないことを確認する。

## 対象外

- UIデザインの刷新
- CSSフレームワークの別製品への置換
- JavaScriptフレームワークの導入
- API・DB・Goバックエンドの変更
