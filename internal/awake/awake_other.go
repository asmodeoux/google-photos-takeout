//go:build !windows && !darwin

package awake

func hold() (func(), error) { return nil, nil }
