// Next-file autoplay for folder-based file sources (local/SMB/WebDAV/NFS).
// Pure decision table — no I/O. Mirrored decision-for-decision by
// appletv-swift/TV-Remote/Player/AutoplayNextFile.swift; a rule learned on
// one side belongs in both test files.
package filesource

import (
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// MaxAutoplayDirEntries is a safety ceiling for one parent-directory listing.
// Above this, autoplay gives up rather than scanning a pathological folder.
const MaxAutoplayDirEntries = 500

// isoAutoplayExclude is playable manually but never chained — Blu-ray/BDMV
// structure is not suitable for sequential file autoplay.
const isoAutoplayExclude = ".iso"

// NextVideo picks the next autoplay candidate after currentPath within the
// same directory listing. Never crosses directories. Candidates are ordered by
// filename using natural numeric order, regardless of naming convention.
//
// Chaining is same-kind only: video → next video, audio → next audio. An
// album must not jump into a stray video file, and a TV episode must not
// jump into an mp3 sitting in the same folder.
func NextVideo(entries []Entry, currentPath string) (Entry, bool) {
	if len(entries) == 0 || strings.TrimSpace(currentPath) == "" {
		return Entry{}, false
	}
	if len(entries) > MaxAutoplayDirEntries {
		return Entry{}, false
	}
	currentPath = normalizeAutoplayPath(currentPath)

	var current Entry
	foundCurrent := false
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		p := normalizeAutoplayPath(e.Path)
		if p == currentPath {
			current = e
			current.Path = p
			foundCurrent = true
			break
		}
	}
	if !foundCurrent {
		return Entry{}, false
	}
	// Current may itself be filtered out (iso/extra/neither kind); then there
	// is no chain. Kind is taken from the finished item so a mixed folder
	// never crosses audio↔video.
	if !isAutoplayCandidate(current) {
		return Entry{}, false
	}
	wantAudio := current.IsAudio

	candidates := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		// Same-kind only (audio stays in the album; video stays in the season).
		if e.IsAudio != wantAudio {
			continue
		}
		if !isAutoplayCandidate(e) {
			continue
		}
		cp := e
		cp.Path = normalizeAutoplayPath(e.Path)
		candidates = append(candidates, cp)
	}
	if len(candidates) == 0 {
		return Entry{}, false
	}

	sortAutoplayCandidates(candidates)

	idx := -1
	for i, e := range candidates {
		if normalizeAutoplayPath(e.Path) == currentPath {
			idx = i
			break
		}
	}
	if idx < 0 || idx+1 >= len(candidates) {
		return Entry{}, false
	}
	return candidates[idx+1], true
}

// isAutoplayCandidate keeps only playable media of one kind: never
// directories, subtitles, images, metadata, or .iso. Extras (sample/trailer/
// OP/ED/…) are excluded from the chain too. Audio and video both use this
// table; same-kind filtering is done by the caller.
func isAutoplayCandidate(e Entry) bool {
	if e.IsDir {
		return false
	}
	// Exactly one of IsVideo / IsAudio — never both, never neither.
	if e.IsVideo == e.IsAudio {
		return false
	}
	ext := strings.ToLower(filepath.Ext(e.Name))
	if ext == "" {
		ext = strings.ToLower(filepath.Ext(e.Path))
	}
	if ext == isoAutoplayExclude {
		return false
	}
	if isAutoplayExtra(e.Name) {
		return false
	}
	return true
}

var (
	// OP / ED opening-ending extras. Token boundaries avoid matching inside
	// ordinary words (OPENING, WEDDING, FEED).
	reAutoplayOP = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])(?:NC)?OP\d*(?:$|[^A-Za-z0-9])`)
	reAutoplayED = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])ED\d*(?:$|[^A-Za-z0-9])`)

	// Optional episode extraction is retained only for gap diagnostics.
	reEpSxxExx   = regexp.MustCompile(`(?i)S(\d{1,3})E(\d{1,4})`)
	reEpNxNN     = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])(\d{1,2})x(\d{1,4})(?:$|[^A-Za-z0-9])`)
	reEpEPorE    = regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9])(?:EP|E)(\d{1,4})(?:$|[^A-Za-z0-9])`)
	reEpChinese  = regexp.MustCompile(`第(\d{1,4})[集话話]`)
	reEpBrackets = regexp.MustCompile(`\[(\d{1,4})\]`)
	// Fansub "Show - 02 [1080p]" style: separator-bounded 1–3 digit token.
	// Scanned before trailing bare number; quality tokens (720, …) are skipped.
	reEpSeparated = regexp.MustCompile(`(?:^|[\s._\-\[(【])(\d{1,3})(?:$|[\s._\-\])】])`)
	// Trailing 1–3 digit number only (years like 2019 stay out of episode space).
	reEpTrailing = regexp.MustCompile(`(?:^|[^0-9])(\d{1,3})$`)
)

// qualityLikeNumbers are common resolution tokens that must not be treated as
// episode numbers when a separator-bounded digit scan is used.
var qualityLikeNumbers = map[int]bool{
	360: true, 480: true, 576: true, 720: true, 1080: true, 1440: true, 2160: true,
}

func isAutoplayExtra(name string) bool {
	base := stripExtension(name)
	lower := strings.ToLower(base)
	for _, tok := range []string{"sample", "trailer", "preview", "预告", "花絮", "特典"} {
		if strings.Contains(lower, tok) {
			return true
		}
	}
	if reAutoplayOP.MatchString(base) || reAutoplayED.MatchString(base) {
		return true
	}
	return false
}

