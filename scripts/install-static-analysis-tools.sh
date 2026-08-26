#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
go_version=$(awk '$1 == "go" { print $2; exit }' "$project_dir/go.mod")
if [ -z "$go_version" ]; then
    echo "go.modのGo versionを取得できません" >&2
    exit 1
fi
export GOTOOLCHAIN="go$go_version"

staticcheck_version=v0.7.0
gosec_version=2.28.0
tool_dir=${STATIC_ANALYSIS_BIN_DIR:-$(go env GOPATH)/bin}
mkdir -p "$tool_dir"

GOBIN="$tool_dir" go install "honnef.co/go/tools/cmd/staticcheck@$staticcheck_version"

os=$(go env GOOS)
arch=$(go env GOARCH)
case "$os/$arch" in
    darwin/amd64) gosec_sha256=ad23af3a6bfef8112a2da386acd61ede1374c8d022c06d8ef130ccf9748311d4 ;;
    darwin/arm64) gosec_sha256=6c4993a0ab5e3007d66c87cbcb4e3948f8000971f8eeaf3ac269cbc87a603ba4 ;;
    linux/amd64)  gosec_sha256=d7882e505b1ff345d458bf0e893eec8019bc849f861ad73a212869540dd505ff ;;
    linux/arm64)  gosec_sha256=63259681b6e4b9e7a24d4e187b485e75d3844d28d512b0c97dc831e51d374720 ;;
    *)
        echo "gosecの未対応platformです: $os/$arch" >&2
        exit 1
        ;;
esac

asset="gosec_${gosec_version}_${os}_${arch}.tar.gz"
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT INT TERM
curl -fsSL "https://github.com/securego/gosec/releases/download/v${gosec_version}/${asset}" -o "$tmp_dir/$asset"
if command -v sha256sum >/dev/null 2>&1; then
    actual_sha256=$(sha256sum "$tmp_dir/$asset" | awk '{ print $1 }')
else
    actual_sha256=$(shasum -a 256 "$tmp_dir/$asset" | awk '{ print $1 }')
fi
if [ "$actual_sha256" != "$gosec_sha256" ]; then
    echo "gosec archiveのSHA-256が一致しません" >&2
    exit 1
fi

tar -xzf "$tmp_dir/$asset" -C "$tmp_dir" gosec
install -m 0755 "$tmp_dir/gosec" "$tool_dir/gosec"

"$tool_dir/staticcheck" -version
"$tool_dir/gosec" -version
