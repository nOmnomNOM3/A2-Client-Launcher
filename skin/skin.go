// Package skin draws the launcher window in Rage of Mages 2's own menu style.
// It is pure Go (image/draw) so the same code paints the Windows window and
// renders previews/tests anywhere. The art comes from the game's files via
// tools/gen_ui_assets.py (lm.256 frame, scrlbars.256 slider, font1.16 font).
package skin

import (
	"bytes"
	"embed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strconv"
	"strings"
)

//go:embed assets/*.png assets/font.txt
var assets embed.FS

// Window size in pixels.
const W, H = 640, 448

// Buttons.
const (
	None = iota
	Play
	Repair
	Folder
	Exit
	Settings // main page: open the settings screen
	Save     // settings page buttons
	Back
	Undo
	OpenIni
)

// Settings screen hit ids: TabID+i for tab i, RowPrev+2*i / RowPrev+2*i+1
// for the "<" / ">" halves of row i's value box.
const (
	TabID   = 50
	RowPrev = 100
)

// Pages.
const (
	PageMain = iota
	PageSettings
)

// SRow is one line of the settings screen.
type SRow struct {
	Label, Value      string
	Changed, Disabled bool
	Col               int // 0 left, 1 right column
}

// State is everything the window shows.
type State struct {
	Title    string
	News     string
	Status   string
	Folder   string  // game folder, shown under the news
	Version  string  // launcher version, shown bottom-left of the news panel
	Progress float64 // 0..1; <0 hides the knob
	Busy     bool    // disables the buttons
	Hover    int
	Pressed  int
	Scroll   int // first news line shown

	Page int      // PageMain or PageSettings
	Tabs []string // settings tabs
	Tab  int
	Rows []SRow // rows of the current tab
}

var (
	frame                    [9]*image.RGBA
	barCap, barTrack, barKnb *image.RGBA
	glyphs                   *image.Gray
	advance                  [224]int
)

func load(name string) *image.RGBA {
	b, err := assets.ReadFile("assets/" + name)
	if err != nil {
		panic(err)
	}
	im, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		panic(err)
	}
	out := image.NewRGBA(im.Bounds())
	draw.Draw(out, out.Bounds(), im, im.Bounds().Min, draw.Src)
	return out
}

func init() {
	for i := range frame {
		frame[i] = load("frame_" + strconv.Itoa(i) + ".png")
	}
	barCap, barTrack, barKnb = load("bar_cap.png"), load("bar_track.png"), load("bar_knob.png")
	f := load("font.png")
	glyphs = image.NewGray(f.Bounds())
	for i := range glyphs.Pix {
		glyphs.Pix[i] = f.Pix[i*4] // grayscale coverage
	}
	txt, _ := assets.ReadFile("assets/font.txt")
	for i, s := range strings.Fields(string(txt)) {
		if i < len(advance) {
			advance[i], _ = strconv.Atoi(s)
		}
	}
	advance[0] = 4 // space
}

// Rects of the controls.
var (
	newsRect   = image.Rect(44, 62, W-44, 290)
	statusY    = 302
	barRect    = image.Rect(44, 322, W-44, 346)
	slot       = [4]image.Rectangle{image.Rect(44, 364, 128, 394), image.Rect(136, 364, 220, 394), image.Rect(228, 364, 312, 394), image.Rect(320, 364, 404, 394)}
	bigSlot    = image.Rect(W-44-180, 358, W-44, 400)
	pageButton = [2]map[int]image.Rectangle{
		{Repair: slot[0], Folder: slot[1], Settings: slot[2], Exit: slot[3], Play: bigSlot},
		{Back: slot[0], Undo: slot[1], OpenIni: slot[2], Exit: slot[3], Save: bigSlot},
	}
	buttonRect = pageButton[0]
	buttonText = map[int]string{Play: "PLAY", Repair: "Repair", Folder: "Folder", Exit: "Exit",
		Settings: "Settings", Save: "SAVE", Back: "Back", Undo: "Undo", OpenIni: "Edit ini"}
)

// Settings layout: tabs across the top of the panel, rows in two columns.
const (
	tabY, tabH   = 68, 20
	tabW         = 96
	rowY0, rowDY = 96, 18
	rowsPerCol   = 10
	colW         = 270
	labelW       = 122
)

