//go:build !darwin

package media

import "fmt"

func cloneFile(src, dst string) error {
	return fmt.Errorf("clonefile unavailable")
}
