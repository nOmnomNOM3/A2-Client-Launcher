package skin

import "testing"

func TestRenderAndHits(t *testing.T) {
	im := Render(State{News: "hello\nworld", Status: "Ready", Progress: 0.5, Hover: Play})
	if im.Bounds().Dx() != W || im.Bounds().Dy() != H {
		t.Fatal("size")
	}
	if im.Pix[im.PixOffset(W/2, H/2)+3] != 255 {
		t.Fatal("centre should be opaque")
	}
	if im.Pix[3] != 0 {
		t.Fatal("top-left corner outside the skull should be transparent")
	}
	if len(Shape(im)) == 0 {
		t.Fatal("empty shape")
	}
	for id, r := range buttonRect {
		c := r.Min.Add(r.Size().Div(2))
		if ButtonAt(c.X, c.Y) != id {
			t.Fatalf("button %d not hit", id)
		}
	}
	if ButtonAt(5, 5) != None {
		t.Fatal("frame is not a button")
	}
}

func TestWrap(t *testing.T) {
	for _, l := range Wrap("the quick brown fox jumps over the lazy dog again and again", 120) {
		if TextWidth(l, 1) > 120 {
			t.Fatalf("line too wide: %q", l)
		}
	}
	if got := len(Wrap("a\nb\n\nc", 500)); got != 4 {
		t.Fatalf("lines = %d", got)
	}
}

func TestSettingsHitTest(t *testing.T) {
	st := State{Page: PageSettings, Tabs: []string{"A", "B"}, Rows: []SRow{{Label: "x"}, {Label: "y", Col: 1}}}
	_, b0 := rowRects(st.Rows, 0)
	_, b1 := rowRects(st.Rows, 1)
	if HitTest(st, b0.Min.X+3, b0.Min.Y+3) != RowPrev || HitTest(st, b0.Max.X-3, b0.Min.Y+3) != RowPrev+1 {
		t.Fatal("row 0 arrows")
	}
	if HitTest(st, b1.Max.X-3, b1.Min.Y+3) != RowPrev+3 || b1.Min.X <= b0.Max.X {
		t.Fatal("row 1 should be in the right column")
	}
	r := tabRect(1)
	if HitTest(st, r.Min.X+2, r.Min.Y+2) != TabID+1 {
		t.Fatal("tab")
	}
	if HitTest(st, bigSlot.Min.X+5, bigSlot.Min.Y+5) != Save || HitTest(State{}, bigSlot.Min.X+5, bigSlot.Min.Y+5) != Play {
		t.Fatal("page buttons")
	}
}

func TestRepairReadsInstallWhenFresh(t *testing.T) {
	if got := ButtonLabel(State{Fresh: true}, Repair); got != "Install" {
		t.Fatalf("fresh: %q", got)
	}
	if got := ButtonLabel(State{}, Repair); got != "Repair" {
		t.Fatalf("installed: %q", got)
	}
	if got := ButtonLabel(State{Fresh: true}, Play); got != "PLAY" {
		t.Fatalf("play: %q", got)
	}
}
