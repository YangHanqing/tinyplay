// Native macOS menu-bar shell for TinyPlay.
//
// This is a real AppKit app (NSStatusItem + WKWebView) — no Electron, no
// webview wrapper framework. It is intentionally a single source file compiled
// with `swiftc` (see ../build-app.sh) instead of an .xcodeproj, so the CI build
// is reproducible and reviewable.
//
// Responsibilities (mirrors macos/README.md):
//   1. Launch the bundled Go core (Contents/Resources/tvremote-core) as a
//      sidecar, telling it where the bundled mpv lives via TVREMOTE_MPV_EXE and
//      asking it to write its LAN URL to a temp file via TVREMOTE_URL_FILE.
//   2. Show a menu-bar item with open-main, language, feedback, and quit.
//   3. The remote item opens a small native window with the intro + QR page served by
//      the core at /desktop.
//   4. Terminate the sidecar on quit.

import AppKit
import ApplicationServices
import Foundation
import ServiceManagement
import WebKit
import Network

private let feedbackMailAddress = "tinyday.app@gmail.com"

/// TinyPlay is an accessory (LSUIElement) app by default — tray/menu-bar only,
/// no Dock icon — but a real on-screen window with no Dock icon or Cmd-Tab
/// entry is disorienting and behaves unlike every other Mac app. Promote to
/// `.regular` for as long as any of TinyPlay's windows (the intro/QR window,
/// the website playback window) is open, and demote back to `.accessory` the
/// moment the last one closes, matching Windows (whose WebView2 window is a
/// normal top-level window and already gets a taskbar entry while open).
private enum DockVisibility {
	private static var openWindows = Set<ObjectIdentifier>()

	static func windowOpened(_ window: NSWindow) {
		openWindows.insert(ObjectIdentifier(window))
		NSApp.setActivationPolicy(.regular)
	}

	static func windowClosed(_ window: NSWindow) {
		openWindows.remove(ObjectIdentifier(window))
		if openWindows.isEmpty {
			NSApp.setActivationPolicy(.accessory)
		}
	}
}

// Menu-bar / alert copy for the native shell. Keys used by the web /desktop
// page still go through the Go core's i18n; this table only covers AppKit UI.
// Keep zh-CN + English here (menu preference "auto" follows the system locale).
private func L(_ key: String) -> String {
	let preference = UserDefaults.standard.string(forKey: "TinyPlayLanguage") ?? "auto"
	let zh = preference == "zh-CN" || (preference == "auto" && Locale.current.languageCode?.lowercased().hasPrefix("zh") == true)
	let table: [String: (String, String)] = [
        "open_main": ("\u{6253}\u{5F00}\u{4E3B}\u{754C}\u{9762}", "Open Main Interface"),
        "open_logs": ("\u{6253}\u{5F00}\u{65E5}\u{5FD7}\u{76EE}\u{5F55}", "Open Logs"),
		"settings": ("\u{8BBE}\u{7F6E}", "Settings"),
		"dlna_receiver": ("DLNA \u{63A5}\u{6536}\u{5668}", "DLNA Receiver"),
		"feedback": ("\u{53CD}\u{9988}", "Send Feedback"),
		"feedback_mail_subject": ("TinyPlay \u{7528}\u{6237}\u{53CD}\u{9988}", "TinyPlay Feedback"),
		"feedback_mail_body": (
			"\u{8BF7}\u{5199}\u{660E}\u{4F60}\u{7684}\u{53CD}\u{9988}\u{5185}\u{5BB9}\u{3002}\n\n\u{2022} \u{529F}\u{80FD}\u{9700}\u{6C42}\u{FF1A}\u{76F4}\u{63A5}\u{63CF}\u{8FF0}\u{5E0C}\u{671B}\u{589E}\u{52A0}\u{7684}\u{529F}\u{80FD}\u{5373}\u{53EF}\u{3002}\n\u{2022} \u{95EE}\u{9898}\u{53CD}\u{9988}\u{FF08}Bug\u{FF09}\u{FF1A}\u{8BF7}\u{63CF}\u{8FF0}\u{73B0}\u{8C61}\u{4E0E}\u{590D}\u{73B0}\u{6B65}\u{9AA4}\u{FF0C}\u{5E76}\u{9644}\u{4E0A}\u{65E5}\u{5FD7}\u{76EE}\u{5F55}\u{4E2D}\u{7684}\u{65E5}\u{5FD7}\u{6587}\u{4EF6}\u{FF08}\u{53EF}\u{5728}\u{4E3B}\u{754C}\u{9762}\u{5E95}\u{90E8}\u{70B9}\u{51FB}\u{300C}\u{6253}\u{5F00}\u{65E5}\u{5FD7}\u{76EE}\u{5F55}\u{300D}\u{83B7}\u{53D6}\u{FF09}\u{3002}\n\n",
			"Please describe your feedback.\n\n• Feature request: write what you would like to add.\n• Bug report: describe what happened and the steps to reproduce it, and attach the log files from the logs folder (use “Open Logs” at the bottom of the main window).\n\n"
		),
		"advanced_settings": ("高级设置", "Advanced Settings"),
		"mpv_menu": ("自定义 MPV 播放器", "Custom MPV Player"),
		"mpv_custom_path_menu": ("选择自定义路径…", "Choose Custom Path…"),
		"mpv_use_bundled_menu": ("使用内置 MPV 播放器", "Use Bundled MPV Player"),
		"mpv_dialog_title": ("选择 mpv 可执行文件", "Choose an mpv Executable"),
		"mpv_invalid_title": ("无法使用该文件", "Couldn't Use That File"),
		"mpv_invalid_body": ("所选文件不是有效的 mpv 播放器：\n\n%@", "The selected file isn't a working mpv player:\n\n%@"),
		"mpv_custom_active_tip": ("当前使用自定义 mpv：%@", "Using custom mpv: %@"),
		"mpv_default_tip": ("当前使用内嵌 mpv 播放器", "Using the bundled mpv player"),
		"mpv_custom_stale_tip": ("自定义路径已失效，正在使用内嵌 mpv", "Custom path is no longer valid; using the bundled mpv"),
		"webcache_menu": ("网页缓存", "Web Cache"),
		"webcache_menu_tip": ("内置浏览器的缓存占用与上限", "Built-in browser cache usage and limit"),
		"webcache_clear": ("清除缓存", "Clear Cache"),
		"webcache_clear_tip": ("清空内置浏览器的缓存文件，不会退出网站登录", "Delete the built-in browser's cache files; site logins are kept"),
		"webcache_clear_done": ("网页缓存已清除", "Web Cache Cleared"),
		"webcache_busy_title": ("请先关闭网页窗口", "Close the Web Window First"),
		"webcache_busy_body": ("正在浏览网页时无法清除缓存。关闭网页窗口后再试。", "The cache can't be cleared while a web page is open. Close the web window and try again."),
		"webcache_limit_menu": ("缓存上限", "Cache Limit"),
		"webcache_limit_unlimited": ("不限制", "Unlimited"),
		"autostart_menu": ("开机启动", "Start at Login"),
		"autostart_menu_tip": ("登录系统后自动在后台运行", "Run in the background after you sign in"),
		"autostart_on_menu": ("开启", "On"),
		"autostart_off_menu": ("关闭", "Off"),
		"autostart_failed_title": ("无法修改开机自启动", "Couldn't Change Startup Setting"),
		"autostart_failed_body": ("开机自启动设置未能保存：\n\n%@", "The startup setting couldn't be saved:\n\n%@"),
		"autostart_approval_title": ("请在系统设置中允许", "Approval Needed"),
		"autostart_approval_body": (
			"macOS 需要你确认后才能让 TinyPlay 开机启动。请在“系统设置 → 通用 → 登录项”中打开 TinyPlay。",
			"macOS needs your approval before TinyPlay can start at login. Turn TinyPlay on in System Settings → General → Login Items."
		),
		"open_login_items": ("打开登录项设置", "Open Login Items"),
		"cancel": ("取消", "Cancel"),
		"quit": ("\u{9000}\u{51FA}", "Quit"),
		"language": ("\u{8BED}\u{8A00}", "Language"),
		"automatic": ("\u{81EA}\u{52A8}", "Automatic"),
		"chinese": ("\u{4E2D}\u{6587}", "Chinese"),
		"about": ("\u{5173}\u{4E8E} TinyPlay", "About TinyPlay"),
		"version_label": ("\u{7248}\u{672C}", "Version"),
		"third_party_notices": ("\u{67E5}\u{770B}\u{7B2C}\u{4E09}\u{65B9}\u{58F0}\u{660E}", "View Third-Party Notices"),
		"check_updates": ("\u{68C0}\u{67E5}\u{66F4}\u{65B0}", "Check for Updates"),
		"update_available_title": ("\u{6709}\u{65B0}\u{7248}\u{672C}\u{53EF}\u{7528}", "Update Available"),
		"update_available_body": ("TinyPlay %@ \u{5DF2}\u{53D1}\u{5E03}\u{3002}\n\u{5F53}\u{524D}\u{7248}\u{672C}\u{FF1A}%@", "TinyPlay %@ is available.\nCurrent version: %@"),
		"update_download": ("\u{6253}\u{5F00}\u{4E0B}\u{8F7D}\u{9875}\u{9762}", "Open Download Page"),
		"update_remind": ("3 \u{5929}\u{540E}\u{63D0}\u{9192}", "Remind Me in 3 Days"),
		"update_skip": ("\u{8DF3}\u{8FC7}\u{6B64}\u{7248}\u{672C}", "Skip This Version"),
		"update_latest": ("\u{4F60}\u{6B63}\u{5728}\u{4F7F}\u{7528}\u{6700}\u{65B0}\u{7248}\u{672C}\u{3002}", "You're using the latest version."),
		"update_failed": ("\u{6682}\u{65F6}\u{65E0}\u{6CD5}\u{68C0}\u{67E5}\u{66F4}\u{65B0}\u{FF0C}\u{8BF7}\u{7A0D}\u{540E}\u{518D}\u{8BD5}\u{3002}", "Couldn't check for updates. Please try again later."),
		"ok": ("\u{597D}\u{7684}", "OK"),
		"feedback_no_mail_client": (
			"\u{7CFB}\u{7EDF}\u{672A}\u{68C0}\u{6D4B}\u{5230}\u{90AE}\u{4EF6}\u{5BA2}\u{6237}\u{7AEF}\u{FF0C}\u{53CD}\u{9988}\u{90AE}\u{7BB1}\u{5730}\u{5740}\u{5DF2}\u{590D}\u{5236}\u{5230}\u{526A}\u{8D34}\u{677F}\u{FF0C}\u{8BF7}\u{624B}\u{52A8}\u{53D1}\u{9001}\u{90AE}\u{4EF6}\u{81F3}\u{FF1A}\n%@",
			"No mail client was found — only a web browser is registered to handle mailto: links. The feedback address has been copied to your clipboard; please email us manually at:\n%@"
		),
		// Small on-screen indicator so a slow remote source (IPTV, remote Emby)
		// doesn't leave the screen blank/frozen with no sign the phone's tap
		// registered. See ToastPanelController.
		"toast_connecting": ("正在连接…", "Connecting…"),
		"toast_autoplay_countdown": ("%d 秒后自动播放下一集", "Next episode in %ds"),
    ]
    guard let pair = table[key] else { return key }
    return zh ? pair.0 : pair.1
}

private func resolvedWebLanguage(_ preference: String) -> String {
	guard preference == "auto" else { return preference }
	let identifier = Locale.current.identifier.lowercased()
	if identifier.hasPrefix("zh-hant") || identifier.hasPrefix("zh-tw") || identifier.hasPrefix("zh-hk") { return "zh-TW" }
	if identifier.hasPrefix("zh") { return "zh-CN" }
	for code in ["ja", "ko", "es", "fr", "de"] where identifier.hasPrefix(code) { return code }
	return "en"
}

// MARK: - Release update discovery

private let tinyPlayReleaseAPI = "https://api.github.com/repos/YangHanqing/tinyplay/releases/latest"
private let tinyPlayReleasePage = "https://github.com/YangHanqing/tinyplay/releases/latest"
private let skippedUpdateVersionKey = "TinyPlaySkippedUpdateVersion"
private let updateRemindVersionKey = "TinyPlayUpdateRemindVersion"
private let updateRemindAfterKey = "TinyPlayUpdateRemindAfter"

private struct TinyPlayRelease: Decodable {
	let tag_name: String
	let html_url: String
	let draft: Bool
	let prerelease: Bool
}

private struct TinyPlayUpdate {
	let version: String
	let pageURL: URL
}

private struct TinyPlayVersion: Comparable {
	let major: Int
	let minor: Int
	let patch: Int

	static func < (lhs: TinyPlayVersion, rhs: TinyPlayVersion) -> Bool {
		if lhs.major != rhs.major { return lhs.major < rhs.major }
		if lhs.minor != rhs.minor { return lhs.minor < rhs.minor }
		return lhs.patch < rhs.patch
	}
}

