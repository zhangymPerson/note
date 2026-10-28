//go:build windows

package daemon

import (
	"errors"
	"syscall"
)

// ProcessAlive 判断 pid 是否仍指向一个**未退出**的进程。
//
// 这里不能用 os.FindProcess：Windows 上它只做 OpenProcess，对「已经退出、
// 但进程对象还被某个句柄引用着」的 pid 依然成功，于是刚优雅退出的实例会
// 被判成存活（stop 白等 10 秒后去强杀、start 以为端口还占着）。正确做法是
// 拿一个可等待的句柄，用 WaitForSingleObject(0) 看它是否已 signaled。
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
			// 拿不到句柄说明进程存在但不属于当前用户：算活着，别当成可清理的残留。
			return true
		}
		return false
	}
	defer syscall.CloseHandle(handle)

	event, err := syscall.WaitForSingleObject(handle, 0)
	if err != nil {
		return false
	}
	return event == syscall.WAIT_TIMEOUT
}
