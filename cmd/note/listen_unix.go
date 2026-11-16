//go:build !windows

package main

import (
	"errors"
	"syscall"
)

// isAddrInUse 判断监听错误是否来自「地址已被占用」。
func isAddrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}
