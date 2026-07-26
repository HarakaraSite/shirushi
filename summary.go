package main

// summary.go：Henjiへ渡す前の、静的HTMLから本文候補を取り出す処理を置くファイルです。
// この増分では外部プロセス、API、DB更新を扱わず、本文抽出だけを独立して検証します。

import (
	"html"         // HTMLエンティティを表示テキストへ戻すために使うパッケージ
	"strings"      // タグ名と空白の正規化に使うパッケージ
	"unicode"      // Unicode文字単位の空白判定に使うパッケージ
	"unicode/utf8" // Unicode文字数を安全に数えるために使うパッケージ
)

const (
	// summarySourceMaxChars：本文抽出後に通常保持する最大文字数です。
	// Henjiへ渡す実際の上限は、モデル選定後に別途算出します。
	summarySourceMaxChars = 40000
	summarySourceMaxLines = 512
	summaryMinimumChars   = 600
	summaryMinimumBlocks  = 3
)

// summaryCandidate：優先順位付きの本文候補を一つ保持します。
type summaryCandidate struct {
	priority   int
	startDepth int
	text       strings.Builder
}

// htmlFrame：開いている要素と、本文から除外する範囲かを保持します。
type htmlFrame struct {
	name    string
	ignored bool
}

// extractSummarySource：HTMLから要約用の本文候補を一つ返します。
// 候補がない、または短すぎる場合は空文字を返し、呼び出し側はHenjiを起動しません。
func extractSummarySource(source string) string {
	var candidates []*summaryCandidate
	var stack []htmlFrame
	ignoredDepth := 0

	appendText := func(text string, block bool) {
		if ignoredDepth > 0 {
			return
		}
		text = html.UnescapeString(text)
		for _, candidate := range candidates {
			if len(stack) >= candidate.startDepth {
				if block {
					candidate.text.WriteByte('\n')
				}
				if text != "" {
					candidate.text.WriteString(text)
				}
			}
		}
	}

	for pos := 0; pos < len(source); {
		nextTag := strings.IndexByte(source[pos:], '<')
		if nextTag < 0 {
			appendText(source[pos:], false)
			break
		}
		nextTag += pos
		appendText(source[pos:nextTag], false)

		end := findTagEnd(source, nextTag)
		if end < 0 {
			// 閉じ山括弧がない残りはHTMLタグと断定せず、テキストとして安全に扱います。
			appendText(source[nextTag:], false)
			break
		}
		name, attrs, closing, selfClosing := parseHTMLTag(source[nextTag+1 : end])
		pos = end + 1
		if name == "" {
			continue
		}

		if closing {
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].name != name {
					continue
				}
				for _, frame := range stack[i:] {
					if frame.ignored {
						ignoredDepth--
					}
				}
				stack = stack[:i]
				break
			}
			continue
		}
		// scriptやstyleの内容には、JavaScript/CSS中の "</...>" のように
		// HTMLタグに見える文字列が含まれます。そこを通常のHTMLとして走査すると、
		// 後続の<main>まで一つの疑似タグに飲み込むことがあるため、終了タグまで
		// まとめて飛ばします。これらは本文候補から常に除外する要素です。
		if isRawTextIgnoredSummaryElement(name) {
			if after, found := skipRawTextSummaryElement(source, pos, name); found {
				pos = after
				continue
			}
			// 閉じタグがないraw text要素の残りは安全に本文へ使いません。
			break
		}

		ignored := isIgnoredSummaryElement(name, attrs)
		if ignored {
			ignoredDepth++
		}
		stack = append(stack, htmlFrame{name: name, ignored: ignored})
		if priority := summaryCandidatePriority(name, attrs); priority > 0 {
			candidates = append(candidates, &summaryCandidate{priority: priority, startDepth: len(stack)})
		}
		if isSummaryBlockElement(name) {
			appendText("", true)
		}
		if selfClosing {
			if ignored {
				ignoredDepth--
			}
			stack = stack[:len(stack)-1]
		}
	}

	var best string
	bestPriority := 0
	for _, candidate := range candidates {
		normalized := normalizeSummaryText(candidate.text.String())
		if candidate.priority > bestPriority || (candidate.priority == bestPriority && utf8.RuneCountInString(normalized) > utf8.RuneCountInString(best)) {
			best = normalized
			bestPriority = candidate.priority
		}
	}
	if utf8.RuneCountInString(best) < summaryMinimumChars || summaryBlockCount(best) < summaryMinimumBlocks {
		return ""
	}
	return truncateSummarySource(best, summarySourceMaxChars, summarySourceMaxLines)
}

// isRawTextIgnoredSummaryElement：中身をHTMLとして読み取らない除外要素を判定します。
func isRawTextIgnoredSummaryElement(name string) bool {
	switch name {
	case "script", "style", "noscript", "template":
		return true
	default:
		return false
	}
}

// skipRawTextSummaryElement：raw text要素の終了タグ直後の位置を返します。
// HTMLタグ名は大文字小文字を区別しないため、検索時にも小文字化して比較します。
func skipRawTextSummaryElement(source string, start int, name string) (int, bool) {
	lower := strings.ToLower(source[start:])
	marker := "</" + name
	for offset := 0; ; {
		index := strings.Index(lower[offset:], marker)
		if index < 0 {
			return 0, false
		}
		index += offset
		afterName := index + len(marker)
		// </scripture> のような別のタグを終了タグと誤認しないよう、
		// 名前の直後がタグを終えられる文字かを確認します。
		if afterName < len(lower) && !strings.ContainsRune(" \t\r\n/>", rune(lower[afterName])) {
			offset = afterName
			continue
		}
		end := findTagEnd(source, start+index)
		if end < 0 {
			return 0, false
		}
		return end + 1, true
	}
}

