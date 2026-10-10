package main

import "syscall"

// diskSpace reports free and total bytes on the filesystem holding dir, or
// zeros when it can't tell.
func diskSpace(dir string) (free, total int64) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(dir, &s); err != nil {
		return 0, 0
	}
	return int64(s.Bavail) * int64(s.Bsize), int64(s.Blocks) * int64(s.Bsize)
}
