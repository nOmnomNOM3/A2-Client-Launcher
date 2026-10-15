package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Settings the launcher can change outside the game. Every key here was
// checked against the files players actually run:
//   - ddraw.ini: cnc-ddraw 7.1.0.0 (git~541b5de) as shipped in the client; only
//     keys present in that ddraw.ini's own documentation are used.
//   - allods2.cfg: read by a2mgr.dll ParseConfig2x (config.cpp); key names
//     confirmed in the shipped a2mgr.dll.
//   - registry: allods2.exe stores its options menu under
//     HKLM\SOFTWARE\1C\Allods 2 (32-bit view; per-user VirtualStore copy
//     when not run as admin). Value names confirmed in allods2.exe.

// Store is where a setting lives.
type Store int

const (
	DDraw Store = iota // ddraw.ini [ddraw] (and a game-specific [allods2] override)
	Cfg                // allods2.cfg
	Reg                // allods2.exe registry options (REG_DWORD)
)

// Choice is one value a setting can take: the keys it writes.
type Choice struct {
	Label string
	Set   map[string]string
}

type Setting struct {
	Tab, Col int
	Label    string
	Help     string
	Store    Store
	Def      map[string]string // value assumed when a key is missing
	Choices  []Choice
	Cur      int // index into Choices, -1 = custom value (left alone)
	Orig     int
	Disabled string // why it cannot be changed now ("" = editable)
}

func (s *Setting) Value() string {
	if s.Disabled != "" {
		return "n/a"
	}
	if s.Cur < 0 {
		return "Custom"
	}
	return s.Choices[s.Cur].Label
}

func (s *Setting) Changed() bool { return s.Cur != s.Orig }

// Step moves to the next (+1) or previous (-1) choice.
func (s *Setting) Step(d int) {
	if s.Disabled != "" || len(s.Choices) == 0 {
		return
	}
	n := len(s.Choices)
	if s.Cur < 0 {
		if d > 0 {
			s.Cur = 0
		} else {
			s.Cur = n - 1
		}
		return
	}
	s.Cur = ((s.Cur+d)%n + n) % n
}

func c(label string, kv ...string) Choice {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return Choice{label, m}
}

func onOff(key string) []Choice { return []Choice{c("Off", key, "false"), c("On", key, "true")} }
func reg01(key string) []Choice { return []Choice{c("Off", key, "0"), c("On", key, "1")} }
func cfg01(key string) []Choice { return []Choice{c("Off", key, "0"), c("On", key, "1")} }

func res(store Store, wk, hk string, sizes ...string) []Choice {
	var out []Choice
	for _, s := range sizes {
		p := strings.SplitN(s, "x", 2)
		out = append(out, c(s, wk, p[0], hk, p[1]))
	}
	return out
}

// Tabs of the settings screen.
var SettingsTabs = []string{"Display", "Game"}

