package main

import (
	"reflect"
	"testing"

	"note/internal/cli"
	"note/internal/config"
)

// note config list 的第四行与默认提示词占位都在这里定：四项顺序固定，
// 空模型列表与空提示词都换成可读的说明，而不是让用户看到一片空白。
func TestCLIAIItemsShowsModelsAndPromptPlaceholder(t *testing.T) {
	got := cliAIItems(config.AI{
		BaseURL: "https://api.example.com/v1",
		APIKey:  "sk-abcdefghijklmnop",
		Models:  []string{"gpt-4o-mini", "deepseek-chat"},
	})
	want := []cli.ConfigItem{
		{Key: "ai.base_url", Value: "https://api.example.com/v1"},
		{Key: "ai.api_key", Value: config.MaskSecret("sk-abcdefghijklmnop")},
		{Key: "ai.models", Value: "gpt-4o-mini, deepseek-chat"},
		{Key: "ai.prompt", Value: "（默认提示词）"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cliAIItems:\n got %#v\nwant %#v", got, want)
	}

	empty := cliAIItems(config.AI{})
	if empty[2].Value != "（未配置）" {
		t.Fatalf("空模型列表应当显示未配置，得到 %q", empty[2].Value)
	}
}
