package core

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var logMu sync.Mutex

// LogPath is <install dir>\.a2launcher\launcher.log.
func LogPath(root string) string { return filepath.Join(root, ".a2launcher", "launcher.log") }

// Logf appends one timestamped line to the launcher log. Errors are ignored:
// logging must never break a launch. The file is trimmed when it passes 1 MB.
func Logf(root, format string, a ...any) {
	if root == "" {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	p := LogPath(root)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if fi, err := os.Stat(p); err == nil && fi.Size() > 1<<20 {
		os.Rename(p, p+".old")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), fmt.Sprintf(format, a...))
}
