package progress

import "golang.org/x/term"

func fdIsatty(fd int) bool { return term.IsTerminal(fd) }
