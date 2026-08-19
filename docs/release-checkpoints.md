# Release checkpoints

## v1.2.0 — 2026-08-19

| Check | Result | Evidence |
|---|---|---|
| `CGO_ENABLED=0 go test ./...` | passed | local pre-tag gate |
| `go vet ./...` | passed | local pre-tag gate |
| `staticcheck ./...` | passed | local pre-tag gate |
| Linux/macOS amd64/arm64 builds | passed | four CGO-free binaries built locally; `SHA256SUMS` verified with `sha256sum -c` |
| 404-check browser E2E | passed | `ai-dev` managed headless Chromium: running progress/disabled button, completion, `/404.svg`, `modified_at`, and zero console errors verified |
| Race test | not selected | C compiler is not installed in `ai-dev`; user approved release without race |
| Forgejo release workflow | pending | tag-triggered native Actions log |
| Four binaries and `SHA256SUMS` | pending | public Forgejo Release assets |
