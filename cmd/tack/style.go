//go:build !plain

package main

import (
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

type presentation struct {
	writer      io.Writer
	tty, styled bool
	width       int
}

func newPresentation(w io.Writer) presentation {
	p := presentation{writer: w, width: 100}
	if f, ok := w.(*os.File); ok {
		p.tty = term.IsTerminal(f.Fd())
		if cols, _, err := term.GetSize(f.Fd()); err == nil {
			p.width = cols
		}
	}
	p.styled = p.tty && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	return p
}
func (p presentation) header(command string) string {
	if !p.styled {
		return ""
	}
	r := lipgloss.NewRenderer(p.writer)
	return r.NewStyle().Foreground(lipgloss.Color("141")).Bold(true).Render("tack") + "  " + r.NewStyle().Foreground(lipgloss.Color("245")).Render(command+" · read-only") + "\n\n"
}
func (p presentation) truncate(s string) string {
	if p.tty {
		return ansi.Truncate(s, p.width, "…")
	}
	return s
}
