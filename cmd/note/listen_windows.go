//go:build windows

package main

import (
	"errors"
	"syscall"
)

// wsaEADDRINUSE 是 Winsock 的「地址已被占用」错误码（WSAEADDRINUSE = 10048）。
// Go 的 syscall 包没有导出这个名字；zerrors_windows.go 里的 EADDRINUSE 是自造的
// APPLICATION_ERROR 值，不会与真实 errno 相等，一并比较只是留个兜底。
const wsaEADDRINUSE = syscall.Errno(10048)

// isAddrInUse 判断监听错误是否来自「地址已被占用」。
func isAddrInUse(err error) bool {
	return errors.Is(err, wsaEADDRINUSE) || errors.Is(err, syscall.EADDRINUSE)
}
