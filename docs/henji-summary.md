# Henji article summaries

日本語版: [henji-summary.ja.md](henji-summary.ja.md)

Shirushi can summarize the static HTML of a saved bookmark in Japanese with a generative AI model through [Henji](https://forge.harakara.site/littleisland/henji). This feature is optional: bookmarks work normally without Henji.

Summaries are best-effort. Shirushi leaves the existing Excerpt unchanged when the article candidate is insufficient, page retrieval or Henji fails, or the output does not meet the required format.

## Before you start

Install and configure Henji separately, including the credentials required by the provider you select. Shirushi does not read, store, or expose provider API keys. It starts Henji as an external command and sends the extracted article candidate to its standard input.

The implementation is tested with Henji v2.1.7. It needs a compatible Henji command that supports API/model selection and JSON Schema output.

## Start Shirushi with Henji

By default, Shirushi looks for `henji` on `PATH` and uses `openrouter / google/gemini-2.5-flash-lite`.

```bash
SHIRUSHI_PASSWORD='yourpassword' ./shirushi
```

Use another executable location when necessary:

```bash
SHIRUSHI_PASSWORD='yourpassword' ./shirushi \
  --henji-path /opt/bin/henji
```

Override the provider and model as a pair:

```bash
SHIRUSHI_PASSWORD='yourpassword' ./shirushi \
  --henji-api openrouter \
  --henji-model example/model \
  --henji-max-input-chars 4000000
```

`--henji-api` and `--henji-model` must be given together. Known API/model pairs have a built-in input limit. An unknown pair must also supply a positive `--henji-max-input-chars`, expressed in UTF-8 bytes, so Shirushi does not guess the model limit.

## Use it in the Web UI

When Henji is available, each saved bookmark card has an **AI** button. It is not shown in either the create or edit dialog.

After confirmation, Shirushi accepts the job immediately and works in the background. On success, it replaces the bookmark Excerpt with a Japanese summary of one to five non-empty lines and at most 400 Unicode characters.

There is no progress indicator, completion notification, polling, automatic retry, or recovery after a server restart. Reload the list later to see the updated card. Opening the edit dialog always fetches the current bookmark from the server, so a completed summary is included even before a page reload.

Multiple summary starts for the same bookmark are allowed. The result that completes last remains.

## What Shirushi sends to Henji

Shirushi fetches the saved bookmark URL with its existing SSRF protection. It does not execute JavaScript, open a headless browser, or retrieve authenticated pages.

It extracts candidates from static HTML, preferring README/article/main regions and excluding navigation, headers, footers, sidebars, advertising, social controls, and raw script/style content. A candidate must contain at least 600 non-whitespace Unicode characters and three text blocks. Otherwise, Henji is not started and the Excerpt is unchanged.

The normal extracted-text limit is 40,000 Unicode characters. The actual standard-input limit is the smaller of that limit and the selected model's effective `max-input-chars` after the fixed instruction, JSON Schema, framing, and a safety margin are reserved. Long input preserves the beginning and end of the candidate.

Henji receives a fixed Japanese instruction and a JSON Schema that accepts only `summary`. Shirushi validates the returned JSON itself and accepts only a non-empty summary within the line and character limits.

## Failure and security behavior

If Henji is not found, the Web UI hides the **AI** button. `GET /api/capabilities` then returns `{"henji_summary":false}`, and a summary-start request returns `204 No Content` without fetching the URL or changing the database.

For insufficient source text, network failures, Henji failures, timeouts, or invalid output, the existing Excerpt remains unchanged. Shirushi does not show Henji stderr, article text, provider responses, or provider configuration to users.

Shirushi runs Henji with an argument array rather than a shell command, passes the candidate through standard input, applies a 120-second timeout, limits stdout to 16 KiB, and allows at most three summary jobs concurrently.

For endpoint details, see [API Reference](api.md).
