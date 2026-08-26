# Release procedure

Shirushi releases are created by pushing a `v*` tag. The confirmed machine-readable contract is
`.forgejo/release-profile.yml`; `.forgejo/workflows/release.yml` is rendered from that profile.

## Portable workflow gates

The Forgejo Actions release job runs these checks before uploading any asset:

1. `CGO_ENABLED=0 go test ./...`
2. `go vet ./...`
3. CGO-free builds for Linux and macOS on amd64 and arm64
4. `SHA256SUMS` generation for all four binaries

Failure output remains in the native Forgejo Actions job log. A failed gate must not create a
Release or upload diagnostics as release assets.

## Pre-tag reference check

### 404-check browser E2E

Run this check in the development environment using the repository-local Playwright Test setup.
Firefox is the primary browser, and Chromium is also available for cross-browser confirmation. The
managed browser binaries are shared through the user-level Playwright cache. Bind both the
application and its 404 fixture to `127.0.0.1`.

Verify the following flow with a temporary database and browser session:

1. Log in and display a bookmark whose URL returns HTTP 404.
2. Click **404チェック** and accept the confirmation dialog.
3. Confirm that the button displays progress and is disabled while the background job runs.
4. Confirm the completion message and that the card image becomes `/404.svg`.
5. Confirm through the bookmark API that `modified_at` is non-null.
6. Confirm there are no browser console errors, then close the temporary session.

Record release-specific results in `docs/release-checkpoints.md`. The login visual-regression test
is available through `npm run test:e2e:firefox` and runs in the branch and pull-request test
workflow. Migration of the bookmark 404 scenario to Playwright Test remains a required TODO before
the next release. Browser E2E, race tests, and other host-dependent checks do not run in the
portable Forgejo release workflow.

## Publish

After all selected gates pass and their evidence is recorded:

1. Commit and push the release changes to `main`.
2. Create and push the approved version tag.
3. Observe the Forgejo Actions run until completion.
4. Confirm the public Release contains four binaries and `SHA256SUMS`.