private func parseTinyPlayVersion(_ raw: String) -> TinyPlayVersion? {
	let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
	let value = trimmed.hasPrefix("v") ? String(trimmed.dropFirst()) : trimmed
	guard !value.isEmpty, !value.contains("-"), !value.contains("+") else { return nil }
	let parts = value.split(separator: ".", omittingEmptySubsequences: false)
	guard parts.count == 3 else { return nil }
	var numbers: [Int] = []
	for part in parts {
		guard !part.isEmpty,
			!(part.count > 1 && part.first == "0"),
			let number = Int(part), number >= 0 else { return nil }
		numbers.append(number)
	}
	return TinyPlayVersion(major: numbers[0], minor: numbers[1], patch: numbers[2])
}

private func tinyPlayReleaseTag(from url: URL) -> String? {
	guard url.host?.lowercased() == "github.com" else { return nil }
	let prefix = "/YangHanqing/tinyplay/releases/tag/"
	guard url.path.hasPrefix(prefix) else { return nil }
	let tag = String(url.path.dropFirst(prefix.count))
	return tag.isEmpty ? nil : tag
}

private func makeUpdateRequest(_ url: URL) -> URLRequest {
	var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: 6)
	request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
	request.setValue("TinyPlay update checker", forHTTPHeaderField: "User-Agent")
	return request
}

private func fetchLatestTinyPlayUpdate(completion: @escaping (TinyPlayUpdate?) -> Void) {
	guard let apiURL = URL(string: tinyPlayReleaseAPI), let pageURL = URL(string: tinyPlayReleasePage) else {
		completion(nil)
		return
	}
	URLSession.shared.dataTask(with: makeUpdateRequest(apiURL)) { data, response, _ in
		if let response = response as? HTTPURLResponse,
			response.statusCode == 200,
			let data,
			let release = try? JSONDecoder().decode(TinyPlayRelease.self, from: data),
			!release.draft, !release.prerelease,
			let releaseURL = URL(string: release.html_url),
			tinyPlayReleaseTag(from: releaseURL) == release.tag_name {
			completion(TinyPlayUpdate(version: release.tag_name, pageURL: releaseURL))
			return
		}

		// github.com and api.github.com occasionally behave differently behind
		// network filters. The documented latest-release redirect is a cheap,
		// safe fallback; its final host and path are checked before use.
		URLSession.shared.dataTask(with: makeUpdateRequest(pageURL)) { _, fallbackResponse, _ in
			guard let response = fallbackResponse as? HTTPURLResponse,
				(200..<300).contains(response.statusCode),
				let finalURL = response.url,
				let tag = tinyPlayReleaseTag(from: finalURL) else {
				completion(nil)
				return
			}
			completion(TinyPlayUpdate(version: tag, pageURL: finalURL))
		}.resume()
	}.resume()
}

// MARK: - Full-screen standby restore policy (pure / testable)

/// What the shell should do after reading `GET /api/player/state`.
private enum StandbyRestoreStep: Equatable {
	/// No action: keep watching, leave focus alone.
	case idle
	/// `running` became true — arm restore eligibility for the later stop.
	case armSession
	/// Observed stop after a real session — bring the native full-screen window forward.
	/// Autoplay (`finding_next` / `next_available`) must NOT suppress this: the
	/// standby Space should cover the desktop during the episode countdown.
	case restore
}

/// Pure decision for the standby-restore state machine.
///
/// - `sawActivePlayback`: true once this shell has observed `running=true`.
/// - First later `running=false` restores immediately, independent of autoplay.
private func evaluateStandbyRestore(
	sawActivePlayback: Bool,
	running: Bool
) -> StandbyRestoreStep {
	if running {
		return .armSession
	}
	guard sawActivePlayback else {
		return .idle
	}
	return .restore
}

/// Parse the JSON fields the shell cares about from `/api/player/state`.
private func parsePlayerStateForStandby(_ object: [String: Any]) -> (running: Bool, revision: UInt64)? {
	let running: Bool
	if let b = object["running"] as? Bool {
		running = b
	} else if let n = object["running"] as? NSNumber {
		running = n.boolValue
	} else {
		return nil
	}
	let revision: UInt64
	if let n = object["playback_revision"] as? NSNumber {
		revision = n.uint64Value
	} else if let i = object["playback_revision"] as? Int, i >= 0 {
		revision = UInt64(i)
	} else if let u = object["playback_revision"] as? UInt64 {
		revision = u
	} else {
		return nil
	}
	return (running, revision)
}

/// Parse the desktop-toast fields from the same `/api/player/state` payload.
/// desktop_toast_mode is "" (no toast), "connecting" (a Play() attempt has not
/// yet produced a frame), or "autoplay" (host autoplay's next-episode grace
/// period is counting down). See desktopToastState in the Go core.
private func parseDesktopToast(_ object: [String: Any]) -> (mode: String, remainingSeconds: Int?) {
	let mode = (object["desktop_toast_mode"] as? String) ?? ""
	var remainingSeconds: Int?
	if let n = object["autoplay_remaining_ms"] as? NSNumber {
		remainingSeconds = Int((n.doubleValue / 1000.0).rounded(.up))
	}
	return (mode, remainingSeconds)
}

// MARK: - Desktop "connecting/buffering" toast

/// A small, always-on-top, non-activating indicator so a slow remote source
/// (IPTV, remote Emby/Jellyfin/Plex) doesn't leave the monitor blank or
/// frozen with no sign the phone's tap registered — mpv itself does not
/// create its window until it has probed enough of the stream to know the
/// video format, which can take several seconds. Deliberately plain AppKit
/// (NSPanel + NSTextField + NSProgressIndicator), not a WebView: it must
/// appear in milliseconds and must not itself become the next thing the user
/// is waiting on.
///
/// Independent of mpv's own window so the same code covers both cases the
/// user can be in: nothing on screen yet (cold start) or mpv already
/// full-screen on a previous title (host autoplay counting down to the next
/// episode). `.canJoinAllSpaces` + `.fullScreenAuxiliary` is what lets this
/// panel — a different process from mpv — float over mpv's own native
/// full-screen Space; without it macOS hides auxiliary windows the instant
/// another app's window goes full-screen.
private final class ToastPanelController {
	private var panel: NSPanel?
	private var label: NSTextField?

	func apply(mode: String, remainingSeconds: Int?) {
		guard !mode.isEmpty else {
			hide()
			return
		}
		let text: String
		switch mode {
		case "autoplay":
			text = String(format: L("toast_autoplay_countdown"), max(remainingSeconds ?? 0, 0))
		default:
			text = L("toast_connecting")
		}
		show(text: text)
	}

	func hide() {
		panel?.orderOut(nil)
	}

	private func show(text: String) {
		let panel = self.panel ?? makePanel()
		self.panel = panel
		label?.stringValue = text
		layout(panel)
		if !panel.isVisible {
			panel.orderFrontRegardless()
		}
	}

	private func makePanel() -> NSPanel {
		let spinner = NSProgressIndicator()
		spinner.style = .spinning
		spinner.controlSize = .small
		spinner.startAnimation(nil)

		let label = NSTextField(labelWithString: "")
		label.font = NSFont.systemFont(ofSize: 13, weight: .medium)
		label.textColor = .labelColor
		self.label = label

		let stack = NSStackView(views: [spinner, label])
		stack.orientation = .horizontal
		stack.alignment = .centerY
		stack.spacing = 8
		stack.edgeInsets = NSEdgeInsets(top: 9, left: 14, bottom: 9, right: 16)
		stack.translatesAutoresizingMaskIntoConstraints = true

		let effect = NSVisualEffectView()
		effect.material = .hudWindow
		effect.state = .active
		effect.blendingMode = .withinWindow
		effect.wantsLayer = true
		effect.layer?.cornerRadius = 11
		effect.layer?.masksToBounds = true
		effect.addSubview(stack)
		stack.frame = effect.bounds
		stack.autoresizingMask = [.width, .height]

		let panel = NSPanel(
			contentRect: NSRect(x: 0, y: 0, width: 200, height: 40),
			styleMask: [.nonactivatingPanel, .borderless],
			backing: .buffered,
			defer: false
		)
		panel.isOpaque = false
		panel.backgroundColor = .clear
		panel.hasShadow = true
		panel.ignoresMouseEvents = true
		panel.isReleasedWhenClosed = false
		panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary, .ignoresCycle]
		panel.level = .statusBar
		panel.contentView = effect
		return panel
	}

	private func layout(_ panel: NSPanel) {
		guard let screen = NSScreen.main, let content = panel.contentView else { return }
		let fitting = content.fittingSize
		let width = max(fitting.width, 160)
		let height = max(fitting.height, 36)
		// visibleFrame already excludes the Dock and menu bar (and shrinks when
		// the Dock is always-on). A small margin sits the pill just above that
		// edge — better than a fixed offset from screen.frame, which overlaps a
		// tall Dock. Auto-hide Dock still leaves a comfortable bottom gap.
		let visible = screen.visibleFrame
		let margin: CGFloat = 28
		let x = visible.midX - width / 2
		let y = visible.minY + margin
		panel.setFrame(NSRect(x: x, y: y, width: width, height: height), display: panel.isVisible)
	}
}

final class AppDelegate: NSObject, NSApplicationDelegate, WKScriptMessageHandler, WKNavigationDelegate, NSMenuDelegate {
    private var statusItem: NSStatusItem!
    private var window: NSWindow?
    private let core = Process()
    private var coreURL: String = "http://127.0.0.1:1980"
    private let urlFile = NSTemporaryDirectory() + "tvremote-url-\(ProcessInfo.processInfo.processIdentifier).txt"
	private var lanBrowser: NWBrowser?
	private var webView: WKWebView?
	private var localNetworkDenied = false
	private var dlnaMenuItem: NSMenuItem?
	/// "高级设置 → 自定义 MPV 播放器" submenu item. Its tooltip is refreshed
	/// from GET /desktop/mpv each time the tray menu opens (see menuWillOpen)
	/// so it reflects whichever mpv is actually in effect right now, including
	/// the "custom path went stale, fell back to bundled" state.
	private var mpvAdvancedMenuItem: NSMenuItem?
	/// "高级设置 → 开机启动 → 开启/关闭" leaf items. Their state is read back
	/// from SMAppService (never cached in UserDefaults) each time the menu
	/// opens, so a login item the user removed in System Settings shows as
	/// off here too.
	private var autostartOnMenuItem: NSMenuItem?
	private var autostartOffMenuItem: NSMenuItem?
	/// Retained so menuWillOpen can restate the measured cache size and the
	/// selected budget instead of leaving whatever the last build wrote.
	private var webCacheClearMenuItem: NSMenuItem?
	private var webCacheLimitMenuItems: [NSMenuItem] = []
	private static let fullscreenMessageName = "tinyplaySetFullscreen"
	private static let checkForUpdatesMessageName = "tinyplayCheckForUpdates"
	private static let showAboutMessageName = "tinyplayShowAbout"
	private static let restartMessageName = "tinyplayRestart"
	private let compactContentSize = NSSize(width: 900, height: 540)
	private var fullscreenTransitionRequested = false

	// Full-screen standby restore: when mpv exits (or is stopped) while the
	// TinyPlay window is still in a native full-screen Space, re-activate that
	// Space so the user is not stranded on the desktop. Driven by a single
	// in-flight long-poll on GET /api/player/state?after_revision=… rather than
	// a 1 Hz timer.
	private var playerStateTask: URLSessionDataTask?
	private var playerStateRetryWorkItem: DispatchWorkItem?
	private var sawActivePlayback = false
	private var lastPlaybackRevision: UInt64?
	/// Monitor stays active until applicationWillTerminate cancels it.
	private var playbackStandbyMonitorActive = false
	private var updateCheckInFlight = false
	/// Client timeout must exceed the Go long-poll bound (25s) so a quiet
	/// server does not look like a network failure.
	private let playerStateRequestTimeout: TimeInterval = 30
	/// Bounded backoff after transient request failures (no tight retry loop).
	private let playerStateRetryDelay: TimeInterval = 1.5
	/// Piggybacks on the same long-poll above — see ToastPanelController.
	private let toastPanel = ToastPanelController()

    func applicationDidFinishLaunching(_ notification: Notification) {
		// Must be read here: the launch Apple event is only the "current" one
		// while the app is still starting up.
		let openedAtLogin = Self.launchedAtLogin()
        primeLocalNetworkAccess()
        startCore()
        setupMenuBar()
        // Show the QR window once on first launch so the user sees it immediately.
		waitForCoreURL { [weak self] in
			self?.startPlaybackStandbyMonitor()
			self?.startWebsiteShell()
			self?.startDesktopInputShell()
			self?.startPairingRequestWatcher()
			// …but not when the system started us at login: the point of
			// start-at-login is to be ready in the menu bar, not to put a
			// window in front of whatever the person actually signed in to do.
			if !openedAtLogin { self?.openMainWindow() }
			self?.scheduleAutomaticUpdateCheck()
		}
    }

	/// Whether macOS launched TinyPlay as a login item rather than the user
	/// opening it. The launch Apple event carries this; there is no property to
	/// ask for it later, which is why the answer is captured at startup.
	private static func launchedAtLogin() -> Bool {
		guard let event = NSAppleEventManager.shared().currentAppleEvent,
			event.eventID == AEEventID(kAEOpenApplication),
			let property = event.paramDescriptor(forKeyword: AEKeyword(keyAEPropData))
		else { return false }
		return property.enumCodeValue == AEKeyword(keyAELaunchedAsLogInItem)
	}

    // MARK: - Local network permission

