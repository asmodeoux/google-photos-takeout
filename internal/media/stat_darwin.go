//go:build darwin

package media

import "golang.org/x/sys/unix"

func Stat(path string) (FS, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return FS{}, err
	}
	name := ""
	for _, b := range st.Fstypename {
		if b == 0 {
			break
		}
		name += string(byte(b))
	}
	free := uint64(st.Bavail) * uint64(st.Bsize)
	return FS{Type: name, Free: free, APFS: name == "apfs"}, nil
}
