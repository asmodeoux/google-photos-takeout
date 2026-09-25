package progress

import (
	"io"
	"os"

	"golang.org/x/term"
)

func fdIsatty(fd int) bool { return term.IsTerminal(fd) }

func termWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 0
	}
	cols, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return cols
}
