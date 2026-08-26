# Pico CSS 除去計画

## 目的

Pico CSSを除去し、Shirushiの画面を独自CSSだけで表示する。
依存と上書きの二重構造をなくして、見た目の由来が`static/css/style.css`だけで分かる状態にする。

デザイン刷新は行わず、現在のダークテーマ、操作性、レスポンシブ表示、アクセシビリティを維持する。

## 結論

除去は実現可能である。
画面のレイアウトと部品の大半はすでにShirushi独自のclass・idで実装されているため、Go、API、DBの変更は不要である。JavaScriptもPico固有クラスに依存していない。

主作業は、Picoが暗黙に提供しているフォーム部品と基本スタイルを`style.css`へ明示することである。

## 現状（2026-08-23確認）

- `static/index.html`はPico CSS v2.1.1の`static/css/pico.min.css`を先に読み込み、`static/css/style.css`で上書きしている。
- `pico.min.css`は71,040 bytes（gzip約10KB）。
- `style.css`は19,135 bytes・880行。
- `style.css`は`--pico-*`変数を19種類参照・上書きし、`!important`を19か所使用している。
- モーダルは`dialog`要素ではなく、独自のオーバーレイとボックスで実装済みである。
- HTMLとJavaScriptにはインラインスタイルが19か所ある。表示状態をJavaScriptで切り替えるものもあるため、Pico除去時に一律移動はしない。

## Picoに残っている依存

単にPicoの`link`を削除した場合、主に次の部分でブラウザ標準表示への差し戻しや表示差が発生する。

- `html`、`body`、見出し、リンクの文字組み
- `button`、`input`、`select`、`textarea`の共通スタイル
- checkboxのサイズ、色、配置
- placeholder、hover、disabled、autofill
- Tab操作時のfocus表示
- selectやsearch inputのブラウザ差

カード、ヘッダー、検索欄の配置、タグ、ページネーション、バルク操作、モーダルの配置は、すでに独自CSSが担当している。

## 変更対象

### 必須

- `static/css/style.css`
- `static/index.html`
- `static/css/pico.min.css`（最終段階で削除）

### 原則変更しない

- `static/js/app.js`
- Goソース
- API、DBスキーマ

表示状態をインラインstyleで切り替えている既存JavaScriptは、今回の除去とは分離する。CSS化が明らかに安全な装飾用インラインstyleだけは、必要に応じて独自classへ移してよい。

## 実施計画

各段階を個別に確認し、一度に全変更を行わない。

### 第1段階: 現行表示の基準を保存する

1. 現行状態でPC幅とモバイル幅のスクリーンショットを取得する。
2. ログイン画面、一覧、追加モーダル、編集モーダル、タグ管理、バルク操作、タグドロップダウンを記録する。
3. Chrome系ブラウザのcomputed styleで、button、input、select、checkboxの主要値を確認する。
4. `go test ./...`と`node --check static/js/app.js`を実行し、変更前の成功を確認する。

この段階ではファイルを変更しない。

### 第2段階: デザイントークンを独自化する

1. `:root`へ背景、surface、フォーム背景、accent hover、focus ring、角丸、font、line-heightの`--s-*`トークンを追加する。
2. `style.css`内の`var(--pico-*)`参照を対応する`var(--s-*)`へ置換する。
3. 不要になった19種類の`--pico-*`定義を削除する。
4. `html`に`color-scheme: dark`、font、font-size、line-heightを明示する。
5. この時点ではPicoの読み込みを残し、既存画面との差を確認する。

完了条件:

- `style.css`内に`--pico-`が残っていない。
- Picoを読み込んだ状態で意図しない視覚差分がない。

### 第3段階: 基本要素とフォーム部品を自前化する

`style.css`に、必要最小限のルールを明示する。

1. 基本要素
   - `html`、`body`
   - 見出し
   - リンク
   - `[hidden]`
2. フォーム共通
   - `button`、`input`、`select`、`textarea`で`font: inherit`
   - 背景、文字色、border、角丸、padding
   - placeholder
   - hover
   - `:focus-visible`
   - disabled
3. 部品別
   - selectの高さ、余白、矢印表示
   - checkboxの寸法、`accent-color`、ラベルとの配置
   - textareaのresizeと最小高
   - search inputのブラウザ差
   - login inputのautofill
