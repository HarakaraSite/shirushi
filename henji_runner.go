package main

// henji_runner.go：本文候補を一度だけHenjiへ渡し、検証済みの要約だけを返す処理です。
// HTTP APIやDB更新はここで行わず、外部プロセスとの安全な境界だけを担当します。

import (
	"bytes"         // stdoutを上限付きで保持するために使うパッケージ
	"context"       // Henjiの実行時間に上限を設けるために使うパッケージ
	"encoding/json" // JSON Schema出力を厳密に読むために使うパッケージ
	"errors"        // 出力上限超過を区別するために使うパッケージ
	"fmt"           // 呼び出し失敗を呼び出し元へ伝えるために使うパッケージ
	"io"            // stderr破棄とJSON末尾検査に使うパッケージ
	"os"            // Schema用一時ファイルを安全に作るために使うパッケージ
	"os/exec"       // shellを経由せずHenjiを起動するために使うパッケージ
	"strings"       // stdinと要約テキストを整形するために使うパッケージ
	"time"          // Henji実行のtimeoutに使うパッケージ
	"unicode/utf8"  // UTF-8の途中で本文を切らないために使うパッケージ
)

const (
	henjiSummaryTimeout     = 120 * time.Second
	henjiSummaryStdoutLimit = 16 * 1024
)

var errHenjiSummaryOutputTooLarge = errors.New("Henjiの出力が上限を超えました")

type henjiSummaryOutput struct {
	Summary string `json:"summary"`
}

// runHenjiSummary：設定済みのHenjiを一度だけ起動して、検証済みsummaryを返します。
func runHenjiSummary(ctx context.Context, settings HenjiSummarySettings, source string) (string, error) {
	path, err := exec.LookPath(settings.Path)
	if err != nil {
		return "", fmt.Errorf("本文要約でHenjiが利用できません: %w", err)
	}
	schema := summarySchemaForAPI(settings.API)
	inputBudget := summaryInputBudget(settings.MaxInputBytes, summaryPrompt, schema)
	prepared, err := truncateSummaryInputToBytes(source, inputBudget)
	if err != nil {
		return "", err
	}

	schemaFile, err := os.CreateTemp("", "shirushi-henji-summary-schema-*.json")
	if err != nil {
		return "", fmt.Errorf("JSON Schemaファイルを作成できません: %w", err)
	}
	schemaPath := schemaFile.Name()
	defer os.Remove(schemaPath)
	if err := schemaFile.Chmod(0o600); err != nil {
		schemaFile.Close()
		return "", fmt.Errorf("JSON Schemaファイルの権限を設定できません: %w", err)
	}
	if _, err := schemaFile.WriteString(schema); err != nil {
		schemaFile.Close()
		return "", fmt.Errorf("JSON Schemaファイルへ書き込めません: %w", err)
	}
	if err := schemaFile.Close(); err != nil {
		return "", fmt.Errorf("JSON Schemaファイルを閉じられません: %w", err)
	}

	commandCtx, cancel := context.WithTimeout(ctx, henjiSummaryTimeout)
	defer cancel()
	args := henjiSummaryArgs(settings, schemaPath)
	cmd := exec.CommandContext(commandCtx, path, args...)
	cmd.Stdin = strings.NewReader(prepared)
	stdout := &limitedBuffer{limit: henjiSummaryStdoutLimit}
	cmd.Stdout = stdout
	// stderrは進捗やproviderの詳細を含み得るため、利用者レスポンスにもログにも出しません。
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if errors.Is(stdout.err, errHenjiSummaryOutputTooLarge) {
			return "", stdout.err
		}
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return "", errors.New("Henjiの実行がタイムアウトしました")
		}
		return "", fmt.Errorf("本文要約でHenjiの実行に失敗しました: %w", err)
	}
	if stdout.err != nil {
		return "", stdout.err
	}
	return parseHenjiSummaryOutput(stdout.String())
}

// henjiSummaryArgs：Henjiへ渡す引数を一つずつ組み立てます。
// 文字列をshellへ渡さないため、本文やmodel名に記号があってもコマンド解釈されません。
func henjiSummaryArgs(settings HenjiSummarySettings, schemaPath string) []string {
	return []string{
		"-q",
		"-a", settings.API,
		"-m", settings.Model,
		"--no-cache",
		"--max-tokens", "1024",
		"--json-schema", schemaPath,
		"--json-schema-retries", "0",
		summaryPrompt,
	}
}

// summarySchemaForAPI：GoogleだけはadditionalPropertiesを拒否するためSchemaを分けます。
func summarySchemaForAPI(api string) string {
	if api == "google" {
		return googleSummarySchema
	}
	return strictSummarySchema
}

// limitedBuffer：外部プロセスのstdoutを上限以上メモリへ保持しないWriterです。
type limitedBuffer struct {
	bytes.Buffer
	limit int
	err   error
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	if b.Len()+len(value) > b.limit {
		b.err = errHenjiSummaryOutputTooLarge
		return 0, b.err
	}
	return b.Buffer.Write(value)
}

// parseHenjiSummaryOutput：JSONの形、行数、文字数をShirushi側でも検証します。
func parseHenjiSummaryOutput(raw string) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var output henjiSummaryOutput
	if err := decoder.Decode(&output); err != nil {
		return "", errors.New("Henjiの出力が期待したJSONではありません")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return "", errors.New("Henjiの出力に余分なJSONがあります")
	}
	summary := strings.ReplaceAll(strings.TrimSpace(output.Summary), "\r\n", "\n")
	if summary == "" || utf8.RuneCountInString(summary) > 400 {
		return "", errors.New("Henjiの要約が空、または文字数上限を超えています")
	}
	lines := strings.Split(summary, "\n")
	if len(lines) > 5 {
		return "", errors.New("Henjiの要約が5行を超えています")
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			return "", errors.New("Henjiの要約に空行があります")
		}
	}
	return summary, nil
}

// truncateSummaryInputToBytes：UTF-8の途中で切らず、必要時だけ先頭80%・末尾20%を残します。
func truncateSummaryInputToBytes(value string, budget int) (string, error) {
	if budget <= 0 {
		return "", errors.New("Henjiへ渡せる本文の予算がありません")
	}
	if len(value) <= budget {
		return value, nil
	}
	marker := "\n[中略]\n"
	if budget <= len(marker) {
		return "", errors.New("Henjiへ渡せる本文の予算が小さすぎます")
	}
	remaining := budget - len(marker)
	head := takeUTF8Prefix(value, remaining*80/100)
	tail := takeUTF8Suffix(value, remaining-len(head))
	return head + marker + tail, nil
}

func takeUTF8Prefix(value string, budget int) string {
	if budget <= 0 {
		return ""
	}
	used := 0
	for index, r := range value {
		size := utf8.RuneLen(r)
		if used+size > budget {
			return value[:index]
		}
		used += size
	}
	return value
}

func takeUTF8Suffix(value string, budget int) string {
	if budget <= 0 {
		return ""
	}
	used := 0
	start := len(value)
	for index := len(value); index > 0; {
		r, size := utf8.DecodeLastRuneInString(value[:index])
		if r == utf8.RuneError && size == 0 {
			break
		}
		if used+size > budget {
			break
		}
		used += size
		start = index - size
		index -= size
	}
	return value[start:]
}
