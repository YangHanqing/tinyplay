# TinyPlay

> 把你已经在用的小主机变成客厅播放器，用手机轻松遥控。

[English](README.md) · **[查看 TinyPlay 静态介绍页](https://yanghanqing.github.io/tinyplay/)**

TinyPlay 是为家里那台已经在跑下载任务、Docker 容器或家庭服务的 N100、NUC、
迷你主机或 Mac 准备的。把经常闲着的 HDMI 接口连到电视，它就能
再兼任一台由手机遥控、以 mpv 为播放内核的客厅播放器。

桌面端支持连接 Emby、Jellyfin、Plex，也可以直接浏览 SMB/WebDAV 共享；它会驱动
随应用捆绑的 mpv，并在家庭局域网内提供适合手机使用的媒体库和遥控页面。手机
无需安装 App。

## 下载

前往 [GitHub Releases](../../releases/latest) 下载最新版本。

- **Windows x86-64：**解压完整压缩包后运行 `TinyPlay.exe`。Windows 版本目前
  尚未签名，系统可能显示 SmartScreen 警告。
- **macOS：**同时提供 Apple Silicon（`TinyPlay-macos-arm64.dmg`）和 Intel
  （`TinyPlay-macos-intel.dmg`）两个版本。打开 DMG，把 TinyPlay 拖入“应用程序”。

手机和电脑需要连接同一个可信局域网。TinyPlay 的遥控页面没有独立身份验证，
请勿将其端口暴露到公网。

产品截图、功能说明、使用流程和完整的客厅播放器选购指南，请查看
**[TinyPlay 静态介绍页](https://yanghanqing.github.io/tinyplay/)**。

## 开源协议

TinyPlay 自身源码采用 [MIT 协议](LICENSE) 开源。捆绑的第三方组件遵循各自的
许可证，详见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
