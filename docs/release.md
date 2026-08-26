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
is available locally through `npm run test:e2e:firefox`. Migration of the bookmark 404 scenario to
Playwright Test remains a required TODO before the next release. Browser E2E, race tests, and other
host-dependent checks do not run in the portable Forgejo workflows.

### Host Firefox user acceptance

User approval from Firefox running on the host is a required pre-tag gate. Use a disposable binary
and database in the development VM; do not expose the check server on the VM network or reuse
production data.

The application source being approved must already be committed. From the repository root in the VM,
run the following commands in Bash with a clean working tree. Choose a temporary password only for
this check:

```bash
if test -n "$(git status --porcelain)"; then
  echo 'Working tree must be clean before user acceptance.' >&2
  exit 1
fi
CHECK_COMMIT=$(git rev-parse HEAD) || exit 1
CHECK_DIR=$(mktemp -d) || exit 1
if ! CGO_ENABLED=0 go build -trimpath -o "$CHECK_DIR/shirushi" .; then
  rm -rf "$CHECK_DIR"
  unset CHECK_DIR CHECK_COMMIT
  exit 1
fi
if ! read -rsp 'Temporary Shirushi password: ' SHIRUSHI_CHECK_PASSWORD; then
  rm -rf "$CHECK_DIR"
  unset CHECK_DIR CHECK_COMMIT
  exit 1
fi
echo
printf 'Candidate commit: %s\n' "$CHECK_COMMIT"
(
  cd -- "$CHECK_DIR" || exit 1
  SHIRUSHI_ADDR=127.0.0.1:18180 \
    SHIRUSHI_PASSWORD="$SHIRUSHI_CHECK_PASSWORD" \
    "$CHECK_DIR/shirushi"
)
```

Keep that process running. On the host, bind the same host-local port to the VM-local listener. Replace
the placeholder with the normal SSH destination for the development VM:

```sh
ssh -o ExitOnForwardFailure=yes -N \
  -L 127.0.0.1:18180:127.0.0.1:18180 \
  <vm-ssh-destination>
```

Open `http://127.0.0.1:18180` in host Firefox and verify:

1. Login, logout, and the remember-me checkbox work.
2. The desktop layout, search, page-size select, and pagination have no clipping or horizontal overflow.
3. Bookmark add, edit, delete, tag suggestions, and tag management work.
4. Selection, bulk tag add/remove, bulk delete, import, and export work.
5. Hover, disabled, and keyboard focus-visible states remain clear.
6. Firefox DevTools shows no unexpected console error or failed CSS request; only `style.css` is loaded
   as an application stylesheet.

The user must explicitly approve the candidate after this check. Record `CHECK_COMMIT`, the proposed
tag, host Firefox version, result, and user approval in `docs/release-checkpoints.md` before tagging.
The checkpoint-only documentation commit may be added after approval. If application source,
configuration, embedded static files, or the release workflow changes after the check, repeat this
user acceptance against the new commit before tagging.

After stopping Shirushi with Ctrl-C, remove the disposable data and clear the temporary password:

```sh
rm -rf "$CHECK_DIR"
unset SHIRUSHI_CHECK_PASSWORD CHECK_DIR CHECK_COMMIT
```

Stop the host SSH command with Ctrl-C as well.

## Publish

After all selected gates pass and their evidence is recorded:

1. Commit and push the release changes to `main`.
2. Create and push the approved version tag.
3. Observe the Forgejo Actions run until completion.
4. Confirm the public Release contains four binaries and `SHA256SUMS`.
