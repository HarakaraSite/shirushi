#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
go_version=$(awk '$1 == "go" { print $2; exit }' "$project_dir/go.mod")
if [ -z "$go_version" ]; then
    echo "go.modのGo versionを取得できません" >&2
    exit 1
fi
export GOTOOLCHAIN="go$go_version"
tool_dir=${STATIC_ANALYSIS_BIN_DIR:-$(go env GOPATH)/bin}

for tool in staticcheck gosec; do
    if [ ! -x "$tool_dir/$tool" ]; then
        echo "$tool_dir/$tool がありません。scripts/install-static-analysis-tools.shを先に実行してください" >&2
        exit 1
    fi
done

cd "$project_dir"
"$tool_dir/staticcheck" ./...
"$tool_dir/gosec" \
    -exclude G104 \
    -nosec-require-justification \
    -nosec-require-rules \
    ./...
