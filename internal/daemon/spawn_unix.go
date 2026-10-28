//go:build !windows

package daemon

import "syscall"

// sysProcAttr 让子进程脱离当前终端会话（Setsid）：父进程的终端关闭、
// Ctrl+C、SIGHUP 都不会波及后台实例。
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
