//go:build windows

package main

// The window is plain Win32 painted with GDI: the skin package renders the
// frame (game art, title and button labels in the game font) into an image,
// WM_PAINT blits it at a whole-number scale (nearest neighbour) and then draws
// the body text (news, status, folder) with GDI ClearType Segoe UI at the real
// device size. The process is per-monitor DPI aware (manifest), so Windows
// never bitmap-stretches the window: that stretching was what made v1.1.0
// blurry. This keeps every pixel in the game's style (walk/common
// controls cannot draw skull frames or the game font), needs no extra
// dependency beyond golang.org/x/sys, and the exact same Render code makes the
// headless preview (cmd/uipreview).

import (
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"image"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/nOmnomNOM3/A2-Client-Launcher/core"
	"github.com/nOmnomNOM3/A2-Client-Launcher/skin"
)

const hasGUI = true

//go:embed game.ico
var gameIco []byte

func init() { runtime.LockOSThread() } // Win32 UI must stay on one OS thread

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassEx   = user32.NewProc("RegisterClassExW")
	pCreateWindowEx    = user32.NewProc("CreateWindowExW")
	pDefWindowProc     = user32.NewProc("DefWindowProcW")
	pGetMessage        = user32.NewProc("GetMessageW")
	pTranslateMessage  = user32.NewProc("TranslateMessage")
	pDispatchMessage   = user32.NewProc("DispatchMessageW")
	pPostMessage       = user32.NewProc("PostMessageW")
	pPostQuitMessage   = user32.NewProc("PostQuitMessage")
	pShowWindow        = user32.NewProc("ShowWindow")
	pAllowSetFg        = user32.NewProc("AllowSetForegroundWindow")
	pBeginPaint        = user32.NewProc("BeginPaint")
	pEndPaint          = user32.NewProc("EndPaint")
	pInvalidateRect    = user32.NewProc("InvalidateRect")
	pLoadCursor        = user32.NewProc("LoadCursorW")
	pSetWindowRgn      = user32.NewProc("SetWindowRgn")
	pGetSystemMetrics  = user32.NewProc("GetSystemMetrics")
	pMessageBox        = user32.NewProc("MessageBoxW")
	pSetCapture        = user32.NewProc("SetCapture")
	pReleaseCapture    = user32.NewProc("ReleaseCapture")
	pScreenToClient    = user32.NewProc("ScreenToClient")
	pSendMessage       = user32.NewProc("SendMessageW")
	pGetDC             = user32.NewProc("GetDC")
	pReleaseDC         = user32.NewProc("ReleaseDC")
	pCreateIconFromRes = user32.NewProc("CreateIconFromResourceEx")
	pTrackMouseEvent   = user32.NewProc("TrackMouseEvent")
	pDestroyWindow     = user32.NewProc("DestroyWindow")
	pGetDpiForWindow   = user32.NewProc("GetDpiForWindow")
	pSetWindowPos      = user32.NewProc("SetWindowPos")
	pBitBlt            = gdi32.NewProc("BitBlt")
	pCreateCompatDC    = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatBmp   = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject      = gdi32.NewProc("SelectObject")
	pDeleteDC          = gdi32.NewProc("DeleteDC")
	pCreateFont        = gdi32.NewProc("CreateFontW")
	pSetBkMode         = gdi32.NewProc("SetBkMode")
	pSetTextColor      = gdi32.NewProc("SetTextColor")
	pTextOut           = gdi32.NewProc("TextOutW")
	pGetTextExtent     = gdi32.NewProc("GetTextExtentPoint32W")
	pSetDIBits         = gdi32.NewProc("SetDIBitsToDevice")
	pCreateRectRgn     = gdi32.NewProc("CreateRectRgn")
	pCombineRgn        = gdi32.NewProc("CombineRgn")
	pDeleteObject      = gdi32.NewProc("DeleteObject")
	pGetDeviceCaps     = gdi32.NewProc("GetDeviceCaps")
	pSHBrowseForFolder = shell32.NewProc("SHBrowseForFolderW")
	pSHGetPathFromID   = shell32.NewProc("SHGetPathFromIDListW")
	pCoInitializeEx    = ole32.NewProc("CoInitializeEx")
	pCoTaskMemFree     = ole32.NewProc("CoTaskMemFree")
	pGetModuleHandle   = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmDestroy     = 0x0002
	wmPaint       = 0x000F
	wmClose       = 0x0010
	wmEraseBkgnd  = 0x0014
	wmSetIcon     = 0x0080
	wmNCHitTest   = 0x0084
	wmKeyDown     = 0x0100
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmMouseWheel  = 0x020A
	wmMouseLeave  = 0x02A3
	wmApp         = 0x8000
	wmDPIChanged  = 0x02E0

	htClient  = 1
	htCaption = 2

	mbOK          = 0x0
	mbYesNo       = 0x4
	mbIconError   = 0x10
	mbIconWarning = 0x30
	mbIconInfo    = 0x40
	idYes         = 6
)

