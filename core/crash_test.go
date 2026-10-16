package core

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectCrashExitCode(t *testing.T) {
	root := t.TempDir()
	before := SnapshotLogs(root)
	r := DetectCrash(root, 1, before)
	if !r.Crashed || !strings.Contains(r.Reason, "exit code 1") {
		t.Fatalf("got %+v", r)
	}
	r = DetectCrash(root, 0, before)
	if r.Crashed {
		t.Fatalf("clean exit with no logs should not crash: %+v", r)
	}
}

func TestDetectCrashNewLog(t *testing.T) {
	root := t.TempDir()
	before := SnapshotLogs(root)
	write(t, root, "a2mgr.log", "started\n")
	r := DetectCrash(root, 0, before)
	if r.Crashed {
		t.Fatalf("a new non-allods2 log alone must not be a crash: %+v", r)
	}
}

func TestDetectCrashAppendWithoutMarker(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "logs"), 0o755)
	write(t, root, "logs/game.log", "hello\n")
	before := SnapshotLogs(root)
	write(t, root, "logs/game.log", "hello\nmore\n")
	r := DetectCrash(root, 0, before)
	if r.Crashed {
		t.Fatalf("append without marker should not crash: %+v", r)
	}
}

func readTestdata(t *testing.T, name string) string {
	b, err := os.ReadFile(filepath.Join("testdata", "crashlogs", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Real logs from a clean quit on 2026-10-15: yesterday's log already existed,
// today's log is brand new and ends with "a2mgr closed." -> no crash.
func TestDetectCrashRealCleanQuitNewDayLog(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "logs"), 0o755)
	write(t, root, "logs/allods2_14-10-2026.log", readTestdata(t, "allods2_14-10-2026.log"))
	before := SnapshotLogs(root)
	write(t, root, "logs/allods2_15-10-2026.log", readTestdata(t, "allods2_15-10-2026.log"))
	r := DetectCrash(root, 0, before)
	if r.Crashed {
		t.Fatalf("clean quit with new day's log must not crash: %+v", r)
	}
	if r := DetectCrash(root, 2, before); !r.Crashed || !strings.Contains(r.Reason, "exit code 2") {
		t.Fatalf("nonzero exit must crash: %+v", r)
	}
}

// Real 14-10 log appended to during a session: clean close -> no crash.
func TestDetectCrashRealAppendedClean(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "logs"), 0o755)
	write(t, root, "logs/allods2_14-10-2026.log", "[old] a2mgr closed.\n")
	before := SnapshotLogs(root)
	write(t, root, "logs/allods2_14-10-2026.log", "[old] a2mgr closed.\n"+readTestdata(t, "allods2_14-10-2026.log"))
	if r := DetectCrash(root, 0, before); r.Crashed {
		t.Fatalf("got %+v", r)
	}
}

// Same real log with the final "a2mgr closed." removed -> crash, even with exit 0.
func TestDetectCrashRealLogMissingClosed(t *testing.T) {
	for _, name := range []string{"allods2_14-10-2026.log", "allods2_15-10-2026.log"} {
		root := t.TempDir()
		os.MkdirAll(filepath.Join(root, "logs"), 0o755)
		before := SnapshotLogs(root)
		body := readTestdata(t, name)
		i := strings.LastIndex(body, "a2mgr closed.")
		if i < 0 {
			t.Fatalf("%s: sample lacks closed line", name)
		}
		body = body[:strings.LastIndex(body[:i], "\n")+1]
		write(t, root, "logs/"+name, body)
		r := DetectCrash(root, 0, before)
		if !r.Crashed || !strings.Contains(r.Reason, "a2mgr closed") {
			t.Fatalf("%s: expected crash, got %+v", name, r)
		}
	}
}

// A second session started after a clean close but never closed -> crash.
func TestDetectCrashRestartedNotClosed(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "logs"), 0o755)
	before := SnapshotLogs(root)
	write(t, root, "logs/allods2_15-10-2026.log", readTestdata(t, "allods2_15-10-2026.log")+
		"[15.10.2026 22:00:00] a2mgr (v1.13)/zxmgr (v2.0) started.\n")
	if r := DetectCrash(root, 0, before); !r.Crashed {
		t.Fatalf("expected crash, got %+v", r)
	}
}

func TestWriteCrashReportContents(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "logs"), 0o755)
	write(t, root, "allods2.exe", "exe")
	write(t, root, "a2mgr.dll", "dll")
	write(t, root, "world.res", "world")
	write(t, root, "ddraw.ini", "ini")
	write(t, root, "logs/game.log", "FATAL ERROR: can't load foo\n")
	write(t, root, "chr/secret.a2c", "PERSONAL")
	write(t, root, "allods2.cfg", "personal cfg")
	before := map[string]LogStat{}
	det := DetectCrash(root, 3, before)
	path, err := WriteCrashReport(root, det, "2026.10.14")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, filepath.Join(StateDir, "crash")) {
		t.Fatalf("unexpected path %s", path)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		if strings.Contains(strings.ToLower(f.Name), "chr") || strings.Contains(f.Name, "allods2.cfg") {
			t.Fatalf("personal file leaked into zip: %s", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 64)
		n, _ := rc.Read(buf)
		rc.Close()
		if strings.Contains(string(buf[:n]), "PERSONAL") {
			t.Fatal("personal content in zip")
		}
	}
	for _, need := range []string{"report.json", "report.txt", "logs/game.log"} {
		if !names[need] {
			t.Fatalf("missing %s in %v", need, names)
		}
	}
}
