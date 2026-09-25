package awake

import "testing"

func TestHoldReleaseIsIdempotent(t *testing.T) {
	release, err := Hold()
	if err != nil {
		t.Logf("hold: %v (not fatal)", err)
	}
	if release == nil {
		t.Fatal("release is nil")
	}
	release()
	release()
}

func TestHoldWithoutCaffeinate(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	release, err := Hold()
	if err != nil {
		t.Fatalf("missing caffeinate must not be an error: %v", err)
	}
	release()
}
