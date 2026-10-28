//go:build !windows

package daemon

import (
	"errors"
	"syscall"
)

// ProcessAlive 判断 pid 是否仍指向一个存在的进程。
// signal 0 只做权限与存在性检查；EPERM 说明进程存在但不属于当前用户，
// 对本机单用户场景同样算「活着」（别把别人的进程误判成可覆盖的残留）。
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
