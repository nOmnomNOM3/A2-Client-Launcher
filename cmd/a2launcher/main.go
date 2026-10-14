// Command a2launcher installs, updates and starts the Rage of Mages 2 client.
package main

import (
	"flag"
	"os"

	"github.com/nOmnomNOM3/A2-Client-Launcher/core"
)

func main() {
	console := flag.Bool("console", false, "text mode (no window)")
	repair := flag.Bool("repair", false, "re-check every file")
	dir := flag.String("dir", "", "game folder")
	murl := flag.String("manifest", "", "manifest URL (testing)")
	noLaunch := flag.Bool("nolaunch", false, "update only, do not start the game")
	flag.Parse()

	if exe, err := os.Executable(); err == nil {
		core.CleanupOldExe(exe)
	}
	cfg := core.LoadConfig()
	if *dir != "" {
		cfg.InstallDir = *dir
	}
	if *murl != "" {
		cfg.ManifestURL = *murl
	}
	cfg.InstallDir = installDir(cfg)
	s := &session{cfg: cfg}
	if *console || !hasGUI {
		os.Exit(runConsole(s, *repair, *noLaunch))
	}
	runGUI(s)
}
