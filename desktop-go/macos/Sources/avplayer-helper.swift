// avplayer-helper — a tiny native macOS video player used only as the Dolby
// Vision Profile 5 fallback for the TV Remote MPV app.
//
// Why it exists: mpv (and IINA) render single-layer Dolby Vision Profile 5 with
// a purple tint on macOS because they don't process the DV RPU. Apple's
// AVFoundation has a licensed DV decoder and renders it correctly via the system
// EDR pipeline. So for DV P5 the Go core spawns this helper instead of mpv,
// fed by an Emby remux (stream-copy) URL — the HEVC+DV bitstream is preserved.
//
// To avoid touching the rest of the app, this helper is wire-compatible with the
// subset of mpv's JSON IPC that the Go core and phone frontend actually use:
//   · listens on the --input-ipc-server=<unix socket> path (it is the server),
//   · accepts {"command":[...]} lines (loadfile/seek/set_property/quit/…),
//   · emits {"event":"property-change","name":...,"data":...} for the handful of
//     properties the core observes (time-pos, duration, pause, volume, …).
// Anything it doesn't understand is ignored — those controls simply no-op, which
// is the accepted, narrow degradation for this fallback path.

import AppKit
import AVKit
import AVFoundation
import Darwin
import QuartzCore

// ── argument parsing (mpv-style) ─────────────────────────────────────────────

var mediaURL: String = ""
var socketPath: String = ""
var startSeconds: Double = 0

for arg in CommandLine.arguments.dropFirst() {
    if arg.hasPrefix("--input-ipc-server=") {
        socketPath = String(arg.dropFirst("--input-ipc-server=".count))
    } else if arg.hasPrefix("--start=") {
        startSeconds = Double(arg.dropFirst("--start=".count)) ?? 0
    } else if !arg.hasPrefix("--") && mediaURL.isEmpty {
        mediaURL = arg
    }
}

guard !mediaURL.isEmpty, let url = URL(string: mediaURL) else {
    FileHandle.standardError.write("avplayer-helper: missing media URL\n".data(using: .utf8)!)
    exit(2)
}

// ── IPC server (mpv-compatible JSON over a unix socket) ──────────────────────

final class IPCServer {
    private var clientFD: Int32 = -1
    private let writeLock = NSLock()
    private let onCommand: ([Any]) -> Void

    init(path: String, onCommand: @escaping ([Any]) -> Void) {
        self.onCommand = onCommand
        Thread.detachNewThread { [weak self] in self?.serve(path: path) }
    }

    private func serve(path: String) {
        unlink(path)
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { return }
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        let cap = MemoryLayout.size(ofValue: addr.sun_path) // 104 on Darwin
        _ = path.withCString { src in
            withUnsafeMutablePointer(to: &addr.sun_path) { dst in
                dst.withMemoryRebound(to: CChar.self, capacity: cap) {
                    strncpy($0, src, cap - 1)
                }
            }
        }
        let len = socklen_t(MemoryLayout<sockaddr_un>.size)
        let ok = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { bind(fd, $0, len) }
        }
        guard ok == 0, listen(fd, 1) == 0 else { close(fd); return }
        while true {
            let conn = accept(fd, nil, nil)
            if conn < 0 { continue }
            writeLock.lock(); clientFD = conn; writeLock.unlock()
            readLoop(conn)
        }
    }

    private func readLoop(_ conn: Int32) {
        var buffer = Data()
        var chunk = [UInt8](repeating: 0, count: 4096)
        while true {
            let n = read(conn, &chunk, chunk.count)
            if n <= 0 { break }
            buffer.append(contentsOf: chunk[0..<n])
            while let nl = buffer.firstIndex(of: 0x0A) {
                let line = buffer.subdata(in: buffer.startIndex..<nl)
                buffer.removeSubrange(buffer.startIndex...nl)
                handle(line)
            }
        }
    }

    private func handle(_ line: Data) {
        guard
            let obj = try? JSONSerialization.jsonObject(with: line) as? [String: Any],
            let command = obj["command"] as? [Any]
        else { return }
        DispatchQueue.main.async { self.onCommand(command) }
    }

    /// Emit an mpv-style property-change event.
    func emit(_ name: String, _ data: Any) {
        let payload: [String: Any] = ["event": "property-change", "name": name, "data": data]
        guard
            let json = try? JSONSerialization.data(withJSONObject: payload),
            var bytes = String(data: json, encoding: .utf8)
        else { return }
        bytes += "\n"
        let out = [UInt8](bytes.utf8)
        writeLock.lock()
        let fd = clientFD
        if fd >= 0 { _ = out.withUnsafeBytes { write(fd, $0.baseAddress, out.count) } }
        writeLock.unlock()
    }
}

