package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExtractSummarySource_PrefersReadmeAndIgnoresNavigation(t *testing.T) {
	// README候補をmainより優先し、ナビゲーションや広告を本文に混ぜないことを確認します。
	first := strings.Repeat("Senpai の本文その一です。", 40)
	second := strings.Repeat("Senpai の本文その二です。", 40)
	third := strings.Repeat("Senpai の本文その三です。", 40)
	html := `<main><nav>操作メニュー</nav><article>` + strings.Repeat("本文候補ではありません。", 80) + `</article><section class="markdown-body"><p>` + first + `</p><p>` + second + `</p><p>` + third + `</p></section><aside class="related">関連記事</aside></main>`
	got := extractSummarySource(html)
	if !strings.Contains(got, "Senpai の本文その一です") {
		t.Fatalf("README本文を抽出できません: %q", got)
	}
	if strings.Contains(got, "操作メニュー") || strings.Contains(got, "関連記事") {
		t.Fatalf("除外対象が本文に混ざっています: %q", got)
	}
}

func TestExtractSummarySource_RequiresEnoughTextAndBlocks(t *testing.T) {
	// 文字数とブロック数の両方が足りないHTMLは、Henjiを呼ばないため空文字にします。
	short := `<article><p>` + strings.Repeat("短い本文。", 100) + `</p><p>二段落目。</p></article>`
	if got := extractSummarySource(short); got != "" {
		t.Fatalf("本文不足は空文字のはずです: %q", got)
	}
}

func TestExtractSummarySource_SkipsTagLikeTextInsideScript(t *testing.T) {
	// script内の文字列をHTMLタグと誤認すると、その後のmainまで走査を飛ばします。
	// Odin公式サイトで観測した形を模し、scriptの後の本文を抽出できることを確認します。
	first := strings.Repeat("Odin はデータ指向のプログラミング言語です。", 25)
	second := strings.Repeat("明示的な設計と高い実行性能を重視します。", 25)
	third := strings.Repeat("公式ドキュメントでは利用例と導入方法を案内します。", 25)
	html := `<script>const highlighter = '</",contains:[example]>'; </script><main><p>` + first + `</p><p>` + second + `</p><p>` + third + `</p></main>`
	got := extractSummarySource(html)
	if !strings.Contains(got, "Odin はデータ指向のプログラミング言語です") {
		t.Fatalf("script後の本文を抽出できません: %q", got)
	}
	if strings.Contains(got, "contains:[example]") {
		t.Fatalf("script内の文字列が本文に混ざっています: %q", got)
	}
}

func TestTruncateSummarySource_KeepsHeadAndTailWithinUnicodeLimit(t *testing.T) {
	// []runeを使うことで、日本語を含むUTF-8文字列を途中で壊さずに上限へ収めます。
	value := strings.Repeat("先頭", 200) + "末尾の重要な結論" + strings.Repeat("終端", 200)
	got := truncateSummarySource(value, 100, summarySourceMaxLines)
	if utf8.RuneCountInString(got) > 100 {
		t.Fatalf("文字数上限を超えています: %d", utf8.RuneCountInString(got))
	}
	if !strings.Contains(got, "[中略]") || !strings.Contains(got, "先頭") || !strings.Contains(got, "終端") {
		t.Fatalf("先頭・中略・末尾を保持できていません: %q", got)
	}
}