func tabRect(i int) image.Rectangle {
	x := newsRect.Min.X + 10 + i*(tabW+6)
	return image.Rect(x, tabY, x+tabW, tabY+tabH)
}

// rowRects returns row i's label origin and value box.
func rowRects(rows []SRow, i int) (image.Point, image.Rectangle) {
	col, r := rows[i].Col, 0
	for j := 0; j < i; j++ {
		if rows[j].Col == col {
			r++
		}
	}
	if r >= rowsPerCol { // overflow: next column
		col, r = col+1, r-rowsPerCol
	}
	x := newsRect.Min.X + 10 + col*(colW+12)
	y := rowY0 + r*rowDY
	return image.Pt(x, y), image.Rect(x+labelW, y-1, x+colW, y+rowDY-3)
}

func pageButtons(page int) map[int]image.Rectangle {
	if page == PageSettings {
		return pageButton[1]
	}
	return pageButton[0]
}

// ButtonAt returns the button under (x, y) on the main page, or None.
func ButtonAt(x, y int) int { return HitTest(State{}, x, y) }

// HitTest returns the control under (x, y) for st's page, or None.
func HitTest(st State, x, y int) int {
	p := image.Pt(x, y)
	for id, r := range pageButtons(st.Page) {
		if p.In(r) {
			return id
		}
	}
	if st.Page == PageSettings {
		for i := range st.Tabs {
			if p.In(tabRect(i)) {
				return TabID + i
			}
		}
		for i := range st.Rows {
			_, box := rowRects(st.Rows, i)
			if p.In(box) {
				if x < box.Min.X+box.Dx()/2 {
					return RowPrev + 2*i
				}
				return RowPrev + 2*i + 1
			}
		}
	}
	return None
}

// InNews reports whether (x, y) is over the news panel (for the scroll wheel).
func InNews(x, y int) bool { return image.Pt(x, y).In(newsRect) }

var (
	textCol  = color.RGBA{228, 220, 204, 255}
	goldCol  = color.RGBA{236, 196, 96, 255}
	dimCol   = color.RGBA{150, 136, 150, 255}
	shadow   = color.RGBA{16, 8, 16, 255}
	goldHi   = color.RGBA{240, 200, 104, 255}
	goldMid  = color.RGBA{176, 132, 52, 255}
	goldLo   = color.RGBA{96, 64, 20, 255}
	panelDim = color.RGBA{0, 0, 0, 110}
)

// AppTitle is shown in the frame, the window caption and the taskbar.
const AppTitle = "Biffin Boys Rage of Mages 2 HAT Autopatcher"

// BodyFont, when set, replaces the bitmap game font for body text (news,
// status, folder, version). Its text is not drawn into the image; RenderOps
// returns it as TextOps for the caller to draw (Windows: GDI ClearType).
type BodyFont interface {
	Width(s string) int // in skin pixels
	Height() int        // line height in skin pixels
}

// TextOp is one piece of body text at skin-pixel position (X, Y) (top-left).
type TextOp struct {
	X, Y int
	S    string
	C    color.RGBA
}

// Body is the body font in use; nil means the bitmap game font.
var Body BodyFont

func bodyW(s string) int {
	if Body != nil {
		return Body.Width(s)
	}
	return TextWidth(s, 1)
}

func lineHeight() int {
	if Body != nil {
		return Body.Height() + 3
	}
	return lineH
}

// Render paints the whole window, body text included (via the game font,
// or dropped if Body is set; use RenderOps then).
func Render(st State) *image.RGBA { im, _ := RenderOps(st); return im }