type wndClassEx struct {
	cbSize, style          uint32
	wndProc                uintptr
	clsExtra, wndExtra     int32
	instance, icon, cursor uintptr
	background             uintptr
	menuName, className    *uint16
	iconSm                 uintptr
}

type msgT struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      [2]int32
	private uint32
}

type paintStruct struct {
	hdc       uintptr
	erase     int32
	rc        [4]int32
	restore   int32
	incUpdate int32
	reserved  [32]byte
}

type bitmapInfo struct {
	size                   uint32
	width, height          int32
	planes, bitCount       uint16
	compression, sizeImage uint32
	xPPM, yPPM             int32
	clrUsed, clrImportant  uint32
	colors                 [4]uint32
}

type trackMouse struct {
	size, flags uint32
	hwnd        uintptr
	hover       uint32
}

// winUI is the launcher window. All fields are touched on the UI thread only.
type winUI struct {
	s        *session
	hwnd     uintptr
	scale    int
	st       skin.State
	frame    []byte // BGRA, scaled
	mu       sync.Mutex
	queue    []func()
	qmu      sync.Mutex
	tracking bool
	ops      []skin.TextOp
	font     *gdiFont
}

// gdiFont is the crisp body font: Segoe UI with ClearType, created at the
// window's device scale. It implements skin.BodyFont in skin pixels.
type gdiFont struct {
	hfont, dc uintptr
	k         int
}

func newGDIFont(k int) *gdiFont {
	face, _ := syscall.UTF16PtrFromString("Segoe UI")
	h := -13 * k // 13 skin px tall characters, like the game font
	hf, _, _ := pCreateFont.Call(uintptr(h), 0, 0, 0, 400, 0, 0, 0, 1 /*DEFAULT_CHARSET*/, 0, 0,
		5 /*CLEARTYPE_QUALITY*/, 0, uintptr(unsafe.Pointer(face)))
	dc, _, _ := pCreateCompatDC.Call(0)
	pSelectObject.Call(dc, hf)
	return &gdiFont{hfont: hf, dc: dc, k: k}
}

func (f *gdiFont) free() {
	pDeleteDC.Call(f.dc)
	pDeleteObject.Call(f.hfont)
}

func (f *gdiFont) extent(s string) (int, int) {
	u := syscall.StringToUTF16(s)
	var sz [2]int32
	if len(u) > 1 {
		pGetTextExtent.Call(f.dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&sz)))
	}
	return int(sz[0]), int(sz[1])
}

func (f *gdiFont) Width(s string) int { w, _ := f.extent(s); return (w + f.k - 1) / f.k }
func (f *gdiFont) Height() int {
	_, h := f.extent("Ag")
	return (h + f.k - 1) / f.k
}

// setScale switches to whole-number scale k (window size, shape and font).
func (w *winUI) setScale(k int) {
	w.scale = k
	if w.font != nil {
		w.font.free()
	}
	w.font = newGDIFont(k)
	skin.Body = w.font
}

var theUI *winUI

// sync runs f on the UI thread (like walk's Synchronize).
func (w *winUI) sync(f func()) {
	w.qmu.Lock()
	w.queue = append(w.queue, f)
	w.qmu.Unlock()
	pPostMessage.Call(w.hwnd, wmApp, 0, 0)
}

