package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseHenjiSummaryOutput(t *testing.T) {
	// SchemaだけでなくShirushi側でも、summary以外を受け取らないことを確認します。
	got, err := parseHenjiSummaryOutput(`{"summary":"一行目\n二行目"}`)
	if err != nil || got != "一行目\n二行目" {
		t.Fatalf("正常な要約を読めません: got=%q err=%v", got, err)
	}
	if _, err := parseHenjiSummaryOutput(`{"summary":"ok","other":"拒否"}`); err == nil {
		t.Fatal("未知fieldを拒否できていません")
	}
	if _, err := parseHenjiSummaryOutput(`{"summary":"1\n2\n3\n4\n5\n6"}`); err == nil {
		t.Fatal("6行の要約を拒否できていません")
	}
}

func TestTruncateSummaryInputToBytes_PreservesUTF8AndBudget(t *testing.T) {
	// 日本語を含む本文でもUTF-8の途中で壊さず、先頭・末尾を残せることを確認します。
	value := strings.Repeat("先頭", 30) + strings.Repeat("中間", 30) + strings.Repeat("末尾", 30)
	got, err := truncateSummaryInputToBytes(value, 100)
	if err != nil {
		t.Fatalf("切詰めに失敗しました: %v", err)
	}
	if len(got) > 100 || !utf8.ValidString(got) {
		t.Fatalf("UTF-8またはバイト上限を守れていません: bytes=%d valid=%t", len(got), utf8.ValidString(got))
	}
	if !strings.Contains(got, "[中略]") || !strings.HasPrefix(got, "先頭") || !strings.HasSuffix(got, "末尾") {
		t.Fatalf("先頭・中略・末尾を保持できていません: %q", got)
	}
}

func TestSummarySchemaForAPI(t *testing.T) {
	// GoogleだけはadditionalPropertiesを受け付けないSchemaを使います。
	if strings.Contains(summarySchemaForAPI("google"), "additionalProperties") {
		t.Fatal("Google用SchemaにadditionalPropertiesが含まれています")
	}
	if !strings.Contains(summarySchemaForAPI("openrouter"), "additionalProperties") {
		t.Fatal("OpenRouter用Schemaがstrictではありません")
	}
}

func TestHenjiSummaryArgs(t *testing.T) {
	// API/model/promptは一つずつargvに置かれ、shell文字列にはなりません。
	settings := HenjiSummarySettings{API: "openrouter", Model: "google/gemini-2.5-flash-lite"}
	got := henjiSummaryArgs(settings, "/tmp/schema.json")
	want := []string{
		"-q", "-a", "openrouter", "-m", "google/gemini-2.5-flash-lite",
		"--no-cache", "--max-tokens", "1024", "--json-schema", "/tmp/schema.json",
		"--json-schema-retries", "0", summaryPrompt,
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("Henji引数が違います: got=%q want=%q", got, want)
	}
}