// RenderOps paints the window. Pixels outside the frame are transparent
// (alpha 0); the Windows side turns them into the window shape.
func RenderOps(st State) (*image.RGBA, []TextOp) {
	var ops []TextOp
	var im *image.RGBA
	body := func(x, y int, t string, c color.RGBA) {
		if Body != nil {
			ops = append(ops, TextOp{x, y, t, c})
		} else {
			Text(im, x, y, t, c, 1)
		}
	}
	im = image.NewRGBA(image.Rect(0, 0, W, H))
	// Patterned purple background inside the frame.
	tile(im, frame[0], image.Rect(16, 16, W-16, H-16))
	// Edges, then the skull corners on top.
	tile(im, frame[2], image.Rect(48, 0, W-48, 48))
	tile(im, frame[7], image.Rect(48, H-48, W-48, H))
	tile(im, frame[4], image.Rect(0, 48, 48, H-48))
	tile(im, frame[5], image.Rect(W-48, 48, W, H-48))
	over(im, frame[1], 0, 0)
	over(im, frame[3], W-48, 0)
	over(im, frame[6], 0, H-48)
	over(im, frame[8], W-48, H-48)

	title := st.Title
	if title == "" {
		title = AppTitle
	}
	Text(im, (W-TextWidth(title, 1))/2, 30, title, goldCol, 1)

	// News panel (or the settings panel).
	fill(im, newsRect, panelDim)
	bevel(im, newsRect, goldLo, goldMid)
	if st.Page == PageSettings {
		renderSettings(im, st, body)
	} else {
		renderNews(im, st, body)
	}

	// Status and slider-style progress bar.
	status := st.Status
	for bodyW(status) > barRect.Dx()-70 && len(status) > 4 {
		status = strings.TrimSuffix(status, "...")
		status = status[:len(status)-1] + "..."
	}
	body(barRect.Min.X+4, statusY, status, textCol)
	drawBar(im, st.Progress)

	for id, r := range pageButtons(st.Page) {
		drawButton(im, r, buttonText[id], id == Play || id == Save, st.Busy && id != Exit, st.Hover == id, st.Pressed == id)
	}
	if st.Version != "" {
		v := "v" + st.Version
		body(barRect.Max.X-4-bodyW(v), statusY, v, dimCol)
	}
	return im, ops
}

func renderNews(im *image.RGBA, st State, body func(x, y int, t string, c color.RGBA)) {
	inner := newsRect.Inset(10)
	lines := wrap(st.News, inner.Dx())
	maxLines := (inner.Dy() - 18) / lineHeight()
	if st.Scroll > len(lines)-maxLines {
		st.Scroll = len(lines) - maxLines
	}
	if st.Scroll < 0 {
		st.Scroll = 0
	}
	y := inner.Min.Y
	for i := st.Scroll; i < len(lines) && i < st.Scroll+maxLines; i++ {
		col := textCol
		if strings.HasPrefix(lines[i], "#") {
			lines[i], col = strings.TrimLeft(lines[i], "# "), goldCol
		}
		body(inner.Min.X, y, lines[i], col)
		y += lineHeight()
	}
	if len(lines) > maxLines {
		body(inner.Max.X-bodyW("more..."), inner.Max.Y-15, "more...", dimCol)
	}
	foot := st.Folder
	if foot != "" {
		foot = "Folder: " + foot
		for bodyW(foot) > inner.Dx()-120 && len(foot) > 12 {
			foot = foot[:8] + "..." + foot[12:]
		}
		body(inner.Min.X, inner.Max.Y-15, foot, dimCol)
	}
}

func renderSettings(im *image.RGBA, st State, body func(x, y int, t string, c color.RGBA)) {
	for i, t := range st.Tabs {
		r := tabRect(i)
		on := i == st.Tab
		hov := st.Hover == TabID+i
		c := color.RGBA{40, 24, 44, 255}
		if on {
			c = color.RGBA{72, 48, 78, 255}
		}
		fill(im, r, c)
		if on {
			bevel(im, r, goldHi, goldLo)
		} else {
			bevel(im, r, goldLo, shadow)
		}
		col := dimCol
		if on || hov {
			col = goldCol
		}
		Text(im, r.Min.X+(r.Dx()-TextWidth(t, 1))/2, r.Min.Y+3, t, col, 1)
	}
	fill(im, image.Rect(newsRect.Min.X+10, tabY+tabH, newsRect.Max.X-10, tabY+tabH+1), goldLo)
	for i, row := range st.Rows {
		lp, box := rowRects(st.Rows, i)
		lc := textCol
		if row.Disabled {
			lc = dimCol
		}
		body(lp.X, lp.Y, row.Label, lc)
		fill(im, box, color.RGBA{20, 10, 22, 200})
		bevel(im, box, shadow, goldLo)
		hp, hn := st.Hover == RowPrev+2*i, st.Hover == RowPrev+2*i+1
		if !row.Disabled {
			arrow(im, box.Min.X+4, box.Min.Y+box.Dy()/2, -1, hp)
			arrow(im, box.Max.X-5, box.Min.Y+box.Dy()/2, 1, hn)
		}
		vc := textCol
		switch {
		case row.Disabled || row.Value == "Custom":
			vc = dimCol
		case row.Changed:
			vc = goldCol
		}
		v := row.Value
		for bodyW(v) > box.Dx()-24 && len(v) > 3 {
			v = v[:len(v)-1]
		}
		body(box.Min.X+(box.Dx()-bodyW(v))/2, lp.Y, v, vc)
	}
}