// ── player ───────────────────────────────────────────────────────────────────

private enum AspectMode {
    case fit
    case zoom
    case stretch
    case original
}

private final class PlayerRenderView: NSView {
    let playerLayer = AVPlayerLayer()
    private var aspectMode: AspectMode = .fit

    override init(frame frameRect: NSRect) {
        super.init(frame: frameRect)
        configure()
    }

    required init?(coder: NSCoder) {
        super.init(coder: coder)
        configure()
    }

    private func configure() {
        wantsLayer = true
        layer?.backgroundColor = NSColor.black.cgColor
        playerLayer.videoGravity = .resizeAspect
        layer?.addSublayer(playerLayer)
    }

    func setAspect(_ mode: AspectMode) {
        aspectMode = mode
        needsLayout = true
        layoutSubtreeIfNeeded()
    }

    override func layout() {
        super.layout()
        layoutPlayerLayer()
    }

    private func layoutPlayerLayer() {
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        defer { CATransaction.commit() }

        switch aspectMode {
        case .fit:
            playerLayer.videoGravity = .resizeAspect
            playerLayer.frame = bounds
        case .zoom:
            playerLayer.videoGravity = .resizeAspectFill
            playerLayer.frame = bounds
        case .stretch:
            playerLayer.videoGravity = .resize
            playerLayer.frame = bounds
        case .original:
            playerLayer.videoGravity = .resizeAspect
            let natural = playerLayer.player?.currentItem?.presentationSize ?? .zero
            let width = natural.width.isFinite && natural.width > 0 ? natural.width : bounds.width
            let height = natural.height.isFinite && natural.height > 0 ? natural.height : bounds.height
            playerLayer.bounds = CGRect(origin: .zero, size: CGSize(width: width, height: height))
            playerLayer.position = CGPoint(x: bounds.midX, y: bounds.midY)
        }
    }
}

final class PlayerController: NSObject {
    let player = AVPlayer()
    private var ipc: IPCServer!
    private var window: NSWindow!
    private var playerView: PlayerRenderView!
    private var desiredRate: Float = 1.0
    private var didSeekToStart = false
    private var statusObs: NSKeyValueObservation?
    private var panscan = 0.0
    private var keepaspect = true
    private var unscaled = false

    func start(url: URL, socketPath: String, startAt: Double) {
        // Window + AVPlayerLayer-backed render view, fullscreen.
        let view = PlayerRenderView(frame: NSRect(x: 0, y: 0, width: 1280, height: 720))
        view.playerLayer.player = player
        playerView = view

        window = NSWindow(
            contentRect: view.frame,
            styleMask: [.titled, .closable, .resizable, .miniaturizable],
            backing: .buffered, defer: false)
        window.title = "TV Remote (Dolby Vision)"
        window.contentView = view
        window.delegate = self
        window.center()
        window.makeKeyAndOrderFront(nil)
        window.toggleFullScreen(nil)
        NSApp.activate(ignoringOtherApps: true)

        replaceItem(url: url, startAt: startAt)

        // IPC server (only after the player exists).
        if !socketPath.isEmpty {
            ipc = IPCServer(path: socketPath) { [weak self] cmd in self?.dispatch(cmd) }
        }

        // ~0.5s heartbeat: feed the Go core the properties it observes.
        player.addPeriodicTimeObserver(
            forInterval: CMTime(seconds: 0.5, preferredTimescale: 600),
            queue: .main
        ) { [weak self] _ in self?.tick() }

        // ESC closes; Cmd-W too (handled by .closable).
        NSEvent.addLocalMonitorForEvents(matching: .keyDown) { ev in
            if ev.keyCode == 53 { NSApp.terminate(nil); return nil }
            return ev
        }
    }

    private func replaceItem(url: URL, startAt: Double) {
        let item = AVPlayerItem(url: url)
        didSeekToStart = (startAt <= 0)
        statusObs = item.observe(\.status, options: [.new]) { [weak self] it, _ in
            guard let self = self, it.status == .readyToPlay, !self.didSeekToStart else { return }
            self.didSeekToStart = true
            self.playerView.needsLayout = true
            self.player.seek(to: CMTime(seconds: startAt, preferredTimescale: 600))
        }
        NotificationCenter.default.addObserver(
            self, selector: #selector(playedToEnd),
            name: .AVPlayerItemDidPlayToEndTime, object: item)
        player.replaceCurrentItem(with: item)
        player.play()
        player.rate = desiredRate
    }

