// Command uipreview renders the launcher window to a PNG (no Windows needed).
// Usage: go run ./cmd/uipreview out.png
package main

import (
	"image/png"
	"os"

	"github.com/nOmnomNOM3/A2-Client-Launcher/skin"
)

func main() {
	out := "launcher_preview.png"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	im := skin.Render(skin.State{
		News: "# Welcome to the BiffinBoys server!\n" +
			"Press PLAY to update your game and join biffinboys.ddns.net.\n\n" +
			"# Version 2026.10.14\n" +
			"- First release of the new launcher.\n" +
			"- Maps download from the server when you join a game.\n" +
			"- If the game crashes, the launcher saves a report you can send us.",
		Status:   "Downloading 1,204/4,940 files - 78.2/319.5 MB",
		Folder:   `C:\Users\Player\Games\Rage of Mages 2`,
		Version:  "1.1.0",
		Progress: 0.245,
		Hover:    skin.Play,
	})
	f, err := os.Create(out)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, im); err != nil {
		panic(err)
	}
}
