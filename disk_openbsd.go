//go:build openbsd
// +build openbsd

package main

import "golang.org/x/sys/unix"

func diskUsage(path string) (total, free int64, err error) {
	var st unix.Statfs_t
	if err = unix.Statfs(path, &st); err != nil {
		return
	}
	b := int64(st.F_bsize)
	total = int64(st.F_blocks) * b
	free = int64(st.F_bavail) * b
	return
}
