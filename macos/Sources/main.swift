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
//   2. Show a menu-bar item with entries for the remote, logs, and quit.
//   3. The remote item opens a small native window with the intro + QR page served by
//      the core at /desktop.
//   4. Terminate the sidecar on quit.

import AppKit
import Foundation
import WebKit
import Network

private func L(_ key: String) -> String {
	let preference = UserDefaults.standard.string(forKey: "TinyPlayLanguage") ?? "auto"
	let zh = preference == "zh-CN" || (preference == "auto" && Locale.current.language.languageCode?.identifier.lowercased().hasPrefix("zh") == true)
	let table: [String: (String, String)] = [
        "open_main": ("\u{6253}\u{5F00}\u{4E3B}\u{754C}\u{9762}", "Open Main Interface"),
        "open_logs": ("\u{6253}\u{5F00}\u{65E5}\u{5FD7}\u{76EE}\u{5F55}", "Open Logs"),
		"settings": ("\u{8BBE}\u{7F6E}", "Settings"),
		"dlna_receiver": ("DLNA \u{63A5}\u{6536}\u{5668}", "DLNA Receiver"),
		"quit": ("\u{9000}\u{51FA}", "Quit"),
		"language": ("\u{8BED}\u{8A00}", "Language"),
		"automatic": ("\u{81EA}\u{52A8}", "Automatic"),
		"chinese": ("\u{4E2D}\u{6587}", "Chinese"),
		"about": ("\u{5173}\u{4E8E} TinyPlay", "About TinyPlay"),
		"version_label": ("\u{7248}\u{672C}", "Version"),
		"third_party_notices": ("\u{67E5}\u{770B}\u{7B2C}\u{4E09}\u{65B9}\u{58F0}\u{660E}", "View Third-Party Notices"),
		"check_updates": ("\u{68C0}\u{67E5}\u{66F4}\u{65B0}\u{2026}", "Check for Updates…"),
		"update_available_title": ("\u{6709}\u{65B0}\u{7248}\u{672C}\u{53EF}\u{7528}", "Update Available"),
		"update_available_body": ("TinyPlay %@ \u{5DF2}\u{53D1}\u{5E03}\u{3002}\n\u{5F53}\u{524D}\u{7248}\u{672C}\u{FF1A}%@", "TinyPlay %@ is available.\nCurrent version: %@"),
		"update_download": ("\u{6253}\u{5F00}\u{4E0B}\u{8F7D}\u{9875}\u{9762}", "Open Download Page"),
		"update_remind": ("3 \u{5929}\u{540E}\u{63D0}\u{9192}", "Remind Me in 3 Days"),
		"update_skip": ("\u{8DF3}\u{8FC7}\u{6B64}\u{7248}\u{672C}", "Skip This Version"),
		"update_latest": ("\u{4F60}\u{6B63}\u{5728}\u{4F7F}\u{7528}\u{6700}\u{65B0}\u{7248}\u{672C}\u{3002}", "You're using the latest version."),
		"update_failed": ("\u{6682}\u{65F6}\u{65E0}\u{6CD5}\u{68C0}\u{67E5}\u{66F4}\u{65B0}\u{FF0C}\u{8BF7}\u{7A0D}\u{540E}\u{518D}\u{8BD5}\u{3002}", "Couldn't check for updates. Please try again later."),
		"ok": ("\u{597D}\u{7684}", "OK"),
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

final class AppDelegate: NSObject, NSApplicationDelegate, WKScriptMessageHandler, WKNavigationDelegate {
    private var statusItem: NSStatusItem!
    private var window: NSWindow?
    private let core = Process()
    private var coreURL: String = "http://127.0.0.1:1980"
    private let urlFile = NSTemporaryDirectory() + "tvremote-url-\(ProcessInfo.processInfo.processIdentifier).txt"
	private var lanBrowser: NWBrowser?
	private var webView: WKWebView?
	private var localNetworkDenied = false
	private var dlnaMenuItem: NSMenuItem?
	private static let fullscreenMessageName = "tinyplaySetFullscreen"
	private let compactContentSize = NSSize(width: 380, height: 600)
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

    func applicationDidFinishLaunching(_ notification: Notification) {
        primeLocalNetworkAccess()
        startCore()
        setupMenuBar()
        // Show the QR window once on first launch so the user sees it immediately.
        waitForCoreURL { [weak self] in
			self?.startPlaybackStandbyMonitor()
			self?.startWebsiteShell()
			self?.openMainWindow()
			self?.scheduleAutomaticUpdateCheck()
		}
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
        if core.isRunning { core.terminate() }
    }

	// MARK: - Website playback window (experimental)

	private var websiteShell: WebsiteShellController?

	private func startWebsiteShell() {
		websiteShell?.stop()
		let shell = WebsiteShellController(coreURL: loopbackCoreURL(coreURL))
		websiteShell = shell
		shell.start()
	}

	private func stopWebsiteShell() {
		websiteShell?.stop()
		websiteShell = nil
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
    private func waitForCoreURL(then ready: @escaping () -> Void) {
        var attempts = 0
        func poll() {
            attempts += 1
            if let s = try? String(contentsOfFile: urlFile, encoding: .utf8),
               !s.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                coreURL = s.trimmingCharacters(in: .whitespacesAndNewlines)
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
		menu.addItem(NSMenuItem(title: L("open_logs"), action: #selector(openLogs), keyEquivalent: ""))
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
		let settings = NSMenuItem(title: L("settings"), action: nil, keyEquivalent: "")
		let settingsMenu = NSMenu()
		let dlna = NSMenuItem(title: L("dlna_receiver"), action: #selector(toggleDLNAReceiver(_:)), keyEquivalent: "")
		dlna.target = self
		dlna.state = .off
		settingsMenu.addItem(dlna)
		settings.submenu = settingsMenu
		dlnaMenuItem = dlna
		menu.addItem(settings)
        menu.addItem(.separator())
		menu.addItem(NSMenuItem(title: L("check_updates"), action: #selector(checkForUpdates), keyEquivalent: ""))
        menu.addItem(NSMenuItem(title: L("about"), action: #selector(showAbout), keyEquivalent: ""))
        menu.addItem(NSMenuItem(title: L("quit"), action: #selector(quit), keyEquivalent: "q"))
		statusItem.menu = menu
	}

	// MARK: - Updates

	private func appVersion() -> String {
		Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.0.0"
	}

	private func scheduleAutomaticUpdateCheck() {
		DispatchQueue.main.asyncAfter(deadline: .now() + 8) { [weak self] in
			self?.performUpdateCheck(manual: false)
		}
	}

	@objc private func checkForUpdates() {
		performUpdateCheck(manual: true)
	}

	private func performUpdateCheck(manual: Bool) {
		guard !updateCheckInFlight, parseTinyPlayVersion(appVersion()) != nil else { return }
		updateCheckInFlight = true
		let currentVersion = appVersion()
		fetchLatestTinyPlayUpdate { [weak self] update in
			DispatchQueue.main.async {
				guard let self else { return }
				self.updateCheckInFlight = false
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
		let webView = WKWebView(frame: NSRect(origin: .zero, size: compactContentSize), configuration: config)
		webView.navigationDelegate = self
		webView.load(URLRequest(url: desktopURL()))
		self.webView = webView

        let w = NSWindow(
            contentRect: NSRect(origin: .zero, size: compactContentSize),
            styleMask: [.titled, .closable, .resizable],
            backing: .buffered, defer: false)
        w.title = "TinyPlay"
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
        w.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
	}

	private func desktopURL() -> URL {
		let preference = UserDefaults.standard.string(forKey: "TinyPlayLanguage") ?? "auto"
		let resolved = resolvedWebLanguage(preference)
		var components = URLComponents(string: coreURL + "/desktop")!
		var query = [URLQueryItem(name: "lang", value: resolved)]
		if localNetworkDenied {
			query.append(URLQueryItem(name: "local_network", value: "denied"))
		}
		components.queryItems = query
		return components.url!
	}

	/// Bridge from the /desktop page: request native NSWindow full screen so
	/// standby covers the display, not just the compact 380×600 content view.
	func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
		guard message.name == Self.fullscreenMessageName else { return }
		let enter: Bool
		if let value = message.body as? Bool {
			enter = value
		} else if let number = message.body as? NSNumber {
			enter = number.boolValue
		} else {
			return
		}
		setNativeFullscreen(enter)
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
		webView?.configuration.userContentController.removeScriptMessageHandler(forName: Self.fullscreenMessageName)
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
}

extension AppDelegate {
	/// After a page reload while already full screen, re-apply the standby layout.
	func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
		if window?.styleMask.contains(.fullScreen) == true {
			notifyPageFullscreen(true)
		}
	}
}

// MARK: - Website shell (loopback command poll + Safari-style WKWebView)

/// Prefer 127.0.0.1 so /desktop/website/* loopback guards accept shell traffic
/// even when the advertised phone URL is a LAN address.
private func loopbackCoreURL(_ coreURL: String) -> String {
	guard let url = URL(string: coreURL), let port = url.port else {
		return "http://127.0.0.1:1980"
	}
	return "http://127.0.0.1:\(port)"
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
		case "login":
			// Broker-supplied fixed login route only; never phone-provided free-form URL.
			guard let destination = URL(string: url), !url.isEmpty else {
				report(["status": "error", "error": "login_unavailable", "action": "login", "command_id": id])
				return
			}
			webView?.load(URLRequest(url: destination))
			report(["open": true, "status": "login", "action": "login", "command_id": id])
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
