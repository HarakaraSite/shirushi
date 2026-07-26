package main

// summary_live_test.go：実サイトのHTML構造が変わっても通常のテストを不安定にしないよう、
// SHIRUSHI_LIVE_SUMMARY_TEST=1 を指定したときだけ本文抽出を確認するテストです。

import (
	"context"
	"os"
	"testing"
	"unicode/utf8"
)

func TestLiveExtractSummarySource_RepresentativePages(t *testing.T) {
	if os.Getenv("SHIRUSHI_LIVE_SUMMARY_TEST") != "1" {
		t.Skip("実サイトへの通信は SHIRUSHI_LIVE_SUMMARY_TEST=1 のときだけ実行します")
	}

	// 本文あり・README表示・長文・本文不足の受入確認を、実際の静的HTMLで行います。
	// 取得結果はサイト側の変更で変わり得るため、失敗時はselectorや閾値を勝手に緩めず
	// 記録を残してconcept reviewへ戻します。
	tests := []struct {
		name       string
		url        string
		wantSource bool
		wantReadme bool
	}{
		{name: "Fil-C", url: "https://fil-c.org/", wantSource: true},
		{name: "Fil-C Pizlix", url: "https://fil-c.org/pizlix", wantSource: true},
		{name: "Senpai", url: "https://git.sr.ht/~delthas/senpai/", wantSource: true, wantReadme: true},
		{name: "YAMA HACK", url: "https://yamahack.com/7651", wantSource: true},
		{name: "Odin", url: "https://odin-lang.org/", wantSource: true},
		{name: "Sakana AI", url: "https://sakana.ai/", wantSource: false},
		{name: "Rephial", url: "https://rephial.org/", wantSource: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, _, err := fetchHTML(tt.url, 2*1024*1024)
			if err != nil {
				t.Fatalf("HTML取得に失敗しました: %v", err)
			}
			source := extractSummarySource(html)
			t.Logf("HTML bytes=%d, extracted chars=%d, blocks=%d", len(html), utf8.RuneCountInString(source), summaryBlockCount(source))
			if (source != "") != tt.wantSource {
				t.Fatalf("本文候補の成立結果が期待と違います: got %t, want %t", source != "", tt.wantSource)
			}
			if tt.wantReadme && source == "" {
				t.Fatal("README表示の本文候補を抽出できません")
			}
		})
	}
}

// TestLiveRunHenjiSummary_FilCPizlix：本番と同じHenji起動・stdin・JSON検証を通す、
// 費用を伴い得る手動受入テストです。通常のgo testでは実行しません。
func TestLiveRunHenjiSummary_FilCPizlix(t *testing.T) {
	if os.Getenv("SHIRUSHI_LIVE_HENJI_TEST") != "1" {
		t.Skip("実Henji呼出しは SHIRUSHI_LIVE_HENJI_TEST=1 のときだけ実行します")
	}

	settings, err := parseHenjiSummarySettings(nil)
	if err != nil {
		t.Fatalf("Henji設定を読めません: %v", err)
	}
	html, _, err := fetchHTML("https://fil-c.org/pizlix", 2*1024*1024)
	if err != nil {
		t.Fatalf("PizlixのHTML取得に失敗しました: %v", err)
	}
	source := extractSummarySource(html)
	if source == "" {
		t.Fatal("Pizlixの本文候補が成立しません")
	}
	summary, err := runHenjiSummary(context.Background(), settings, source)
	if err != nil {
		t.Fatalf("Henji要約に失敗しました: %v", err)
	}
	t.Logf("summary=%q", summary)
}