func (w *winUI) Status(t string) { w.sync(func() { w.st.Status = t; w.redraw() }) }
func (w *winUI) News(t string) {
	if t == "" {
		return
	}
	w.sync(func() { w.st.News = t; w.st.Scroll = 0; w.redraw() })
}
func (w *winUI) Bytes(done, total int64) {
	p := 0.0
	if total > 0 {
		p = float64(done) / float64(total)
	}
	w.sync(func() {
		if int(p*1000) != int(w.st.Progress*1000) {
			w.st.Progress = p
			w.redraw()
		}
	})
}

func (w *winUI) busy(b bool) { w.st.Busy = b; w.redraw() }

func (w *winUI) redraw() {
	im, ops := skin.RenderOps(w.st)
	w.frame, w.ops = toBGRA(im, w.scale), ops
	pInvalidateRect.Call(w.hwnd, 0, 0)
}

// toBGRA converts to a top-down BGRA buffer, nearest-neighbour scaled by k.
func toBGRA(im *image.RGBA, k int) []byte {
	W, H := skin.W*k, skin.H*k
	out := make([]byte, W*H*4)
	for y := 0; y < H; y++ {
		src := im.Pix[(y/k)*im.Stride:]
		row := out[y*W*4:]
		for x := 0; x < W; x++ {
			s := src[(x/k)*4:]
			row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = s[2], s[1], s[0], 255
		}
	}
	return out
}

func msgBox(hwnd uintptr, text string, flags uintptr) uintptr {
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(skin.AppTitle)
	r, _, _ := pMessageBox.Call(hwnd, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), flags)
	return r
}

func showCrashDialog(hwnd uintptr, zipPath string) {
	if msgBox(hwnd, "The game crashed. A report was saved to:\n"+zipPath+"\n\nOpen the folder now?", mbYesNo|mbIconError) == idYes {
		exec.Command("explorer", "/select,", zipPath).Start()
	}
}

// browseFolder shows the shell folder picker; "" if cancelled.
func browseFolder(hwnd uintptr, title string) string {
	t, _ := syscall.UTF16PtrFromString(title)
	var bi struct {
		owner       uintptr
		root        uintptr
		displayName *uint16
		title       *uint16
		flags       uint32
		fn          uintptr
		lparam      uintptr
		image       int32
	}
	buf := make([]uint16, 260)
	bi.owner, bi.displayName, bi.title = hwnd, &buf[0], t
	bi.flags = 0x0001 | 0x0040 // BIF_RETURNONLYFSDIRS | BIF_NEWDIALOGSTYLE
	pidl, _, _ := pSHBrowseForFolder.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return ""
	}
	defer pCoTaskMemFree.Call(pidl)
	path := make([]uint16, 1024)
	if r, _, _ := pSHGetPathFromID.Call(pidl, uintptr(unsafe.Pointer(&path[0]))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(path)
}

// loadIcon builds an HICON of the given size from the embedded game.ico.
func loadIcon(size int) uintptr {
	if len(gameIco) < 6 {
		return 0
	}
	n := int(binary.LittleEndian.Uint16(gameIco[4:]))
	best, bestBits := -1, 0
	for i := 0; i < n && 6+16*i+16 <= len(gameIco); i++ {
		e := gameIco[6+16*i:]
		w, bits := int(e[0]), int(binary.LittleEndian.Uint16(e[6:]))
		if w == 0 {
			w = 256
		}
		if w == size && bits > bestBits {
			best, bestBits = i, bits
		}
	}
	if best < 0 {
		return 0
	}
	e := gameIco[6+16*best:]
	sz, off := binary.LittleEndian.Uint32(e[8:]), binary.LittleEndian.Uint32(e[12:])
	if int(off+sz) > len(gameIco) {
		return 0
	}
	h, _, _ := pCreateIconFromRes.Call(uintptr(unsafe.Pointer(&gameIco[off])), uintptr(sz), 1, 0x00030000, uintptr(size), uintptr(size), 0)
	return h
}