// episodeRef is a parsed (season, episode) pair. Season 0 means "unknown /
// not encoded" and still compares equal to another unknown season so bare
// EP02 / 第02集 ordering works inside one folder.
type episodeRef struct {
	Season  int
	Episode int
	OK      bool
}

func parseEpisodeRef(name string) episodeRef {
	base := stripExtension(name)
	if m := reEpSxxExx.FindStringSubmatch(base); len(m) == 3 {
		return episodeRef{atoi(m[1]), atoi(m[2]), true}
	}
	if m := reEpNxNN.FindStringSubmatch(base); len(m) == 3 {
		return episodeRef{atoi(m[1]), atoi(m[2]), true}
	}
	if m := reEpEPorE.FindStringSubmatch(base); len(m) == 2 {
		return episodeRef{0, atoi(m[1]), true}
	}
	if m := reEpChinese.FindStringSubmatch(base); len(m) == 2 {
		return episodeRef{0, atoi(m[1]), true}
	}
	if m := reEpBrackets.FindStringSubmatch(base); len(m) == 2 {
		return episodeRef{0, atoi(m[1]), true}
	}
	// Prefer the first non-quality separator-bounded number (fansub " - 02 ").
	if all := reEpSeparated.FindAllStringSubmatch(base, -1); len(all) > 0 {
		for _, m := range all {
			n := atoi(m[1])
			if n <= 0 || qualityLikeNumbers[n] {
				continue
			}
			return episodeRef{0, n, true}
		}
	}
	if m := reEpTrailing.FindStringSubmatch(base); len(m) == 2 {
		n := atoi(m[1])
		if n > 0 && !qualityLikeNumbers[n] {
			return episodeRef{0, n, true}
		}
	}
	return episodeRef{}
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func stripExtension(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return name
	}
	return name[:len(name)-len(ext)]
}

// TitleWithoutExtension is the display title for a file-source next item.
func TitleWithoutExtension(name string) string {
	return stripExtension(name)
}

// ParentDir returns the parent of a slash-normalized relative path. Empty
// string means the source root.
func ParentDir(relPath string) string {
	relPath = normalizeAutoplayPath(relPath)
	if relPath == "" {
		return ""
	}
	dir := path.Dir(relPath)
	if dir == "." {
		return ""
	}
	return dir
}

func normalizeAutoplayPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	return strings.Trim(p, "/")
}

func sortAutoplayCandidates(entries []Entry) {
	// Insertion sort is fine: listings are capped and we need a stable pure
	// natural-filename comparator mirrored by Swift.
	for i := 1; i < len(entries); i++ {
		j := i
		for j > 0 && naturalLess(entries[j].Name, entries[j-1].Name) {
			entries[j], entries[j-1] = entries[j-1], entries[j]
			j--
		}
	}
}

// naturalLess compares filenames by splitting into text/digit runs and
// comparing digit runs numerically (leading zeros ignored).
func naturalLess(a, b string) bool {
	ra, rb := splitNaturalRuns(a), splitNaturalRuns(b)
	n := len(ra)
	if len(rb) < n {
		n = len(rb)
	}
	for i := 0; i < n; i++ {
		da, db := ra[i], rb[i]
		if da.digits && db.digits {
			// Strip leading zeros for numeric compare; all-zeros → 0.
			na := strings.TrimLeft(da.s, "0")
			nb := strings.TrimLeft(db.s, "0")
			if na == "" {
				na = "0"
			}
			if nb == "" {
				nb = "0"
			}
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			// Equal numeric value: shorter zero-padded form sorts first so
			// the comparison is total (EP02 vs EP2 stay deterministic).
			if da.s != db.s {
				return da.s < db.s
			}
			continue
		}
		// Digit vs text: digit runs sort before text at the same position
		// only when both sides are the same kind; mixed kinds compare the
		// raw lowercased run so order stays total.
		if da.digits != db.digits {
			// Prefer the original character order via lowercase raw form.
			if da.s != db.s {
				return da.s < db.s
			}
			continue
		}
		if da.s != db.s {
			return da.s < db.s
		}
	}
	return len(ra) < len(rb)
}

type naturalRun struct {
	digits bool
	s      string // text: lowercased; digits: original digit characters
}

func splitNaturalRuns(name string) []naturalRun {
	if name == "" {
		return nil
	}
	var out []naturalRun
	var b strings.Builder
	var dig bool
	first := true
	for _, r := range name {
		isDig := unicode.IsDigit(r)
		if first {
			dig = isDig
			first = false
		} else if isDig != dig {
			out = append(out, flushRun(b.String(), dig))
			b.Reset()
			dig = isDig
		}
		if dig {
			b.WriteRune(r)
		} else {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	if b.Len() > 0 {
		out = append(out, flushRun(b.String(), dig))
	}
	return out
}

func flushRun(s string, dig bool) naturalRun {
	return naturalRun{digits: dig, s: s}
}

// EpisodeGap reports whether current → next skips episode numbers (same
// season, next > current+1). Used by the wiring layer for a log line.
func EpisodeGap(currentName, nextName string) (gap bool, from, to int) {
	c, n := parseEpisodeRef(currentName), parseEpisodeRef(nextName)
	if !c.OK || !n.OK || c.Season != n.Season {
		return false, 0, 0
	}
	if n.Episode > c.Episode+1 {
		return true, c.Episode, n.Episode
	}
	return false, c.Episode, n.Episode
}
