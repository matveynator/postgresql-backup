//go:build linux
// +build linux

package main

import "syscall"

func diskUsage(path string) (total, free int64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(path, &st); err != nil {
		return
	}
	b := int64(st.Bsize)
	total = int64(st.Blocks) * b
	free = int64(st.Bavail) * b
	return
}