// arrow draws a small solid triangle pointing left (d<0) or right at (x, cy).
func arrow(im *image.RGBA, x, cy, d int, hot bool) {
	c := goldMid
	if hot {
		c = goldHi
	}
	for i := 0; i < 5; i++ {
		px := x + i
		if d > 0 {
			px = x - i
		}
		fill(im, image.Rect(px, cy-i, px+1, cy+i+1), c)
	}
}

const lineH = 17

func drawBar(im *image.RGBA, p float64) {
	r := barRect
	tile(im, barTrack, image.Rect(r.Min.X+24, r.Min.Y, r.Max.X-24, r.Max.Y))
	if p >= 0 {
		if p > 1 {
			p = 1
		}
		lo, hi := r.Min.X+24, r.Max.X-24
		x := lo + int(float64(hi-lo)*p)
		fill(im, image.Rect(lo, r.Min.Y+9, x, r.Min.Y+15), color.RGBA{214, 120, 236, 200})
		fill(im, image.Rect(lo, r.Min.Y+10, x, r.Min.Y+11), color.RGBA{255, 210, 255, 160})
		kx := x - barKnb.Bounds().Dx()/2
		if kx < lo {
			kx = lo
		}
		if kx > hi-barKnb.Bounds().Dx() {
			kx = hi - barKnb.Bounds().Dx()
		}
		over(im, barKnb, kx, r.Min.Y)
	}
	over(im, barCap, r.Min.X, r.Min.Y)
	over(im, barCap, r.Max.X-24, r.Min.Y)
}

func drawButton(im *image.RGBA, r image.Rectangle, label string, big, disabled, hover, pressed bool) {
	// Dark plum face with the game's thin gold rim, like the Ok/Cancel buttons.
	for y := r.Min.Y; y < r.Max.Y; y++ {
		t := float64(y-r.Min.Y) / float64(r.Dy())
		c := color.RGBA{uint8(52 - 22*t), uint8(34 - 16*t), uint8(56 - 22*t), 255}
		if hover && !disabled {
			c.R, c.G, c.B = c.R+16, c.G+8, c.B+18
		}
		fill(im, image.Rect(r.Min.X, y, r.Max.X, y+1), c)
	}
	outline(im, r, shadow)
	hi, lo := goldHi, goldLo
	if pressed {
		hi, lo = lo, hi
	}
	bevel(im, r.Inset(1), hi, lo)
	outline(im, r.Inset(2), color.RGBA{24, 12, 24, 255})
	if big {
		bevel(im, r.Inset(3), goldMid, goldLo)
	}
	col := textCol
	switch {
	case disabled:
		col = dimCol
	case hover:
		col = goldCol
	}
	scale := 1
	if big {
		scale = 2
	}
	tx := r.Min.X + (r.Dx()-TextWidth(label, scale))/2
	ty := r.Min.Y + (r.Dy()-15*scale)/2 + 1
	if pressed {
		tx, ty = tx+1, ty+1
	}
	Text(im, tx, ty, label, col, scale)
}

// --- text ---

func glyphIndex(r rune) int {
	switch r {
	case '…':
		r = '.'
	case '—', '–':
		r = '-'
	case '‘', '’':
		r = '\''
	case '“', '”':
		r = '"'
	}
	if r >= 32 && r < 256 {
		return int(r) - 32
	}
	return '?' - 32
}

func expand(s string) string { return strings.ReplaceAll(s, "…", "...") }

// TextWidth is the width of s in pixels at the given scale.
func TextWidth(s string, scale int) int {
	w := 0
	for _, r := range expand(s) {
		w += (advance[glyphIndex(r)] + 2) * scale
	}
	return w
}

