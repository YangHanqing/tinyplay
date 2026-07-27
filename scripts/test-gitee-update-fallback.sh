#!/usr/bin/env bash
# Verifies that Gitee can serve as TinyPlay's unauthenticated update-check
# fallback. It intentionally does not require a token and does not download
# release assets.
set -euo pipefail

api="${GITEE_RELEASE_API:-https://gitee.com/api/v5/repos/hanqingyang/tinyplay/releases/latest}"
page_base="${GITEE_RELEASE_PAGE_BASE:-https://gitee.com/hanqingyang/tinyplay/releases/tag}"
response="$(mktemp)"
trap 'rm -f "$response"' EXIT

http_code="$(curl --silent --show-error --output "$response" --write-out '%{http_code}' \
  --connect-timeout 5 --max-time 15 --retry 2 --retry-all-errors --retry-delay 1 \
  --header 'Accept: application/json' \
  --header 'User-Agent: TinyPlay update-check probe' \
  "$api")"

if [[ "$http_code" != 2* ]]; then
  echo "Gitee latest-release API returned HTTP $http_code" >&2
  cat "$response" >&2
  exit 1
fi

tag="$(jq -r '.tag_name // empty' "$response")"
prerelease="$(jq -r '.prerelease // false' "$response")"
if [[ ! "$tag" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Gitee returned an invalid stable tag: $tag" >&2
  exit 1
fi
if [ "$prerelease" != false ]; then
  echo "Gitee latest release is marked prerelease" >&2
  exit 1
fi

page_code="$(curl --silent --show-error --location --output /dev/null --write-out '%{http_code}' \
  --connect-timeout 5 --max-time 15 --retry 2 --retry-all-errors --retry-delay 1 \
  --header 'User-Agent: TinyPlay update-check probe' \
  "$page_base/$tag")"
if [[ "$page_code" != 2* ]]; then
  echo "Gitee release page for $tag returned HTTP $page_code" >&2
  exit 1
fi

echo "Gitee update fallback is ready: $tag ($page_base/$tag)"
