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

# G101: untaggedTokenはcredentialではなくquery sentinelです。
# G124: session CookieはHttpOnly/SameSiteを固定し、Secureだけをdeployment設定にしています。
# G120: import request全体はParseMultipartForm直前のMaxBytesReaderで制限しています。
# G204: Henji pathは信頼する起動設定で、CommandContextはshellを起動しません。
gosec_exclude_rules='bookmark\.go$:G101;auth\.go$:G124;importexport\.go$:G120;henji_runner\.go$:G204'
"$tool_dir/gosec" \
    -exclude G104 \
    -exclude-rules "$gosec_exclude_rules" \
    ./...
