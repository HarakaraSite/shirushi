# Changelog

日本語版: [CHANGELOG.ja.md](CHANGELOG.ja.md)

## 1.3.0

### Changed

- Replaced the bundled Pico CSS dependency with Shirushi-owned base, form, layout, and interaction
  styles while preserving the existing desktop and mobile workflows.
- Unified hover, disabled, and keyboard focus-visible states under Shirushi design tokens.

### Quality

- Added repository-local Playwright visual and interaction regression coverage for Firefox and
  Chromium, including the asynchronous 404-check flow.

## 1.2.1

### Fixed

- Prevented false 404 results from sites that require an HTML `Accept` request header, including
  crates.io and claude.com pages.

## 1.2.0

### Added

- An asynchronous **404 check** action that checks all saved bookmark URLs with bounded concurrency and replaces thumbnails for HTTP 404 responses with a bundled 404 image.
- Progress polling and completion results for the 404 check, including failed-request counts.

### Changed

- SQLite schema generations are now recorded in `PRAGMA user_version` after migrations complete.
- The Forgejo tag-release workflow now runs tests and vet, builds four CGO-free binaries, and publishes `SHA256SUMS`.

### Notes

- A 404 result updates both `image_url` and `modified_at`. If the URL is edited while a check is running, the stale result does not overwrite the edited bookmark.
- 404 job progress is held in memory and is not recovered after restarting Shirushi.

## 1.1.0

### Added

- `GET /api/bookmarks/by-url` for browser extensions and other API clients to look up a saved bookmark by its normalized, exact URL.

## 1.0.0

### Added

- Optional Japanese article summaries for saved bookmarks through [Henji](https://forge.harakara.site/littleisland/henji).
- `GET /api/bookmarks/{id}` to retrieve the latest bookmark before editing.
- Configurable Henji executable, provider, model, and input limit through startup arguments.

### Changed

- The bookmark card now provides an **AI** action when Henji is available.
- The edit dialog retrieves the current bookmark from the server before opening, so a completed summary is not overwritten by stale page data.
- The edit dialog has more room for reading and editing a long Excerpt.

### Notes

- Henji is optional and is configured separately. Shirushi does not manage provider API keys.
- Summary jobs are asynchronous. The UI intentionally has no progress display, completion notification, polling, automatic retry, or restart recovery.