4. キーボード操作
   - button、リンク、入力欄、select、checkboxに共通の見やすいfocus ringを設定する。
   - mouse clickでは不要なringを強制せず、原則`:focus-visible`を使う。

既存の全称リセット`*`、カード、モーダル、レスポンシブ指定は必要以上に作り直さない。

完了条件:

- ブラウザ開発者ツールでPicoを無効にしても、主要部品が現行に近い表示になる。
- Tabだけで主要操作へ移動でき、現在位置が視認できる。

### 第4段階: Pico由来の上書きを整理する

1. Picoとの競合回避だけを目的とした高詳細度セレクタとコメントを整理する。
2. 不要になった`!important`を削除する。
3. インラインstyleに勝つためのものとautofill対策は、理由を確認してから残す。
4. 装飾だけを目的とするインラインstyleを移動する場合は、独自classを追加する。
5. JavaScriptが`element.style.display`を参照・変更する箇所は、この作業で方式変更しない。

完了条件:

- 残る`!important`にはPico以外の明確な理由がある。
- 「Picoのリセット」「Picoとの競合」など、事実でなくなったコメントが残っていない。

### 第5段階: Picoを削除する

1. `static/index.html`から`/css/pico.min.css`の`link`を削除する。
2. `data-theme="dark"`がPico対策だけなら属性と説明コメントを削除する。ダーク指定はCSSの`color-scheme`で管理する。
3. `static/css/pico.min.css`を削除する。
4. `rg -n -i 'pico' static`でコード上の参照がないことを確認する。
5. Goの`//go:embed static`はディレクトリ全体を対象としているため変更しない。

完了条件:

- Picoファイル、読み込み、変数参照がない。
- ページのCSSリクエストが`style.css`だけになっている。

### 第6段階: 回帰確認と調整

以下をChromeとFirefoxで確認する。モバイルは実機または幅375px前後、PCは幅1280px以上を基準にする。

- ログイン、ログアウト、ログイン状態維持checkbox
- 一覧、検索、表示件数select、上下ページネーション
- 追加・編集モーダルと長文textarea
- タグ入力、候補表示、追加、削除、タグ管理
- カード選択、全選択、バルクタグ追加・削除、一括削除
- インポート、エクスポート、404チェック、AIボタン
- hover、disabled、autofill
- Tab移動、Enter/Space操作、focus-visible
- 幅375px前後で横スクロールや画面外へのドロップダウン突出がないこと

最後に第1段階のスクリーンショットと比較し、意図しない配色、余白、font-size、部品高の差を調整する。

## 検証コマンド

```sh
# Go側の回帰確認
go test ./...

# JavaScriptの構文確認
node --check static/js/app.js

# Pico参照の残存確認
rg -n -i 'pico' static

# 独自CSSに残ったimportantの確認
rg -n '!important' static/css/style.css
```

ブラウザ操作確認は、確認用DBでサーバーを起動し、Playwright Testまたは手動で実施する。

## 受入条件

- `pico.min.css`がリポジトリと配信対象から削除されている。
- `static/`内にPicoへの参照がない。
- 現在のダークテーマと主要レイアウトが維持されている。
- ログインから各主要操作まで、現行と同じ手順で利用できる。
- button、input、select、textarea、checkboxがChromeとFirefoxで崩れない。
- PC幅とモバイル幅の両方で横スクロールや操作不能な部品がない。
- Tab移動時のfocus-visibleが明確である。
- `go test ./...`と`node --check static/js/app.js`が成功する。
- ブラウザコンソールにCSS削除に起因する404やエラーがない。

## ロールバック方針

各段階を独立した小さなコミットにする。
最終削除後に重大な表示不具合が見つかった場合は、まずPico削除コミットだけを戻せる構成にする。DBやAPIは変更しないため、データ移行やバックエンドのロールバックは発生しない。

## 見積もり

- 基準取得とトークン整理: 2〜3時間
- フォーム部品とfocusの自前化: 3〜5時間
- Pico削除とCSS整理: 1〜2時間
- PC・モバイル・ブラウザ回帰確認: 3〜5時間

合計は1〜2日を目安とする。

## 対象外

- UIデザインの刷新
- 別のCSSフレームワークへの置換
- JavaScriptフレームワークの導入
- 表示状態管理方式の全面変更
- API、DB、Goバックエンドの変更
