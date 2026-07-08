# macOS 原生桌面壳

macOS 版本使用 AppKit、`NSStatusItem` 和 `WKWebView`，不依赖 Electron。
Swift 壳负责菜单栏和窗口，Go core 作为子进程提供本地服务，mpv 作为独立播放器。

## App 结构

```text
TinyPlay.app/
  Contents/
    MacOS/TVRemote
    Resources/
      tvremote-core
      THIRD_PARTY_NOTICES.md
      mpv/bin/mpv
      mpv/bin/libs/*.dylib
```

## 普通构建

```sh
cd desktop-go
make build-app-mac
```

`build-app.sh` 只生成适合本机调试的临时签名 App。

## 本地正式发布

正式发布优先使用本机钥匙串中的 Developer ID。App Store Connect Team API
私钥应放在所有 Git 仓库之外，并设置为 `600` 权限。

```sh
VERSION=0.9.0 \
SIGN_IDENTITY="Developer ID Application: … (TEAMID)" \
AC_API_KEY_PATH="/安全目录/AuthKey_XXXX.p8" \
AC_KEY_ID=XXXX \
AC_ISSUER_ID=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx \
./macos/release-local.sh
```

`release-local.sh` 会完成：

1. 编译 Go core 和 Swift 壳；
2. 收集并重写 mpv 动态库；
3. 在系统临时目录逐层完成 Developer ID 签名；
4. 使用 `notarytool` 提交 Apple 公证；
5. staple 公证票据并用 Gatekeeper 验证；
6. 生成最终 `tvremote-macos.zip`。

项目位于 macOS File Provider 管理的 Documents 目录时，构建物可能带有受保护的
`com.apple.provenance` 属性。因此正式签名必须在临时目录完成，脚本已经自动处理。