// Text draws s with a 1px drop shadow, top-left at (x, y).
func Text(im *image.RGBA, x, y int, s string, c color.RGBA, scale int) {
	textRaw(im, x+scale, y+scale, s, shadow, scale)
	textRaw(im, x, y, s, c, scale)
}

func textRaw(im *image.RGBA, x, y int, s string, c color.RGBA, scale int) {
	for _, r := range expand(s) {
		g := glyphIndex(r)
		gx, gy := (g%16)*16, (g/16)*15
		for j := 0; j < 15; j++ {
			for i := 0; i < 16; i++ {
				a := glyphs.GrayAt(gx+i, gy+j).Y
				if a == 0 {
					continue
				}
				for sy := 0; sy < scale; sy++ {
					for sx := 0; sx < scale; sx++ {
						blend(im, x+i*scale+sx, y+j*scale+sy, c, a)
					}
				}
			}
		}
		x += (advance[g] + 2) * scale
	}
}

// Wrap splits text into lines no wider than w pixels. Explicit newlines are kept.
func Wrap(text string, w int) []string {
	return wrapWith(text, w, func(s string) int { return TextWidth(s, 1) })
}

func wrap(text string, w int) []string { return wrapWith(text, w, bodyW) }

func wrapWith(text string, w int, width func(string) int) []string {
	var out []string
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			try := word
			if line != "" {
				try = line + " " + word
			}
			if width(try) <= w || line == "" {
				line = try
				continue
			}
			out = append(out, line)
			line = word
		}
		out = append(out, line)
	}
	return out
}

// --- pixels ---

func blend(im *image.RGBA, x, y int, c color.RGBA, a uint8) {
	if !(image.Point{x, y}).In(im.Rect) {
		return
	}
	i := im.PixOffset(x, y)
	p := im.Pix[i : i+4]
	A := uint32(a) * uint32(c.A) / 255
	p[0] = uint8((uint32(c.R)*A + uint32(p[0])*(255-A)) / 255)
	p[1] = uint8((uint32(c.G)*A + uint32(p[1])*(255-A)) / 255)
	p[2] = uint8((uint32(c.B)*A + uint32(p[2])*(255-A)) / 255)
	p[3] = uint8(A + uint32(p[3])*(255-A)/255)
}

func fill(im *image.RGBA, r image.Rectangle, c color.RGBA) {
	draw.Draw(im, r, image.NewUniform(c), image.Point{}, draw.Over)
}

func outline(im *image.RGBA, r image.Rectangle, c color.RGBA) { bevel(im, r, c, c) }

// bevel draws a 1px rectangle: hi on top/left, lo on bottom/right.
func bevel(im *image.RGBA, r image.Rectangle, hi, lo color.RGBA) {
	fill(im, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+1), hi)
	fill(im, image.Rect(r.Min.X, r.Min.Y, r.Min.X+1, r.Max.Y), hi)
	fill(im, image.Rect(r.Min.X, r.Max.Y-1, r.Max.X, r.Max.Y), lo)
	fill(im, image.Rect(r.Max.X-1, r.Min.Y, r.Max.X, r.Max.Y), lo)
}

func over(im *image.RGBA, src *image.RGBA, x, y int) {
	draw.Draw(im, src.Bounds().Add(image.Pt(x, y)), src, image.Point{}, draw.Over)
}

func tile(im *image.RGBA, src *image.RGBA, r image.Rectangle) {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	for y := r.Min.Y; y < r.Max.Y; y += sh {
		for x := r.Min.X; x < r.Max.X; x += sw {
			draw.Draw(im, image.Rect(x, y, x+sw, y+sh).Intersect(r), src, image.Point{}, draw.Over)
		}
	}
}

// Shape returns the opaque runs of im, row by row, for a window region.
func Shape(im *image.RGBA) []image.Rectangle {
	var out []image.Rectangle
	for y := 0; y < H; y++ {
		x := 0
		for x < W {
			for x < W && im.Pix[im.PixOffset(x, y)+3] < 128 {
				x++
			}
			s := x
			for x < W && im.Pix[im.PixOffset(x, y)+3] >= 128 {
				x++
			}
			if x > s {
				out = append(out, image.Rect(s, y, x, y+1))
			}
		}
	}
	return out
}
