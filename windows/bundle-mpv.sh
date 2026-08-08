#!/usr/bin/env bash
# Stage the Windows player directory that ships inside TinyPlay-Setup-x64.exe.
#
# Both the public release workflow and the private test-build workflow call this
# script, so the two cannot drift apart on what a working player directory is.
#
#   windows/bundle-mpv.sh [dest-dir]      # default: dist/mpv
#
# It stages three things and then refuses to succeed unless the result can
# actually start on a clean Windows machine:
#
#   1. mpv.exe from zhongfly/mpv-winbuild (plus any sibling DLLs it ships).
#   2. vulkan-1.dll — the Khronos Vulkan loader. mpv.exe imports it directly,
#      and it is NOT part of Windows: it arrives with the GPU driver / Vulkan
#      runtime. On Windows LTSC with a stock or minimal display driver it is
#      simply absent, and the process then dies in the loader before main() —
#      the user sees "缺少 vulkan-1.dll", no player window, and an empty mpv log.
#      Shipping the loader makes the import resolve; with no Vulkan driver
#      present it just reports zero devices and mpv uses d3d11 as usual.
#   3. windows/check-mpv-deps.py, which fails the build if any bundled binary
#      still imports something Windows does not ship and we do not include.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
dest=${1:-dist/mpv}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

python_bin=$(command -v python3 || command -v python || true)
if [[ -z "$python_bin" ]]; then
  echo "python is required for the dependency audit" >&2
  exit 1
fi

# --- 1. mpv -----------------------------------------------------------------
api=$(curl -sL https://api.github.com/repos/zhongfly/mpv-winbuild/releases/latest)
# Pick the plain x86_64 build (not the -v3- or -dev- variants).
mpv_url=$(echo "$api" | grep -oE 'https://[^"]*mpv-x86_64-2[0-9]+[^"]*\.7z' | head -1)
if [[ -z "$mpv_url" ]]; then
  echo "could not find an mpv x86_64 archive in the latest release" >&2
  exit 1
fi
echo "mpv archive: $mpv_url"
curl -fL -o "$work/mpv.7z" "$mpv_url"
7z x "$work/mpv.7z" -o"$work/mpv_extract" >/dev/null
mpv_exe=$(find "$work/mpv_extract" -name mpv.exe | head -1)
if [[ -z "$mpv_exe" ]]; then
  echo "mpv.exe not found in the downloaded archive" >&2
  exit 1
fi
mpv_dir=$(dirname "$mpv_exe")
mkdir -p "$dest"
cp "$mpv_exe" "$dest/"
# These builds are mostly static, but copy any sibling DLLs just in case.
cp "$mpv_dir"/*.dll "$dest/" 2>/dev/null || true

# --- 2. Vulkan loader -------------------------------------------------------
# Pinned MSYS2 build of KhronosGroup/Vulkan-Loader (Apache-2.0). It is the same
# mingw toolchain the mpv build uses, and it imports nothing but Windows system
# DLLs, so it drops in with no runtime of its own.
vulkan_pkg=${VULKAN_LOADER_PKG:-mingw-w64-x86_64-vulkan-loader-1~1.4.357.0-1-any.pkg.tar.zst}
vulkan_url="https://repo.msys2.org/mingw/mingw64/${vulkan_pkg}"
echo "vulkan loader: $vulkan_url"
curl -fL -o "$work/vulkan.pkg.tar.zst" "$vulkan_url"
mkdir -p "$work/vulkan"
# Decompress the .zst with whatever the runner actually has: 7-Zip reads zstd
# only in recent versions, so keep two fallbacks rather than assuming.
if ! 7z x "$work/vulkan.pkg.tar.zst" -o"$work/vulkan" >/dev/null 2>&1; then
  if command -v zstd >/dev/null 2>&1; then
    zstd -d -q -o "$work/vulkan/vulkan.pkg.tar" "$work/vulkan.pkg.tar.zst"
  else
    "$python_bin" -m pip install --quiet --disable-pip-version-check zstandard
    "$python_bin" - "$work/vulkan.pkg.tar.zst" "$work/vulkan/vulkan.pkg.tar" <<'PY'
import sys, zstandard
with open(sys.argv[1], "rb") as src, open(sys.argv[2], "wb") as dst:
    zstandard.ZstdDecompressor().copy_stream(src, dst)
PY
  fi
fi
vulkan_tar=$(find "$work/vulkan" -name '*.tar' | head -1)
if [[ -z "$vulkan_tar" ]]; then
  echo "could not unpack the Vulkan loader package" >&2
  exit 1
fi
tar -xf "$vulkan_tar" -C "$work/vulkan"
vulkan_dll=$(find "$work/vulkan" -name vulkan-1.dll | head -1)
if [[ -z "$vulkan_dll" ]]; then
  echo "vulkan-1.dll not found in the Vulkan loader package" >&2
  exit 1
fi
cp "$vulkan_dll" "$dest/"
vulkan_license=$(find "$work/vulkan" -path '*licenses/vulkan-loader*' -type f | head -1)
if [[ -n "$vulkan_license" ]]; then
  cp "$vulkan_license" "$dest/VULKAN-LOADER-LICENSE.txt"
fi

# --- 3. Audit ---------------------------------------------------------------
"$python_bin" "$here/check-mpv-deps.py" "$dest"
