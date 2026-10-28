//go:build windows

package daemon

import "os"

// ProcessAlive 判断 pid 是否仍指向一个存在的进程。
// Windows 上 os.FindProcess 会真正 OpenProcess，进程不存在时返回错误
// （与 Unix 不同），因此不需要额外探测；权限不足同样返回错误。
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.FindProcess(pid)
	return err == nil
}
