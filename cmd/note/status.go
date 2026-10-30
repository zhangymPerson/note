package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"note/internal/cli"
	"note/internal/store"
)

// statsLookbackDays 与 internal/web 的 handleStats 同口径（53 周）。
const statsLookbackDays = 370

// runStatus 输出使用统计：不开 Web 服务也能看到事件构成。网页与 CLI 的操作都计入，
// 因为埋点下沉在 internal/store 的读写方法里（见 store.ViewNote 与 recordActivityTx）。
func runStatus(globalConfigPath string, args []string) error {
	cfg, err := loadConfig(globalConfigPath, args)
	if err != nil {
		return err
	}

	data, err := collectStats(cfg.DBPath)
	if err != nil {
		return err
	}
	fmt.Print(cli.FormatStats(data))
	return nil
}

// collectStats 把 activities 表读成 cli 需要的最小形状。
// 数据库文件不存在时只返回一个「未创建」的空壳：统计是只读报告，不该顺手建库
// （与 note info 的只读口径一致）。
func collectStats(dbPath string) (cli.StatsData, error) {
	data := cli.StatsData{DBPath: dbPath}
	if _, err := os.Stat(dbPath); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return data, fmt.Errorf("读取数据库 %s：%w", dbPath, err)
		}
		return data, nil
	}
	data.DBExists = true

	st, err := store.Open(dbPath)
	if err != nil {
		return data, err
	}
	defer func() { _ = st.Close() }()

	stats, err := st.GetStats(context.Background(), statsLookbackDays)
	if err != nil {
		return data, err
	}
	data.TotalEvents = stats.TotalEvents
	data.TotalSearches = stats.TotalSearches
	data.ActiveDays = stats.ActiveDays
	data.EventsByKind = make([]cli.EventCount, 0, len(stats.EventsByKind))
	for _, item := range stats.EventsByKind {
		data.EventsByKind = append(data.EventsByKind, cli.EventCount{Kind: item.Kind, Count: item.Count})
	}
	data.TopQueries = make([]cli.QueryCount, 0, len(stats.TopQueries))
	for _, item := range stats.TopQueries {
		data.TopQueries = append(data.TopQueries, cli.QueryCount{Query: item.Query, Count: item.Count})
	}
	return data, nil
}