// NewSettings is the full list (fresh copy, not loaded yet).
func NewSettings() []*Setting {
	d := func(col int, label, help string, def map[string]string, ch []Choice) *Setting {
		return &Setting{Tab: 0, Col: col, Label: label, Help: help, Store: DDraw, Def: def, Choices: ch}
	}
	g := func(col int, st Store, label, help string, def map[string]string, ch []Choice) *Setting {
		return &Setting{Tab: 1, Col: col, Label: label, Help: help, Store: st, Def: def, Choices: ch}
	}
	return []*Setting{
		d(0, "Display mode", "Fullscreen stretched = fills the screen without changing its resolution. Classic = the monitor switches to the game resolution.",
			map[string]string{"windowed": "false", "fullscreen": "false"},
			[]Choice{
				c("Fullscreen", "windowed", "false", "fullscreen", "true"),
				c("Borderless", "windowed", "true", "fullscreen", "true"),
				c("Window", "windowed", "true", "fullscreen", "false"),
				c("Classic fullscreen", "windowed", "false", "fullscreen", "false"),
			}),
		d(0, "Window size", "Size of the window in Window mode. 'Game size' = no stretching.",
			map[string]string{"width": "0", "height": "0"},
			append([]Choice{c("Game size", "width", "0", "height", "0")},
				res(DDraw, "width", "height", "800x600", "1024x768", "1280x720", "1280x960", "1600x900", "1600x1200", "1920x1080", "2560x1440")...)),
		d(0, "Keep aspect ratio", "Black bars instead of stretching the picture out of shape.", map[string]string{"maintas": "false"}, onOff("maintas")),
		d(0, "Integer scaling", "Only whole-number zoom (2x, 3x): sharpest pixels, may add black borders.", map[string]string{"boxing": "false"}, onOff("boxing")),
		d(0, "Resizable window", "Drag the window edges to resize (Window mode).", map[string]string{"resizable": "true"}, onOff("resizable")),
		d(0, "Free mouse cursor", "On = the cursor is not locked inside the game window.", map[string]string{"devmode": "false"}, onOff("devmode")),
		d(0, "Alt+Enter switches", "What Alt+Enter toggles while playing.",
			map[string]string{"toggle_borderless": "false", "toggle_upscaled": "false"},
			[]Choice{
				c("Window/Fullscreen", "toggle_borderless", "false", "toggle_upscaled", "false"),
				c("Window/Borderless", "toggle_borderless", "true", "toggle_upscaled", "false"),
				c("Window/Stretched", "toggle_borderless", "false", "toggle_upscaled", "true"),
			}),
		d(0, "Alt+Enter key", "Turn the Alt+Enter hotkey off if you hit it by accident.", map[string]string{"keytogglefullscreen": "0x0D"},
			[]Choice{c("Alt+Enter", "keytogglefullscreen", "0x0D"), c("Off", "keytogglefullscreen", "0x00")}),
		d(1, "Renderer", "Auto = Direct3D 9 or OpenGL. Try another one if the screen is black or flickers.", map[string]string{"renderer": "auto"},
			[]Choice{c("Auto", "renderer", "auto"), c("Direct3D 9", "renderer", "direct3d9"), c("OpenGL", "renderer", "opengl"), c("GDI (safe)", "renderer", "gdi")}),
		d(1, "Upscale filter", "How the picture is smoothed when stretched (Direct3D 9 and OpenGL).",
			map[string]string{"shader": "", "d3d9_filter": "2"},
			[]Choice{
				c("Sharp", "shader", "Nearest neighbor", "d3d9_filter", "0"),
				c("Smooth", "shader", "Bilinear", "d3d9_filter", "1"),
				c("Bicubic", "shader", "Bicubic", "d3d9_filter", "2"),
				c("Lanczos", "shader", "Lanczos", "d3d9_filter", "3"),
				c("xBR (OpenGL)", "shader", "xBR-lv2", "d3d9_filter", "2"),
			}),
		d(1, "VSync", "Stops tearing; adds a little input lag.", map[string]string{"vsync": "false"}, onOff("vsync")),
		d(1, "FPS limit", "Drawing rate only; does not change game speed.", map[string]string{"maxfps": "-1"},
			[]Choice{c("Monitor rate", "maxfps", "-1"), c("60", "maxfps", "60"), c("120", "maxfps", "120"), c("144", "maxfps", "144"), c("Unlimited", "maxfps", "0")}),
		d(1, "Single CPU core", "Avoids freezes on some PCs. Turn off if sound stutters.", map[string]string{"singlecpu": "true"}, onOff("singlecpu")),
		d(1, "Screenshot key", "cnc-ddraw screenshot (saved to Screenshots).", map[string]string{"keyscreenshot": "0x2C"},
			[]Choice{c("Print Screen", "keyscreenshot", "0x2C"), c("F12", "keyscreenshot", "0x7B"), c("Off", "keyscreenshot", "0x00")}),

		g(0, Cfg, "Game view size", "How much map you see (a2mgr r_width/r_height). Bigger = more map, same unit size.",
			map[string]string{"r_width": Unset, "r_height": Unset},
			append([]Choice{c("Default", "r_width", Unset, "r_height", Unset)},
				res(Cfg, "r_width", "r_height", "800x600", "1024x768", "1280x720", "1366x768", "1600x900", "1920x1080")...)),
		g(0, Cfg, "Edge scroll speed", "Map scrolling speed at the screen edge (a2mgr g_scrollspeed, 0-100).", map[string]string{"g_scrollspeed": Unset},
			[]Choice{c("Default", "g_scrollspeed", Unset), c("Slow", "g_scrollspeed", "50"), c("Medium", "g_scrollspeed", "75"), c("Fast", "g_scrollspeed", "90"), c("Fastest", "g_scrollspeed", "100")}),
		g(0, Cfg, "Show FPS", "Frame counter in the corner (r_fps).", map[string]string{"r_fps": "0"}, cfg01("r_fps")),
		g(0, Cfg, "Show clock", "Clock on screen (r_time).", map[string]string{"r_time": "0"}, cfg01("r_time")),
		g(0, Cfg, "Map grid", "Cell grid over the map (r_grid).", map[string]string{"r_grid": "0"}, cfg01("r_grid")),
		g(0, Cfg, "Screenshot format", "a2mgr screenshots (v_screentype).", map[string]string{"v_screentype": Unset},
			[]Choice{c("PNG", "v_screentype", Unset), c("JPG", "v_screentype", "jpg"), c("BMP", "v_screentype", "bmp")}),
		g(0, Cfg, "Language", "Text language override (z_language).", map[string]string{"z_language": Unset},
			[]Choice{c("Auto", "z_language", Unset), c("English", "z_language", "english"), c("Russian", "z_language", "russian")}),

		g(0, Reg, "Game speed", "Single player only; online games use the server's speed.", nil,
			[]Choice{c("0", "GameSpeed", "0"), c("1", "GameSpeed", "1"), c("2", "GameSpeed", "2"), c("3", "GameSpeed", "3"), c("4", "GameSpeed", "4"),
				c("5", "GameSpeed", "5"), c("6", "GameSpeed", "6"), c("7", "GameSpeed", "7"), c("8", "GameSpeed", "8")}),
		g(0, Reg, "Music", "In-game music on/off (MusicEnabled).", nil, reg01("MusicEnabled")),
		g(1, Reg, "Shadows", "Shadows option of the game menu.", nil, reg01("Shadows")),
		g(1, Reg, "Lighting", "Lighting option of the game menu.", nil, reg01("Lighting")),
		g(1, Reg, "Smoothing", "Smoothing option of the game menu.", nil, reg01("Smoothing")),
		g(1, Reg, "Animation", "Animation option of the game menu.", nil, reg01("Animation")),
		g(1, Reg, "All health bars", "Show health bars of all units (ShowAllHitPoints).", nil, reg01("ShowAllHitPoints")),
		g(1, Reg, "Flying damage", "Floating damage numbers (ShowFlyingHP).", nil, reg01("ShowFlyingHP")),
		g(1, Reg, "Clan names", "Show clan names (ClanNames).", nil, reg01("ClanNames")),
		g(1, Reg, "Message colors", "Coloured chat messages (MessageColors).", nil, reg01("MessageColors")),
		g(1, Reg, "Unit replies", "Units answer orders (Acknowledgement).", nil, reg01("Acknowledgement")),
		g(1, Reg, "Tips", "Tips mode (TipsMode).", nil, reg01("TipsMode")),
	}
}

