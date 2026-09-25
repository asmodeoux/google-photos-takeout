// Package awake keeps the computer from sleeping during a long run. The
// display may still turn off.
package awake

import "sync"

// Hold asks the system not to sleep until release is called. release is never
// nil and may be called more than once. An error means the request failed;
// the run can go on, but the computer may sleep.
func Hold() (release func(), err error) {
	undo, err := hold()
	var once sync.Once
	return func() {
		once.Do(func() {
			if undo != nil {
				undo()
			}
		})
	}, err
}
