# Release checkpoints

## v1.3.0 — 2026-08-26

| Check | Result | Evidence |
|---|---|---|
| Host Firefox user acceptance | passed | User approved candidate commit `5bb5c46c1d08023a09114789cc3c13ccecbb462e` for proposed tag `v1.3.0` using Firefox 154 on the host |
| Final pre-tag tree | passed | `main` commit `b143256ce869803a03a4f1ef05409b32a5c1f3fa`; application Go sources, embedded static files, and the release workflow are unchanged from the user-approved candidate |
| Automated pre-tag candidate | passed | checks below ran against committed release-preparation tree `69ce52cd2da52c406f745e13ca27ff90d924ee90`; subsequent changes are limited to checkpoint/release documentation, local static-analysis tooling, and merge metadata |
| `CGO_ENABLED=0 go test ./...` | passed | local pre-tag gate |
| `go vet ./...` | passed | local pre-tag gate |
| Firefox Playwright suite | passed | 8 passed and 1 Chromium-only mobile baseline skipped; login, authenticated UI, interaction regressions, and the 404-check flow verified |
| Chromium Playwright suite | passed | all 9 tests passed, including the mobile baseline and 404-check flow |
| 404-check browser E2E | passed | repository-local Firefox Playwright Test verified running progress, disabled button, completion dialog, `/404.svg`, non-null `modified_at`, and zero unexpected authenticated-page console errors |
| Linux/macOS amd64/arm64 builds | passed | four CGO-free binaries built locally; Linux binaries verified statically linked and all files passed `sha256sum -c SHA256SUMS` |
| Local static analysis | passed | staticcheck 2026.1 and gosec 2.28.0 completed with zero findings; gosec used the documented local exclusions |
| Main branch CI | passed | [run #47](https://forge.harakara.site/littleisland/shirushi/actions/runs/47) completed successfully for `b143256ce869803a03a4f1ef05409b32a5c1f3fa` |
| Forgejo native Actions logs | passed | Forgejo `16.0.2`; `fja ci logs --tag v1.2.1` retrieved run #30 through `native_actions_run_logs` with HTTP 200 |
| Forgejo release workflow | passed | [run #51](https://forge.harakara.site/littleisland/shirushi/actions/runs/51) completed successfully for tag `v1.3.0` targeting `41dc1dc88e5045ad50d571270a41622675785bf9` |
| Four binaries and `SHA256SUMS` | passed | [v1.3.0 release](https://forge.harakara.site/littleisland/shirushi/releases/tag/v1.3.0); all downloaded binaries passed `sha256sum -c SHA256SUMS`, and both Linux binaries were verified statically linked |

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