    /// On macOS 15+ the system gates any outbound LAN access behind a one-time
    /// local-network permission prompt. Without this, the prompt only fires later — the
    /// moment the Go core first reaches the user's Emby server (192.168.x.x) —
    /// which is confusing because it interrupts mid-flow on the phone. The core
    /// runs as our child process, so it is attributed to this app bundle for
    /// privacy purposes; starting a Bonjour browser here makes macOS surface the
    /// prompt up front, when the user opens the app, and the grant then covers
    /// the core's later Emby connection too.
    private func primeLocalNetworkAccess() {
        let params = NWParameters()
        params.includePeerToPeer = true
        let browser = NWBrowser(for: .bonjour(type: "_http._tcp", domain: nil), using: params)
        browser.stateUpdateHandler = { [weak self] state in
            switch state {
            case .waiting(let error) where self?.isLocalNetworkPolicyDenied(error) == true:
                self?.setLocalNetworkDenied(true)
            case .ready:
                // This also clears the guidance if the person enables access in
                // System Settings while TinyPlay is running.
                self?.setLocalNetworkDenied(false)
            default:
                break
            }
        }
        browser.browseResultsChangedHandler = { _, _ in }
        browser.start(queue: .main)
        lanBrowser = browser // retain so it keeps running (and the prompt stays)
    }

    /// Bonjour reports a denied Local Network grant as this specific DNS-SD
    /// error. Treat other browser failures (for example, a temporarily offline
    /// network) as non-permission failures so we never send the person to
    /// System Settings for the wrong reason.
    private func isLocalNetworkPolicyDenied(_ error: NWError) -> Bool {
        guard case .dns(let code) = error else { return false }
        return code == kDNSServiceErr_PolicyDenied
    }

    private func setLocalNetworkDenied(_ denied: Bool) {
        guard localNetworkDenied != denied else { return }
        localNetworkDenied = denied
        // The Go core owns the bilingual desktop page. Reloading it preserves
        // the selected language and swaps in/out the precise permission help.
        if let webView { webView.load(URLRequest(url: desktopURL())) }
    }

    func applicationWillTerminate(_ notification: Notification) {
		stopPlaybackStandbyMonitor()
		stopWebsiteShell()
		stopDesktopInputShell()
        if core.isRunning { core.terminate() }
    }

	// MARK: - Website playback window (experimental)

	private var websiteShell: WebsiteShellController?
	private var desktopInputShell: DesktopInputShellController?
	private var pairingWatcher: PairingRequestWatcher?

	private func startWebsiteShell() {
		// Trim the browser store before the shell can put a window on it. The
		// walk touches only cache directories, so it is cheap, but keep it off
		// the main thread: a multi-gigabyte cache is exactly the case this
		// exists for, and that is the case where the walk is slowest.
		DispatchQueue.global(qos: .utility).async { WebCache.enforceLimit() }
		websiteShell?.stop()
		let shell = WebsiteShellController(coreURL: coreURL)
		websiteShell = shell
		shell.start()
	}

	private func stopWebsiteShell() {
		websiteShell?.stop()
		websiteShell = nil
	}

	private func startDesktopInputShell() {
		desktopInputShell?.stop()
		let shell = DesktopInputShellController(coreURL: coreURL)
		desktopInputShell = shell
		shell.start()
	}

	private func stopDesktopInputShell() {
		desktopInputShell?.stop()
		desktopInputShell = nil
	}

	private func startPairingRequestWatcher() {
		pairingWatcher?.stop()
		let watcher = PairingRequestWatcher(coreURL: coreURL) { [weak self] in
			self?.openMainWindow()
		}
		pairingWatcher = watcher
		watcher.start()
	}

	private func stopPairingRequestWatcher() {
		pairingWatcher?.stop()
		pairingWatcher = nil
	}

	// MARK: - Playback → full-screen standby restore

	/// Observe Go core player state via one-in-flight long-poll. First request is
	/// immediate; subsequent requests wait on `after_revision` until playback
	/// context changes (play / stop / EOF / completed clear).
	private func startPlaybackStandbyMonitor() {
		stopPlaybackStandbyMonitor()
		playbackStandbyMonitorActive = true
		// Immediate snapshot, then long-poll from the returned revision.
		fetchPlayerStateForStandby(longPollAfter: nil)
	}

	private func stopPlaybackStandbyMonitor() {
		playbackStandbyMonitorActive = false
		playerStateTask?.cancel()
		playerStateTask = nil
		playerStateRetryWorkItem?.cancel()
		playerStateRetryWorkItem = nil
		sawActivePlayback = false
		lastPlaybackRevision = nil
		toastPanel.hide()
	}

	/// Bounded delay before retrying a failed / cancelled-for-error state fetch.
	private func schedulePlayerStateRetry() {
		guard playbackStandbyMonitorActive else { return }
		playerStateRetryWorkItem?.cancel()
		let work = DispatchWorkItem { [weak self] in
			guard let self else { return }
			self.playerStateRetryWorkItem = nil
			guard self.playbackStandbyMonitorActive else { return }
			self.fetchPlayerStateForStandby(longPollAfter: self.lastPlaybackRevision)
		}
		playerStateRetryWorkItem = work
		DispatchQueue.main.asyncAfter(deadline: .now() + playerStateRetryDelay, execute: work)
	}

	/// Single-flight `GET /api/player/state`. Pass `longPollAfter` to wait for a
	/// revision change; `nil` fetches the current snapshot immediately.
	private func fetchPlayerStateForStandby(longPollAfter: UInt64?) {
		guard playbackStandbyMonitorActive else { return }
		// Only one AppKit state request at a time.
		guard playerStateTask == nil else { return }

		var urlString = coreURL + "/api/player/state"
		if let after = longPollAfter {
			urlString += "?after_revision=\(after)"
		}
		guard let url = URL(string: urlString) else {
			schedulePlayerStateRetry()
			return
		}
		var request = URLRequest(url: url)
		request.cachePolicy = .reloadIgnoringLocalCacheData
		request.timeoutInterval = playerStateRequestTimeout
		let task = URLSession.shared.dataTask(with: request) { [weak self] data, response, error in
			DispatchQueue.main.async {
				guard let self else { return }
				self.playerStateTask = nil
				// Termination cancels the in-flight task; do not reschedule.
				if !self.playbackStandbyMonitorActive {
					return
				}
				if let error = error as NSError?,
					error.domain == NSURLErrorDomain,
					error.code == NSURLErrorCancelled {
					return
				}
				if error == nil,
					let response = response as? HTTPURLResponse,
					(200..<300).contains(response.statusCode),
					let data,
					let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
					let parsed = parsePlayerStateForStandby(object) {
					self.lastPlaybackRevision = parsed.revision
					self.applyStandbyRestoreStep(
						evaluateStandbyRestore(
							sawActivePlayback: self.sawActivePlayback,
							running: parsed.running
						)
					)
					let toast = parseDesktopToast(object)
					self.toastPanel.apply(mode: toast.mode, remainingSeconds: toast.remainingSeconds)
					// Continue long-polling so the next episode / stop is observed.
					self.fetchPlayerStateForStandby(longPollAfter: self.lastPlaybackRevision)
					return
				}
				// Transient failure: bounded delay, no tight loop / log storm.
				self.schedulePlayerStateRetry()
			}
		}
		playerStateTask = task
		task.resume()
	}

	private func applyStandbyRestoreStep(_ step: StandbyRestoreStep) {
		switch step {
		case .idle:
			break
		case .armSession:
			sawActivePlayback = true
		case .restore:
			// Eligibility is consumed so we do not loop-activate on subsequent
			// long-poll snapshots that still report running=false.
			sawActivePlayback = false
			restoreFullscreenStandbyIfNeeded()
		}
	}

	/// Bring TinyPlay's existing native full-screen window forward so macOS
	/// switches back to its Space. Never opens a new window or promotes a
	/// compact/closed window.
	private func restoreFullscreenStandbyIfNeeded() {
		guard let w = window, w.styleMask.contains(.fullScreen) else { return }
		activateStandbyWindow(w)
		// mpv's own full-screen Space is often still animating away when the
		// first activation lands, and macOS can swallow an activate issued
		// mid-transition. Retry once after the teardown settles — unless a new
		// playback re-armed the session in the meantime.
		DispatchQueue.main.asyncAfter(deadline: .now() + 1.0) { [weak self] in
			guard let self, !self.sawActivePlayback,
				let w = self.window, w.styleMask.contains(.fullScreen) else { return }
			self.activateStandbyWindow(w)
		}
	}

	private func activateStandbyWindow(_ w: NSWindow) {
		NSApp.activate(ignoringOtherApps: true)
		w.makeKeyAndOrderFront(nil)
		// Accessory (LSUIElement) apps sometimes need an explicit order-front
		// after activate so the full-screen Space actually becomes current.
		w.orderFrontRegardless()
	}

    // MARK: - Sidecar

    private func startCore() {
        let res = Bundle.main.resourceURL!
        let coreBin = res.appendingPathComponent("tvremote-core")
        let mpvBin = res.appendingPathComponent("mpv/bin/mpv") // see build-app.sh layout

        core.executableURL = coreBin
        var env = ProcessInfo.processInfo.environment
		if FileManager.default.fileExists(atPath: mpvBin.path) {
            env["TVREMOTE_MPV_EXE"] = mpvBin.path
		}
		env["TVREMOTE_LANGUAGE"] = UserDefaults.standard.string(forKey: "TinyPlayLanguage") ?? "auto"
        env["TVREMOTE_URL_FILE"] = urlFile
        core.environment = env

        try? FileManager.default.removeItem(atPath: urlFile)
        do {
            try core.run()
        } catch {
            NSLog("TV Remote: failed to launch core: \(error)")
        }
    }

