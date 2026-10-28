package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"note/internal/build"
	"note/internal/cli"
	"note/internal/config"
	"note/internal/store"
	"note/internal/web"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

// run 是全部命令的分发入口。
//
//	note            交互菜单（bubbletea；无终端时退化为打印帮助）
//	note web        启动 Web 服务
//	note info       输出软件与运行信息
//	note help / -h  帮助
func run(args []string) error {
	cmd, opts, err := cli.Parse(args)
	if err != nil {
		// 参数错误：错误信息 + 帮助直接面向用户，退出码 2 与 flag 包惯例一致
		fmt.Fprintf(os.Stderr, "note: %v\n\n%s", err, cli.Usage())
		os.Exit(2)
	}

	switch cmd {
	case cli.CommandHelp:
		topic := ""
		if len(opts.Args) > 0 {
			topic = opts.Args[0]
		}
		fmt.Print(cli.UsageFor(topic))
		return nil

	case cli.CommandMenu:
		action, err := cli.RunMenu()
		if err != nil {
			slog.Warn("interactive menu failed", "error", err)
		}
		switch action {
		case cli.MenuWeb:
			return runWeb(opts.ConfigPath, nil)
		case cli.MenuInfo:
			return runInfo(opts.ConfigPath, nil)
		case cli.MenuFallback:
			// 无终端环境（脚本/管道）跑不了 TUI：打印帮助而非挂住进程
			fmt.Print(cli.Usage())
			return nil
		}
		return nil

	case cli.CommandWeb:
		return runWeb(opts.ConfigPath, opts.Args)

	case cli.CommandInfo:
		return runInfo(opts.ConfigPath, opts.Args)
	}
	return nil
}

// loadConfig 解析全局 -c 与子命令参数并加载配置；
// 子命令里再出现 -c 时后者生效（全局值 append 在前，flag 后值覆盖前值）。
func loadConfig(globalConfigPath string, args []string) (config.Config, error) {
	if globalConfigPath != "" {
		args = append([]string{"--config", globalConfigPath}, args...)
	}
	cfg, warnings, err := config.Load(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cfg, nil // 帮助文本由 cli 层负责，不再向上冒泡
		}
		return cfg, err
	}
	for _, warning := range warnings {
		slog.Warn(warning)
	}
	return cfg, nil
}

// runWeb 启动 Web 服务：加载配置 → 打开数据库 → 装配路由 → 监听并等待信号。
func runWeb(globalConfigPath string, args []string) error {
	cfg, err := loadConfig(globalConfigPath, args)
	if err != nil {
		return err
	}

	slog.Info("starting note", "addr", cfg.Addr, "db", cfg.DBPath, "allow_remote", cfg.AllowRemote)

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := st.Close(); err != nil {
			slog.Error("close database", "error", err)
		}
	}()

	dist, err := web.DistFS()
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           web.New(cfg, st, dist),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(ln)
	}()
	slog.Info("listening", "addr", cfg.Addr)
	fmt.Println("note 已启动，在浏览器打开：")
	for _, url := range config.StartupURLs(cfg.Addr, ln.Addr().String()) {
		fmt.Println(url)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case sig := <-stop:
		slog.Info("shutting down", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		server.Close()
		return err
	}

	return nil
}

// runInfo 输出软件与运行信息：构建信息、运行时、生效配置与数据库只读快照。
// 数据库文件已存在时才打开（读 schema 版本与计数）；从未建库的环境不产生文件。
func runInfo(globalConfigPath string, args []string) error {
	cfg, err := loadConfig(globalConfigPath, args)
	if err != nil {
		return err
	}

	fmt.Print(cli.FormatInfo(buildInfoData(cfg)))
	return nil
}

// buildInfoData 采集 FormatInfo 需要的全部数据；数据库统计失败时降级为不展示计数。
func buildInfoData(cfg config.Config) cli.InfoData {
	info := build.Describe()

	hostname, err := os.Hostname()
	if err != nil {
		hostname = ""
	}

	data := cli.InfoData{
		Label:       info.Label(),
		Version:     info.Version,
		GitHash:     info.GitHash,
		GitDirty:    info.GitDirty != "",
		BuildDate:   info.BuildDate,
		GoVersion:   info.GoVersion,
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		CPUCount:    runtime.NumCPU(),
		Hostname:    hostname,
		ConfigPath:  cfg.ConfigPath,
		ConfigFound: cfg.ConfigFound,
		Addr:        cfg.Addr,
		AllowRemote: cfg.AllowRemote,
		DBPath:      cfg.DBPath,
	}

	if stat, err := os.Stat(cfg.DBPath); err == nil {
		data.DB.Exists = true
		data.DB.SizeBytes = stat.Size()
		if st, err := store.Open(cfg.DBPath); err == nil {
			defer st.Close()
			ctx := context.Background()
			if schema, err := st.SchemaVersion(ctx); err == nil {
				data.DB.SchemaVersion = schema
			}
			if page, err := st.ListNotes(ctx, "", 0, 1); err == nil {
				data.DB.Notes = page.Total
			}
			if ciphers, err := st.ListEncryptedCiphers(ctx); err == nil {
				data.DB.EncryptedNotes = len(ciphers)
			}
			if categories, err := st.ListCategories(ctx); err == nil {
				data.DB.Categories = len(categories)
			}
		}
	}

	return data
}
