//go:build windows

package progress

func fdIsatty(fd int) bool { return false }
