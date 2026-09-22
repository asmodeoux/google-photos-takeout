package state

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Rec is one line of the resume journal.
type Rec struct {
	ID    string `json:"id"`
	SHA   string `json:"sha,omitempty"`
	Stage string `json:"stage"`
	Path  string `json:"path,omitempty"`
	Year  string `json:"year,omitempty"`
	Name  string `json:"name,omitempty"`
	Error string `json:"error,omitempty"`
}

// Journal is the append-only resume log.
type Journal struct {
	path string
	recs map[string]Rec
	f    *os.File
	mu   sync.Mutex
}

func OpenJournal(path string) (*Journal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	j := &Journal{path: path, recs: map[string]Rec{}}
	if b, err := os.ReadFile(path); err == nil {
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
		for sc.Scan() {
			var r Rec
			if json.Unmarshal(sc.Bytes(), &r) == nil && r.ID != "" {
				j.recs[r.ID] = r
			}
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	j.f = f
	return j, nil
}

func (j *Journal) Get(id string) (Rec, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	r, ok := j.recs[id]
	return r, ok
}

func (j *Journal) Put(r Rec) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.recs[r.ID] = r
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, err := j.f.Write(append(b, '\n')); err != nil {
		return err
	}
	return j.f.Sync()
}

func (j *Journal) Close() error {
	if j.f == nil {
		return nil
	}
	return j.f.Close()
}

// Fate is one zip entry's outcome. The ledger must list each entry once.
type Fate struct {
	Zip   string `json:"zip"`
	Entry string `json:"entry"`
	Fate  string `json:"fate"`
}

// Balanced is true when every zip entry has exactly one fate.
func Balanced(fates []Fate, entries int) bool {
	return len(fates) == entries
}
