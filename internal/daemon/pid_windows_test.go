//go:build windows

package daemon

import (
	"os/exec"
	"testing"
	"time"
)

// TestProcessAliveFalseForExitedProcess 复现过一个 Windows 特有坑：
// os.FindProcess 对「已退出但进程对象还没被系统回收」的 pid 依然成功，
// 于是后台实例明明已经优雅退出，stop 仍以为它活着、等满 10 秒后去强杀，
// 而 start 又因为「进程在但不响应」拒绝启动。存活判定必须看等待句柄。
func TestProcessAliveFalseForExitedProcess(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	pid := cmd.Process.Pid
	// 故意不 Wait：这是「不是自己的子进程」的真实形态。
	time.Sleep(500 * time.Millisecond)

	if ProcessAlive(pid) {
		t.Fatalf("已退出的 pid %d 被判为存活", pid)
	}

	// 收尾，避免留下僵尸句柄。
	_ = cmd.Wait()
}
