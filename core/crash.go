package core

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// KeyFiles are hashed into every crash report (no player-owned content).
var KeyFiles = []string{
	"allods2.exe",
	"a2mgr.dll",
	"world.res",
	"data/world.res",
	"ddraw.ini",
}

var crashMarkers = []string{
	"fatal error",
	"can't load",
	"cant load",
}

// LogStat is the size and hash of a log file before the game starts.
type LogStat struct {
	Size int64
	SHA  string
}

// SnapshotLogs records every *.log under root and root/logs (and a2mgr*.log).
func SnapshotLogs(root string) map[string]LogStat {
	out := map[string]LogStat{}
	for _, p := range listLogFiles(root) {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		sha, _ := fileSHA256(p)
		out[rel] = LogStat{Size: st.Size(), SHA: sha}
	}
	return out
}

func listLogFiles(root string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		p = filepath.Clean(p)
		if seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, pat := range []string{
		filepath.Join(root, "*.log"),
		filepath.Join(root, "a2mgr*.log"),
		filepath.Join(root, "logs", "*.log"),
		filepath.Join(root, "logs", "*", "*.log"),
	} {
		m, _ := filepath.Glob(pat)
		for _, p := range m {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				add(p)
			}
		}
	}
	return out
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CrashResult is the outcome of watching one game run.
type CrashResult struct {
	Crashed  bool
	Reason   string
	ExitCode int
	// NewOrChanged are log paths (slash) that appeared or changed since the snapshot.
	NewOrChanged []string
	// MarkerHits are log paths whose new/changed content mentioned FATAL ERROR / can't load.
	MarkerHits []string
}

// DetectCrash decides whether the game run looks like a crash.
// A crash is: a non-zero exit code, OR the newest new/changed allods2 log
// (a2mgr writes logs/allods2_DD-MM-YYYY.log) has no "a2mgr closed." line
// after its latest "started." line. A brand-new log file on its own (e.g. a
// new day's log) is NOT a crash. FATAL ERROR / "can't load" lines are still
// recorded in MarkerHits for the report but do not by themselves mean a crash.
func DetectCrash(root string, exitCode int, before map[string]LogStat) CrashResult {
	r := CrashResult{ExitCode: exitCode}
	var newestA2 string
	var newestTime time.Time
	for _, p := range listLogFiles(root) {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		sha, _ := fileSHA256(p)
		prev, ok := before[rel]
		if ok && prev.Size == st.Size() && prev.SHA == sha {
			continue
		}
		r.NewOrChanged = append(r.NewOrChanged, rel)
		if logHasMarker(p) {
			r.MarkerHits = append(r.MarkerHits, rel)
		}
		if strings.HasPrefix(strings.ToLower(filepath.Base(rel)), "allods2") {
			if newestA2 == "" || st.ModTime().After(newestTime) ||
				(st.ModTime().Equal(newestTime) && rel > newestA2) {
				newestA2, newestTime = rel, st.ModTime()
			}
		}
	}
	if exitCode != 0 {
		r.Crashed = true
		r.Reason = fmt.Sprintf("exit code %d", exitCode)
		return r
	}
	if newestA2 != "" && !logClosedCleanly(filepath.Join(root, filepath.FromSlash(newestA2))) {
		r.Crashed = true
		r.Reason = "no 'a2mgr closed.' after last start in " + newestA2
	}
	return r
}

// logClosedCleanly reports whether the log has an "a2mgr closed." line after
// its latest "started." line. A log with no "started." line is treated as
// clean only if it has a closed line.
func logClosedCleanly(p string) bool {
	b, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	lastStart, lastClosed := -1, -1
	for i, line := range strings.Split(string(b), "\n") {
		l := strings.ToLower(strings.TrimSpace(line))
		if strings.HasSuffix(l, "a2mgr closed.") {
			lastClosed = i
		} else if strings.HasSuffix(l, "started.") {
			lastStart = i
		}
	}
	return lastClosed >= 0 && lastClosed > lastStart
}

