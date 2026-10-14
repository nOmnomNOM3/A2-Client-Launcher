package main

import (
	"context"
	"fmt"
	"os"
)

type consoleUI struct{ last string }

func (c *consoleUI) Status(t string) {
	if t != c.last {
		fmt.Println(t)
		c.last = t
	}
}
func (c *consoleUI) Bytes(int64, int64) {}
func (c *consoleUI) News(t string) {
	if t != "" {
		fmt.Println("News:", t)
	}
}

// runConsole: a2launcher -console [-repair] [-dir PATH] [-nolaunch]
func runConsole(s *session, repair, noLaunch bool) int {
	restart, err := s.update(context.Background(), &consoleUI{}, repair)
	if restart {
		restartSelf()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	if noLaunch {
		return 0
	}
	if err := s.launch(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}
