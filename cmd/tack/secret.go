//go:build !plain

package main

import (
	"bufio"
	"os"

	"github.com/charmbracelet/x/term"
)

// terminalSecret reads from the terminal with echo off, so pasted values stay hidden.
func terminalSecret(*bufio.Reader) secretReader {
	return func() (string, error) {
		b, err := term.ReadPassword(os.Stdin.Fd())
		return string(b), err
	}
}