    @objc private func playedToEnd() { NSApp.terminate(nil) }

    private func tick() {
        guard let item = player.currentItem else { return }
        let pos = CMTimeGetSeconds(player.currentTime())
        if pos.isFinite { ipc?.emit("time-pos", pos) }
        let dur = CMTimeGetSeconds(item.duration)
        if dur.isFinite && dur > 0 {
            ipc?.emit("duration", dur)
            if pos.isFinite { ipc?.emit("percent-pos", max(0, min(100, pos / dur * 100))) }
        }
        ipc?.emit("pause", player.rate == 0)
        ipc?.emit("core-idle", player.rate == 0)
        ipc?.emit("paused-for-cache", player.timeControlStatus == .waitingToPlayAtSpecifiedRate)
        ipc?.emit("volume", Double(player.volume) * 100)
        ipc?.emit("mute", player.isMuted)
        ipc?.emit("speed", Double(desiredRate))
    }

    // ── command dispatch (mpv JSON IPC subset) ────────────────────────────────

    private func dispatch(_ cmd: [Any]) {
        guard let verb = cmd.first as? String else { return }
        switch verb {
        case "quit", "stop":
            NSApp.terminate(nil)
        case "loadfile":
            if cmd.count >= 2, let u = cmd[1] as? String, let url = URL(string: u) {
                replaceItem(url: url, startAt: 0)
            }
        case "seek":
            if cmd.count >= 2, let secs = toDouble(cmd[1]) {
                let mode = (cmd.count >= 3 ? cmd[2] as? String : nil) ?? "relative"
                let base = mode.hasPrefix("absolute") ? 0 : CMTimeGetSeconds(player.currentTime())
                player.seek(to: CMTime(seconds: base + secs, preferredTimescale: 600))
            }
        case "set_property", "set":
            if cmd.count >= 3, let name = cmd[1] as? String { setProperty(name, cmd[2]) }
        default:
            break // observe_property and unsupported verbs are ignored
        }
    }

    private func setProperty(_ name: String, _ value: Any) {
        switch name {
        case "pause":
            if asBool(value) { player.pause() }
            else { player.play(); player.rate = desiredRate }
        case "volume":
            if let v = toDouble(value) { player.volume = Float(max(0, min(100, v)) / 100) }
        case "mute":
            player.isMuted = asBool(value)
        case "speed":
            if let v = toDouble(value), v > 0 {
                desiredRate = Float(v)
                if player.rate != 0 { player.rate = desiredRate }
            }
        case "panscan":
            panscan = toDouble(value) ?? 0
            recomputeAspect()
        case "keepaspect":
            keepaspect = asBool(value)
            recomputeAspect()
        case "video-unscaled":
            unscaled = asBool(value)
            recomputeAspect()
        case "video-aspect-override":
            recomputeAspect()
        default:
            break // start, title, sub-delay, … not supported here
        }
    }

    private func recomputeAspect() {
        if unscaled { playerView.setAspect(.original) }
        else if !keepaspect { playerView.setAspect(.stretch) }
        else if panscan >= 1 { playerView.setAspect(.zoom) }
        else { playerView.setAspect(.fit) }
    }
}

extension PlayerController: NSWindowDelegate {
    func windowWillClose(_ notification: Notification) { NSApp.terminate(nil) }
}

// ── helpers ───────────────────────────────────────────────────────────────────

func toDouble(_ v: Any) -> Double? {
    if let d = v as? Double { return d }
    if let i = v as? Int { return Double(i) }
    if let n = v as? NSNumber { return n.doubleValue }
    if let s = v as? String { return Double(s) }
    return nil
}

func asBool(_ v: Any) -> Bool {
    if let b = v as? Bool { return b }
    if let s = v as? String { return s == "yes" || s == "true" }
    if let d = toDouble(v) { return d != 0 }
    return false
}

// ── entry point ────────────────────────────────────────────────────────────────

let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let controller = PlayerController()
let delegate = HelperAppDelegate(controller: controller, url: url, socket: socketPath, start: startSeconds)
app.delegate = delegate
app.run()

final class HelperAppDelegate: NSObject, NSApplicationDelegate {
    let controller: PlayerController
    let url: URL
    let socket: String
    let start: Double
    init(controller: PlayerController, url: URL, socket: String, start: Double) {
        self.controller = controller; self.url = url; self.socket = socket; self.start = start
    }
    func applicationDidFinishLaunching(_ notification: Notification) {
        controller.start(url: url, socketPath: socket, startAt: start)
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
}
