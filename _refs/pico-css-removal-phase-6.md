# Pico CSS除去 第6段階 回帰確認計画

## 目的

Pico CSS削除後の`chore/remove-pico-css`を最終確認し、既存visual baselineだけでは検知しにくい主要操作、キーボードfocus、disabled、CSS読込条件をPlaywright回帰テストで固定する。

デザインやアプリ仕様は変更せず、Firefox・Chromiumの既存snapshot画像を更新しない。

## 前提

- 第1〜第5段階は完了済み。
- `static/css/pico.min.css`、Pico stylesheet link、`data-theme`は削除済み。
- `static/`内のPico参照は0件。
- Firefox・Chromiumで既存snapshotは削除後も成功済み。
- Playwright E2Eはローカル専用で、Forgejo CIには追加しない。
- 404シナリオの完全なPlaywright移行は今回のblockerにしない。

## 変更対象

- `_refs/pico-css-removal-phase-6.md`
- `e2e/authenticated-ui.spec.js`
- 必要な場合のみ既存E2E helper相当部分

原則として`static/`、Go、API、DB、snapshot画像は変更しない。回帰確認で実装上の問題が見つかった場合だけ、原因に最も近いファイルを局所修正し、テスト追加とは別コミットにする。

## Step 1: fixtureの拡張

既存の認証後fixtureを保ちながら、テストごとに次を指定・記録できるよう最小限拡張する。

- bookmark総件数
- 全タグ一覧
- 非GET APIの成功fixture
- API requestのmethod、path、JSON body記録
- import成功結果

既存4シナリオのレスポンスとsnapshotは変えない。

## Step 2: CSS・アクセシビリティ回帰

Firefox・Chromiumの両方で次を自動確認する。

- stylesheet linkが`/css/style.css`の1件だけである。
- password、remember-me checkbox、login buttonへTab移動できる。
- header buttonとexport linkへTab移動できる。
- focus対象が`:focus-visible`に一致し、ringが描画される。
- disabled buttonがpointer操作を受けず、不透明度で状態を判別できる。
- selectに独自chevronがあり、textareaが縦方向resizeである。
- `[hidden]`要素が非表示である。

Firefoxの初回Tabがdocument/bodyへ移動する差を許容し、任意sleepではなくactiveElementを上限付きで待つ。

## Step 3: 主要操作回帰

固定fixtureでネットワーク・DBを変更せず、次を確認する。

### 編集・タグ

- カードの編集ボタンから編集モーダルを開ける。
- URL、タイトル、著者、excerpt、選択済みタグがfixture値で表示される。
- タグ候補を表示・選択・解除できる。
- タグ管理を開き、使用中・未使用表示を描画して閉じられる。

### バルク操作

- checkbox、全選択、選択解除が同期する。
- バルクタグ追加・削除が正しいbookmark ID/tag IDを送信する。
- 一括削除が確認dialog後に正しいIDを送信する。

### import・export・pagination

- export linkのURLとdownload属性を確認する。
- import file選択でmultipart requestと完了dialogを確認する。
- 複数ページ条件でpaginationを表示し、disabled状態と次ページrequestを確認する。

404チェックとAI要約は、既存のGo/JSテストおよび手動確認範囲を維持し、今回の追加E2Eの必須条件にはしない。

## Step 4: レスポンシブ・エラー確認

- 既存375×812 Chromium snapshotと横overflow assertionを維持する。
- desktop 1280×720でもdocument横overflowがないことを主要操作テストで確認する。
- page errorと想定外4xx/5xxがないことを全追加シナリオで確認する。

## コミット境界

計画を実装前に独立コミットする。

```text
docs: plan final Pico removal regression
```

fixtureと回帰テストは1つのテストコミットにまとめる。

```text
test(e2e): cover Pico removal regressions
```

実装不具合が見つかった場合は、修正を別コミットにする。

## 検証

```sh
npm run test:e2e:firefox
npm run test:e2e:chromium
CGO_ENABLED=0 go test ./...
go vet ./...
CGO_ENABLED=0 go build ./...
node --check static/js/app.js
node --check e2e/authenticated-ui.spec.js
git diff --check

# 0件
rg -n -i 'pico' static

# style.cssの1件だけ
rg -n '<link rel="stylesheet"' static/index.html
```

## ユーザーgate

合意済み範囲のfixture調整、テスト安定化、既存仕様に合わせた局所修正、検証再実行では停止しない。

次の場合だけ判断を求める。

- snapshot更新または意図的なデザイン変更が必要になる。
- JavaScript状態管理、HTML構造、API、DBの互換性変更が必要になる。
- import/exportや404確認で外部システム・実データを変更する必要がある。
- branch push、Pull Request作成、`main`へのmergeが必要になる。

## 完了条件

- 既存snapshotが更新なしでFirefox・Chromiumとも成功する。
- CSS読込、focus-visible、disabledを自動テストで固定している。
- 編集、タグ管理、タグ候補、バルク追加・削除・一括削除、import/export、paginationの代表操作がfixture上で成功する。
- desktop・mobileで横overflowがない。
- ブラウザエラーと想定外4xx/5xxがない。
- Go test、vet、静的build、Node構文確認、`git diff --check`が成功する。
- `static/`、Go、API、DB、snapshot画像に不要な変更がない。
- 独立reviewでBlocking Findingがない。

## ロールバック

回帰テスト追加はプロダクション動作を変更しない。問題があればテストコミットだけをrevertできる。実装修正が発生した場合は必ず別コミットとし、第5段階のPico削除コミット`f134f9a`を単独revertできる境界を維持する。
