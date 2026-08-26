# Pico CSS除去 第3〜第5段階 実装計画

## 目的

完了済みの第1・第2段階を引き継ぎ、Pico CSSが暗黙に提供している基本要素・フォーム状態を`static/css/style.css`へ明示し、Picoとの競合用上書きを整理した後、Pico CSSの読み込みとファイルを削除する。

デザイン刷新は行わず、既存のFirefox・Chromium snapshotを更新せずに現在の配色、寸法、レイアウト、レスポンシブ表示、キーボード操作を維持する。

## 現在の前提

- 作業ブランチは`chore/remove-pico-css`。
- 認証後UIとログイン画面のvisual baselineは保存済み。
- `static/css/style.css`内の`--pico-*`参照は0件。
- font、フォーム寸法・色、placeholder、focus、select、checkbox、buttonの基本ルールはbaseline維持に必要な範囲で前倒し済み。
- `static/index.html`は引き続き`/css/pico.min.css`を読み込んでいる。
- 404シナリオのPlaywright移行は今回のblockerにしない。

## 変更対象

### 必須

- `static/css/style.css`
- `static/index.html`
- `static/css/pico.min.css`（第5段階で削除）

### 原則変更しない

- `static/js/app.js`
- Goソース、API、DBスキーマ
- 表示状態を切り替える既存インラインstyle
- E2E snapshot画像

テストの決定性に問題が見つかった場合だけE2Eコードを局所修正し、CSS変更とは別コミットにする。

## Step 3: 残る基本要素・フォーム依存の自前化

Picoを読み込んだ状態で、実際にShirushiが使用する要素に限定して独自ルールを追加する。

### 基本要素

- `[hidden] { display: none !important; }`を明示し、hidden属性を保証する。
- 見出しのfont-weight、line-height、必要なmarginを現行表示に合わせて明示する。
- 通常リンクの色・text-decoration継承を明示し、`.title-link`と`.header-btn`の既存指定を維持する。
- `a:focus-visible`へ見やすい独自focus ringを設定する。
- 画像のvertical alignmentなど、実画面でPico無効時に差分となる値だけを明示する。

### フォーム・状態

- `textarea`のdisplay、vertical resize、overflow、行数に基づく現行高を維持する。
- `select`へ独自chevronをdata URIで設定し、appearance、位置、サイズを明示する。
- search inputのtextfield appearance、outline offset、WebKit decorationを明示する。
- buttonとフォーム部品のdisabled状態を独自化し、opacity、cursor、pointer-eventsを現行挙動に合わせる。
- 入力欄、select、textarea、checkbox、button、リンクのfocus-visibleを独自トークンで管理する。
- login autofill対策はブラウザ制約のため維持する。
- file inputは非表示の既存インラインstyleを維持し、不要な全面自前化はしない。

### 検証

- Picoを読み込んだ通常状態でFirefox・Chromium snapshotが更新なしで成功する。
- Tab操作でログイン、ヘッダー操作、検索、select、カードリンク、checkbox、モーダル操作のfocusが視認できる。
- disabled buttonが操作不能で、見た目でも判別できる。

### コミット

```text
refactor(css): own remaining base and form styles
```

## Step 4: Pico由来の上書き整理

Step 3完了後もPicoを読み込んだ状態で整理する。

### 実装

- 「Pico CSSリセット」「Picoとの競合」など、削除後に事実でなくなるコメントを一般的な役割の説明へ変更する。
- Picoの高詳細度に勝つためだけの`!important`を削除する。
- inline style、ブラウザautofill、動的表示などPico以外の理由がある`!important`は理由をコメントして残す。
- selector詳細度は必要最小限にし、既存class/id構造やJavaScriptの参照を変更しない。
- 未使用トークンを確認し、後続削除後にも用途がないものだけ削除する。

### Pico無効比較

第5段階のコミット前に、作業用の一時コピーまたはブラウザ側resource blockingでPicoを無効化し、通常状態とのcomputed styleとsnapshotをFirefox・Chromiumで比較する。検証のために一時変更したlinkはコミットしない。

差分がある場合は、原因となるPicoルールだけを独自CSSへ追加する。snapshot更新で差分を隠さない。

### コミット

```text
refactor(css): remove Pico-specific overrides
```

## Step 5: Pico CSSの削除

Step 3・4が成功してから独立コミットで実施する。

### 実装

1. `static/index.html`からPico CSSの説明コメントと`/css/pico.min.css`の`link`を削除する。
2. Picoのテーマ選択だけを目的とする`data-theme="dark"`と説明コメントを削除する。
3. `static/css/pico.min.css`を削除する。
4. `static/`内のPico参照が0件であることを確認する。
5. Goの`//go:embed static`と配信処理は変更しない。

### コミット

```text
refactor(css): remove Pico CSS
```

このコミットだけをrevertすればPico読み込みを復元できる境界を保つ。

## 検証

各Stepで関連検証を行い、第5段階後に全件を再実行する。

```sh
npm run test:e2e:firefox
npm run test:e2e:chromium
CGO_ENABLED=0 go test ./...
go vet ./...
node --check static/js/app.js
node --check e2e/authenticated-ui.spec.js
git diff --check

# 第5段階後は0件
rg -n -i 'pico' static

# 残存理由を確認
rg -n '!important' static/css/style.css
```

加えて`CGO_ENABLED=0 go build ./...`でPicoファイル削除後も静的資産のembedを含むビルドが成功することを確認する。

## ユーザーgate

合意済み範囲の局所調整、snapshotを維持するためのCSS追加、検証の再実行では停止しない。

次の場合だけ判断を求める。

- 既存snapshotを維持できず、snapshot更新または意図的なデザイン変更が必要になる。
- JavaScriptの表示状態管理、HTML構造、Go/API/DBの変更が必要になる。
- 既存操作やアクセシビリティとの互換性を変える必要がある。
- `main`へのmerge/pushが必要になる。

## 完了条件

- `static/css/pico.min.css`が削除されている。
- `static/index.html`にPico linkと`data-theme`がない。
- `static/`内の大文字小文字を問わない`pico`参照が0件。
- CSSリクエストはShirushiの`style.css`だけである。
- Firefox・Chromiumの既存snapshotが更新なしで成功する。
- desktop・mobileで横方向overflowがない。
- button、input、select、textarea、checkbox、リンクのfocus-visibleとdisabled状態が維持される。
- Go test、vet、静的build、Node構文確認、`git diff --check`が成功する。
- JS、Go、API、DB、snapshot画像に変更がない。

## ロールバック

- Step 3、Step 4、Step 5を独立コミットにする。
- 表示問題があれば最初にStep 5の削除コミットだけをrevertし、Picoを復元する。
- 独自CSS側の問題ならStep 4、Step 3の順で戻す。
- DB/API変更は行わないため、データ移行やバックエンドrollbackは不要。
