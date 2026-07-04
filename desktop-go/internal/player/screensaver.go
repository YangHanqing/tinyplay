package player

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tvremote/internal/i18n"
)

const (
	screensaverDelay         = 5 * time.Minute
	screensaverImageInterval = time.Minute
	screensaverBackdropID    = 61
	screensaverTextID        = 62
	screensaverMaxImages     = 12
	screensaverMaxWidth      = 1920
	screensaverMaxHeight     = 1080
)

type screensaverState struct {
	active       bool
	pauseStarted time.Time
	nextImageAt  time.Time
	imageIndex   int
	screenW      int
	screenH      int
	bufferW      int
	bufferH      int
	rawPath      string
}

func (p *Player) screensaverRun() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		p.updateScreensaver()
	}
}

func (p *Player) updateScreensaver() {
	running := p.isRunning()

	p.mu.Lock()
	ctx := p.ctx
	nativeMode := p.nativeMode
	p.mu.Unlock()

	p.propsMu.Lock()
	paused, pausedOK := p.liveProps["pause"].(bool)
	screenW := intProp(p.liveProps, "osd-width")
	screenH := intProp(p.liveProps, "osd-height")
	if screenW <= 0 {
		screenW = intProp(p.liveProps, "dwidth")
	}
	if screenH <= 0 {
		screenH = intProp(p.liveProps, "dheight")
	}
	p.propsMu.Unlock()

	if !running || nativeMode || ctx.ItemID == "" || !pausedOK || !paused {
		p.screensaverMu.Lock()
		p.screensaver.pauseStarted = time.Time{}
		p.screensaverMu.Unlock()
		p.hideScreensaver()
		return
	}

	if screenW <= 0 || screenH <= 0 {
		screenW, screenH = 1920, 1080
	}

	now := time.Now()
	p.screensaverMu.Lock()
	if p.screensaver.pauseStarted.IsZero() {
		p.screensaver.pauseStarted = now
		p.screensaverMu.Unlock()
		return
	}
	if now.Sub(p.screensaver.pauseStarted) < screensaverDelay {
		p.screensaverMu.Unlock()
		return
	}
	needsImage := !p.screensaver.active ||
		now.After(p.screensaver.nextImageAt) ||
		p.screensaver.screenW != screenW ||
		p.screensaver.screenH != screenH
	p.screensaver.active = true
	p.screensaver.screenW = screenW
	p.screensaver.screenH = screenH
	p.screensaverMu.Unlock()

	if needsImage {
		p.refreshScreensaverBackdrop(ctx.ItemID, ctx.PosterItemID, screenW, screenH)
	}
	p.refreshScreensaverText(ctx, screenW, screenH, now)
}

func (p *Player) refreshScreensaverBackdrop(itemID, posterItemID string, screenW, screenH int) {
	p.screensaverMu.Lock()
	imageIndex := p.screensaver.imageIndex % screensaverMaxImages
	p.screensaver.imageIndex = (p.screensaver.imageIndex + 1) % screensaverMaxImages
	p.screensaver.nextImageAt = time.Now().Add(screensaverImageInterval)
	p.screensaverMu.Unlock()

	var data []byte
	if p.ScreensaverImageProvider != nil {
		data = p.ScreensaverImageProvider(itemID, posterItemID, imageIndex)
		if data == nil && imageIndex != 0 {
			data = p.ScreensaverImageProvider(itemID, posterItemID, 0)
		}
	}

	bufferW, bufferH := overlayBufferSize(screenW, screenH)
	raw := renderBackdropOverlay(data, bufferW, bufferH)
	path, err := writeScreensaverRaw(raw)
	if err != nil {
		return
	}

	p.sendNative(map[string]any{
		"_name":  "overlay-add",
		"id":     screensaverBackdropID,
		"x":      0,
		"y":      0,
		"file":   path,
		"offset": 0,
		"fmt":    "bgra",
		"w":      bufferW,
		"h":      bufferH,
		"stride": bufferW * 4,
		"dw":     screenW,
		"dh":     screenH,
	})

	p.screensaverMu.Lock()
	oldPath := p.screensaver.rawPath
	p.screensaver.rawPath = path
	p.screensaver.bufferW = bufferW
	p.screensaver.bufferH = bufferH
	p.screensaverMu.Unlock()
	if oldPath != "" && oldPath != path {
		_ = os.Remove(oldPath)
	}
}

func (p *Player) refreshScreensaverText(ctx PlayContext, screenW, screenH int, now time.Time) {
	p.sendNative(map[string]any{
		"_name":  "osd-overlay",
		"id":     screensaverTextID,
		"format": "ass-events",
		"data":   screensaverASS(ctx, screenW, screenH, now),
		"res_x":  screenW,
		"res_y":  screenH,
		"z":      10,
	})
}

