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

func TestDetectCrashFatalInChangedLog(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "logs"), 0o755)
	write(t, root, "logs/game.log", "session start\n")
	before := SnapshotLogs(root)
	write(t, root, "logs/game.log", "session start\nFATAL ERROR: can't load data\\graphics\\x.256\n")
	r := DetectCrash(root, 0, before)
	if !r.Crashed || len(r.MarkerHits) == 0 {
		t.Fatalf("expected fatal marker crash, got %+v", r)
	}
}

func TestDetectCrashNewLog(t *testing.T) {
	root := t.TempDir()
	before := SnapshotLogs(root)
	write(t, root, "a2mgr.log", "started\n")
	r := DetectCrash(root, 0, before)
	if !r.Crashed || !strings.Contains(r.Reason, "new log") {
		t.Fatalf("expected new log crash, got %+v", r)
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
