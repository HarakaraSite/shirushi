package main

import "testing"

func TestSummaryInputBudget_ConfiguredCandidatesHaveFortyThousandCharacterHeadroom(t *testing.T) {
	// 実行環境で確認した候補の個別max-input-charsを、秘密情報を含めず再現します。
	// どの候補も40,000 Unicode文字を最悪4 bytes/文字として扱えることを確認します。
	tests := []struct {
		name          string
		maxInputBytes int
		schema        string
	}{
		{name: "google/gemini-flash-lite-latest", maxInputBytes: 4000000, schema: googleSummarySchema},
		{name: "openrouter/deepseek/deepseek-v4-flash", maxInputBytes: 4194304, schema: strictSummarySchema},
		{name: "openai/gpt-5.6-terra", maxInputBytes: 794000, schema: strictSummarySchema},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := summaryInputBudget(tt.maxInputBytes, summaryPrompt, tt.schema)
			limit := summaryUnicodeLimit(budget)
			t.Logf("prompt bytes=%d schema bytes=%d source budget bytes=%d conservative Unicode limit=%d", len(summaryPrompt), len(tt.schema), budget, limit)
			if limit != summarySourceMaxChars {
				t.Fatalf("40,000 Unicode文字を安全に渡せません: got %d", limit)
			}
		})
	}
}

func TestSummaryInputBudget_RejectsTooSmallModel(t *testing.T) {
	// 入力上限が小さく、本文成立条件を満たせない候補を比較対象から外せることを確認します。
	budget := summaryInputBudget(3000, summaryPrompt, strictSummarySchema)
	if got := summaryUnicodeLimit(budget); got >= summaryMinimumChars {
		t.Fatalf("小さすぎる候補を成立可能として扱っています: %d", got)
	}
}
