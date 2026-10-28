package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"note/internal/cli"
	"note/internal/config"
	"note/internal/daemon"
)

const (
	// readyTimeout 是 start 等待子进程就绪的上限；本机回环起服务是毫秒级，
	// 十秒还没就绪基本意味着端口被占或启动即崩。
	readyTimeout = 10 * time.Second
	// stopTimeout 是 stop 等待优雅退出的上限，超过就强制结束。
	stopTimeout = 10 * time.Second
	// pollInterval 是就绪与退出轮询的间隔。
	pollInterval = 100 * time.Millisecond
	// logTailLines 是启动失败时回显的日志行数。
	logTailLines = 20
)

// runWebStart 后台启动 Web 服务：已有一个后台实例在跑时直接打印它，
// 否则拉起脱离终端的子进程，等它就绪后打印地址、pid 与日志路径。
func runWebStart(globalConfigPath string, args []string) error {
	cfg, err := loadConfig(globalConfigPath, args)
	if err != nil {
		return err
	}

	statePath := daemon.StatePath(cfg.DBPath)
	logPath := daemon.LogPath(cfg.DBPath)

	if st, err := daemon.Load(statePath); err == nil && !sameDB(st.DB, cfg.DBPath) {
		// 状态文件是按目录放的（note.json），同一目录下的另一个库会共用它。
		// 这种情况既不能当成「已在运行」，也不能覆盖掉别人的状态文件。
		fmt.Printf("数据目录 %s 里已有另一个实例登记（数据库 %s）。\n", filepath.Dir(cfg.DBPath), st.DB)
		fmt.Println("换一个数据目录，或先用 note web stop 停掉它。")
		return exitError(1)
	} else if err == nil {
		if daemon.ProcessAlive(st.Pid) {
			if _, probeErr := daemon.Probe(st.Addr); probeErr == nil {
				fmt.Printf("note 已在后台运行（pid %d）\n", st.Pid)
				printURLs(st.URLs)
				fmt.Printf("  日志  %s\n", st.Log)
				return nil
			}
			// 进程在但不响应：不重复拉起，免得两个实例抢同一个库。
			fmt.Printf("检测到 pid %d 仍在运行，但没有响应 %s\n", st.Pid, st.Addr)
			fmt.Printf("先看日志 %s，或 note web stop 后再启动。\n", st.Log)
			return exitError(1)
		}
		// 残留状态文件：清理后继续。
		if err := daemon.RemoveIfPid(statePath, st.Pid); err != nil {
			return fmt.Errorf("清理残留状态文件: %w", err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	childArgs := make([]string, 0, len(args)+3)
	if globalConfigPath != "" {
		childArgs = append(childArgs, "--config", globalConfigPath)
	}
	childArgs = append(childArgs, "web")
	childArgs = append(childArgs, args...)

	cmd, err := daemon.RunDetached(exe, childArgs, logPath, statePath)
	if err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.Now().Add(readyTimeout)
	for {
		if st, err := daemon.Load(statePath); err == nil {
			if _, probeErr := daemon.Probe(st.Addr); probeErr == nil {
				fmt.Println("note 已在后台启动，在浏览器打开：")
				printURLs(st.URLs)
				fmt.Printf("  pid   %d\n", st.Pid)
				fmt.Printf("  日志  %s\n", st.Log)
				fmt.Println("停止：note web stop    状态：note web status")
				return nil
			}
		}

		select {
		case waitErr := <-exited:
			_ = daemon.RemoveIfPid(statePath, cmd.Process.Pid)
			fmt.Printf("启动失败：后台进程已退出（%v）\n", waitErr)
			printLogTail(logPath)
			return exitError(1)
		default:
		}

		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = daemon.RemoveIfPid(statePath, cmd.Process.Pid)
			fmt.Printf("启动失败：%s 内未就绪（端口可能被占用）\n", readyTimeout)
			printLogTail(logPath)
			return exitError(1)
		}
		time.Sleep(pollInterval)
	}
}

// runWebStop 优雅停止后台实例：优先走关停端点，超时才强制结束。
func runWebStop(globalConfigPath string, args []string) error {
	cfg, err := loadConfig(globalConfigPath, args)
	if err != nil {
		return err
	}
	statePath := daemon.StatePath(cfg.DBPath)

	st, err := daemon.Load(statePath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// 没有状态文件：可能压根没起，也可能是前台实例。
		if _, probeErr := daemon.Probe(cfg.Addr); probeErr == nil {
			fmt.Println("检测到运行中的实例（前台启动或状态文件缺失）。")
			fmt.Println("note web stop 只能关停后台实例，请回到它的终端按 Ctrl+C。")
			return exitError(1)
		}
		fmt.Println("note web 未在后台运行。")
		return nil
	case err != nil:
		fmt.Printf("状态文件 %s 无法解析：%v\n", statePath, err)
		fmt.Println("确认没有实例在运行后，删除该文件再重试。")
		return exitError(1)
	}

	if !sameDB(st.DB, cfg.DBPath) {
		fmt.Printf("状态文件 %s 记录的是另一个数据库（%s），不归本次 stop 管。\n", statePath, st.DB)
		return exitError(1)
	}

	if !daemon.ProcessAlive(st.Pid) {
		_ = daemon.RemoveIfPid(statePath, st.Pid)
		fmt.Println("note web 未在运行（已清理残留状态文件）。")
		return nil
	}

	if err := daemon.RequestStop(st); err != nil {
		fmt.Printf("优雅关停请求失败：%v\n", err)
		if errors.Is(err, daemon.ErrStopDenied) {
			// 令牌不对：端口上跑的多半不是这个状态文件描述的实例，
			// 这时按 pid 强杀可能杀掉无辜进程，宁可留给用户自己看一眼。
			fmt.Printf("状态文件里的 pid %d 未被结束，确认后可手动处理该进程。\n", st.Pid)
			return exitError(1)
		}
	}

	if waitExit(st.Pid, stopTimeout) {
		_ = daemon.RemoveIfPid(statePath, st.Pid)
		fmt.Printf("已停止（pid %d）。\n", st.Pid)
		return nil
	}

	// 优雅退出没等到：强杀。SQLite 的 WAL 是崩溃安全的，不会丢数据，
	// 但 checkpoint 没跑完，下次启动要重放日志。
	if proc, err := os.FindProcess(st.Pid); err == nil {
		_ = proc.Kill()
	}
	_ = daemon.RemoveIfPid(statePath, st.Pid)
	fmt.Printf("已强制结束（pid %d）：WAL 未完成 checkpoint，数据安全但下次启动会重放日志。\n", st.Pid)
	return nil
}

// runWebStatus 输出后台实例的运行情况：HTTP 探测再与状态文件合并判定。
func runWebStatus(globalConfigPath string, args []string) error {
	cfg, err := loadConfig(globalConfigPath, args)
	if err != nil {
		return err
	}

	statePath := daemon.StatePath(cfg.DBPath)
	logPath := daemon.LogPath(cfg.DBPath)

	data := cli.StatusData{
		LogPath:   logPath,
		DBPath:    cfg.DBPath,
		StatePath: statePath,
	}
	if stat, statErr := os.Stat(logPath); statErr == nil {
		data.LogExists = true
		data.LogSizeBytes = stat.Size()
	}

	probeAddr := cfg.Addr
	if st, err := daemon.Load(statePath); err == nil && !sameDB(st.DB, cfg.DBPath) {
		// 同一目录下另一个库的实例：不冒领它的 pid，只探测本次配置的地址。
		data.StateOtherDB = st.DB
	} else if err == nil {
		data.StateExists = true
		probeAddr = st.Addr
		data.Pid = st.Pid
		data.URLs = st.URLs
		data.StartedAt = st.StartedAt
		data.Version = st.Version

		if !daemon.ProcessAlive(st.Pid) {
			// 残留状态文件：清理并说明，别让它看起来像「在运行」。
			if err := daemon.RemoveIfPid(statePath, st.Pid); err != nil {
				return fmt.Errorf("清理残留状态文件: %w", err)
			}
			data.StateExists = false
			data.StalePid = st.Pid
			data.Pid = 0
			data.URLs = nil
			data.StartedAt = ""
			data.Version = ""
			probeAddr = cfg.Addr
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("状态文件 %s 无法解析：%v\n", statePath, err)
		fmt.Println("确认没有实例在运行后，删除该文件再重试。")
		return exitError(1)
	}

	info, probeErr := daemon.Probe(probeAddr)
	switch {
	case probeErr == nil:
		// 前台实例没有状态文件，pid 未知（格式层会省掉这一行）。
		data.Running = true
		data.StartedAt = info.StartedAt
		data.UptimeSeconds = info.UptimeSeconds
		data.Version = info.Version
		if len(data.URLs) == 0 {
			data.URLs = config.StartupURLs(probeAddr, probeAddr)
		}
	case data.Pid > 0 && daemon.ProcessAlive(data.Pid):
		data.Unresponsive = true
	}

	fmt.Print(cli.FormatStatus(data))

	switch {
	case data.Running:
		return nil
	case data.Unresponsive:
		return exitError(1)
	default:
		return exitError(3)
	}
}

// sameDB 判断状态文件记录的数据库是否就是本次命令操作的数据库。
// 路径允许一侧是相对路径、一侧是绝对路径：能比较就按文件路径比较。
func sameDB(recorded, configured string) bool {
	if recorded == "" || configured == "" {
		return recorded == configured
	}
	if filepath.Clean(recorded) == filepath.Clean(configured) {
		return true
	}
	absRecorded, err1 := filepath.Abs(recorded)
	absConfigured, err2 := filepath.Abs(configured)
	return err1 == nil && err2 == nil && absRecorded == absConfigured
}

// waitExit 轮询等待 pid 退出，返回是否在超时前退出。
func waitExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !daemon.ProcessAlive(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(pollInterval)
	}
}

// printURLs 逐行打印可点击地址；后台实例由子进程算好写进状态文件。
func printURLs(urls []string) {
	for _, url := range urls {
		fmt.Println(url)
	}
}

// printLogTail 把日志最后几行回显到终端：后台启动失败没有终端输出，
// 不回显这段用户就只能自己去翻文件。
func printLogTail(path string) {
	lines, err := tailLines(path, logTailLines)
	if err != nil || len(lines) == 0 {
		fmt.Printf("日志：%s\n", path)
		return
	}
	fmt.Printf("日志 %s 最后 %d 行：\n", path, len(lines))
	for _, line := range lines {
		fmt.Println("  " + line)
	}
}

// tailLines 读取文件最后 n 行；文件不存在时返回错误由调用方降级。
func tailLines(path string, n int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var ring []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		ring = append(ring, scanner.Text())
		if len(ring) > n {
			ring = ring[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ring, nil
}
