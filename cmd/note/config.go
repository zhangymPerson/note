package main

import (
	"errors"
	"fmt"
	"os"

	"note/internal/cli"
	"note/internal/config"
)

// runConfig 实现 note config：list 展示 AI 三项（密钥只显示掩码），
// set 原子写入 note.yaml。值本身合不合法由 config.SetValue 判定，
// 这里只负责挑配置文件、把结果渲染成终端文本。
func runConfig(globalConfigPath string, opts cli.ConfigOptions) error {
	path, err := resolveConfigPath(globalConfigPath, opts.ConfigPath)
	if err != nil {
		return err
	}
	if opts.List {
		return listConfig(path)
	}
	return setConfig(path, opts.Key, opts.Value)
}

// resolveConfigPath 决定要读写的配置文件：子命令里的 -c 优先于全局 -c，
// 都没有就用用户配置目录下的默认路径。
func resolveConfigPath(global, sub string) (string, error) {
	switch {
	case sub != "":
		return sub, nil
	case global != "":
		return global, nil
	default:
		return config.DefaultConfigFile()
	}
}

// listConfig 只读配置文件本身：文件不存在时如实报告「还没创建」，不顺手建一个。
func listConfig(path string) error {
	ai, found, err := config.ReadAI(path)
	if err != nil {
		return err
	}
	fmt.Print(cli.FormatConfigList(path, found, cliAIItems(ai)))
	return nil
}

// setConfig 写入后立刻读回并回显：「写进去的」和「读出来的」不一致时用户能当场发现。
// 值不合法属于用法错误（退出码 2），写盘失败才是运行时故障（退出码 1）。
func setConfig(path, key, value string) error {
	if err := config.SetValue(path, key, value); err != nil {
		var invalid config.ValueError
		if errors.As(err, &invalid) {
			fmt.Fprintf(os.Stderr, "note: %v\n", err)
			return exitError(2)
		}
		return err
	}

	ai, _, err := config.ReadAI(path)
	if err != nil {
		return err
	}
	for _, item := range cliAIItems(ai) {
		if item.Key == key {
			fmt.Print(cli.FormatConfigSet(path, item))
			return nil
		}
	}
	return fmt.Errorf("写入后没读回 %s", key)
}

// cliAIItems 把 AI 四项转成 list 的行；密钥只给掩码，空值换成可读的占位。
func cliAIItems(ai config.AI) []cli.ConfigItem {
	return []cli.ConfigItem{
		{Key: "ai.base_url", Value: ai.BaseURL},
		{Key: "ai.api_key", Value: config.MaskSecret(ai.APIKey)},
		{Key: "ai.models", Value: config.ModelsDisplay(ai.Models)},
		{Key: "ai.prompt", Value: config.PromptDisplay(ai.Prompt)},
	}
}
