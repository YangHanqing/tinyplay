# TinyPlay

> 把闲置的 Windows 小主机、笔记本或 Mac mini 变成轻量级客厅影音播放器，用手机轻松遥控。

[English](README.md) · **[查看 TinyPlay 静态介绍页](https://yanghanqing.github.io/tinyplay/)**

TinyPlay 是为闲置 Windows 小主机、笔记本和 Mac mini 打造的轻量级客厅影音播放器。把电脑接上电视，它就变成一台由手机遥控、以 mpv 为播放内核的播放终端。

这类硬件的性能通常远超电视盒子，但用键盘鼠标操作电视不够自然，传统遥控器也不适合文字搜索和精确拖动进度条。

TinyPlay 的思路是：**让硬件负责播放，让手机负责交互。**

<p align="center">
  <img src="docs/public/hero.jpg" alt="手机控制连接电视的电脑播放内容" width="760">
</p>

## 功能亮点

* **手机浏览器遥控** — 扫码即用，无需安装 App
* **网页浏览器** — 无需键盘鼠标，也能在电视上浏览哔哩哔哩、爱奇艺、优酷、腾讯视频和抖音等常见流媒体网站
* **媒体服务器** — 连接 Emby、Jellyfin 或 Plex，支持海报墙、选集、搜索和续播
* **文件浏览** — 浏览 SMB、WebDAV 共享文件夹或本地目录，找到文件即可播放
* **直播源** — 接入 IPTV 频道列表，支持收藏和最近观看
* **DLNA 投屏** — 局域网内的其他应用可直接投屏，手机遥控器仍可暂停、继续和跳转
* **多服务器** — 同时挂载多个媒体源，随时切换
* **跨平台** — 支持 Windows 和 macOS，包括 Apple Silicon 与 Intel；另有 [Apple TV 版](https://apps.apple.com/app/tinyplay-video-remote/id6788041703)
* **mpv 内核** — 支持 MKV、HDR、杜比视界（Dolby Vision）、TrueHD 和 PGS 字幕等常见影音格式

## 界面一览

<p align="center">
  <img src="docs/public/screenshot-library.png" alt="TinyPlay 手机媒体库界面" height="320">
  <img src="docs/public/screenshot-online-video.png" alt="手机控制电脑端网页视频" height="320">
</p>

## 下载

前往 [GitHub Releases](../../releases/latest) 下载最新版本。

* **Windows x86-64** — 运行 `TinyPlay-Setup-x64.exe`。安装器会创建开始菜单和桌面快捷方式，并在完成后启动 TinyPlay。以后安装新版本时，它会识别已有安装、自动关闭 TinyPlay 并原位升级，保留你的设置。Windows 版本目前尚未签名，系统可能显示 SmartScreen 警告。
* **macOS** — 同时提供 Apple Silicon（`TinyPlay-macos-arm64.dmg`）和 Intel（`TinyPlay-macos-intel.dmg`）版本。打开 DMG，将 TinyPlay 拖入“应用程序”文件夹。

手机与运行 TinyPlay 的电脑需要连接到同一个局域网。

## 使用指南

产品截图、功能说明、使用流程和完整的客厅播放器选购指南，请查看：

**[TinyPlay 静态介绍页](https://yanghanqing.github.io/tinyplay/)**

## TinyPlay 与 Kodi

Kodi 适合想把电视深度定制成全能媒体中心的人；TinyPlay 更适合想少配置、用手机遥控、快速播放视频的人。

**TinyPlay 更适合你，如果你：**

* 💡 只想快速搭起来，不想配置太多
* 📱 习惯用手机控制，也不想额外安装 App
* 🎬 主要用于播放视频，不需要复杂的媒体库分类
* 🔧 看重轻量级和开箱即用

一句话：**Kodi = 全能媒体中心；TinyPlay = 轻量级电脑电视盒子 + 手机遥控器。**

## 开源协议

TinyPlay 基于 [GPL-3.0 协议](LICENSE) 开源。捆绑的第三方组件遵循各自的许可证，详见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