// findTagEnd：引用符内の > をタグ終端と誤認しないように、タグの終端を探します。
func findTagEnd(source string, start int) int {
	var quote byte
	for i := start + 1; i < len(source); i++ {
		if quote != 0 {
			if source[i] == quote {
				quote = 0
			}
			continue
		}
		if source[i] == '\'' || source[i] == '"' {
			quote = source[i]
			continue
		}
		if source[i] == '>' {
			return i
		}
	}
	return -1
}

// parseHTMLTag：本文抽出に必要なタグ名とclass/id/roleだけを、寛容に読み取ります。
func parseHTMLTag(raw string) (string, map[string]string, bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "!") || strings.HasPrefix(raw, "?") {
		return "", nil, false, false
	}
	closing := strings.HasPrefix(raw, "/")
	if closing {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "/"))
	}
	selfClosing := strings.HasSuffix(raw, "/")
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "/"))
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return "", nil, closing, selfClosing
	}
	name := strings.ToLower(fields[0])
	attrs := map[string]string{}
	rest := strings.TrimSpace(strings.TrimPrefix(raw, fields[0]))
	for rest != "" {
		keyEnd := strings.IndexFunc(rest, func(r rune) bool { return unicode.IsSpace(r) || r == '=' })
		if keyEnd < 0 {
			attrs[strings.ToLower(rest)] = ""
			break
		}
		key := strings.ToLower(rest[:keyEnd])
		rest = strings.TrimSpace(rest[keyEnd:])
		value := ""
		if strings.HasPrefix(rest, "=") {
			rest = strings.TrimSpace(rest[1:])
			if strings.HasPrefix(rest, "\"") || strings.HasPrefix(rest, "'") {
				quote := rest[0]
				rest = rest[1:]
				if end := strings.IndexByte(rest, quote); end >= 0 {
					value, rest = rest[:end], strings.TrimSpace(rest[end+1:])
				} else {
					value, rest = rest, ""
				}
			} else {
				end := strings.IndexFunc(rest, unicode.IsSpace)
				if end < 0 {
					value, rest = rest, ""
				} else {
					value, rest = rest[:end], strings.TrimSpace(rest[end:])
				}
			}
		}
		attrs[key] = value
	}
	return name, attrs, closing, selfClosing
}

func summaryCandidatePriority(name string, attrs map[string]string) int {
	words := strings.ToLower(attrs["class"] + " " + attrs["id"])
	for _, target := range []string{"readme", "markdown-body", "article-body", "article-content", "entry-content", "post-content"} {
		if strings.Contains(words, target) {
			return 4
		}
	}
	switch {
	case name == "article":
		return 3
	case strings.EqualFold(attrs["role"], "main"):
		return 2
	case name == "main":
		return 1
	default:
		return 0
	}
}

func isIgnoredSummaryElement(name string, attrs map[string]string) bool {
	for _, ignored := range []string{"script", "style", "noscript", "template", "svg", "canvas", "nav", "header", "footer", "aside", "form", "dialog"} {
		if name == ignored {
			return true
		}
	}
	if _, hidden := attrs["hidden"]; hidden || strings.EqualFold(attrs["aria-hidden"], "true") {
		return true
	}
	words := strings.ToLower(attrs["class"] + " " + attrs["id"])
	for _, ignored := range []string{"ad", "ads", "advertisement", "affiliate", "breadcrumb", "related", "recommendation", "share", "social", "sidebar", "newsletter", "cookie", "modal"} {
		if containsHTMLWord(words, ignored) {
			return true
		}
	}
	return false
}

func containsHTMLWord(value, target string) bool {
	for _, word := range strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if word == target {
			return true
		}
	}
	return false
}

func isSummaryBlockElement(name string) bool {
	switch name {
	case "p", "h1", "h2", "h3", "h4", "h5", "h6", "li", "blockquote", "pre", "code", "td", "th", "br":
		return true
	default:
		return false
	}
}

func normalizeSummaryText(value string) string {
	lines := strings.Split(value, "\n")
	seen := map[string]bool{}
	var normalized []string
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		normalized = append(normalized, line)
	}
	return strings.Join(normalized, "\n")
}

func summaryBlockCount(value string) int {
	if value == "" {
		return 0
	}
	return len(strings.Split(value, "\n"))
}

// truncateSummarySource：長文でも主題と結論を残せるよう、先頭と末尾を比例配分で保持します。
func truncateSummarySource(value string, maxChars, maxLines int) string {
	lines := strings.Split(value, "\n")
	if len(lines) > maxLines {
		lines = append(lines[:maxLines*80/100], append([]string{"[中略]"}, lines[len(lines)-maxLines*20/100:]...)...)
		value = strings.Join(lines, "\n")
	}
	runes := []rune(value)
	if len(runes) <= maxChars {
		return value
	}
	marker := []rune("\n[中略]\n")
	remaining := maxChars - len(marker)
	head := remaining * 80 / 100
	tail := remaining - head
	return string(runes[:head]) + string(marker) + string(runes[len(runes)-tail:])
}
