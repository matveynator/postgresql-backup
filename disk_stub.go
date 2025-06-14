//go:build !linux && !android && !darwin && !freebsd && !openbsd && !netbsd
// +build !linux,!android,!darwin,!freebsd,!openbsd,!netbsd

package main

import "errors"

// заглушка для экзотических ОС, чтобы проект всегда собирался.
func diskUsage(string) (int64, int64, error) {
	return 0, 0, errors.New("diskUsage not supported on this platform")
}
