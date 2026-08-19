# Release checkpoints

## v1.2.1 — 2026-08-19

| Check | Result | Evidence |
|---|---|---|
| `CGO_ENABLED=0 go test ./...` | passed | local pre-tag gate |
| `go vet ./...` | passed | local pre-tag gate |
| `staticcheck ./...` | passed | local pre-tag gate |
| Linux/macOS amd64/arm64 builds | passed | four CGO-free binaries built locally; `SHA256SUMS` verified with `sha256sum -c` |
| Provided live URL set | passed | two non-404 URLs returned 200 and nine expected 404 URLs returned 404 with the production `Accept` header |
| 404-check browser E2E | passed | `ai-dev` managed headless Chromium: running progress/disabled button, completion, `/404.svg`, `modified_at`, completed counters, and zero authenticated-page console errors verified |
| Race test | not selected | C compiler is not installed in `ai-dev`; user approved release without race |
| Forgejo release workflow | passed | [run #30](https://forge.harakara.site/littleisland/shirushi/actions/runs/30) completed successfully for tag `v1.2.1` |
| Four binaries and `SHA256SUMS` | passed | [v1.2.1 release](https://forge.harakara.site/littleisland/shirushi/releases/tag/v1.2.1); all downloaded binaries passed `sha256sum -c SHA256SUMS` |

## v1.2.0 — 2026-08-19

| Check | Result | Evidence |
|---|---|---|
| `CGO_ENABLED=0 go test ./...` | passed | local pre-tag gate |
| `go vet ./...` | passed | local pre-tag gate |
| `staticcheck ./...` | passed | local pre-tag gate |
| Linux/macOS amd64/arm64 builds | passed | four CGO-free binaries built locally; `SHA256SUMS` verified with `sha256sum -c` |
| 404-check browser E2E | passed | `ai-dev` managed headless Chromium: running progress/disabled button, completion, `/404.svg`, `modified_at`, and zero console errors verified |
| Race test | not selected | C compiler is not installed in `ai-dev`; user approved release without race |
| Forgejo release workflow | passed | [run #27](https://forge.harakara.site/littleisland/shirushi/actions/runs/27) completed successfully for tag `v1.2.0` |
| Four binaries and `SHA256SUMS` | passed | [v1.2.0 release](https://forge.harakara.site/littleisland/shirushi/releases/tag/v1.2.0); all downloaded binaries passed `sha256sum -c SHA256SUMS` |
