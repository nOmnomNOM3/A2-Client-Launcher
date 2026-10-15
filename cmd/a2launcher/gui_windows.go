//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/nOmnomNOM3/A2-Client-Launcher/core"
)

const hasGUI = true

type winUI struct {
	mw      *walk.MainWindow
	status  *walk.Label
	bar     *walk.ProgressBar
	news    *walk.TextEdit
	dirEdit *walk.LineEdit
	buttons []*walk.PushButton
	mu      sync.Mutex
}

func (w *winUI) Status(t string) { w.mw.Synchronize(func() { w.status.SetText(t) }) }
func (w *winUI) News(t string) {
	if t == "" {
		return
	}
	w.mw.Synchronize(func() { w.news.SetText(t) })
}
func (w *winUI) Bytes(done, total int64) {
	pct := 0
	if total > 0 {
		pct = int(done * 1000 / total)
	}
	w.mw.Synchronize(func() { w.bar.SetValue(pct) })
}

func (w *winUI) busy(b bool) {
	for _, btn := range w.buttons {
		btn.SetEnabled(!b)
	}
}

func showCrashDialog(owner walk.Form, zipPath string) {
	var dlg *walk.Dialog
	var accept *walk.PushButton
	_, err := Dialog{
		AssignTo: &dlg,
		Title:    "Rage of Mages 2",
		MinSize:  Size{Width: 420, Height: 140},
		Layout:   VBox{},
		Children: []Widget{
			Label{Text: "The game crashed. A report was saved to:\n" + zipPath},
			Composite{Layout: HBox{}, Children: []Widget{
				HSpacer{},
				PushButton{Text: "Open folder", OnClicked: func() {
					exec.Command("explorer", filepath.Dir(zipPath)).Start()
				}},
				PushButton{AssignTo: &accept, Text: "OK", OnClicked: func() { dlg.Accept() }},
			}},
		},
		DefaultButton: &accept,
	}.Run(owner)
	if err != nil {
		walk.MsgBox(owner, "Rage of Mages 2",
			"The game crashed. A report was saved to:\n"+zipPath,
			walk.MsgBoxIconError)
	}
}

func runGUI(s *session) {
	w := &winUI{}
	var play, repair, browse *walk.PushButton
	run := func(full, launch bool) {
		if !w.mu.TryLock() {
			return
		}
		w.busy(true)
		go func() {
			defer w.mu.Unlock()
			restart, err := s.update(context.Background(), w, full)
			if restart {
				restartSelf()
			}
			doLaunch := false
			w.mw.Synchronize(func() {
				if err != nil {
					w.status.SetText("Problem: see message")
					if !launch || s.manifest != nil {
						w.busy(false)
						walk.MsgBox(w.mw, "Rage of Mages 2", err.Error(), walk.MsgBoxIconError)
						return
					}
					// Offline: offer to play with the files we have.
					if walk.MsgBox(w.mw, "Rage of Mages 2", err.Error()+"\n\nStart the game anyway with the files you already have?",
						walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != walk.DlgCmdYes {
						w.busy(false)
						return
					}
				}
				if !launch {
					w.busy(false)
					return
				}
				doLaunch = true
			})
			if !doLaunch {
				return
			}
			w.mw.Synchronize(func() { w.playAndWatch(s) })
		}()
	}
	err := MainWindow{
		AssignTo: &w.mw,
		Title:    "Rage of Mages 2 — BiffinBoys (launcher " + version + ")",
		MinSize:  Size{Width: 560, Height: 380},
		Size:     Size{Width: 620, Height: 420},
		Layout:   VBox{},
		Children: []Widget{
			Label{Text: "News"},
			TextEdit{AssignTo: &w.news, ReadOnly: true, VScroll: true, Text: "Click Play to update and start the game."},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{Text: "Game folder:"},
				LineEdit{AssignTo: &w.dirEdit, Text: s.cfg.InstallDir, ReadOnly: true},
				PushButton{AssignTo: &browse, Text: "Change…", OnClicked: func() {
					dlg := walk.FileDialog{Title: "Choose the game folder (a new empty folder is fine)", InitialDirPath: s.cfg.InstallDir}
					if ok, _ := dlg.ShowBrowseFolder(w.mw); ok {
						s.cfg.InstallDir = dlg.FilePath
						w.dirEdit.SetText(dlg.FilePath)
						core.SaveConfig(s.cfg)
					}
				}},
			}},
			ProgressBar{AssignTo: &w.bar, MinValue: 0, MaxValue: 1000},
			Label{AssignTo: &w.status, Text: "Ready"},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				PushButton{AssignTo: &repair, Text: "Repair files", OnClicked: func() { run(true, false) }},
				HSpacer{},
				PushButton{AssignTo: &play, Text: "  PLAY  ", OnClicked: func() {
					core.SaveConfig(s.cfg)
					run(false, true)
				}},
			}},
		},
	}.Create()
	if err != nil {
		walk.MsgBox(nil, "Rage of Mages 2", fmt.Sprintf("Could not open the launcher window: %v\nTry: launcher.exe -console", err), walk.MsgBoxIconError)
		return
	}
	w.buttons = []*walk.PushButton{play, repair, browse}
	if font, err := walk.NewFont(w.mw.Font().Family(), 14, walk.FontBold); err == nil {
		play.SetFont(font)
	}
	w.mw.Run()
}

// playAndWatch must run on the UI thread for Hide; waiting happens in a new goroutine.
func (w *winUI) playAndWatch(s *session) {
	before := core.SnapshotLogs(s.cfg.InstallDir)
	proc, err := s.startGame()
	if err != nil {
		w.busy(false)
		walk.MsgBox(w.mw, "Rage of Mages 2", err.Error(), walk.MsgBoxIconError)
		return
	}
	w.status.SetText("Game running…")
	w.mw.Hide()
	go func() {
		zipPath, err := s.watchGame(proc, before)
		w.mw.Synchronize(func() {
			if err != nil {
				w.mw.Show()
				w.busy(false)
				walk.MsgBox(w.mw, "Rage of Mages 2", err.Error(), walk.MsgBoxIconError)
				return
			}
			if zipPath != "" {
				w.mw.Show()
				w.busy(false)
				showCrashDialog(w.mw, zipPath)
			}
			os.Exit(0)
		})
	}()
}
