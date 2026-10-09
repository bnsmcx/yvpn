//go:build !linux

package main

// The agent only ever runs on a Linux droplet; elsewhere it builds for tests
// and reports no disk figures, which disables the free-space check.
func diskSpace(dir string) (free, total int64) { return 0, 0 }