// scaleFor picks the whole-number scale for a DPI: 100-125% -> 1,
// 150-249% -> 2, ... Integer scaling with nearest neighbour keeps the game art
// pixel-exact; the body text is drawn by GDI at the true size anyway.
func scaleFor(dpi int) int {
	k := (dpi + 48) / 96
	if k < 1 {
		k = 1
	}
	sw, _, _ := pGetSystemMetrics.Call(0)
	sh, _, _ := pGetSystemMetrics.Call(1)
	for k > 1 && (int(sw) < skin.W*k || int(sh) < skin.H*k) {
		k--
	}
	return k
}

func systemDPI() int {
	dc, _, _ := pGetDC.Call(0)
	dpi, _, _ := pGetDeviceCaps.Call(dc, 88) // LOGPIXELSX
	pReleaseDC.Call(0, dc)
	return int(dpi)
}

func windowDPI(hwnd uintptr) int {
	if pGetDpiForWindow.Find() == nil { // Windows 10 1607+
		if d, _, _ := pGetDpiForWindow.Call(hwnd); d != 0 {
			return int(d)
		}
	}
	return systemDPI()
}

func (w *winUI) setShape() {
	im, _ := skin.RenderOps(w.st)
	k := int32(w.scale)
	rgn, _, _ := pCreateRectRgn.Call(0, 0, 0, 0)
	for _, r := range skin.Shape(im) {
		p, _, _ := pCreateRectRgn.Call(uintptr(int32(r.Min.X)*k), uintptr(int32(r.Min.Y)*k), uintptr(int32(r.Max.X)*k), uintptr(int32(r.Max.Y)*k))
		pCombineRgn.Call(rgn, rgn, p, 2) // RGN_OR
		pDeleteObject.Call(p)
	}
	pSetWindowRgn.Call(w.hwnd, rgn, 1) // the system owns rgn now
}

func lparamXY(lp uintptr) (int, int) {
	return int(int16(lp & 0xffff)), int(int16((lp >> 16) & 0xffff))
}

func (w *winUI) click(id int) {
	if w.st.Busy && id != skin.Exit {
		return
	}
	switch id {
	case skin.Play:
		core.SaveConfig(w.s.cfg)
		w.run(false, true)
	case skin.Repair:
		w.run(true, false)
	case skin.Folder:
		if d := browseFolder(w.hwnd, "Choose the game folder (a new empty folder is fine)"); d != "" {
			w.s.cfg.InstallDir = d
			w.st.Folder = d
			core.SaveConfig(w.s.cfg)
			w.redraw()
		}
	case skin.Exit:
		pDestroyWindow.Call(w.hwnd)
	}
}

func wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	w := theUI
	if w == nil || w.hwnd == 0 {
		r, _, _ := pDefWindowProc.Call(hwnd, uintptr(msg), wp, lp)
		return r
	}
	k := w.scale
	switch msg {
	case wmApp:
		w.qmu.Lock()
		q := w.queue
		w.queue = nil
		w.qmu.Unlock()
		for _, f := range q {
			f()
		}
		return 0
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		var ps paintStruct
		dc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		W, H := skin.W*k, skin.H*k
		if len(w.frame) < W*H*4 {
			pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
			return 0
		}
		bi := bitmapInfo{size: 40, width: int32(W), height: -int32(H), planes: 1, bitCount: 32}
		// Compose off-screen (no flicker): art at integer scale, then text.
		mdc, _, _ := pCreateCompatDC.Call(dc)
		bmp, _, _ := pCreateCompatBmp.Call(dc, uintptr(W), uintptr(H))
		oldBmp, _, _ := pSelectObject.Call(mdc, bmp)
		pSetDIBits.Call(mdc, 0, 0, uintptr(W), uintptr(H), 0, 0, 0, uintptr(H),
			uintptr(unsafe.Pointer(&w.frame[0])), uintptr(unsafe.Pointer(&bi)), 0)
		oldFont, _, _ := pSelectObject.Call(mdc, w.font.hfont)
		pSetBkMode.Call(mdc, 1) // TRANSPARENT
		for _, op := range w.ops {
			u := syscall.StringToUTF16(op.S)
			if len(u) < 2 {
				continue
			}
			x, y := op.X*k, op.Y*k
			pSetTextColor.Call(mdc, 0x081008) // shadow (BGR)
			pTextOut.Call(mdc, uintptr(x+1), uintptr(y+1), uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1))
			pSetTextColor.Call(mdc, uintptr(op.C.R)|uintptr(op.C.G)<<8|uintptr(op.C.B)<<16)
			pTextOut.Call(mdc, uintptr(x), uintptr(y), uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1))
		}
		pBitBlt.Call(dc, 0, 0, uintptr(W), uintptr(H), mdc, 0, 0, 0x00CC0020) // SRCCOPY
		pSelectObject.Call(mdc, oldFont)
		pSelectObject.Call(mdc, oldBmp)
		pDeleteObject.Call(bmp)
		pDeleteDC.Call(mdc)
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmDPIChanged:
		// Moved to a monitor with another scaling: re-render at its scale.
		if k2 := scaleFor(int(wp & 0xffff)); k2 != w.scale {
			rc := (*[4]int32)(unsafe.Pointer(lp))
			w.setScale(k2)
			pSetWindowPos.Call(hwnd, 0, uintptr(rc[0]), uintptr(rc[1]), uintptr(skin.W*k2), uintptr(skin.H*k2), 0x0014) // NOZORDER|NOACTIVATE
			w.setShape()
			w.redraw()
		}
		return 0
	case wmNCHitTest:
		x, y := lparamXY(lp)
		pt := [2]int32{int32(x), int32(y)}
		pScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
		cx, cy := int(pt[0])/k, int(pt[1])/k
		if skin.ButtonAt(cx, cy) != skin.None || skin.InNews(cx, cy) {
			return htClient
		}
		return htCaption // drag the window by its frame and background
	case wmMouseMove:
		x, y := lparamXY(lp)
		if !w.tracking {
			tm := trackMouse{size: uint32(unsafe.Sizeof(trackMouse{})), flags: 2, hwnd: hwnd} // TME_LEAVE
			pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tm)))
			w.tracking = true
		}
		if h := skin.ButtonAt(x/k, y/k); h != w.st.Hover {
			w.st.Hover = h
			w.redraw()
		}
		return 0
	case wmMouseLeave:
		w.tracking = false
		if w.st.Hover != skin.None {
			w.st.Hover = skin.None
			w.redraw()
		}
		return 0
	case wmLButtonDown:
		x, y := lparamXY(lp)
		if b := skin.ButtonAt(x/k, y/k); b != skin.None {
			w.st.Pressed = b
			pSetCapture.Call(hwnd)
			w.redraw()
		}
		return 0
	case wmLButtonUp:
		x, y := lparamXY(lp)
		b := w.st.Pressed
		if b != skin.None {
			pReleaseCapture.Call()
			w.st.Pressed = skin.None
			w.redraw()
			if skin.ButtonAt(x/k, y/k) == b {
				w.click(b)
			}
		}
		return 0
	case wmMouseWheel:
		d := int(int16(wp >> 16))
		if d > 0 {
			w.st.Scroll -= 3
		} else {
			w.st.Scroll += 3
		}
		if w.st.Scroll < 0 {
			w.st.Scroll = 0
		}
		w.redraw()
		return 0
	case wmKeyDown:
		switch wp {
		case 0x0D: // Enter
			w.click(skin.Play)
		case 0x1B: // Esc
			w.click(skin.Exit)
		}
		return 0
	case wmClose:
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func (w *winUI) run(full, launch bool) {
	if !w.mu.TryLock() {
		return
	}
	w.busy(true)
	s := w.s
	go func() {
		defer w.mu.Unlock()
		restart, err := s.update(context.Background(), w, full)
		if restart {
			restartSelf()
		}
		w.sync(func() {
			if err != nil {
				w.st.Status = "Problem: see message"
				w.redraw()
				if !launch || s.manifest != nil {
					w.busy(false)
					msgBox(w.hwnd, err.Error(), mbOK|mbIconError)
					return
				}
				// Offline: offer to play with the files we have.
				if msgBox(w.hwnd, err.Error()+"\n\nStart the game anyway with the files you already have?", mbYesNo|mbIconWarning) != idYes {
					w.busy(false)
					return
				}
			}
			if !launch {
				w.busy(false)
				return
			}
			w.playAndWatch()
		})
	}()
}

