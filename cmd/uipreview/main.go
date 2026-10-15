// Command uipreview renders the launcher window to a PNG (no Windows needed).
// Usage: go run ./cmd/uipreview out.png [scale] [body-font.ttf] [settings-tab] [game-dir]
// settings-tab 0/1 renders the settings screen with ddraw.ini/allods2.cfg from game-dir.
//
// On Windows the body text is GDI ClearType Segoe UI; here it is drawn with a
// stand-in TTF (Liberation Sans by default) at the same size so the layout
// matches. Without a usable TTF it falls back to the game font.
package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"strconv"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/nOmnomNOM3/A2-Client-Launcher/core"
	"github.com/nOmnomNOM3/A2-Client-Launcher/skin"
)

type ttf struct {
	face font.Face
	k    int
}

func (t ttf) Width(s string) int {
	return (font.MeasureString(t.face, s).Ceil() + t.k - 1) / t.k
}
func (t ttf) Height() int { return (t.face.Metrics().Height.Ceil() + t.k - 1) / t.k }

func main() {
	out, k := "launcher_preview.png", 1
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if len(os.Args) > 2 {
		k, _ = strconv.Atoi(os.Args[2])
	}
	fontPath := "/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf"
	if len(os.Args) > 3 {
		fontPath = os.Args[3]
	}
	var body *ttf
	if b, err := os.ReadFile(fontPath); err == nil {
		if f, err := opentype.Parse(b); err == nil {
			// Height -13*k in GDI = 13*k px em at 72 dpi.
			if face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(13 * k), DPI: 72, Hinting: font.HintingFull}); err == nil {
				body = &ttf{face, k}
				skin.Body = body
			}
		}
	}
	st := skin.State{
		News: "# Welcome to the BiffinBoys server!\n" +
			"Press PLAY to update your game and join biffinboys.ddns.net.\n\n" +
			"# Version 2026.10.15\n" +
			"- Faster first install (bundled downloads).\n" +
			"- Sharper text, new default folder C:\\Allods2.\n" +
			"- If the game crashes, the launcher saves a report you can send us.",
		Status:   "Downloading 1,204/4,940 files - 78.2/319.5 MB",
		Folder:   `C:\Allods2`,
		Version:  "1.2.0",
		Progress: 0.245,
		Hover:    skin.Play,
	}
	if len(os.Args) > 4 {
		tab, _ := strconv.Atoi(os.Args[4])
		dir := "core/testdata"
		if len(os.Args) > 5 {
			dir = os.Args[5]
		}
		reg := memKV{"GameSpeed": "8", "MusicEnabled": "1", "Shadows": "1", "Lighting": "1", "Smoothing": "1", "Animation": "1",
			"ShowAllHitPoints": "1", "ShowFlyingHP": "1", "ClanNames": "1", "MessageColors": "1", "Acknowledgement": "1", "TipsMode": "1"}
		sf, err := core.LoadSettings(dir, reg, nil)
		if err != nil {
			panic(err)
		}
		var rows []skin.SRow
		for _, it := range sf.Items {
			if it.Tab != tab {
				continue
			}
			if it.Label == "Display mode" || it.Label == "Window size" {
				it.Step(1) // show a pending change in gold
			}
			rows = append(rows, skin.SRow{Label: it.Label, Value: it.Value(), Changed: it.Changed(), Disabled: it.Disabled != "", Col: it.Col})
		}
		st.Page, st.Tabs, st.Tab, st.Rows = skin.PageSettings, core.SettingsTabs, tab, rows
		st.Status, st.Progress, st.Hover = "Window size: size of the window in Window mode.", -1, skin.Save
	}
	im, ops := skin.RenderOps(st)
	// Integer nearest-neighbour scale, like the Windows blit.
	big := image.NewRGBA(image.Rect(0, 0, skin.W*k, skin.H*k))
	for y := 0; y < skin.H*k; y++ {
		for x := 0; x < skin.W*k; x++ {
			big.Set(x, y, im.At(x/k, y/k))
		}
	}
	if body != nil {
		asc := body.face.Metrics().Ascent
		for _, op := range ops {
			for _, p := range []struct {
				d int
				c color.RGBA
			}{{1, color.RGBA{8, 16, 8, 255}}, {0, op.C}} {
				d := font.Drawer{Dst: big, Src: image.NewUniform(p.c), Face: body.face,
					Dot: fixed.Point26_6{X: fixed.I(op.X*k + p.d), Y: fixed.I(op.Y*k+p.d) + asc}}
				d.DrawString(op.S)
			}
		}
	}
	f, err := os.Create(out)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, big); err != nil {
		panic(err)
	}
}

type memKV map[string]string

func (m memKV) Get(k string) (string, bool) { v, ok := m[k]; return v, ok }
func (m memKV) Set(k, v string)             { m[k] = v }
