#!/usr/bin/env python3
"""Audit the bundled Windows player directory for non-system DLL dependencies.

Why this exists: mpv.exe from the upstream Windows builds is *almost* static,
but it still carries a plain (non-delay-loaded) import of `vulkan-1.dll`, which
is **not** part of Windows — it ships with the GPU driver / Vulkan runtime. On a
machine without it (Windows LTSC with a stock or minimal display driver is the
common case) the loader refuses to start the process at all: no window, no
playback, and an empty mpv log, because nothing in mpv ever runs.

Bundling the missing DLL fixes today's build; this audit is what keeps it fixed.
Every import of every bundled binary must be either a DLL that Windows itself
ships, an API-set stub, or a file we bundle alongside it. Anything else fails
the packaging job instead of shipping an app that cannot launch its player.

Usage: check-mpv-deps.py <dir>
"""

import os
import struct
import sys

# DLLs that ship with Windows 10/11 itself (including LTSC). Being on this list
# means "the OS provides it", not merely "it exists on the build machine" — the
# build runner has a full graphics stack that a user's LTSC box may not.
SYSTEM_DLLS = {
    "advapi32.dll", "audioses.dll", "avicap32.dll", "avrt.dll", "bcrypt.dll",
    "bcryptprimitives.dll", "cabinet.dll", "cfgmgr32.dll", "combase.dll",
    "comctl32.dll", "comdlg32.dll", "credui.dll", "crypt32.dll", "d2d1.dll",
    "d3d9.dll", "d3d11.dll", "d3d12.dll", "d3dcompiler_47.dll", "dbghelp.dll",
    "dnsapi.dll", "dsound.dll", "dwmapi.dll", "dwrite.dll", "dxgi.dll",
    "dxva2.dll", "evr.dll", "gdi32.dll", "gdi32full.dll", "gdiplus.dll",
    "glu32.dll", "hid.dll", "imagehlp.dll", "imm32.dll", "iphlpapi.dll",
    "kernel32.dll", "kernelbase.dll", "ksuser.dll", "mf.dll", "mfplat.dll",
    "mfreadwrite.dll", "mmdevapi.dll", "mpr.dll", "msacm32.dll", "mswsock.dll",
    "msvcrt.dll", "msvcp_win.dll", "ncrypt.dll", "netapi32.dll",
    "normaliz.dll", "ntdll.dll", "ole32.dll", "oleacc.dll", "oleaut32.dll",
    "opengl32.dll", "pdh.dll", "powrprof.dll", "propsys.dll", "psapi.dll",
    "rpcrt4.dll", "secur32.dll", "setupapi.dll", "shcore.dll", "shell32.dll",
    "shlwapi.dll", "sspicli.dll", "user32.dll", "userenv.dll", "ucrtbase.dll",
    "urlmon.dll", "usp10.dll", "uxtheme.dll", "version.dll", "wininet.dll",
    "winhttp.dll", "winmm.dll", "winspool.drv", "wintrust.dll", "wldap32.dll",
    "ws2_32.dll", "wtsapi32.dll", "xinput1_4.dll",
}

# API-set contract stubs; resolved by the OS loader, never shipped as files.
SYSTEM_PREFIXES = ("api-ms-win-", "ext-ms-")

BINARY_SUFFIXES = (".exe", ".dll")


def _sections(data, pe):
    count = struct.unpack_from("<H", data, pe + 6)[0]
    opt_size = struct.unpack_from("<H", data, pe + 20)[0]
    base = pe + 24 + opt_size
    out = []
    for i in range(count):
        off = base + 40 * i
        va, size, raw = struct.unpack_from("<III", data, off + 12)[:3]
        out.append((va, size, raw))
    return out


def imported_dlls(path):
    """Return the plain + delay-load imports of a PE file, lowercased."""
    with open(path, "rb") as fh:
        data = fh.read()
    if data[:2] != b"MZ":
        return []
    pe = struct.unpack_from("<I", data, 0x3C)[0]
    if data[pe:pe + 4] != b"PE\0\0":
        return []
    opt = pe + 24
    magic = struct.unpack_from("<H", data, opt)[0]
    dirs = opt + (112 if magic == 0x20B else 96)
    secs = _sections(data, pe)

    def to_off(rva):
        for va, size, raw in secs:
            if va <= rva < va + max(size, 1):
                return raw + (rva - va)
        return None

    def cstr(off):
        end = data.index(b"\0", off)
        return data[off:end].decode("ascii", "replace")

    names = []
    # (directory index, descriptor size, offset of the name field)
    for index, step, name_field in ((1, 20, 12), (13, 32, 4)):
        rva = struct.unpack_from("<I", data, dirs + 8 * index)[0]
        if not rva:
            continue
        off = to_off(rva)
        if off is None:
            continue
        while data[off:off + step] != b"\0" * step:
            name_rva = struct.unpack_from("<I", data, off + name_field)[0]
            if not name_rva:
                break
            name_off = to_off(name_rva)
            if name_off is None:
                break
            names.append(cstr(name_off).lower())
            off += step
    return names


def main():
    if len(sys.argv) != 2:
        print("usage: check-mpv-deps.py <dir>", file=sys.stderr)
        return 2
    root = sys.argv[1]
    binaries = sorted(
        name for name in os.listdir(root)
        if name.lower().endswith(BINARY_SUFFIXES)
    )
    if not any(name.lower() == "mpv.exe" for name in binaries):
        print("ERROR: mpv.exe is not in " + root, file=sys.stderr)
        return 1
    bundled = {name.lower() for name in binaries}

    missing = {}
    for name in binaries:
        for dep in imported_dlls(os.path.join(root, name)):
            if dep in bundled or dep in SYSTEM_DLLS:
                continue
            if dep.startswith(SYSTEM_PREFIXES):
                continue
            missing.setdefault(dep, []).append(name)

    for name in binaries:
        deps = imported_dlls(os.path.join(root, name))
        print("%s -> %d imports" % (name, len(deps)))
    if missing:
        print("", file=sys.stderr)
        print("ERROR: bundled player depends on DLLs that Windows does not "
              "ship and this package does not include:", file=sys.stderr)
        for dep, users in sorted(missing.items()):
            print("  %s (imported by %s)" % (dep, ", ".join(users)),
                  file=sys.stderr)
        print("", file=sys.stderr)
        print("Ship the DLL next to mpv.exe, or add it to SYSTEM_DLLS if it is "
              "genuinely part of Windows. Do NOT ignore this: a missing import "
              "stops mpv from starting at all, with an empty log.",
              file=sys.stderr)
        return 1
    print("OK: every dependency is either a Windows system DLL or bundled.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