// playAndWatch runs on the UI thread; all waiting happens in goroutines and
// every failure path re-enables the buttons and shows the window.
func (w *winUI) playAndWatch() {
	s := w.s
	fail := func(msg string) {
		pShowWindow.Call(w.hwnd, 5) // SW_SHOW
		w.st.Status = "Problem: see message"
		w.busy(false)
		msgBox(w.hwnd, msg, mbOK|mbIconError)
	}
	before := core.SnapshotLogs(s.cfg.InstallDir)
	// Let the game take the foreground: a DirectDraw game started behind the
	// launcher can sit invisible waiting for focus.
	pAllowSetFg.Call(^uintptr(0)) // ASFW_ANY
	run, err := s.startGame()
	if err != nil {
		fail(err.Error())
		return
	}
	w.st.Status = "Starting the game..."
	w.redraw()
	go func() {
		exited, res, err := s.earlyCheck(run)
		if err != nil {
			w.sync(func() { fail(err.Error()) })
			return
		}
		w.sync(func() {
			w.st.Status = "Game running..."
			w.redraw()
			pShowWindow.Call(w.hwnd, 0) // SW_HIDE
		})
		zipPath, err := s.watchGame(run, exited, res, before)
		w.sync(func() {
			if err != nil {
				fail(err.Error())
				return
			}
			if zipPath != "" {
				pShowWindow.Call(w.hwnd, 5)
				w.busy(false)
				showCrashDialog(w.hwnd, zipPath)
			}
			os.Exit(0)
		})
	}()
}

func runGUI(s *session) {
	pCoInitializeEx.Call(0, 0x2) // apartment-threaded, for the folder picker
	w := &winUI{s: s}
	w.setScale(scaleFor(systemDPI()))
	w.st = skin.State{
		News:     "Click PLAY to update and start the game.",
		Status:   "Ready",
		Folder:   s.cfg.InstallDir,
		Version:  version,
		Progress: 0,
	}
	theUI = w

	inst, _, _ := pGetModuleHandle.Call(0)
	cls, _ := syscall.UTF16PtrFromString("RoM2Launcher")
	title, _ := syscall.UTF16PtrFromString(skin.AppTitle)
	cursor, _, _ := pLoadCursor.Call(0, 32512) // IDC_ARROW
	big, small := loadIcon(32), loadIcon(16)
	wc := wndClassEx{
		style:   0x0008, // CS_DBLCLKS
		wndProc: syscall.NewCallback(wndProc), instance: inst, cursor: cursor,
		className: cls, icon: big, iconSm: small,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		fail(err)
		return
	}
	W, H := skin.W*w.scale, skin.H*w.scale
	sw, _, _ := pGetSystemMetrics.Call(0)
	sh, _, _ := pGetSystemMetrics.Call(1)
	const style = 0x80000000 | 0x00080000 | 0x00020000 // WS_POPUP | WS_SYSMENU | WS_MINIMIZEBOX
	hwnd, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)), style,
		uintptr((int(sw)-W)/2), uintptr((int(sh)-H)/2), uintptr(W), uintptr(H), 0, 0, inst, 0)
	if hwnd == 0 {
		fail(err)
		return
	}
	w.hwnd = hwnd
	if k := scaleFor(windowDPI(hwnd)); k != w.scale { // the monitor it opened on
		w.setScale(k)
		W, H = skin.W*k, skin.H*k
		pSetWindowPos.Call(hwnd, 0, uintptr((int(sw)-W)/2), uintptr((int(sh)-H)/2), uintptr(W), uintptr(H), 0x0014)
	}
	if big != 0 {
		pSendMessage.Call(hwnd, wmSetIcon, 1, big)
	}
	if small != 0 {
		pSendMessage.Call(hwnd, wmSetIcon, 0, small)
	}
	w.setShape()
	w.redraw()
	pShowWindow.Call(hwnd, 5)

	var m msgT
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func fail(err error) {
	msgBox(0, fmt.Sprintf("Could not open the launcher window: %v\nTry: launcher.exe -console", err), mbOK|mbIconError)
}