func logHasMarker(p string) bool {
	b, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	// Cap read for huge logs: already fully read; for very large files Glob list is enough.
	if len(b) > 4<<20 {
		b = b[len(b)-4<<20:]
	}
	low := strings.ToLower(string(b))
	for _, m := range crashMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// CrashReportMeta is written as report.json inside the zip.
type CrashReportMeta struct {
	Time            string            `json:"time"`
	ExitCode        int               `json:"exit_code"`
	Reason          string            `json:"reason"`
	ManifestVersion string            `json:"manifest_version,omitempty"`
	Windows         string            `json:"windows"`
	GOOS            string            `json:"goos"`
	GOARCH          string            `json:"goarch"`
	KeyHashes       map[string]string `json:"key_hashes"`
	LogsIncluded    []string          `json:"logs_included"`
	NewOrChanged    []string          `json:"new_or_changed_logs"`
}

// WriteCrashReport builds .a2launcher/crash/<time>.zip with meta, key hashes and recent logs.
// Personal files (chr, screenshots, cfg, saves) are never included.
func WriteCrashReport(root string, det CrashResult, manifestVersion string) (string, error) {
	dir := filepath.Join(root, StateDir, "crash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create crash folder: %w", err)
	}
	stamp := time.Now().Format("20060102-150405")
	zipPath := filepath.Join(dir, stamp+".zip")
	f, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	zw := zip.NewWriter(f)

	meta := CrashReportMeta{
		Time:            time.Now().Format(time.RFC3339),
		ExitCode:        det.ExitCode,
		Reason:          det.Reason,
		ManifestVersion: manifestVersion,
		Windows:         windowsVersion(),
		GOOS:            runtime.GOOS,
		GOARCH:          runtime.GOARCH,
		KeyHashes:       map[string]string{},
		NewOrChanged:    det.NewOrChanged,
	}
	for _, rel := range KeyFiles {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			continue
		}
		sha, err := fileSHA256(p)
		if err != nil {
			continue
		}
		meta.KeyHashes[rel] = sha
	}

	// Prefer new/changed logs; otherwise the most recent few.
	logs := pickLogsForReport(root, det.NewOrChanged)
	meta.LogsIncluded = logs
	for _, rel := range logs {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := zipFile(zw, p, "logs/"+filepath.Base(rel)); err != nil {
			// Skip unreadable logs; still finish the report.
			continue
		}
	}

	raw, _ := json.MarshalIndent(meta, "", "  ")
	w, err := zw.Create("report.json")
	if err != nil {
		zw.Close()
		return "", err
	}
	if _, err := w.Write(raw); err != nil {
		zw.Close()
		return "", err
	}
	summary := fmt.Sprintf("Rage of Mages 2 crash report\n\nExit code: %d\nReason: %s\nManifest: %s\nWindows: %s\nTime: %s\n\nKey file hashes:\n",
		meta.ExitCode, meta.Reason, meta.ManifestVersion, meta.Windows, meta.Time)
	for _, rel := range KeyFiles {
		if h, ok := meta.KeyHashes[rel]; ok {
			summary += fmt.Sprintf("  %s: %s\n", rel, h)
		}
	}
	summary += "\nLogs included:\n"
	for _, rel := range meta.LogsIncluded {
		summary += "  " + rel + "\n"
	}
	sw, err := zw.Create("report.txt")
	if err != nil {
		zw.Close()
		return "", err
	}
	if _, err := io.WriteString(sw, summary); err != nil {
		zw.Close()
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	return zipPath, nil
}

func pickLogsForReport(root string, prefer []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(rel string) {
		rel = filepath.ToSlash(rel)
		if seen[rel] {
			return
		}
		p := filepath.Join(root, filepath.FromSlash(rel))
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return
		}
		seen[rel] = true
		out = append(out, rel)
	}
	for _, rel := range prefer {
		add(rel)
	}
	// Also grab a2mgr logs and anything under logs/ even if unchanged (recent context).
	for _, p := range listLogFiles(root) {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		base := strings.ToLower(filepath.Base(rel))
		if strings.Contains(base, "a2mgr") || strings.HasPrefix(rel, "logs/") {
			add(rel)
		}
	}
	// Cap to keep zips small.
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

func zipFile(zw *zip.Writer, src, name string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	hdr, err := zip.FileInfoHeader(st)
	if err != nil {
		return err
	}
	hdr.Name = name
	hdr.Method = zip.Deflate
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	// Cap each log at 2 MB from the end.
	const max = 2 << 20
	if st.Size() > max {
		if _, err := in.Seek(st.Size()-max, io.SeekStart); err != nil {
			return err
		}
	}
	_, err = io.Copy(w, in)
	return err
}

func windowsVersion() string {
	if runtime.GOOS != "windows" {
		return runtime.GOOS
	}
	// Best-effort without x/sys: environment is enough for a support zip.
	if v := os.Getenv("OS"); v != "" {
		maj := os.Getenv("PROCESSOR_ARCHITECTURE")
		return strings.TrimSpace(v + " " + maj)
	}
	return "windows"
}
