//go:build windows

package media

func Stat(path string) (FS, error) {
	return FS{Type: "unknown"}, nil
}
