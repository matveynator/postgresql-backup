//go:build netbsd
// +build netbsd

package main

import "golang.org/x/sys/unix"

func diskUsage(path string) (total, free int64, err error) {
	var st unix.Statvfs_t
	if err = unix.Statvfs(path, &st); err != nil {
		return
	}
	b := int64(st.Frsize)
	total = int64(st.Blocks) * b
	free = int64(st.Bavail) * b
	return
}
