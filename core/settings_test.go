package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type memKV map[string]string

func (m memKV) Get(k string) (string, bool) { v, ok := m[k]; return v, ok }
func (m memKV) Set(k, v string)             { m[k] = v }

func find(sf *SettingsFiles, label string) *Setting {
	for _, s := range sf.Items {
		if s.Label == label {
			return s
		}
	}
	return nil
}

func TestSettingsRoundTripKeepsEverythingElse(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.ReadFile("testdata/ddraw.ini")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "ddraw.ini"), orig, 0o644)
	cfg := "# my notes\r\nr_fps 1 // keep\r\nalias:f5 help\r\n"
	os.WriteFile(filepath.Join(dir, "allods2.cfg"), []byte(cfg), 0o644)
	reg := memKV{"GameSpeed": "8", "Shadows": "1"}

	sf, err := LoadSettings(dir, reg, func() map[string]uint32 { return map[string]uint32{"GameSpeed": 8} })
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"Display mode": "Classic fullscreen", "Window size": "Game size", "Upscale filter": "Custom",
		"Renderer": "Auto", "FPS limit": "Monitor rate", "Show FPS": "On", "Show clock": "Off", "Game speed": "8", "Shadows": "On",
		"Music": "n/a" /* missing value -> custom */, "Game view size": "Default"}
	for l, v := range want {
		s := find(sf, l)
		if s == nil {
			t.Fatalf("no setting %q", l)
		}
		got := s.Value()
		if l == "Music" {
			if s.Cur != -1 {
				t.Errorf("Music should be custom when missing")
			}
			continue
		}
		if got != v {
			t.Errorf("%s = %q, want %q", l, got, v)
		}
	}
	if sf.Changed() {
		t.Fatal("nothing changed yet")
	}
	// Change some.
	set := func(l, v string) {
		s := find(sf, l)
		for i, ch := range s.Choices {
			if ch.Label == v {
				s.Cur = i
				return
			}
		}
		t.Fatalf("%s has no %s", l, v)
	}
	set("Display mode", "Borderless")
	set("Window size", "1920x1080")
	set("Upscale filter", "Sharp")
	set("Game view size", "1280x720")
	set("Show FPS", "Off")
	set("Game speed", "5")
	backup, err := sf.Save()
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(backup, "ddraw.ini")); string(b) != string(orig) {
		t.Fatal("ddraw.ini backup differs")
	}
	if _, err := os.Stat(filepath.Join(backup, "registry.json")); err != nil {
		t.Fatal(err)
	}
	nb, _ := os.ReadFile(filepath.Join(dir, "ddraw.ini"))
	ol := strings.Split(string(orig), "\r\n")
	nl := strings.Split(string(nb), "\r\n")
	if len(ol) != len(nl) {
		t.Fatalf("line count %d -> %d", len(ol), len(nl))
	}
	changed := map[string]bool{}
	for i := range ol {
		if ol[i] != nl[i] {
			changed[nl[i]] = true
		}
	}
	for _, l := range []string{"windowed=true", "fullscreen=true", "width=1920", "height=1080", "shader=Nearest neighbor", "d3d9_filter=0"} {
		if !changed[l] {
			t.Errorf("missing change %q", l)
		}
		delete(changed, l)
	}
	if len(changed) != 0 {
		t.Errorf("unexpected changes: %v", changed)
	}
	cb, _ := os.ReadFile(filepath.Join(dir, "allods2.cfg"))
	if got := string(cb); got != "# my notes\r\nr_fps 0\r\nalias:f5 help\r\nr_height 720\r\nr_width 1280\r\n" {
		t.Errorf("cfg = %q", got)
	}
	if reg["GameSpeed"] != "5" || reg["Shadows"] != "1" {
		t.Errorf("reg = %v", reg)
	}
	// Reload: values stick, nothing pending.
	sf2, _ := LoadSettings(dir, reg, nil)
	if v := find(sf2, "Display mode").Value(); v != "Borderless" {
		t.Errorf("reload display = %s", v)
	}
	if v := find(sf2, "Game view size").Value(); v != "1280x720" {
		t.Errorf("reload view = %s", v)
	}
	// Back to default removes the keys again.
	set2 := find(sf2, "Game view size")
	set2.Cur = 0
	sf2.Save()
	cb, _ = os.ReadFile(filepath.Join(dir, "allods2.cfg"))
	if strings.Contains(strings.ReplaceAll(string(cb), "# r_", ""), "r_width") {
		t.Errorf("r_width still active: %q", cb)
	}
}

func TestSettingsMissingFiles(t *testing.T) {
	sf, err := LoadSettings(t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if find(sf, "Renderer").Disabled == "" || find(sf, "Shadows").Disabled == "" {
		t.Fatal("should be disabled")
	}
	if find(sf, "Show FPS").Disabled != "" || find(sf, "Show FPS").Value() != "Off" {
		t.Fatal("cfg works without a file")
	}
}

func TestDDrawGameSectionOverride(t *testing.T) {
	f := &TextFile{EOL: "\n", Lines: []string{"[ddraw]", "vsync=false", "", "[allods2]", "vsync=true"}}
	kv := DDrawKV{f}
	if v, _ := kv.Get("vsync"); v != "true" {
		t.Fatal("override not read")
	}
	kv.Set("vsync", "false")
	if f.Lines[1] != "vsync=false" || f.Lines[4] != "vsync=false" {
		t.Fatal(f.Lines)
	}
	kv.Set("boxing", "true")
	if f.Lines[2] != "boxing=true" {
		t.Fatalf("new key placed wrong: %q", f.Lines)
	}
}
