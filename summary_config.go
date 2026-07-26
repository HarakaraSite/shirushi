package main

// summary_config.go：本文要約で使うHenjiのprovider/modelを、起動引数から読む処理です。

import (
	"errors"  // 起動引数の不正な組み合わせを返すために使うパッケージ
	"flag"    // 起動引数を標準形式で解析するために使うパッケージ
	"fmt"     // 利用できない余分な引数を説明するために使うパッケージ
	"io"      // flagの利用方法を通常起動時に表示しないために使うパッケージ
	"strings" // 余分な引数を一つのエラー文へまとめるために使うパッケージ
)

const (
	// 指定がない場合は、比較で高速かつ4/4成功したOpenRouter経由Geminiを使います。
	// APIキーやendpointはHenji設定が管理し、ShirushiはこのIDだけを渡します。
	defaultHenjiSummaryAPI    = "openrouter"
	defaultHenjiSummaryModel  = "google/gemini-2.5-flash-lite"
	defaultHenjiSummaryPath   = "henji"
	defaultHenjiMaxInputBytes = 4000000
)

// parseHenjiSummarySettings：本文要約のprovider/modelを起動引数から一度だけ読み取ります。
// 一方だけ指定すると存在しない組み合わせを作りやすいため、変更時は必ず対で指定させます。
func parseHenjiSummarySettings(args []string) (HenjiSummarySettings, error) {
	settings := HenjiSummarySettings{
		Path:          defaultHenjiSummaryPath,
		API:           defaultHenjiSummaryAPI,
		Model:         defaultHenjiSummaryModel,
		MaxInputBytes: defaultHenjiMaxInputBytes,
	}
	flags := flag.NewFlagSet("shirushi", flag.ContinueOnError)
	// flagの標準usageはWebサーバー起動時には冗長なので、mainが返すエラーだけを表示します。
	flags.SetOutput(io.Discard)
	path := flags.String("henji-path", defaultHenjiSummaryPath, "Henji executable path for bookmark summaries")
	api := flags.String("henji-api", "", "Henji API name for bookmark summaries")
	model := flags.String("henji-model", "", "Henji model ID for bookmark summaries")
	maxInputBytes := flags.Int("henji-max-input-chars", 0, "Henji max-input-chars in UTF-8 bytes for bookmark summaries")
	if err := flags.Parse(args); err != nil {
		return HenjiSummarySettings{}, err
	}
	if flags.NArg() != 0 {
		return HenjiSummarySettings{}, fmt.Errorf("未対応の起動引数があります: %s", strings.Join(flags.Args(), " "))
	}
	if (*api == "") != (*model == "") {
		return HenjiSummarySettings{}, errors.New("--henji-api と --henji-model は必ず対で指定してください")
	}
	if *api != "" {
		settings.API = *api
		settings.Model = *model
		if known, ok := knownHenjiMaxInputBytes(settings.API, settings.Model); ok {
			settings.MaxInputBytes = known
		} else if *maxInputBytes <= 0 {
			return HenjiSummarySettings{}, errors.New("未知のprovider/modelには --henji-max-input-chars を指定してください")
		}
	}
	if *maxInputBytes > 0 {
		settings.MaxInputBytes = *maxInputBytes
	}
	if settings.MaxInputBytes <= 0 {
		return HenjiSummarySettings{}, errors.New("--henji-max-input-chars は正の値にしてください")
	}
	if *path == "" {
		return HenjiSummarySettings{}, errors.New("--henji-path は空にできません")
	}
	settings.Path = *path
	return settings, nil
}

// knownHenjiMaxInputBytes：比較済みの候補だけは確認済み設定値を返します。
// それ以外を推測しないことで、Henjiの無警告な末尾切詰めを防ぎます。
func knownHenjiMaxInputBytes(api, model string) (int, bool) {
	known := map[string]int{
		"openrouter\x00google/gemini-2.5-flash-lite": 4000000,
		"google\x00gemini-flash-lite-latest":         4000000,
		"openrouter\x00deepseek/deepseek-v4-flash":   4194304,
		"openai\x00gpt-5.6-terra":                    794000,
	}
	value, ok := known[api+"\x00"+model]
	return value, ok
}
