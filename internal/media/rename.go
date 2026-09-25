package media

import "sync/atomic"

// RenameRetries counts renames that had to wait for another process to let go
// of a file. Windows Defender and the search indexer cause most of them.
var RenameRetries atomic.Int64

// Rename moves src to dst and never replaces an existing dst. When dst exists it
// returns an error that matches fs.ErrExist, so the caller can pick another name.
func Rename(src, dst string) error {
	return renameNoReplace(src, dst)
}
