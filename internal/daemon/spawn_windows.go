//go:build windows

package daemon

import "syscall"

const (
	// CREATE_NEW_PROCESS_GROUP 让子进程拥有自己的进程组（收不到父控制台的 Ctrl+C）。
	createNewProcessGroup = 0x00000200
	// DETACHED_PROCESS 不分配控制台窗口；它与 CREATE_NEW_CONSOLE 互斥，不能同时给。
	detachedProcess = 0x00000008
)

// sysProcAttr 让子进程脱离父控制台且不弹新窗口：
// HideWindow 抑制控制台窗口闪现，DETACHED_PROCESS 保证父窗口关闭后进程存活。
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: detachedProcess | createNewProcessGroup,
	}
}
