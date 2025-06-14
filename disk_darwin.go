//go:build darwin
// +build darwin

package main

import "golang.org/x/sys/unix"

func diskUsage(path string) (total, free int64, err error) {
	var st unix.Statfs_t
	if err = unix.Statfs(path, &st); err != nil {
		return
	}
	b := int64(st.Bsize)
	total = int64(st.Blocks) * b
	free = int64(st.Bavail) * b
	return
}
