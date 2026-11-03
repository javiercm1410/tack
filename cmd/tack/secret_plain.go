//go:build plain

package main

import "bufio"

// The plain build has no portable way to turn off echo, so input stays visible.
func terminalSecret(reader *bufio.Reader) secretReader {
	return func() (string, error) { return reader.ReadString('\n') }
}