func (p *Player) dismissScreensaver() {
	p.screensaverMu.Lock()
	if !p.screensaver.pauseStarted.IsZero() {
		p.screensaver.pauseStarted = time.Now()
	}
	p.screensaverMu.Unlock()
	p.hideScreensaver()
}

func (p *Player) hideScreensaver() {
	p.screensaverMu.Lock()
	wasActive := p.screensaver.active
	rawPath := p.screensaver.rawPath
	p.screensaver.active = false
	p.screensaver.nextImageAt = time.Time{}
	p.screensaver.rawPath = ""
	p.screensaverMu.Unlock()

	if !wasActive && rawPath == "" {
		return
	}
	p.send([]any{"overlay-remove", screensaverBackdropID})
	p.sendNative(map[string]any{
		"_name":  "osd-overlay",
		"id":     screensaverTextID,
		"format": "none",
	})
	if rawPath != "" {
		_ = os.Remove(rawPath)
	}
}

func intProp(props map[string]any, key string) int {
	switch v := props[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

func overlayBufferSize(screenW, screenH int) (int, int) {
	if screenW <= 0 || screenH <= 0 {
		return 1920, 1080
	}
	scale := math.Min(float64(screensaverMaxWidth)/float64(screenW), float64(screensaverMaxHeight)/float64(screenH))
	if scale > 1 {
		scale = 1
	}
	w := max(2, int(math.Round(float64(screenW)*scale)))
	h := max(2, int(math.Round(float64(screenH)*scale)))
	return w, h
}

func renderBackdropOverlay(data []byte, w, h int) []byte {
	out := make([]byte, w*h*4)
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		fillDimBlack(out)
		return out
	}

	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW <= 0 || srcH <= 0 {
		fillDimBlack(out)
		return out
	}
	scale := math.Max(float64(w)/float64(srcW), float64(h)/float64(srcH))
	cropW := float64(w) / scale
	cropH := float64(h) / scale
	startX := float64(b.Min.X) + (float64(srcW)-cropW)/2
	startY := float64(b.Min.Y) + (float64(srcH)-cropH)/2

	for y := 0; y < h; y++ {
		sy := clampInt(int(startY+float64(y)/scale), b.Min.Y, b.Max.Y-1)
		for x := 0; x < w; x++ {
			sx := clampInt(int(startX+float64(x)/scale), b.Min.X, b.Max.X-1)
			r, g, bl, _ := img.At(sx, sy).RGBA()
			i := (y*w + x) * 4
			out[i+0] = byte((bl >> 8) * 42 / 100)
			out[i+1] = byte((g >> 8) * 42 / 100)
			out[i+2] = byte((r >> 8) * 42 / 100)
			out[i+3] = 168
		}
	}
	return out
}

func fillDimBlack(out []byte) {
	for i := 0; i+3 < len(out); i += 4 {
		out[i+0] = 0
		out[i+1] = 0
		out[i+2] = 0
		out[i+3] = 168
	}
}

func writeScreensaverRaw(data []byte) (string, error) {
	dir := filepath.Join(os.TempDir(), "tvremote-screensaver")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "backdrop-*.bgra")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func screensaverASS(ctx PlayContext, screenW, screenH int, now time.Time) string {
	marginX := max(42, screenW/24)
	marginY := max(42, screenH/18)
	x := screenW - marginX
	y := screenH - marginY
	timeText := now.Format("15:04")
	dateText := now.Format("2006-01-02") + " " + localizedWeekday(now.Weekday())
	title := ctx.Title
	if ctx.EpisodeLabel != "" && ctx.SeriesTitle != "" {
		title = ctx.SeriesTitle + "  " + ctx.EpisodeLabel
	} else if ctx.SeriesTitle != "" {
		title = ctx.SeriesTitle
	}
	return fmt.Sprintf(
		`{\an3\pos(%d,%d)\fs72\bord1\shad0\c&HDDDDDD&\alpha&H30&}%s\N{\fs28\alpha&H60&}%s\N{\fs24\alpha&H70&}%s`,
		x, y, assEscape(timeText), assEscape(dateText), assEscape(title),
	)
}

func localizedWeekday(w time.Weekday) string {
	switch w {
	case time.Monday:
		return i18n.System("weekday_monday")
	case time.Tuesday:
		return i18n.System("weekday_tuesday")
	case time.Wednesday:
		return i18n.System("weekday_wednesday")
	case time.Thursday:
		return i18n.System("weekday_thursday")
	case time.Friday:
		return i18n.System("weekday_friday")
	case time.Saturday:
		return i18n.System("weekday_saturday")
	default:
		return i18n.System("weekday_sunday")
	}
}

func assEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "{", `\{`)
	s = strings.ReplaceAll(s, "}", `\}`)
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