    /// Poll the handshake file the core writes its LAN URL into (up to ~5s).
    ///
    /// Only the port is kept: every shell→core call is made over 127.0.0.1. The
    /// core's /desktop endpoints are loopback-only because the QR image carries
    /// the pairing secret, and shell /api/ calls rely on the same-machine
    /// exemption rather than holding a device token. The LAN address a phone
    /// should use is rendered by the core itself, on the /desktop page.
    private func waitForCoreURL(then ready: @escaping () -> Void) {
        var attempts = 0
        func poll() {
            attempts += 1
            if let s = try? String(contentsOfFile: urlFile, encoding: .utf8),
               !s.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                coreURL = loopbackCoreURL(s.trimmingCharacters(in: .whitespacesAndNewlines))
                self.reloadDLNAReceiverState()
                ready()
                return
            }
            if attempts > 50 { ready(); return } // give up, use default
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.1, execute: poll)
        }
        poll()
    }

    // MARK: - Menu bar

	/// Compact menu-bar surface: open the QR window, pick a language, send
	/// feedback, quit. Logs / updates / about / DLNA stay on the main window
	/// (and phone settings) so the tray does not mirror the whole UI.
	private func setupMenuBar() {
		if statusItem == nil { statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength) }
		if let button = statusItem.button {
			// The original white template icon follows menu-bar light/dark mode and
			// stays visually quiet beside the system status icons.
			button.image = NSImage(systemSymbolName: "play.tv", accessibilityDescription: "TinyPlay")
			button.image?.isTemplate = true
		}
		let menu = NSMenu()
		menu.addItem(NSMenuItem(title: L("open_main"), action: #selector(openMainWindow), keyEquivalent: ""))
		let language = NSMenuItem(title: L("language"), action: nil, keyEquivalent: "")
		let languageMenu = NSMenu()
		let selected = UserDefaults.standard.string(forKey: "TinyPlayLanguage") ?? "auto"
		for (value, title) in [
			("auto", L("automatic")), ("en", "English"), ("zh-CN", "简体中文"),
			("zh-TW", "繁體中文"), ("ja", "日本語"), ("ko", "한국어"),
			("es", "Español"), ("fr", "Français"), ("de", "Deutsch"),
		] {
			let item = NSMenuItem(title: title, action: #selector(changeLanguage(_:)), keyEquivalent: "")
			item.representedObject = value
			item.state = value == selected ? .on : .off
			languageMenu.addItem(item)
		}
		language.submenu = languageMenu
		menu.addItem(language)
		menu.addItem(.separator())
		let advanced = NSMenuItem(title: L("advanced_settings"), action: nil, keyEquivalent: "")
		let advancedMenu = NSMenu()
		let mpv = NSMenuItem(title: L("mpv_menu"), action: nil, keyEquivalent: "")
		let mpvMenu = NSMenu()
		mpvMenu.addItem(NSMenuItem(title: L("mpv_custom_path_menu"), action: #selector(chooseCustomMPV), keyEquivalent: ""))
		mpvMenu.addItem(NSMenuItem(title: L("mpv_use_bundled_menu"), action: #selector(restoreDefaultMPV), keyEquivalent: ""))
		mpv.submenu = mpvMenu
		advancedMenu.addItem(mpv)
		mpvAdvancedMenuItem = mpv
		// SMAppService (the only login-item API this shell knows how to drive)
		// is macOS 13+ only, so the whole submenu is left out below that rather
		// than shown non-functional or backed by the deprecated pre-13 API.
		if #available(macOS 13.0, *) {
			let autostart = NSMenuItem(title: L("autostart_menu"), action: nil, keyEquivalent: "")
			autostart.toolTip = L("autostart_menu_tip")
			let autostartMenu = NSMenu()
			let autostartOn = NSMenuItem(title: L("autostart_on_menu"), action: #selector(setAutostart(_:)), keyEquivalent: "")
			autostartOn.representedObject = true
			let autostartOff = NSMenuItem(title: L("autostart_off_menu"), action: #selector(setAutostart(_:)), keyEquivalent: "")
			autostartOff.representedObject = false
			autostartMenu.addItem(autostartOn)
			autostartMenu.addItem(autostartOff)
			autostartOnMenuItem = autostartOn
			autostartOffMenuItem = autostartOff
			autostart.submenu = autostartMenu
			refreshAutostartCheckbox()
			advancedMenu.addItem(autostart)
		}

		// Web cache. The built-in browser is the one part of TinyPlay whose
		// on-disk footprint has no ceiling of its own.
		let webCache = NSMenuItem(title: L("webcache_menu"), action: nil, keyEquivalent: "")
		webCache.toolTip = L("webcache_menu_tip")
		let webCacheMenu = NSMenu()
		let clearItem = NSMenuItem(title: L("webcache_clear"), action: #selector(clearWebCache), keyEquivalent: "")
		clearItem.toolTip = L("webcache_clear_tip")
		webCacheMenu.addItem(clearItem)
		webCacheClearMenuItem = clearItem

		let limit = NSMenuItem(title: L("webcache_limit_menu"), action: nil, keyEquivalent: "")
		let limitMenu = NSMenu()
		var limitItems: [NSMenuItem] = []
		for (mb, title) in [
			(512, "512 MB"),
			(WebCache.defaultLimitMB, "1 GB"),
			(2048, "2 GB"),
			(WebCache.unlimited, L("webcache_limit_unlimited")),
		] {
			let item = NSMenuItem(title: title, action: #selector(setWebCacheLimit(_:)), keyEquivalent: "")
			item.representedObject = mb
			limitMenu.addItem(item)
			limitItems.append(item)
		}
		limit.submenu = limitMenu
		webCacheLimitMenuItems = limitItems
		refreshWebCacheLimitCheckbox()
		webCacheMenu.addItem(limit)
		webCache.submenu = webCacheMenu
		advancedMenu.addItem(webCache)

		advanced.submenu = advancedMenu
		menu.addItem(advanced)
		menu.addItem(.separator())
		menu.addItem(NSMenuItem(title: L("feedback"), action: #selector(sendFeedback), keyEquivalent: ""))
		menu.addItem(.separator())
		menu.addItem(NSMenuItem(title: L("quit"), action: #selector(quit), keyEquivalent: "q"))
		// DLNA is toggled from the main window now; keep the stored item nil so
		// a stale checkbox cannot reappear after a language refresh rebuilds the menu.
		dlnaMenuItem = nil
		menu.delegate = self
		statusItem.menu = menu
		reloadMPVStatus()
	}

	/// Refreshes the "高级设置" tooltip right before the tray menu is shown, so
	/// it reflects whichever mpv is actually in effect (including a stale
	/// custom path having silently fallen back to bundled) without needing a
	/// standing poll.
	func menuWillOpen(_ menu: NSMenu) {
		if menu === statusItem.menu {
			reloadMPVStatus()
			refreshAutostartCheckbox()
			refreshWebCacheMenu()
		}
	}

	// MARK: - Web cache

	/// Restates the measured size and the selected budget each time the menu
	/// opens, for the same reason the autostart checkbox is re-read from the OS:
	/// a number the user is about to act on should not be a cached guess.
	private func refreshWebCacheMenu() {
		webCacheClearMenuItem?.title = "\(L("webcache_clear")) (\(WebCache.formatBytes(WebCache.usageBytes())))"
		refreshWebCacheLimitCheckbox()
	}

	private func refreshWebCacheLimitCheckbox() {
		let active = WebCache.limitMB
		for item in webCacheLimitMenuItems {
			item.state = (item.representedObject as? Int) == active ? .on : .off
		}
	}

	@objc private func clearWebCache() {
		if websiteShell?.isWindowOpen == true {
			let alert = NSAlert()
			alert.messageText = L("webcache_busy_title")
			alert.informativeText = L("webcache_busy_body")
			alert.runModal()
			return
		}
		WebCache.clear { [weak self] freed in
			DispatchQueue.main.async {
				let alert = NSAlert()
				alert.messageText = L("webcache_clear_done")
				alert.informativeText = WebCache.formatBytes(freed)
				alert.runModal()
				self?.refreshWebCacheMenu()
			}
		}
	}

	@objc private func setWebCacheLimit(_ sender: NSMenuItem) {
		guard let mb = sender.representedObject as? Int else { return }
		WebCache.limitMB = mb
		refreshWebCacheLimitCheckbox()
	}

	// MARK: - Start at login

	/// Whether TinyPlay is registered as a login item right now. `.enabled` is
	/// the only state that actually starts the app: `.requiresApproval` means
	/// the registration exists but macOS is waiting for the user to allow it in
	/// System Settings, and showing that as a checked box would promise
	/// something that will not happen.
	private var autostartEnabled: Bool {
		guard #available(macOS 13.0, *) else { return false }
		return SMAppService.mainApp.status == .enabled
	}

	private func refreshAutostartCheckbox() {
		autostartOnMenuItem?.state = autostartEnabled ? .on : .off
		autostartOffMenuItem?.state = autostartEnabled ? .off : .on
	}

	/// SMAppService registers *this app bundle*, which is why start-at-login is
	/// menu-bar-shell work rather than something the Go core could do — the
	/// core runs as this bundle's child process and has no bundle of its own.
	/// Each leaf ("开启"/"关闭") sets an explicit target via representedObject,
	/// unlike a single checkbox's implicit flip — mirroring the Windows tray's
	/// two-leaf submenu.
	@available(macOS 13.0, *)
	@objc private func setAutostart(_ sender: NSMenuItem) {
		guard let target = sender.representedObject as? Bool else { return }
		let service = SMAppService.mainApp
		do {
			if target {
				if !autostartEnabled {
					try service.register()
					// A first registration on macOS 13+ commonly lands in
					// `.requiresApproval`: the item is filed but switched off until
					// the person allows it. Say so and offer the exact settings
					// pane, instead of leaving the menu silently stay off.
					if service.status == .requiresApproval {
						showAutostartApprovalAlert()
					}
				}
			} else if autostartEnabled {
				try service.unregister()
			}
		} catch {
			showAutostartFailureAlert(error.localizedDescription)
		}
		refreshAutostartCheckbox()
	}

	@available(macOS 13.0, *)
	private func showAutostartApprovalAlert() {
		let alert = NSAlert()
		alert.messageText = L("autostart_approval_title")
		alert.informativeText = L("autostart_approval_body")
		alert.addButton(withTitle: L("open_login_items"))
		alert.addButton(withTitle: L("cancel"))
		NSApp.activate(ignoringOtherApps: true)
		if alert.runModal() == .alertFirstButtonReturn {
			SMAppService.openSystemSettingsLoginItems()
		}
	}

	private func showAutostartFailureAlert(_ detail: String) {
		let alert = NSAlert()
		alert.alertStyle = .warning
		alert.messageText = L("autostart_failed_title")
		alert.informativeText = String(format: L("autostart_failed_body"), detail)
		alert.addButton(withTitle: L("ok"))
		NSApp.activate(ignoringOtherApps: true)
		alert.runModal()
	}

	/// Some Macs have a web browser — not a mail app — registered as the
	/// system's "mailto:" handler (e.g. Microsoft Edge registers itself for
	/// every URL scheme it can during install, including mailto:, without
	/// actually composing anything). NSWorkspace.shared.open still reports
	/// success in that case since it did launch *an* app, so the failure is
	/// silent: the browser activates and no compose window or page ever
	/// appears. These bundle IDs are known browsers, not mail clients.
	private static let knownBrowserBundleIDs: Set<String> = [
		"com.apple.Safari",
		"com.google.Chrome", "com.google.Chrome.beta", "com.google.Chrome.dev", "com.google.Chrome.canary",
		"com.microsoft.edgemac", "com.microsoft.edgemac.Dev", "com.microsoft.edgemac.Beta",
		"org.mozilla.firefox", "org.mozilla.firefoxdeveloperedition", "org.mozilla.nightly",
		"com.brave.Browser", "com.brave.Browser.beta", "com.brave.Browser.nightly",
		"company.thebrowser.Browser",
		"com.operasoftware.Opera",
		"com.vivaldi.Vivaldi",
	]

	/// Opens the default mail client with a prefilled support address. Feature
	/// ideas can be free-form; bugs should attach logs from the main window.
	@objc private func sendFeedback() {
		var components = URLComponents()
		components.scheme = "mailto"
		components.path = feedbackMailAddress
		components.queryItems = [
			URLQueryItem(name: "subject", value: L("feedback_mail_subject")),
			URLQueryItem(name: "body", value: L("feedback_mail_body")),
		]
		guard let url = components.url else { return }

		if let handlerURL = NSWorkspace.shared.urlForApplication(toOpen: url),
			let bundleID = Bundle(url: handlerURL)?.bundleIdentifier,
			Self.knownBrowserBundleIDs.contains(bundleID) {
			showFeedbackAddressFallback()
			return
		}
		NSWorkspace.shared.open(url)
	}

	private func showFeedbackAddressFallback() {
		NSPasteboard.general.clearContents()
		NSPasteboard.general.setString(feedbackMailAddress, forType: .string)
		let alert = NSAlert()
		alert.messageText = L("feedback")
		alert.informativeText = String(format: L("feedback_no_mail_client"), feedbackMailAddress)
		alert.addButton(withTitle: L("ok"))
		NSApp.activate(ignoringOtherApps: true)
		alert.runModal()
	}

	// MARK: - Updates

	private func appVersion() -> String {
		Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.0.0"
	}

	private func scheduleAutomaticUpdateCheck() {
		DispatchQueue.main.asyncAfter(deadline: .now() + 8) { [weak self] in
			self?.performUpdateCheck(manual: false, completion: nil)
		}
	}

	@objc private func checkForUpdates() {
		performUpdateCheck(manual: true, completion: nil)
	}

	/// Runs the GitHub release probe. `completion` fires on the main queue after
	/// the check (and any modal dialog) finishes — used by the /desktop page so
	/// its "Check for Updates" control can drop the loading spinner.
	private func performUpdateCheck(manual: Bool, completion: (() -> Void)?) {
		guard parseTinyPlayVersion(appVersion()) != nil else {
			if manual { showUpdateMessage(L("update_failed")) }
			completion?()
			return
		}
		guard !updateCheckInFlight else {
			// Another check is already in flight (e.g. tray + page). Don't stack
			// network work; still clear the page spinner if one is waiting.
			completion?()
			return
		}
		updateCheckInFlight = true
		let currentVersion = appVersion()
		fetchLatestTinyPlayUpdate { [weak self] update in
			DispatchQueue.main.async {
				guard let self else {
					completion?()
					return
				}
				self.updateCheckInFlight = false
				defer { completion?() }
				guard let update,
					let latest = parseTinyPlayVersion(update.version),
					let current = parseTinyPlayVersion(currentVersion) else {
					if manual { self.showUpdateMessage(L("update_failed")) }
					return
				}
				guard latest > current else {
					if manual { self.showUpdateMessage(L("update_latest")) }
					return
				}
				if !manual && !self.shouldOfferAutomaticUpdate(update.version) { return }
				self.showUpdateAlert(update, currentVersion: currentVersion)
			}
		}
	}

	private func shouldOfferAutomaticUpdate(_ version: String) -> Bool {
		if UserDefaults.standard.string(forKey: skippedUpdateVersionKey) == version { return false }
		if UserDefaults.standard.string(forKey: updateRemindVersionKey) == version,
			let until = UserDefaults.standard.object(forKey: updateRemindAfterKey) as? Date,
			until > Date() {
			return false
		}
		return true
	}

	private func showUpdateAlert(_ update: TinyPlayUpdate, currentVersion: String) {
		let alert = NSAlert()
		alert.messageText = L("update_available_title")
		alert.informativeText = String(format: L("update_available_body"), update.version, currentVersion)
		alert.addButton(withTitle: L("update_download"))
		alert.addButton(withTitle: L("update_remind"))
		alert.addButton(withTitle: L("update_skip"))
		NSApp.activate(ignoringOtherApps: true)
		switch alert.runModal() {
		case .alertFirstButtonReturn:
			NSWorkspace.shared.open(update.pageURL)
		case .alertSecondButtonReturn:
			UserDefaults.standard.removeObject(forKey: skippedUpdateVersionKey)
			UserDefaults.standard.set(update.version, forKey: updateRemindVersionKey)
			UserDefaults.standard.set(Date().addingTimeInterval(72 * 60 * 60), forKey: updateRemindAfterKey)
		case .alertThirdButtonReturn:
			UserDefaults.standard.set(update.version, forKey: skippedUpdateVersionKey)
			UserDefaults.standard.removeObject(forKey: updateRemindVersionKey)
			UserDefaults.standard.removeObject(forKey: updateRemindAfterKey)
		default:
			break
		}
	}

	private func showUpdateMessage(_ message: String) {
		let alert = NSAlert()
		alert.messageText = "TinyPlay"
		alert.informativeText = message
		alert.addButton(withTitle: L("ok"))
		NSApp.activate(ignoringOtherApps: true)
		alert.runModal()
	}

	@objc private func toggleDLNAReceiver(_ sender: NSMenuItem) {
		setDLNAReceiverEnabled(sender.state != .on) { [weak self, weak sender] enabled in
			DispatchQueue.main.async {
				guard let self, let sender else { return }
				guard let enabled else {
					self.reloadDLNAReceiverState()
					return
				}
				sender.state = enabled ? .on : .off
				if let webView = self.webView { webView.load(URLRequest(url: self.desktopURL())) }
			}
		}
	}

	private func reloadDLNAReceiverState() {
		guard let url = URL(string: coreURL + "/api/settings") else { return }
		URLSession.shared.dataTask(with: url) { [weak self] data, _, _ in
			guard let data,
				let settings = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
				let enabled = settings["dlna_receiver_enabled"] as? Bool else { return }
			DispatchQueue.main.async { self?.dlnaMenuItem?.state = enabled ? .on : .off }
		}.resume()
	}

	private func setDLNAReceiverEnabled(_ enabled: Bool, completion: @escaping (Bool?) -> Void) {
		guard let url = URL(string: coreURL + "/api/settings") else {
			completion(nil)
			return
		}
		var request = URLRequest(url: url)
		request.httpMethod = "PUT"
		request.setValue("application/json", forHTTPHeaderField: "Content-Type")
		request.httpBody = try? JSONSerialization.data(withJSONObject: ["dlna_receiver_enabled": enabled])
		URLSession.shared.dataTask(with: request) { data, response, _ in
			guard let response = response as? HTTPURLResponse,
				(200..<300).contains(response.statusCode),
				let data,
				let settings = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
				let saved = settings["dlna_receiver_enabled"] as? Bool else {
				completion(nil)
				return
			}
			completion(saved)
		}.resume()
	}

	// MARK: - Advanced settings: custom mpv executable

	/// Shows the native "choose an executable" panel, then hands the picked
	/// path to the core to validate + persist. NSOpenPanel (not a save panel)
	/// with directoryURL left at the default so the user can freely navigate
	/// into /usr/local, /opt, etc. — mpv installed via Homebrew or a manual
	/// build commonly lives outside the app's own sandboxed defaults.
	@objc private func chooseCustomMPV() {
		let panel = NSOpenPanel()
		panel.title = L("mpv_dialog_title")
		panel.canChooseFiles = true
		panel.canChooseDirectories = false
		panel.allowsMultipleSelection = false
		panel.treatsFilePackagesAsDirectories = true
		panel.directoryURL = URL(fileURLWithPath: "/usr/local/bin", isDirectory: true)
		NSApp.activate(ignoringOtherApps: true)
		guard panel.runModal() == .OK, let url = panel.url else { return }
		postCustomMPVPath(url.path) { [weak self] errorDetail in
			DispatchQueue.main.async {
				guard let self else { return }
				if let errorDetail {
					self.showMPVInvalidAlert(errorDetail)
				} else {
					self.reloadMPVStatus()
				}
			}
		}
	}

	/// Clears the persisted custom path — same request shape as setting one,
	/// with an empty path. No confirmation dialog: unlike "unpair all", this
	/// only affects a single local preference and is trivially reversible.
	@objc private func restoreDefaultMPV() {
		postCustomMPVPath("") { [weak self] errorDetail in
			DispatchQueue.main.async {
				guard let self else { return }
				if let errorDetail {
					self.showMPVInvalidAlert(errorDetail)
				} else {
					self.reloadMPVStatus()
				}
			}
		}
	}

	private func showMPVInvalidAlert(_ detail: String) {
		let alert = NSAlert()
		alert.alertStyle = .warning
		alert.messageText = L("mpv_invalid_title")
		alert.informativeText = String(format: L("mpv_invalid_body"), detail)
		alert.addButton(withTitle: L("ok"))
		NSApp.activate(ignoringOtherApps: true)
		alert.runModal()
	}

	/// POST /desktop/mpv — see internal/server/handlers_mpv.go. A non-empty
	/// path is validated server-side (ValidateMPV: `<path> --version` must
	/// look like mpv) before it is ever written to config.json, so a failure
	/// here always means the path did not persist.
	/// completion receives nil on success, or an error detail string on
	/// failure — simpler than Result here since the only failure information
	/// the caller needs is the human-readable detail for the alert.
	private func postCustomMPVPath(_ path: String, completion: @escaping (String?) -> Void) {
		guard let url = URL(string: coreURL + "/desktop/mpv") else {
			completion("invalid core URL")
			return
		}
		var request = URLRequest(url: url)
		request.httpMethod = "POST"
		request.setValue("application/json", forHTTPHeaderField: "Content-Type")
		request.httpBody = try? JSONSerialization.data(withJSONObject: ["path": path])
		URLSession.shared.dataTask(with: request) { data, response, _ in
			guard let response = response as? HTTPURLResponse else {
				completion("no response")
				return
			}
			if (200..<300).contains(response.statusCode) {
				completion(nil)
				return
			}
			var detail = "HTTP \(response.statusCode)"
			if let data, let body = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
				let text = body["detail"] as? String {
				detail = text
			}
			completion(detail)
		}.resume()
	}

	/// Refreshes the "高级设置" submenu tooltip from GET /desktop/mpv. Mirrors
	/// reloadDLNAReceiverState's shape (best-effort GET, no error UI — this is
	/// a status readout, not an action the user needs to know failed).
	private func reloadMPVStatus() {
		guard let url = URL(string: coreURL + "/desktop/mpv") else { return }
		URLSession.shared.dataTask(with: url) { [weak self] data, _, _ in
			guard let self, let data,
				let status = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return }
			let source = status["source"] as? String ?? ""
			let path = status["path"] as? String ?? ""
			let customConfigured = status["custom_configured"] as? Bool ?? false
			let customValid = status["custom_valid"] as? Bool ?? false
			let tooltip: String
			if customConfigured && !customValid {
				tooltip = L("mpv_custom_stale_tip")
			} else if source == "custom" {
				tooltip = String(format: L("mpv_custom_active_tip"), path)
			} else {
				tooltip = L("mpv_default_tip")
			}
			DispatchQueue.main.async { self.mpvAdvancedMenuItem?.toolTip = tooltip }
		}.resume()
	}

	/// Version comes from Info.plist (CFBundleShortVersionString/CFBundleVersion),
	/// set by build-app.sh's VERSION at packaging time.
	@objc private func showAbout() {
		let info = Bundle.main.infoDictionary
		let shortVersion = info?["CFBundleShortVersionString"] as? String ?? "0.0.0"
		let build = info?["CFBundleVersion"] as? String ?? shortVersion
		let alert = NSAlert()
		alert.messageText = "TinyPlay"
		alert.informativeText = "\(L("version_label")) \(shortVersion) (\(build))"
		alert.addButton(withTitle: L("ok"))
		alert.addButton(withTitle: L("third_party_notices"))
		NSApp.activate(ignoringOtherApps: true)
		if alert.runModal() == .alertSecondButtonReturn {
			openThirdPartyNotices()
		}
	}

	private func openThirdPartyNotices() {
		guard let url = Bundle.main.resourceURL?.appendingPathComponent("THIRD_PARTY_NOTICES.md") else { return }
		NSWorkspace.shared.open(url)
	}

	@objc private func changeLanguage(_ sender: NSMenuItem) {
		guard let value = sender.representedObject as? String else { return }
		UserDefaults.standard.set(value, forKey: "TinyPlayLanguage")
		setupMenuBar()
		guard let url = URL(string: coreURL + "/api/settings") else { return }
		var request = URLRequest(url: url)
		request.httpMethod = "PUT"
		request.setValue("application/json", forHTTPHeaderField: "Content-Type")
		request.httpBody = try? JSONSerialization.data(withJSONObject: ["language": value])
		URLSession.shared.dataTask(with: request).resume()
		if let webView { webView.load(URLRequest(url: desktopURL())) }
	}

    /// Ask the core to reveal its logs directory in Finder. The core knows the
    /// real path (it resolves the same data dir as config.json), so we just hit
    /// its endpoint instead of guessing the path here.
    @objc private func openLogs() {
        guard let url = URL(string: coreURL + "/desktop/open-logs") else { return }
        URLSession.shared.dataTask(with: url).resume()
    }

    // MARK: - Window

	@objc private func openMainWindow() {
        if let w = window {
            w.makeKeyAndOrderFront(nil)
            NSApp.activate(ignoringOtherApps: true)
            return
        }
		let config = WKWebViewConfiguration()
		config.userContentController.add(self, name: Self.fullscreenMessageName)
		config.userContentController.add(self, name: Self.checkForUpdatesMessageName)
		config.userContentController.add(self, name: Self.showAboutMessageName)
		config.userContentController.add(self, name: Self.restartMessageName)
		let webView = WKWebView(frame: NSRect(origin: .zero, size: compactContentSize), configuration: config)
		webView.navigationDelegate = self
		// Match /desktop compact canvas (#f7f9fc) so the first paint does not
		// flash a mismatched window chrome colour.
		if #available(macOS 12.0, *) {
			webView.underPageBackgroundColor = NSColor(calibratedRed: 247 / 255, green: 249 / 255, blue: 252 / 255, alpha: 1)
		}
		webView.load(URLRequest(url: desktopURL()))
		self.webView = webView

        let w = NSWindow(
            contentRect: NSRect(origin: .zero, size: compactContentSize),
            styleMask: [.titled, .closable, .resizable],
            backing: .buffered, defer: false)
        w.title = "TinyPlay"
		w.backgroundColor = NSColor(calibratedRed: 247 / 255, green: 249 / 255, blue: 252 / 255, alpha: 1)
        // AppKit needs a resizable window to expand its content view into the
        // native full-screen Space. Manual windowed resizing is clamped by the
        // delegate below, so the normal QR window remains compact.
        w.collectionBehavior.insert(.fullScreenPrimary)
		w.standardWindowButton(.zoomButton)?.isHidden = true
        w.contentView = webView
        w.center()
        w.isReleasedWhenClosed = false
        w.delegate = self
        window = w
        DockVisibility.windowOpened(w)
        w.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        pushMonitorHint()
	}

	/// Pushes the NSScreen index the TinyPlay window currently sits on, so the
	/// next fresh mpv spawn opens on the same display (see
	/// player.Player.SetMonitorHint / handlers_desktopdisplay.go). Best-effort:
	/// mpv falls back to its own default placement if this never lands or the
	/// window has not been shown yet.
	private func pushMonitorHint() {
		guard let screen = window?.screen,
			let index = NSScreen.screens.firstIndex(of: screen),
			let url = URL(string: coreURL + "/desktop/display/monitor-hint") else { return }
		var request = URLRequest(url: url)
		request.httpMethod = "POST"
		request.setValue("application/json", forHTTPHeaderField: "Content-Type")
		request.httpBody = try? JSONSerialization.data(withJSONObject: ["screen": index])
		URLSession.shared.dataTask(with: request).resume()
	}

	private func desktopURL() -> URL {
		let preference = UserDefaults.standard.string(forKey: "TinyPlayLanguage") ?? "auto"
		let resolved = resolvedWebLanguage(preference)
		var components = URLComponents(string: coreURL + "/desktop")!
		var query = [URLQueryItem(name: "lang", value: resolved), URLQueryItem(name: "version", value: appVersion())]
		if localNetworkDenied {
			query.append(URLQueryItem(name: "local_network", value: "denied"))
		}
		components.queryItems = query
		return components.url!
	}

	/// Bridge from the /desktop page. Handler names match Windows Bind and the
	/// shared page JS: tinyplaySetFullscreen, tinyplayCheckForUpdates,
	/// tinyplayShowAbout, tinyplayRestart. After an update check, both shells
	/// call window.__tinyplayUpdateCheckDone so the footer spinner clears the
	/// same way.
	func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
		switch message.name {
		case Self.fullscreenMessageName:
			let enter: Bool
			if let value = message.body as? Bool {
				enter = value
			} else if let number = message.body as? NSNumber {
				enter = number.boolValue
			} else {
				return
			}
			setNativeFullscreen(enter)
		case Self.checkForUpdatesMessageName:
			// Page shows a spinner until we call back; tray menu uses checkForUpdates()
			// without a completion.
			performUpdateCheck(manual: true) { [weak self] in
				self?.webView?.evaluateJavaScript(
					"window.__tinyplayUpdateCheckDone && window.__tinyplayUpdateCheckDone()",
					completionHandler: nil)
			}
		case Self.showAboutMessageName:
			showAbout()
		case Self.restartMessageName:
			// Accessibility trust often sticks at false until the process is new.
			restartApp()
		default:
			break
		}
	}

	/// Schedule a relaunch, then quit. TCC re-evaluates Accessibility for the
	/// new process; opening first would just activate this still-untrusted one.
	func restartApp() {
		let path: String
		let openCmd: String
		if Bundle.main.bundleURL.pathExtension == "app" {
			path = Bundle.main.bundlePath
			openCmd = "/usr/bin/open"
		} else if let exe = Bundle.main.executableURL?.path {
			// Dev / non-bundled binary: re-exec the same path after exit.
			path = exe
			openCmd = ""
		} else {
			NSApp.terminate(nil)
			return
		}
		let quoted = "'" + path.replacingOccurrences(of: "'", with: "'\\''") + "'"
		let task = Process()
		task.executableURL = URL(fileURLWithPath: "/bin/sh")
		if openCmd.isEmpty {
			task.arguments = ["-c", "sleep 0.8; exec \(quoted)"]
		} else {
			task.arguments = ["-c", "sleep 0.8; \(openCmd) \(quoted)"]
		}
		try? task.run()
		NSApp.terminate(nil)
	}

	private func setNativeFullscreen(_ enter: Bool) {
		guard let w = window else { return }
		let isFull = w.styleMask.contains(.fullScreen)
		if enter != isFull {
			fullscreenTransitionRequested = true
			w.toggleFullScreen(nil)
		} else {
			// Already in the requested state — still sync page layout.
			notifyPageFullscreen(enter)
		}
	}

	private func notifyPageFullscreen(_ enter: Bool) {
		let js = "window.__tinyplayNativeFullscreen && window.__tinyplayNativeFullscreen(\(enter ? "true" : "false"))"
		webView?.evaluateJavaScript(js, completionHandler: nil)
	}

    @objc private func quit() {
        NSApp.terminate(nil)
    }
}

extension AppDelegate: NSWindowDelegate {
	func windowWillResize(_ sender: NSWindow, to frameSize: NSSize) -> NSSize {
		if fullscreenTransitionRequested || sender.styleMask.contains(.fullScreen) {
			return frameSize
		}
		return compactContentSize
	}

	func windowWillClose(_ notification: Notification) {
		// Closed window cannot be restored (restoreFullscreenStandbyIfNeeded
		// no-ops when window is nil / not full-screen). Keep the long-poll
		// monitor armed so a later reopened full-screen session still works.
		if let w = notification.object as? NSWindow ?? window {
			DockVisibility.windowClosed(w)
		}
		webView?.configuration.userContentController.removeScriptMessageHandler(forName: Self.fullscreenMessageName)
		webView?.configuration.userContentController.removeScriptMessageHandler(forName: Self.checkForUpdatesMessageName)
		webView?.configuration.userContentController.removeScriptMessageHandler(forName: Self.showAboutMessageName)
		webView?.configuration.userContentController.removeScriptMessageHandler(forName: Self.restartMessageName)
		window = nil
		webView = nil
    }

	func windowDidEnterFullScreen(_ notification: Notification) {
		fullscreenTransitionRequested = false
		notifyPageFullscreen(true)
	}

	func windowDidExitFullScreen(_ notification: Notification) {
		fullscreenTransitionRequested = false
		window?.setContentSize(compactContentSize)
		notifyPageFullscreen(false)
	}

	// Dragging the window to another display should not require the user to
	// also reopen it before that choice sticks — keep the pushed hint current
	// for the whole time the window is up, not just at open.
	func windowDidMove(_ notification: Notification) {
		pushMonitorHint()
	}

	func windowDidChangeScreen(_ notification: Notification) {
		pushMonitorHint()
	}
}

extension AppDelegate {
	/// After a page reload while already full screen, re-apply the standby layout.
	func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
		if window?.styleMask.contains(.fullScreen) == true {
			notifyPageFullscreen(true)
		}
	}
}

// MARK: - Web cache

/// Bounds the built-in browser's on-disk data, mirroring the policy in Go's
/// `internal/webcache` — same promise to the user (clearing the cache never
/// signs them out), same default budget, same "sweep at launch, never under a
/// live window" rule.
///
/// The plumbing is deliberately different, and simpler, than the Windows side.
/// WebKit exposes `WKWebsiteDataStore.removeData(ofTypes:modifiedSince:)`,
/// which separates caches from cookies as a first-class API, so nothing here
/// has to reason about Chromium's directory layout the way Go's allow-list
/// does. Two consequences follow:
///
/// - There is no "change cache location" action on macOS. `.default()` owns its
///   own paths under ~/Library, and swapping in a non-default data store to
///   move a few hundred megabytes is not a trade worth making on a platform
///   where a single volume is the norm.
/// - The budget lives in UserDefaults rather than the Go core's config.json.
///   This store is WebKit's, entirely outside the core's knowledge, so routing
///   the preference through an HTTP endpoint would add a hop that serves
///   nothing. `TinyPlayLanguage` already sets that precedent.
enum WebCache {
	/// Kept in step with `config.DefaultWebsiteCacheLimitMB` on the Go side.
	static let defaultLimitMB = 1024
	/// Stored value meaning "never sweep". Not 0: an absent default reads back
	/// as 0, and treating that as unlimited would leave every existing install
	/// uncapped — the problem this setting exists to fix.
	static let unlimited = -1

	private static let limitKey = "TinyPlayWebCacheLimitMB"

	/// Data kinds that are safe to discard. Cookies, local/session storage and
	/// IndexedDB are absent on purpose: they hold the logins the "site logins
	/// are kept" promise is about.
	private static var cacheTypes: Set<String> {
		var types: Set<String> = [
			WKWebsiteDataTypeDiskCache,
			WKWebsiteDataTypeMemoryCache,
			WKWebsiteDataTypeOfflineWebApplicationCache,
		]
		types.insert(WKWebsiteDataTypeFetchCache)
		types.insert(WKWebsiteDataTypeServiceWorkerRegistrations)
		return types
	}

	static var limitMB: Int {
		get {
			let stored = UserDefaults.standard.integer(forKey: limitKey)
			if stored == unlimited { return unlimited }
			return stored > 0 ? stored : defaultLimitMB
		}
		set {
			UserDefaults.standard.set(newValue == unlimited ? unlimited : max(newValue, 1), forKey: limitKey)
		}
	}

	/// Byte budget, or nil when the user turned the cap off. Callers must keep
	/// nil distinct from zero: a zero budget would mean "clear on every launch".
	static var limitBytes: Int64? {
		let mb = limitMB
		return mb == unlimited ? nil : Int64(mb) << 20
	}

	/// Directories WebKit fills with the data `cacheTypes` covers. There is no
	/// public API that reports the size of a data store, and `WKWebsiteDataRecord`
	/// carries no byte count, so the number under the menu item is measured off
	/// disk. It is an estimate of what clearing would reclaim, which is the
	/// number the user is actually deciding on.
	private static var measuredDirectories: [URL] {
		let bundleID = Bundle.main.bundleIdentifier ?? "cn.hqyang.tinyplay.mac"
		let home = FileManager.default.homeDirectoryForCurrentUser
		let library = home.appendingPathComponent("Library", isDirectory: true)
		let websiteData = library
			.appendingPathComponent("WebKit", isDirectory: true)
			.appendingPathComponent(bundleID, isDirectory: true)
			.appendingPathComponent("WebsiteData", isDirectory: true)
		return [
			library.appendingPathComponent("Caches/\(bundleID)/WebKit", isDirectory: true),
			websiteData.appendingPathComponent("CacheStorage", isDirectory: true),
			websiteData.appendingPathComponent("ServiceWorkers", isDirectory: true),
		]
	}

	/// Bytes that `clear` would reclaim. A directory that does not exist yet is
	/// zero, not an error: the profile is created by the first web window.
	static func usageBytes() -> Int64 {
		measuredDirectories.reduce(0) { $0 + directorySize($1) }
	}

	private static func directorySize(_ url: URL) -> Int64 {
		let fm = FileManager.default
		guard let walker = fm.enumerator(
			at: url,
			includingPropertiesForKeys: [.isRegularFileKey, .totalFileAllocatedSizeKey, .fileSizeKey],
			options: [.skipsHiddenFiles]
		) else { return 0 }
		var total: Int64 = 0
		for case let item as URL in walker {
			guard let values = try? item.resourceValues(
				forKeys: [.isRegularFileKey, .totalFileAllocatedSizeKey, .fileSizeKey]
			), values.isRegularFile == true else { continue }
			total += Int64(values.totalFileAllocatedSize ?? values.fileSize ?? 0)
		}
		return total
	}

	static func clear(completion: @escaping (Int64) -> Void) {
		let freed = usageBytes()
		WKWebsiteDataStore.default().removeData(
			ofTypes: cacheTypes,
			modifiedSince: Date(timeIntervalSince1970: 0)
		) {
			completion(freed)
		}
	}

	/// Trims the store at launch when it has outgrown the budget. Called before
	/// any web window can exist; `WKWebsiteDataStore` tolerates removal under a
	/// live page far better than Chromium does, but keeping the two platforms on
	/// the same rule means one behaviour to reason about.
	static func enforceLimit() {
		guard let limit = limitBytes else { return }
		let used = usageBytes()
		guard used > limit else { return }
		// removeData directly rather than clear(): the size is already measured
		// here, and re-walking a multi-gigabyte cache just to log the same
		// number is the one cost this path should not pay.
		WKWebsiteDataStore.default().removeData(
			ofTypes: cacheTypes,
			modifiedSince: Date(timeIntervalSince1970: 0)
		) {
			NSLog("webcache: over the \(formatBytes(limit)) budget, reclaimed \(formatBytes(used))")
		}
	}

    /// Matches Go's `webcache.FormatBytes`. Units stay untranslated — they read
    /// the same in every language this app ships.
	static func formatBytes(_ n: Int64) -> String {
		let unit: Int64 = 1024
		if n < unit { return "\(n) B" }
		var div = unit
		var exp = 0
		var v = n / unit
		while v >= unit && exp < 3 {
			div *= unit
			exp += 1
			v /= unit
		}
		let value = Double(n) / Double(div)
		let suffix = ["KB", "MB", "GB", "TB"][exp]
		return value < 10
			? String(format: "%.1f %@", value, suffix)
			: String(format: "%.0f %@", value, suffix)
	}
}

// MARK: - Website shell (loopback command poll + Safari-style WKWebView)

/// Rewrites the core's advertised LAN address to 127.0.0.1, keeping the port.
/// The shell always talks to the core over loopback: /desktop/* is loopback-only
/// (the QR image carries the pairing secret) and /api/* is token-protected for
/// everyone except same-machine callers.
private func loopbackCoreURL(_ coreURL: String) -> String {
	guard let url = URL(string: coreURL), let port = url.port else {
		return "http://127.0.0.1:1980"
	}
	return "http://127.0.0.1:\(port)"
}

// Native, computer-wide emergency input. This is intentionally separate from
// WebsiteShellController: it drives the current foreground app, so a user can
// recover from a browser dialog or a desktop-level interruption. macOS requires
// Accessibility consent for CGEvent posting; the phone UI stays disabled until
// this controller reports that consent back to the Go core.
/// Raises the intro window when a phone asks to pair by hand.
///
/// Same rule and same reasoning as cmd/tvremote/pairingwatch.go, which does this
/// for the Windows shell — read that file's comment for why it exists. Kept as a
/// separate copy because this shell is Swift; if you change one, change the other.
final class PairingRequestWatcher {
	private let coreURL: String
	private let raise: () -> Void
	private var timer: Timer?
	/// Raise once per request. Someone who closes the window again has answered
	/// in their own way, and a window that keeps returning to the front is worse
	/// than a missed pairing.
	private var lastRaisedNonce = ""
	private var inFlight = false
	private let session: URLSession = {
		let cfg = URLSessionConfiguration.ephemeral
		cfg.timeoutIntervalForRequest = 5
		cfg.timeoutIntervalForResource = 8
		return URLSession(configuration: cfg)
	}()

	init(coreURL: String, raise: @escaping () -> Void) {
		self.coreURL = coreURL
		self.raise = raise
	}

	/// Matches the intro window's own polling cadence, so watching costs no more
	/// than leaving that window open.
	func start() {
		stop()
		let timer = Timer(timeInterval: 2, repeats: true) { [weak self] _ in self?.check() }
		// .common so the poll keeps running while a menu is tracking.
		RunLoop.main.add(timer, forMode: .common)
		self.timer = timer
	}

	func stop() {
		timer?.invalidate()
		timer = nil
	}

	private func check() {
		guard !inFlight, let url = URL(string: coreURL + "/desktop/pairing") else { return }
		inFlight = true
		var request = URLRequest(url: url)
		request.cachePolicy = .reloadIgnoringLocalCacheData
		session.dataTask(with: request) { [weak self] data, response, _ in
			DispatchQueue.main.async {
				guard let self else { return }
				self.inFlight = false
				guard let response = response as? HTTPURLResponse, (200..<300).contains(response.statusCode),
					let data,
					let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return }
				let nonce = (object["pending"] as? [String: Any])?["nonce"] as? String ?? ""
				if nonce.isEmpty {
					// Cleared, so the next request counts as a new event.
					self.lastRaisedNonce = ""
					return
				}
				guard nonce != self.lastRaisedNonce else { return }
				self.lastRaisedNonce = nonce
				self.raise()
			}
		}.resume()
	}
}

final class DesktopInputShellController {
	private let coreURL: String
	private var lastCommandID: UInt64 = 0
	private var pollTask: URLSessionDataTask?
	private var active = false
	private let session: URLSession = {
		let cfg = URLSessionConfiguration.ephemeral
		cfg.timeoutIntervalForRequest = 35
		cfg.timeoutIntervalForResource = 40
		return URLSession(configuration: cfg)
	}()

	init(coreURL: String) { self.coreURL = coreURL }

	func start() {
		active = true
		reportTrust()
		pollOnce()
	}
	func stop() { active = false; pollTask?.cancel(); pollTask = nil }

	private func trusted() -> Bool { AXIsProcessTrusted() }
	private func reportTrust(error: String = "") {
		guard let url = URL(string: coreURL + "/desktop/input/report") else { return }
		let body: [String: Any] = [
			"ready": true,
			"permission_required": true,
			"permission_granted": trusted(),
			"last_error": error,
		]
		guard let data = try? JSONSerialization.data(withJSONObject: body) else { return }
		var request = URLRequest(url: url)
		request.httpMethod = "POST"
		request.setValue("application/json", forHTTPHeaderField: "Content-Type")
		request.httpBody = data
		session.dataTask(with: request).resume()
	}

	private func pollOnce() {
		guard active, let url = URL(string: "\(coreURL)/desktop/input/poll?after=\(lastCommandID)") else { return }
		var request = URLRequest(url: url)
		request.cachePolicy = .reloadIgnoringLocalCacheData
		request.timeoutInterval = 35
		pollTask = session.dataTask(with: request) { [weak self] data, response, error in
			DispatchQueue.main.async {
				guard let self, self.active else { return }
				self.pollTask = nil
				if error == nil, let response = response as? HTTPURLResponse, (200..<300).contains(response.statusCode),
					let data, let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
					if let cmd = object["command"] as? [String: Any] { self.handle(cmd) }
					self.reportTrust()
					self.pollOnce()
					return
				}
				DispatchQueue.main.asyncAfter(deadline: .now() + 1) { [weak self] in self?.pollOnce() }
			}
		}
		pollTask?.resume()
	}

	private func handle(_ command: [String: Any]) {
		if let n = command["id"] as? NSNumber { lastCommandID = n.uint64Value }
		let action = command["action"] as? String ?? ""
		if action == "request_permission" {
			// The prompt is system-owned and asynchronous. We always report the
			// current state immediately; a subsequent Check again reaches here too.
			let options = ["AXTrustedCheckOptionPrompt": true] as CFDictionary
			_ = AXIsProcessTrustedWithOptions(options)
			if let settings = URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility") {
				NSWorkspace.shared.open(settings)
			}
			reportTrust()
			return
		}
		if action == "restart_app" {
			// Phone UI and non-native reloads cannot call the WK bridge; this
			// command is the loopback path to the same relaunch helper.
			(NSApp.delegate as? AppDelegate)?.restartApp()
			return
		}
		guard trusted() else { reportTrust(error: "accessibility_permission_required"); return }
		let dx = (command["dx"] as? NSNumber)?.intValue ?? 0
		let dy = (command["dy"] as? NSNumber)?.intValue ?? 0
		let text = command["text"] as? String ?? ""
		do { try inject(action: action, dx: dx, dy: dy, text: text) }
		catch { reportTrust(error: "input_injection_failed") }
	}

	private func inject(action: String, dx: Int, dy: Int, text: String) throws {
		guard let source = CGEventSource(stateID: .hidSystemState) else { throw NSError(domain: "TinyPlay", code: 1) }
		switch action {
		case "move":
			guard let current = CGEvent(source: source) else { throw NSError(domain: "TinyPlay", code: 2) }
			let point = CGPoint(x: current.location.x + CGFloat(dx), y: current.location.y + CGFloat(dy))
			CGEvent(mouseEventSource: source, mouseType: .mouseMoved, mouseCursorPosition: point, mouseButton: .left)?.post(tap: .cghidEventTap)
		case "left_click", "right_click":
			guard let current = CGEvent(source: source) else { throw NSError(domain: "TinyPlay", code: 3) }
			let right = action == "right_click"
			CGEvent(mouseEventSource: source, mouseType: right ? .rightMouseDown : .leftMouseDown, mouseCursorPosition: current.location, mouseButton: right ? .right : .left)?.post(tap: .cghidEventTap)
			CGEvent(mouseEventSource: source, mouseType: right ? .rightMouseUp : .leftMouseUp, mouseCursorPosition: current.location, mouseButton: right ? .right : .left)?.post(tap: .cghidEventTap)
		case "scroll":
			CGEvent(scrollWheelEvent2Source: source, units: .pixel, wheelCount: 1, wheel1: Int32(dy), wheel2: 0, wheel3: 0)?.post(tap: .cghidEventTap)
		case "key":
			guard let code = keyCode(text) else { throw NSError(domain: "TinyPlay", code: 4) }
			CGEvent(keyboardEventSource: source, virtualKey: code, keyDown: true)?.post(tap: .cghidEventTap)
			CGEvent(keyboardEventSource: source, virtualKey: code, keyDown: false)?.post(tap: .cghidEventTap)
		case "type":
			guard !text.isEmpty else { return }
			let units = Array(text.utf16)
			let down = CGEvent(keyboardEventSource: source, virtualKey: 0, keyDown: true)
			down?.keyboardSetUnicodeString(stringLength: units.count, unicodeString: units)
			down?.post(tap: .cghidEventTap)
			let up = CGEvent(keyboardEventSource: source, virtualKey: 0, keyDown: false)
			up?.keyboardSetUnicodeString(stringLength: units.count, unicodeString: units)
			up?.post(tap: .cghidEventTap)
		default: throw NSError(domain: "TinyPlay", code: 5)
		}
	}

	private func keyCode(_ key: String) -> CGKeyCode? {
		switch key {
		case "escape": return 53
		case "enter": return 36
		case "left": return 123
		case "right": return 124
		case "down": return 125
		case "up": return 126
		default: return nil
		}
	}
}

/// Native website playback window: persistent cookies via default data store,
/// native AppKit full-screen Space, shared controller.js injection.
/// Website mode owns exactly one window and one WKWebView. Selecting a catalog
/// site navigates that singleton page to the site's fixed home URL; it never
/// creates one page instance per site. Current site is derived server-side from
/// the main-frame URL reported on navigation.
final class WebsiteShellController: NSObject, WKNavigationDelegate, WKUIDelegate {
	private let coreURL: String
	private var lastCommandID: UInt64 = 0
	private var pollTask: URLSessionDataTask?
	private var active = false
	private var window: NSWindow?
	private var webView: WKWebView?
	private var controllerJS: String = ""
	private var keyMonitor: Any?
	// The latest open command represented by the current singleton window.
	// Native-close reports include it so Go can reject an older window close
	// after a newer open was already requested.
	private var windowCommandID: UInt64 = 0
	private let session: URLSession = {
		let cfg = URLSessionConfiguration.ephemeral
		cfg.timeoutIntervalForRequest = 35
		cfg.timeoutIntervalForResource = 40
		return URLSession(configuration: cfg)
	}()

	init(coreURL: String) {
		self.coreURL = coreURL
		super.init()
	}

	/// Whether a web page is on screen right now. The web-cache menu asks before
	/// deleting anything; it deliberately cannot close the window itself, since
	/// "clear cache" has no business discarding a page the user is reading.
	var isWindowOpen: Bool { window != nil }

	/// WKWebView's default UA stops at "(KHTML, like Gecko)" — it carries no
	/// "Version/<x> Safari/605.1.15" suffix the way Safari.app's does. Video
	/// sites parse that suffix to identify the browser, and without it bilibili
	/// serves its "browser too old" page instead of the site. Track the host's
	/// real Safari version so the claim stays true as the system updates.
	private static func safariUserAgentSuffix() -> String {
		if let info = NSDictionary(contentsOfFile: "/Applications/Safari.app/Contents/Info.plist"),
		   let version = info["CFBundleShortVersionString"] as? String,
		   !version.isEmpty {
			return "Version/\(version) Safari/605.1.15"
		}
		return "Version/26.5 Safari/605.1.15"
	}

	func start() {
		active = true
		fetchControllerJS { [weak self] in
			self?.pollOnce()
		}
	}

	func stop() {
		active = false
		pollTask?.cancel()
		pollTask = nil
		removeKeyMonitor()
		closeWindow(reportNative: false)
	}

	private func fetchControllerJS(completion: @escaping () -> Void) {
		guard let url = URL(string: coreURL + "/desktop/website/controller.js") else {
			completion()
			return
		}
		session.dataTask(with: url) { [weak self] data, _, _ in
			if let data, let text = String(data: data, encoding: .utf8), !text.isEmpty {
				self?.controllerJS = text
			}
			DispatchQueue.main.async { completion() }
		}.resume()
	}

	private func pollOnce() {
		guard active else { return }
		guard let url = URL(string: "\(coreURL)/desktop/website/poll?after=\(lastCommandID)") else {
			schedulePollRetry()
			return
		}
		var request = URLRequest(url: url)
		request.cachePolicy = .reloadIgnoringLocalCacheData
		request.timeoutInterval = 35
		let task = session.dataTask(with: request) { [weak self] data, response, error in
			DispatchQueue.main.async {
				guard let self, self.active else { return }
				self.pollTask = nil
				if let error = error as NSError?,
					error.domain == NSURLErrorDomain,
					error.code == NSURLErrorCancelled {
					return
				}
				if error == nil,
					let response = response as? HTTPURLResponse,
					(200..<300).contains(response.statusCode),
					let data,
					let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
					if let empty = object["empty"] as? Bool, empty {
						self.pollOnce()
						return
					}
					if let cmd = object["command"] as? [String: Any] {
						self.handleCommand(cmd)
						self.pollOnce()
						return
					}
				}
				self.schedulePollRetry()
			}
		}
		pollTask = task
		task.resume()
	}

	private func schedulePollRetry() {
		guard active else { return }
		DispatchQueue.main.asyncAfter(deadline: .now() + 1.0) { [weak self] in
			self?.pollOnce()
		}
	}

	private func handleCommand(_ cmd: [String: Any]) {
		let idNum = cmd["id"] as? NSNumber
		let id = idNum?.uint64Value ?? UInt64((cmd["id"] as? Int) ?? 0)
		if id > 0 { lastCommandID = id }
		let action = (cmd["action"] as? String) ?? ""
		let url = (cmd["url"] as? String) ?? ""
		let text = (cmd["text"] as? String) ?? ""
		let label = (cmd["label"] as? String) ?? ""

		switch action {
		case "open":
			windowCommandID = id
			openWindow(urlString: url)
			// Do not attach webView.url here: while a reused page begins a new
			// navigation it may still expose the previous site's URL. The real
			// main-frame commit below is the only source of current-site truth.
			report(["open": true, "status": "open", "action": "open", "command_id": id])
		case "close":
			closeWindow(reportNative: false)
			report(["open": false, "status": "closed", "action": "close", "command_id": id])
		case "back":
			webView?.goBack()
			reportNavigation(status: "back", action: "back", commandID: id)
		case "forward":
			webView?.goForward()
			reportNavigation(status: "forward", action: "forward", commandID: id)
		case "home":
			guard let destination = URL(string: url) else {
				report(["status": "error", "error": "home_unavailable", "action": "home", "command_id": id])
				return
			}
			webView?.load(URLRequest(url: destination))
			report(["open": true, "status": "home", "action": "home", "command_id": id])
		case "refresh":
			webView?.reload()
			report(["open": true, "status": "refresh", "action": "refresh", "command_id": id])
		default:
			guard webView != nil else {
				report(["status": "error", "error": "window_not_open", "action": action, "command_id": id])
				return
			}
			runDOMAction(action: action, text: text, label: label, commandID: id)
		}
	}

	/// Stop media before navigating the singleton page or closing its window so
	/// the previous document cannot keep producing audio during the transition.
	/// Removing src is reserved for final teardown; a normal site switch keeps
	/// the WebView itself alive and replaces only its main document.
	private func silenceMedia(_ wv: WKWebView, teardown: Bool = false) {
		let teardownScript = teardown ? "m.removeAttribute('src');m.load();" : ""
		wv.evaluateJavaScript(
			"document.querySelectorAll('video,audio').forEach(function(m){try{m.pause();m.muted=true;\(teardownScript)}catch(e){}});",
			completionHandler: nil
		)
	}

	private func openWindow(urlString: String) {
		guard let destination = URL(string: urlString) else {
			report(["status": "error", "error": "unknown_site", "action": "open", "command_id": windowCommandID])
			return
		}

		// A catalog selection always navigates the existing singleton page to
		// that site's fixed home URL. This is a single browser page switching
		// destinations, not one retained WKWebView per website.
		if let wv = webView, let w = window {
			silenceMedia(wv)
			// A <video> the previous site promoted to WebKit element-fullscreen (or
			// PiP) via requestFullscreen presents in its OWN AppKit full-screen
			// window/Space, layered above our content window. Navigating the reused
			// WebView underneath it leaves that stale presentation covering the
			// screen — the new site loads but stays hidden, so it looks like the
			// switch failed. DOM-only fullscreen (CSS pin / a site's web-fullscreen)
			// dies with the old document on load and needs no teardown; only this
			// native presentation must be dismissed first. closeAllMediaPresentations
			// tears it down synchronously on the main thread before we refocus.
			wv.closeAllMediaPresentations(completionHandler: {})
			wv.stopLoading()
			wv.load(URLRequest(url: destination))
			w.makeKeyAndOrderFront(nil)
			NSApp.activate(ignoringOtherApps: true)
			requestNativeFullscreen(w)
			installKeyMonitor()
			return
		}

		let config = WKWebViewConfiguration()
		config.websiteDataStore = .default()
		config.preferences.javaScriptCanOpenWindowsAutomatically = true
		config.applicationNameForUserAgent = Self.safariUserAgentSuffix()
		if #available(macOS 11.0, *) {
			config.defaultWebpagePreferences.allowsContentJavaScript = true
		}
		// Injected script has no user gesture, so video.play() from the phone
		// would otherwise die on the autoplay policy. Declare all media allowed.
		config.mediaTypesRequiringUserActionForPlayback = []
		// Element fullscreen is disabled by default in WKWebView; enable it so
		// the controller's last-resort requestFullscreen path can work at all
		// (it still needs user activation — the CSS pin is the primary path).
		if #available(macOS 12.3, *) {
			config.preferences.isElementFullscreenEnabled = true
		}
		if !controllerJS.isEmpty {
			let script = WKUserScript(source: controllerJS, injectionTime: .atDocumentStart, forMainFrameOnly: false)
			config.userContentController.addUserScript(script)
		}
		let wv = WKWebView(frame: .zero, configuration: config)
		wv.navigationDelegate = self
		wv.uiDelegate = self
		wv.allowsBackForwardNavigationGestures = true
		self.webView = wv

		let screen = NSScreen.main ?? NSScreen.screens.first
		let frame = screen?.visibleFrame ?? NSRect(x: 0, y: 0, width: 1280, height: 720)
		// Match Safari's window model: a normal titled window whose sole
		// content is WKWebView, promoted by AppKit into a real full-screen
		// Space. There is no fake high-level screen-covering window and no
		// custom traffic-light overlay to compete with WebKit's own element
		// full-screen presentation.
		let w = NSWindow(
			contentRect: frame,
			styleMask: [.titled, .fullSizeContentView, .closable, .miniaturizable, .resizable],
			backing: .buffered,
			defer: false
		)
		w.isOpaque = true
		w.backgroundColor = .black
		w.title = "TinyPlay Website"
		w.isReleasedWhenClosed = false
		w.delegate = self
		w.titlebarAppearsTransparent = true
		w.titleVisibility = .hidden
		w.tabbingMode = .disallowed
		w.collectionBehavior.insert(.fullScreenPrimary)
		w.level = .normal
		self.window = w
		DockVisibility.windowOpened(w)
		installWebView(wv, in: w)
		w.makeKeyAndOrderFront(nil)
		NSApp.activate(ignoringOtherApps: true)
		requestNativeFullscreen(w)
		installKeyMonitor()
		wv.load(URLRequest(url: destination))
	}

	private func requestNativeFullscreen(_ w: NSWindow) {
		guard !w.styleMask.contains(.fullScreen) else { return }
		// AppKit needs the window to be visible/key before it can move it into a
		// full-screen Space. Defer one main-loop turn just like a user clicking
		// Safari's green button after the browser window appears.
		DispatchQueue.main.async { [weak self, weak w] in
			guard let self, let w, self.window === w,
				!w.styleMask.contains(.fullScreen) else { return }
			w.toggleFullScreen(nil)
		}
	}

	/// WKWebView is the window's entire content, like a Safari content area with
	/// no tabs, address bar, bookmarks bar, or app-owned overlay chrome.
	private func installWebView(_ wv: WKWebView, in w: NSWindow) {
		let bounds = w.contentView?.bounds ?? NSRect(origin: .zero, size: w.frame.size)
		wv.frame = bounds
		wv.autoresizingMask = [.width, .height]
		w.contentView = wv
	}

	/// Cmd+W closes the website window like a normal Safari window.
	/// Escape deliberately remains available to exit a site's video fullscreen.
	private func installKeyMonitor() {
		if keyMonitor != nil { return }
		keyMonitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
			guard let self, let window = self.window, event.window == window || window.isKeyWindow else {
				return event
			}
			// Cmd+W
			if event.modifierFlags.contains(.command),
				event.charactersIgnoringModifiers?.lowercased() == "w" {
				self.closeWindow(reportNative: true)
				return nil
			}
			return event
		}
	}

	private func removeKeyMonitor() {
		if let keyMonitor {
			NSEvent.removeMonitor(keyMonitor)
			self.keyMonitor = nil
		}
	}

	private func closeWindow(reportNative: Bool) {
		let closingCommandID = windowCommandID
		windowCommandID = 0
		removeKeyMonitor()
		webView?.navigationDelegate = nil
		webView?.uiDelegate = nil
		if let wv = webView {
			silenceMedia(wv, teardown: true)
			// A <video> in WebKit element-fullscreen/PiP owns a separate AppKit
			// full-screen window; w.close() below need not dismiss it, so tear it
			// down explicitly or closing while a site video is fullscreen can strand
			// an empty full-screen Space.
			wv.closeAllMediaPresentations(completionHandler: {})
		}
		webView?.stopLoading()
		webView = nil
		if let w = window {
			w.delegate = nil
			// Closing a real full-screen window lets AppKit tear down its Space;
			// merely ordering it out can leave an empty full-screen Space behind.
			w.close()
		}
		window = nil
		if reportNative {
			var body: [String: Any] = ["open": false, "status": "closed", "action": "window_closed"]
			if closingCommandID > 0 { body["command_id"] = closingCommandID }
			report(body)
		}
	}

	private func reportNavigation(status: String, action: String, commandID: UInt64) {
		var body: [String: Any] = [
			"open": true,
			"status": status,
			"action": action,
			"command_id": commandID,
		]
		if let current = webView?.url?.absoluteString, !current.isEmpty {
			body["current_url"] = current
		}
		report(body)
	}

	private func reportCurrentMainFrameURL(_ webView: WKWebView, status: String) {
		guard self.webView === webView else { return }
		guard let current = webView.url?.absoluteString, !current.isEmpty else { return }
		report([
			"open": true,
			"status": status,
			"action": "navigation",
			"current_url": current,
		])
	}

	private func runDOMAction(action: String, text: String, label: String, commandID: UInt64) {
		guard let webView else { return }
		// callAsyncJavaScript (not evaluateJavaScript) because handle() returns a
		// Promise for oracle-checked transport actions — the shell must report the
		// confirmed effect, not just that the call was issued. Ensure controller
		// exists (SPA navigations keep user scripts, but be safe).
		let js = """
		if (!window.__tinyplayWebsite) { return {ok: false, status: 'error', error: 'no_controller'}; }
		return window.__tinyplayWebsite.handle({action: action, text: text, label: label});
		"""
		webView.callAsyncJavaScript(
			js,
			arguments: ["action": action, "text": text, "label": label],
			in: nil,
			in: .page
		) { [weak self] result in
			var body: [String: Any] = [
				"open": true,
				"action": action,
				"command_id": commandID,
			]
			switch result {
			case .failure:
				body["status"] = "error"
				body["error"] = "js_error"
			case .success(let value):
				if let dict = value as? [String: Any] {
					if let ok = dict["ok"] as? Bool {
						body["status"] = ok ? ((dict["status"] as? String) ?? "ok") : "error"
					} else {
						body["status"] = (dict["status"] as? String) ?? "ok"
					}
					if let err = dict["error"] as? String, !err.isEmpty {
						body["error"] = err
					}
					if let hint = dict["hint_active"] as? Bool {
						body["hint_active"] = hint
					}
					if let labels = dict["labels"] as? [String] {
						body["hint_labels"] = labels
					}
					if let actions = dict["actions"] as? [String] {
						body["more_actions"] = actions
					}
				} else {
					body["status"] = "ok"
				}
			}
			switch action {
			case "hint_enter":
				if body["hint_active"] == nil { body["hint_active"] = true }
			case "hint_exit", "hint_label":
				if body["hint_active"] == nil { body["hint_active"] = false }
			default: break
			}
			self?.report(body)
		}
	}

	private func report(_ body: [String: Any]) {
		guard let url = URL(string: coreURL + "/desktop/website/report") else { return }
		var request = URLRequest(url: url)
		request.httpMethod = "POST"
		request.setValue("application/json", forHTTPHeaderField: "Content-Type")
		request.httpBody = try? JSONSerialization.data(withJSONObject: body)
		session.dataTask(with: request).resume()
	}

	func webView(_ webView: WKWebView, didCommit navigation: WKNavigation!) {
		// Report as soon as the main frame commits so cross-site changes update
		// current_site_id without waiting for full load.
		reportCurrentMainFrameURL(webView, status: "navigating")
	}

	func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
		// Re-clear stale hints after navigation; controller also does this.
		webView.evaluateJavaScript("window.__tinyplayWebsite&&window.__tinyplayWebsite.clearHints&&window.__tinyplayWebsite.clearHints()", completionHandler: nil)
		reportCurrentMainFrameURL(webView, status: "navigated")
	}

	func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
		reportNavigationFailure(webView, error: error)
	}

	func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
		reportNavigationFailure(webView, error: error)
	}

	/// A load that never lands (unresolvable host, refused connection, TLS or ATS
	/// rejection) is otherwise completely silent: the window stays black and the
	/// phone keeps showing the site as opening. Report it so the phone can say the
	/// address failed instead of looking stuck. Cancellations are ordinary
	/// transport here — stopLoading() before a new load, and the policy .cancel
	/// that re-issues a target=_blank request — so they are not failures.
	private func reportNavigationFailure(_ webView: WKWebView, error: Error) {
		guard self.webView === webView else { return }
		let ns = error as NSError
		if ns.domain == NSURLErrorDomain && ns.code == NSURLErrorCancelled { return }
		// WebKitErrorFrameLoadInterruptedByPolicyChange (download / policy cancel).
		if ns.domain == "WebKitErrorDomain" && ns.code == 102 { return }
		report([
			"open": true,
			"status": "error",
			"error": "navigation_failed",
			"action": "navigation",
		])
	}

	func webView(
		_ webView: WKWebView,
		decidePolicyFor navigationAction: WKNavigationAction,
		decisionHandler: @escaping (WKNavigationActionPolicy) -> Void
	) {
		// Keep target=_blank navigations in the singleton WebView.
		if navigationAction.targetFrame == nil, let request = Optional(navigationAction.request) {
			webView.load(request)
			decisionHandler(.cancel)
			return
		}
		decisionHandler(.allow)
	}

	func webView(
		_ webView: WKWebView,
		createWebViewWith configuration: WKWebViewConfiguration,
		for navigationAction: WKNavigationAction,
		windowFeatures: WKWindowFeatures
	) -> WKWebView? {
		// Video sites frequently use target=_blank for search results. Keep that
		// navigation in the dedicated TV window instead of dropping the popup.
		if navigationAction.targetFrame == nil {
			webView.load(navigationAction.request)
		}
		return nil
	}
}

