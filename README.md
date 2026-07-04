# TinyPlay

> A phone-controlled mpv remote for Windows & macOS.

[中文](#中文) | [English](#english)

## 中文

TinyPlay 是一个轻量桌面应用，让你使用同一局域网中的手机浏览器浏览媒体库，
并遥控电脑上的 mpv 播放器。

### 平台

- Windows x86-64：系统托盘应用，使用系统 WebView2。
- macOS：**仅支持 Apple Silicon（M 系列）**，原生 AppKit 菜单栏应用；**不支持 Intel Mac**。

### 功能

- 浏览媒体库并发送影片到电脑播放；
- 播放、暂停、跳转、音量和倍速控制；
- 音轨、字幕轨、字幕延迟和画面比例控制；
- 桌面端显示二维码，手机无需安装 App；
- 发布包自带 mpv。

### 开发

需要 Go 1.22 或更高版本：

```sh
cd desktop-go
make sync
go test ./...
make run
```

### 安全

本服务面向可信家庭局域网，不应将 HTTP 端口直接暴露到互联网。请勿提交媒体服务器
凭据、配置文件、签名证书或 Apple 公证密钥。

> 当前仍处于 1.0 之前的公开发布准备阶段，源码许可证和完整的二进制第三方许可清单
> 尚未最终确定。

---

## English

TinyPlay is a lightweight desktop application that lets a phone browser
on the same local network browse a media library and control mpv playback on
the computer.

### Platforms

- Windows x86-64: system tray application using the system WebView2 runtime.
- macOS: **Apple Silicon (M-series) only** — native AppKit menu-bar app;
  **Intel Macs are not supported**.

### Features

- Browse a media library and send videos to the desktop player.
- Control playback, seeking, volume, and speed.
- Select audio and subtitle tracks and adjust subtitle delay and aspect ratio.
- Connect from a phone by scanning a QR code; no mobile app is required.
- Release packages bundle mpv.

### Development

Go 1.22 or newer is required:

```sh
cd desktop-go
make sync
go test ./...
make run
```

### Security

The service is intended for trusted home networks. Do not expose its HTTP port
directly to the public internet. Never commit media-server credentials,
configuration files, signing certificates, or Apple notarization keys.

> This project is still preparing for its pre-1.0 public release. The source
> license and complete binary third-party license manifest are not final yet.
