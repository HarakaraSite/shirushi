# Pico CSS除去 第1・第2段階 実装計画

## 目的

`_refs/pico-css-removal.md`の第1・第2段階として、認証後UIの最小限の視覚baselineを保存し、Pico CSSの読み込みを維持したまま`static/css/style.css`の`--pico-*`依存をShirushi独自トークンへ移行する。

この段階ではPico CSS本体の削除、フォーム部品の全面的な自前化、デザイン刷新は行わない。

## 作業ブランチ

`main`の`3345677`から`chore/remove-pico-css`を作成する。

## Step 1: 認証後UIの視覚baseline

### 変更対象

- `e2e/authenticated-ui.spec.js`
- `e2e/authenticated-ui.spec.js-snapshots/`
- 必要な場合のみ`playwright.config.js`

既存の`e2e/login.spec.js`は未認証画面に責務を限定し、変更しない。

### fixture方針

- `scripts/run-e2e-server.sh`の一時DB・一時バイナリを使う。
- 実際のログインフォームから`playwright-test-password`で認証する。
- 認証後画面が取得するGET APIをPlaywrightのroute fixtureで固定する。
- ブックマークは固定ID・固定日時・固定順序の2件、タグは2件とする。
- 外部画像や外部ネットワークへ依存しない。
- 404確認状態はidle、Henji機能は無効として固定する。

### 自動化する代表状態

1. Desktop 1280×720の認証後一覧（Firefox・Chromium）
2. Desktopの追加モーダル（Firefox・Chromium）
3. Desktopのバルクタグドロップダウン（Firefox・Chromium）
4. Mobile 375×812の認証後一覧（Chromium）

編集モーダル、タグ管理、hover、autofill、Tab focusなどはこの段階で過剰にsnapshot化せず、後続段階の手動確認対象とする。

### 決定性

- 日時、件数、ID、配列順を固定する。
- localeを`ja-JP`、timezoneを`Asia/Tokyo`に固定する。
- viewportをテスト内で明示する。
- animationを無効化し、`document.fonts.ready`後に撮影する。
- 任意時間のsleepではなく、対象要素の表示・件数を待つ。
- page errorと想定外の4xx/5xx responseがないことを検証する。

### 検証

```sh
npm run test:e2e:firefox
npm run test:e2e:chromium
CGO_ENABLED=0 go test ./...
go vet ./...
node --check static/js/app.js
git diff --check
```

### コミット

```text
test(e2e): add authenticated UI visual baseline
```

CSS変更前にsnapshotを生成・目視確認し、CSS変更とは別コミットにする。

## Step 2: Shirushi独自トークンへの移行

### 変更対象

- `static/css/style.css`

### トークン対応

| Pico CSS | Shirushi |
|---|---|
| `--pico-background-color` | `--s-background` |
| `--pico-card-background-color` | `--s-surface` |
| `--pico-primary` | `--s-accent` |
| `--pico-primary-hover` | `--s-accent-hover` |
| `--pico-primary-focus` | `--s-focus-ring` |
| `--pico-color` | `--s-text` |
| `--pico-muted-color` | `--s-text-muted` |
| `--pico-muted-border-color` | `--s-border` |
| `--pico-form-element-background-color` | `--s-form-background` |
| `--pico-form-element-border-color` | `--s-form-border` |
| `--pico-form-element-color` | `--s-form-text` |
| `--pico-form-element-placeholder-color` | `--s-form-placeholder` |
| `--pico-form-element-focus-color` | `--s-form-focus` |
| `--pico-border-radius` | `--s-radius` |
| `--pico-card-border-radius` | `--s-card-radius` |
| `--pico-card-box-shadow` | `--s-card-shadow` |
| `--pico-font-family-sans-serif` | `--s-font-family` |
| `--pico-font-size` | `--s-font-size` |
| `--pico-line-height` | `--s-line-height` |

既存値を維持し、意味の異なるトークンを過度に統合しない。

### 実装範囲

- `:root`へ独自トークンを定義する。
- `var(--pico-*)`参照を対応する独自トークンへ置換する。
- `--pico-*`定義を削除する。
- `html`へ`color-scheme: dark`、font、font-size、line-heightを明示する。
- `body`は独自background tokenを使い、既存表示と同じ`sans-serif`を維持する。
- 見出しは独自font tokenを明示する。
- 冒頭コメントを実態に合わせる。
- Pico変数削除でbaseline差分が発生したフォーム寸法、placeholder、focus、select、checkbox、buttonの基本ルールだけを第3段階から前倒しする。

### この段階で変更しないもの

- `static/index.html`のPico CSS linkと`data-theme`
- `static/css/pico.min.css`
- baseline維持に必要な範囲を超えるフォーム状態の全面的な自前化
- Pico競合用`!important`とセレクタの全面整理
- JavaScriptの表示状態管理とインラインstyle
- Go、API、DB
- hard-codedな全色・全寸法の一括トークン化

### 検証

CSS変更後はsnapshotを更新せず比較する。

```sh
# 0件であること
rg -n -- '--pico-' static/css/style.css

# Pico CSSの読み込みが1件残ること
rg -n 'pico\.min\.css' static/index.html

npm run test:e2e:firefox
npm run test:e2e:chromium
CGO_ENABLED=0 go test ./...
go vet ./...
node --check static/js/app.js
git diff --check
```

### コミット

```text
refactor(css): migrate Pico variables to Shirushi tokens
```

## ユーザーgate

通常の実装、検証、コミットでは停止しない。

独自トークン移行によってbaseline差分が発生し、解消に次のどちらかが必要な場合だけユーザー判断を求める。

1. 第3段階のフォーム基本スタイルを今回へ前倒しする。
2. 視覚差を意図したものとして受け入れ、snapshotを更新する。

### Gate判断記録

独自トークン移行後、Pico自身が参照していた変数の削除によりフォーム寸法、focus、checkbox、fontにbaseline差分が発生した。ユーザー判断により第3段階の基本スタイルを必要最小限だけ前倒しし、snapshotを更新せず既存表示を維持する方針を選択した。

前倒し対象はフォームのfont・寸法・色、placeholder、focus、select、checkbox、button、および既存fontの継承関係に限定する。

## 完了条件

- 認証後の代表的なdesktop・mobile baselineが保存されている。
- `static/css/style.css`に`--pico-`が残っていない。
- `static/index.html`は引き続きPico CSSを読み込んでいる。
- CSS変更後もsnapshotを更新せずFirefox・Chromiumで成功する。
- Go test、vet、JavaScript構文確認、`git diff --check`が成功する。
- DB、API、Go、JavaScript状態管理に変更がない。

## ロールバック

baselineとトークン移行を別コミットにする。表示問題が発生した場合はトークン移行コミットだけをrevertし、baselineは原因調査に利用できる状態で残す。
