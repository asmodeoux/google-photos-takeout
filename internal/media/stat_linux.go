//go:build linux

package media

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func Stat(path string) (FS, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return FS{}, err
	}
	free := uint64(st.Bavail) * uint64(st.Bsize)
	return FS{Type: linuxFS(st.Type), Free: free}, nil
}

func linuxFS(t int64) string {
	switch t {
	case unix.EXT4_SUPER_MAGIC:
		return "ext4"
	case unix.TMPFS_MAGIC:
		return "tmpfs"
	case unix.XFS_SUPER_MAGIC:
		return "xfs"
	case unix.BTRFS_SUPER_MAGIC:
		return "btrfs"
	case unix.OVERLAYFS_SUPER_MAGIC:
		return "overlay"
	case unix.MSDOS_SUPER_MAGIC:
		return "vfat"
	case 0x2011bab0:
		return "exfat"
	case 0x7366746e, 0x5346544e:
		return "ntfs"
	case unix.FUSE_SUPER_MAGIC:
		return "fuseblk"
	default:
		return fmt.Sprintf("0x%x", uint64(t))
	}
}