extension WebsiteShellController: NSWindowDelegate {
	func windowShouldClose(_ sender: NSWindow) -> Bool {
		// performClose / system close: tear down and report native close.
		closeWindow(reportNative: true)
		return false
	}

	func windowWillClose(_ notification: Notification) {
		if let w = notification.object as? NSWindow ?? window {
			DockVisibility.windowClosed(w)
		}
		let closingCommandID = windowCommandID
		windowCommandID = 0
		removeKeyMonitor()
		webView = nil
		window = nil
		var body: [String: Any] = ["open": false, "status": "closed", "action": "window_closed"]
		if closingCommandID > 0 { body["command_id"] = closingCommandID }
		report(body)
	}
}

#if STANDBY_RESTORE_SELFTEST
// Compile with: swiftc -D STANDBY_RESTORE_SELFTEST -o /tmp/standby-selftest \
//   macos/Sources/main.swift -framework AppKit -framework WebKit && /tmp/standby-selftest
private func runStandbyRestoreSelfTests() {
	func expect(_ step: StandbyRestoreStep, _ saw: Bool, _ running: Bool, _ file: StaticString = #file, _ line: UInt = #line) {
		let got = evaluateStandbyRestore(sawActivePlayback: saw, running: running)
		precondition(got == step, "expected \(step) got \(got) (saw=\(saw) running=\(running))", file: file, line: line)
	}

	// No session yet: idle regardless of running=false.
	expect(.idle, false, false)

	// First observation of playback arms the session.
	expect(.armSession, false, true)
	expect(.armSession, true, true)

	// First later running=false restores immediately — including during the
	// episode autoplay countdown (autoplay status is intentionally ignored).
	expect(.restore, true, false)

	// After eligibility is consumed (caller clears sawActivePlayback), idle.
	expect(.idle, false, false)

	// Parse helper.
	guard let parsedRunning = parsePlayerStateForStandby([
		"running": true,
		"playback_revision": NSNumber(value: 7),
		"autoplay_status": "finding_next",
	]) else { preconditionFailure("valid running state did not parse") }
	precondition(parsedRunning.running)
	precondition(parsedRunning.revision == 7)
	guard let parsedStopped = parsePlayerStateForStandby(["running": false, "playback_revision": 3]) else {
		preconditionFailure("valid stopped state did not parse")
	}
	precondition(!parsedStopped.running && parsedStopped.revision == 3)
	precondition(parsePlayerStateForStandby(["running": NSNumber(value: 0)]) == nil)
	precondition(parsePlayerStateForStandby(["playback_revision": 3]) == nil)

	fputs("standby restore self-test: ok\n", stdout)
}

runStandbyRestoreSelfTests()
#else
let app = NSApplication.shared
app.setActivationPolicy(.accessory) // menu-bar app, no Dock icon (LSUIElement)
let delegate = AppDelegate()
app.delegate = delegate
app.run()
#endif