// KV reads and writes one store.
type KV interface {
	Get(key string) (string, bool)
	Set(key, val string)
}

// DDrawKV edits ddraw.ini's [ddraw] section. cnc-ddraw lets a section named
// after the exe ([allods2]) override [ddraw]; such overrides are read first
// and updated too, so the change always takes effect.
type DDrawKV struct{ F *TextFile }

func (d DDrawKV) Get(k string) (string, bool) {
	if v, ok := d.F.IniGet("allods2", k); ok {
		return v, true
	}
	return d.F.IniGet("ddraw", k)
}

func (d DDrawKV) Set(k, v string) {
	if _, ok := d.F.IniGet("allods2", k); ok {
		d.F.IniSet("allods2", k, v)
	}
	d.F.IniSet("ddraw", k, v)
}

type CfgKV struct{ F *TextFile }

func (c CfgKV) Get(k string) (string, bool) { return c.F.CfgGet(k) }
func (c CfgKV) Set(k, v string)             { c.F.CfgSet(k, v) }

func norm(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "yes", "y", "true", "1":
		return "true"
	case "no", "n", "false", "0", "":
		if v == "" {
			return ""
		}
		return "false"
	}
	if strings.HasPrefix(v, "0x") {
		var n int
		if _, err := fmt.Sscanf(v, "0x%x", &n); err == nil {
			return fmt.Sprintf("0x%02x", n)
		}
	}
	return v
}

