package main

import "testing"

func TestParseHenjiSummarySettings_Default(t *testing.T) {
	// 引数なしでは、比較で選んだ既定のOpenRouter経由Geminiを使います。
	got, err := parseHenjiSummarySettings(nil)
	if err != nil {
		t.Fatalf("既定設定の解析に失敗しました: %v", err)
	}
	if got.Path != defaultHenjiSummaryPath || got.API != defaultHenjiSummaryAPI || got.Model != defaultHenjiSummaryModel || got.MaxInputBytes != defaultHenjiMaxInputBytes {
		t.Fatalf("既定設定が違います: %#v", got)
	}
}

func TestParseHenjiSummarySettings_UnknownModelRequiresLimit(t *testing.T) {
	// 未確認のmodel上限を推測するとHenjiが末尾を無警告で落とすため、明示値を求めます。
	if _, err := parseHenjiSummarySettings([]string{"--henji-api", "openrouter", "--henji-model", "example/unknown"}); err == nil {
		t.Fatal("未知のmodelに上限指定を求められていません")
	}
	got, err := parseHenjiSummarySettings([]string{"--henji-api", "openrouter", "--henji-model", "example/unknown", "--henji-max-input-chars", "50000"})
	if err != nil || got.MaxInputBytes != 50000 {
		t.Fatalf("明示した上限を使えません: got=%#v err=%v", got, err)
	}
}

func TestParseHenjiSummarySettings_PathOverride(t *testing.T) {
	// 実行ファイルの配置は環境ごとに異なるため、起動引数で絶対パスへ上書きできます。
	got, err := parseHenjiSummarySettings([]string{"--henji-path", "/opt/bin/henji"})
	if err != nil {
		t.Fatalf("実行パスの上書きに失敗しました: %v", err)
	}
	if got.Path != "/opt/bin/henji" {
		t.Fatalf("実行パスが違います: %#v", got)
	}
}

func TestParseHenjiSummarySettings_OverridePair(t *testing.T) {
	// provider/modelを対で渡すと、Henji設定にある任意の候補へ切り替えられます。
	got, err := parseHenjiSummarySettings([]string{"--henji-api", "google", "--henji-model", "gemini-flash-lite-latest"})
	if err != nil {
		t.Fatalf("上書き設定の解析に失敗しました: %v", err)
	}
	if got.API != "google" || got.Model != "gemini-flash-lite-latest" {
		t.Fatalf("上書き設定が違います: %#v", got)
	}
}

func TestParseHenjiSummarySettings_RequiresPair(t *testing.T) {
	// APIだけ、またはmodelだけでは安全に切り替えられないため拒否します。
	if _, err := parseHenjiSummarySettings([]string{"--henji-api", "google"}); err == nil {
		t.Fatal("片方だけの指定を拒否できていません")
	}
}
