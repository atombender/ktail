package main

import (
	"io"
	"os"

	"golang.org/x/crypto/ssh/terminal"
)

func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return terminal.IsTerminal(int(f.Fd()))
	}
	return false
}
