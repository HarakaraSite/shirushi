# Changelog

日本語版: [CHANGELOG.ja.md](CHANGELOG.ja.md)

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
