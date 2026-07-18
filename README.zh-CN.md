# TinyPlay

> 把闲置的 Windows 小主机、笔记本或 Mac mini 变成客厅影音播放器，用手机轻松遥控。

[English](README.md) · **[查看 TinyPlay 静态介绍页](https://yanghanqing.github.io/tinyplay/)**

TinyPlay 专为闲置的 Windows 小主机、笔记本和 Mac mini 设计。将电脑连接电视，即可变成一台由手机遥控、以 mpv 为播放内核的客厅影音播放器。

这类硬件的性能通常远超电视盒子，但用键盘鼠标操作电视不够自然，传统遥控器也不适合文字搜索和精确拖动进度条。

TinyPlay 的思路是：**让硬件负责播放，让手机负责交互。**

对国内用户而言，桌面系统还有两个实际优势：

* 部分流媒体平台可直接通过浏览器观看，无需额外购买价格更高的电视端会员。
* 主流网盘的桌面客户端在下载和播放时，限速策略通常比电视端宽松。

常见硬件已经具备足够的视频解码能力：

* 英特尔（Intel）第 8 代核显即可硬解常见的 4K HEVC、VP9 10-bit 视频，第 11 代起支持 AV1。
* Apple M 系列芯片均支持 4K HEVC 10-bit 硬解，M3 起支持 AV1。
* 相比电视内置播放器，专用播放器在 HDR、杜比视界（Dolby Vision）和动态范围匹配方面通常更省心，但实际效果仍取决于片源、系统设置和显示设备。

## 功能亮点

* **手机浏览器遥控** — 扫码即用，无需安装 App
* **媒体服务器** — 连接 Emby、Jellyfin 或 Plex，支持海报墙、选集、搜索和续播
* **文件浏览** — 浏览 SMB、WebDAV 共享文件夹或本地目录，找到文件即可播放
* **直播源** — 接入 IPTV 频道列表，支持收藏和最近观看
* **DLNA 投屏** — 局域网内的其他应用可直接投屏，手机遥控器仍可暂停、继续和跳转
* **多服务器** — 同时挂载多个媒体源，随时切换
* **跨平台** — 支持 Windows 和 macOS，包括 Apple Silicon 与 Intel
* **mpv 内核** — 支持 MKV、HDR、杜比视界（Dolby Vision）、TrueHD 和 PGS 字幕等常见影音格式

## 下载

前往 [GitHub Releases](../../releases/latest) 下载最新版本。

* **Windows x86-64** — 解压完整压缩包后运行 `TinyPlay.exe`。Windows 版本目前尚未签名，系统可能显示 SmartScreen 警告。
* **macOS** — 同时提供 Apple Silicon（`TinyPlay-macos-arm64.dmg`）和 Intel（`TinyPlay-macos-intel.dmg`）版本。打开 DMG，将 TinyPlay 拖入“应用程序”文件夹。

手机与运行 TinyPlay 的电脑需要连接到同一个局域网。

## 使用指南

产品截图、功能说明、使用流程和完整的客厅播放器选购指南，请查看：

**[TinyPlay 静态介绍页](https://yanghanqing.github.io/tinyplay/)**

## 开源协议

TinyPlay 基于 [GPL-3.0 协议](LICENSE) 开源。捆绑的第三方组件遵循各自的许可证，详见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
