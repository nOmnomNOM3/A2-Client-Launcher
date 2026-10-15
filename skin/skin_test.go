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
