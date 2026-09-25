package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// lockInfo is written into the lock file so a second run can say who holds it.
type lockInfo struct {
	PID     int       `json:"pid"`
	Host    string    `json:"host"`
	Started time.Time `json:"started"`
}

// ErrLocked means another takeout run is using the results folder.
var ErrLocked = errors.New("results folder is in use")

// Lock claims dir for one run by creating dir/lock exclusively. A lock left by a
// process that is no longer running on this host is replaced. The returned
// release removes the lock.
func Lock(dir string) (release func(), err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "lock")
	host, _ := os.Hostname()
	me := lockInfo{PID: os.Getpid(), Host: host, Started: time.Now().UTC()}
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = json.NewEncoder(f).Encode(me)
			f.Close()
			return func() { _ = os.Remove(p) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		var other lockInfo
		b, _ := os.ReadFile(p)
		_ = json.Unmarshal(b, &other)
		if other.Host == host && other.PID > 0 && !processAlive(other.PID) {
			_ = os.Remove(p)
			continue
		}
		return nil, fmt.Errorf("%w by process %d on %s since %s (%s). If no other takeout is running, delete that file", ErrLocked, other.PID, other.Host, other.Started.Format(time.RFC3339), p)
	}
	return nil, fmt.Errorf("%w: could not replace a stale lock at %s", ErrLocked, p)
}