// Match sets s.Cur from the store's current values.
func (s *Setting) Match(kv KV) {
	s.Cur = -1
	for i, ch := range s.Choices {
		ok := true
		for k, want := range ch.Set {
			got, present := kv.Get(k)
			if !present {
				d, hasDef := s.Def[k]
				if !hasDef {
					if want != Unset {
						ok = false
					}
					break
				}
				got = d
			}
			if got == Unset && want == Unset {
				continue
			}
			if got == Unset || want == Unset || norm(got) != norm(want) {
				ok = false
				break
			}
		}
		if ok {
			s.Cur = i
			break
		}
	}
	s.Orig = s.Cur
}

// Apply writes the chosen values of changed settings into kv.
func (s *Setting) Apply(kv KV) {
	if !s.Changed() || s.Cur < 0 || s.Disabled != "" {
		return
	}
	set := s.Choices[s.Cur].Set
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic order for keys appended to the file
	for _, k := range keys {
		kv.Set(k, set[k])
	}
}

// SettingsFiles is the on-disk state of the settings screen.
type SettingsFiles struct {
	Root       string
	DDraw, Cfg *TextFile
	Reg        KV // nil when the game's registry key does not exist yet
	RegBackup  func() map[string]uint32
	Items      []*Setting
}

func (sf *SettingsFiles) kv(s Store) KV {
	switch s {
	case DDraw:
		if sf.DDraw == nil {
			return nil
		}
		return DDrawKV{sf.DDraw}
	case Cfg:
		return CfgKV{sf.Cfg}
	}
	return sf.Reg
}

// LoadSettings reads ddraw.ini and allods2.cfg from root; reg may be nil.
func LoadSettings(root string, reg KV, regBackup func() map[string]uint32) (*SettingsFiles, error) {
	sf := &SettingsFiles{Root: root, Reg: reg, RegBackup: regBackup, Items: NewSettings()}
	dd, err := LoadTextFile(filepath.Join(root, "ddraw.ini"))
	if err != nil {
		return nil, err
	}
	if dd.Exist {
		sf.DDraw = dd
	}
	if sf.Cfg, err = LoadTextFile(filepath.Join(root, "allods2.cfg")); err != nil {
		return nil, err
	}
	for _, s := range sf.Items {
		kv := sf.kv(s.Store)
		if kv == nil {
			switch s.Store {
			case DDraw:
				s.Disabled = "ddraw.ini not found: press PLAY once to install the game."
			default:
				s.Disabled = "Start the game once first; it creates these options."
			}
			s.Cur, s.Orig = -1, -1
			continue
		}
		s.Match(kv)
	}
	return sf, nil
}

// Changed reports whether anything would be written.
func (sf *SettingsFiles) Changed() bool {
	for _, s := range sf.Items {
		if s.Changed() && s.Cur >= 0 {
			return true
		}
	}
	return false
}

// Save backs up ddraw.ini, allods2.cfg and the registry values to
// .a2launcher\backup\settings-<time>, then writes the changes.
func (sf *SettingsFiles) Save() (backup string, err error) {
	backup = filepath.Join(sf.Root, ".a2launcher", "backup", "settings-"+time.Now().Format("20060102-150405"))
	paths := []string{filepath.Join(sf.Root, "ddraw.ini"), filepath.Join(sf.Root, "allods2.cfg")}
	if err := BackupFiles(backup, paths...); err != nil {
		return "", fmt.Errorf("backup failed, nothing changed: %w", err)
	}
	if sf.Reg != nil && sf.RegBackup != nil {
		os.MkdirAll(backup, 0o755)
		b, _ := json.MarshalIndent(sf.RegBackup(), "", "  ")
		if err := os.WriteFile(filepath.Join(backup, "registry.json"), b, 0o644); err != nil {
			return "", fmt.Errorf("backup failed, nothing changed: %w", err)
		}
	}
	for _, s := range sf.Items {
		if kv := sf.kv(s.Store); kv != nil {
			s.Apply(kv)
		}
	}
	if sf.DDraw != nil {
		if err := sf.DDraw.Save(); err != nil {
			return backup, err
		}
	}
	if err := sf.Cfg.Save(); err != nil {
		return backup, err
	}
	for _, s := range sf.Items {
		s.Orig = s.Cur
	}
	Logf(sf.Root, "settings saved (backup %s)", backup)
	return backup, nil
}
