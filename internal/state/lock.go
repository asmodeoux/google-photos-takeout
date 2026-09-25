package state

import (
	"encoding/json"
	"errors"
	"fmt"
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

// Lock claims dir for one run with an operating-system lock on dir/lock
// (flock, or LockFileEx on Windows). The system drops the lock when the
// process ends, however it ends, so a crash never leaves the folder locked.
// The file itself stays; it only says who holds the lock.
func Lock(dir string) (release func(), err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "lock")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		var other lockInfo
		b, _ := os.ReadFile(p)
		if json.Unmarshal(b, &other) != nil || other.PID == 0 {
			return nil, fmt.Errorf("%w: another takeout holds %s", ErrLocked, p)
		}
		return nil, fmt.Errorf("%w by process %d on %s since %s", ErrLocked, other.PID, other.Host, other.Started.Format(time.RFC3339))
	}
	host, _ := os.Hostname()
	b, _ := json.Marshal(lockInfo{PID: os.Getpid(), Host: host, Started: time.Now().UTC()})
	_ = f.Truncate(0)
	_, _ = f.WriteAt(append(b, '\n'), 0)
	return func() {
		unlockFile(f)
		f.Close()
	}, nil
}
