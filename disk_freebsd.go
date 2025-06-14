//go:build freebsd
// +build freebsd

package main

import "golang.org/x/sys/unix"

func diskUsage(path string) (total, free int64, err error) {
	var st unix.Statfs_t
	if err = unix.Statfs(path, &st); err != nil {
		return
	}
	b := int64(st.Bsize)         // размер блока
	total = int64(st.Blocks) * b // общий объём
	free = int64(st.Bavail) * b  // свободно (без учёта root-резерва)
	return
}
