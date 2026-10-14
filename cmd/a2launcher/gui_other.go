//go:build !windows

package main

const hasGUI = false

func runGUI(*session) {}
