package daemon

import (
	"fmt"
	"os"
	"os/exec"
)

// EnvStatePath 是后台子进程的「身份标记」环境变量：值为状态文件路径。
// 父进程启动子进程时注入，子进程据此知道自己该写状态文件并接受关停请求。
// 令牌不经过这里——它由子进程自己生成后写进状态文件，父进程不经手。
const EnvStatePath = "NOTE_DAEMON_STATE"

// RunDetached 启动一个脱离终端的子进程，并把它的 stdout/stderr 指向日志文件。
//
// 日志必须在 fd 层面就接到文件上：slog 的默认 handler 在进程启动时已绑定
// 当时的 os.Stderr，事后改 os.Stderr 变量不会生效，所以只能由父子进程的
// 启动关系来决定日志去向。
//
// 返回的 *exec.Cmd 由调用方按需 Wait（父进程退出后子进程会继续运行）。
func RunDetached(exe string, args []string, logPath, statePath string) (*exec.Cmd, error) {
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", logPath, err)
	}

	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), EnvStatePath+"="+statePath)
	cmd.SysProcAttr = sysProcAttr()

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start detached process: %w", err)
	}
	// 子进程已持有自己的句柄副本，父进程这一份可以立刻关掉；
	// 顺序不能反，反了子进程就继承不到有效的 fd。
	if err := logFile.Close(); err != nil {
		return nil, fmt.Errorf("close log file %s: %w", logPath, err)
	}
	return cmd, nil
}
