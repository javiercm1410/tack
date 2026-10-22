//go:build plain

package main

import "io"

// The plain build needs only the standard library, for restricted environments.
type presentation struct{}

func newPresentation(io.Writer) presentation  { return presentation{} }
func (presentation) header(string) string     { return "" }
func (presentation) truncate(s string) string { return s }
