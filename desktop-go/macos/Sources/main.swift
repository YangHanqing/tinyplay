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
import WebKit
import Network

private func L(_ key: String) -> String {
	let preference = UserDefaults.standard.string(forKey: "TinyPlayLanguage") ?? "auto"
	let zh = preference == "zh-CN" || (preference == "auto" && Locale.current.language.languageCode?.identifier.lowercased().hasPrefix("zh") == true)
	let table: [String: (String, String)] = [
        "open_main": ("\u{6253}\u{5F00}\u{4E3B}\u{754C}\u{9762}", "Open Main Interface"),
        "open_logs": ("\u{6253}\u{5F00}\u{65E5}\u{5FD7}\u{76EE}\u{5F55}", "Open Logs"),
		"quit": ("\u{9000}\u{51FA}", "Quit"),
		"language": ("\u{8BED}\u{8A00}", "Language"),
		"automatic": ("\u{81EA}\u{52A8}", "Automatic"),
		"chinese": ("\u{4E2D}\u{6587}", "Chinese"),
		"about": ("\u{5173}\u{4E8E} TinyPlay", "About TinyPlay"),
		"version_label": ("\u{7248}\u{672C}", "Version"),
		"third_party_notices": ("\u{67E5}\u{770B}\u{7B2C}\u{4E09}\u{65B9}\u{58F0}\u{660E}", "View Third-Party Notices"),
		"ok": ("\u{597D}\u{7684}", "OK"),
    ]
    guard let pair = table[key] else { return key }
    return zh ? pair.0 : pair.1
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem!
    private var window: NSWindow?
    private let core = Process()
    private var coreURL: String = "http://127.0.0.1:1980"
    private let urlFile = NSTemporaryDirectory() + "tvremote-url-\(ProcessInfo.processInfo.processIdentifier).txt"
	private var lanBrowser: NWBrowser?
	private var webView: WKWebView?

    func applicationDidFinishLaunching(_ notification: Notification) {
        primeLocalNetworkAccess()
        startCore()
        setupMenuBar()
        // Show the QR window once on first launch so the user sees it immediately.
        waitForCoreURL { [weak self] in self?.openMainWindow() }
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
        browser.stateUpdateHandler = { _ in }
        browser.browseResultsChangedHandler = { _, _ in }
        browser.start(queue: .main)
        lanBrowser = browser // retain so it keeps running (and the prompt stays)
    }

    func applicationWillTerminate(_ notification: Notification) {
        if core.isRunning { core.terminate() }
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
		for (value, title) in [("auto", L("automatic")), ("zh-CN", L("chinese")), ("en", "English")] {
			let item = NSMenuItem(title: title, action: #selector(changeLanguage(_:)), keyEquivalent: "")
			item.representedObject = value
			item.state = value == selected ? .on : .off
			languageMenu.addItem(item)
		}
		language.submenu = languageMenu
		menu.addItem(language)
        menu.addItem(.separator())
        menu.addItem(NSMenuItem(title: L("about"), action: #selector(showAbout), keyEquivalent: ""))
        menu.addItem(NSMenuItem(title: L("quit"), action: #selector(quit), keyEquivalent: "q"))
		statusItem.menu = menu
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
		let webView = WKWebView(frame: NSRect(x: 0, y: 0, width: 380, height: 600))
		webView.load(URLRequest(url: desktopURL()))
		self.webView = webView

        let w = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 380, height: 600),
            styleMask: [.titled, .closable, .miniaturizable],
            backing: .buffered, defer: false)
        w.title = "TinyPlay"
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
		let resolved = preference == "auto"
			? (Locale.current.language.languageCode?.identifier.lowercased().hasPrefix("zh") == true ? "zh-CN" : "en")
			: preference
		return URL(string: coreURL + "/desktop?lang=" + resolved)!
	}

    @objc private func quit() {
        NSApp.terminate(nil)
    }
}

extension AppDelegate: NSWindowDelegate {
	func windowWillClose(_ notification: Notification) {
		window = nil
		webView = nil
    }
}

let app = NSApplication.shared
app.setActivationPolicy(.accessory) // menu-bar app, no Dock icon (LSUIElement)
let delegate = AppDelegate()
app.delegate = delegate
app.run()
