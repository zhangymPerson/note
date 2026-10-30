package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSetValueCreatesFileFromScratch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.yaml")

	if err := SetValue(path, "ai.base_url", "https://api.openai.com/v1"); err != nil {
		t.Fatalf("set base url: %v", err)
	}

	cfg, _, err := Load([]string{"--config", path})
	if err != nil {
		t.Fatalf("load after set: %v", err)
	}
	if cfg.AI.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("base url not persisted: %+v", cfg.AI)
	}
	if cfg.AI.APIKey != "" || cfg.AI.Prompt != "" {
		t.Fatalf("其它两项应当仍为空: %+v", cfg.AI)
	}
	// 新建文件同样要能通过严格解析，且只写 ai 段
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "ai:") {
		t.Fatalf("写入的文件里没有 ai 段:\n%s", data)
	}
}

func TestSetValueKeepsOtherKeysAndComments(t *testing.T) {
	path := writeConfig(t, `# 我的配置，别删注释
server:
  addr: 127.0.0.1:9090 # 只在本机
database:
  path: ./data/notes.db
user:
  name: 小北
  name_set: true
`)

	if err := SetValue(path, "ai.api_key", "sk-test-1234567890abcdef"); err != nil {
		t.Fatalf("set api key: %v", err)
	}
	if err := SetValue(path, "ai.base_url", "https://proxy.example.com/v1"); err != nil {
		t.Fatalf("set base url: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	for _, want := range []string{"# 我的配置，别删注释", "# 只在本机", "127.0.0.1:9090", "./data/notes.db", "小北"} {
		if !strings.Contains(text, want) {
			t.Fatalf("写入后丢了 %q:\n%s", want, text)
		}
	}

	cfg, _, err := Load([]string{"--config", path})
	if err != nil {
		t.Fatalf("load after set: %v", err)
	}
	if cfg.AI.APIKey != "sk-test-1234567890abcdef" || cfg.AI.BaseURL != "https://proxy.example.com/v1" {
		t.Fatalf("ai 段没写对: %+v", cfg.AI)
	}
	if cfg.Addr != "127.0.0.1:9090" || cfg.UserName != "小北" || !cfg.NameSet {
		t.Fatalf("其它键被改坏了: %+v", cfg)
	}
}

func TestSetValueRejectsUnknownKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.yaml")

	err := SetValue(path, "ai.model", "gpt-4o-mini")
	if err == nil {
		t.Fatal("未知配置项应当报错")
	}
	if !strings.Contains(err.Error(), "ai.base_url") || !strings.Contains(err.Error(), "ai.api_key") {
		t.Fatalf("错误信息应当列出可用键，得到 %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("被拒绝的写入不该创建文件：%v", statErr)
	}
}

func TestSetValueRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		value string
		want  string
	}{
		{"地址没有协议", "ai.base_url", "api.openai.com/v1", "http(s)"},
		{"地址协议不对", "ai.base_url", "ftp://example.com/v1", "http(s)"},
		{"地址带查询串", "ai.base_url", "https://example.com/v1?x=1", "查询串"},
		{"密钥里有空格", "ai.api_key", "sk-in the middle", "空白"},
		{"提示词太长", "ai.prompt", strings.Repeat("字", MaxAIPromptRunes+1), "2000"},
	}
	for _, tc := range cases {
		path := filepath.Join(t.TempDir(), "note.yaml")
		err := SetValue(path, tc.key, tc.value)
		if err == nil {
			t.Fatalf("%s：应当报错", tc.name)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s：错误信息应当含 %q，得到 %v", tc.name, tc.want, err)
		}
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("%s：校验失败不该写文件", tc.name)
		}
	}
}

// 配置文件里已经有严格解析不认识的键时，写回会把它们带出去，所以宁可直接拒绝。
func TestSetValueRefusesFileWithUnknownKeys(t *testing.T) {
	content := "server:\n  addr: 127.0.0.1:8080\nbogus: 1\n"
	path := writeConfig(t, content)

	err := SetValue(path, "ai.base_url", "https://api.openai.com/v1")
	if err == nil {
		t.Fatal("文件里有未知键时应当拒绝写入")
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read config: %v", readErr)
	}
	if string(data) != content {
		t.Fatalf("被拒绝的写入不该改动文件：\n%s", data)
	}
}

func TestSetValueClearsWithEmptyValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.yaml")
	if err := SetValue(path, "ai.api_key", "sk-clear-me-1234567890"); err != nil {
		t.Fatalf("set api key: %v", err)
	}
	if err := SetValue(path, "ai.api_key", ""); err != nil {
		t.Fatalf("clear api key: %v", err)
	}

	ai, found, err := ReadAI(path)
	if err != nil || !found {
		t.Fatalf("read ai: found=%v err=%v", found, err)
	}
	if ai.APIKey != "" {
		t.Fatalf("密钥应当被清空，得到 %q", ai.APIKey)
	}
}

func TestSetValueWritesPromptAsLiteralBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.yaml")
	prompt := "把下面的内容整理成 Markdown：\n\n- 保留标题\n- 去掉寒暄\n\n" + PromptVariable

	if err := SetValue(path, "ai.prompt", prompt); err != nil {
		t.Fatalf("set prompt: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "prompt: |") {
		t.Fatalf("多行提示词应当写成字面量块:\n%s", data)
	}

	ai, _, err := ReadAI(path)
	if err != nil {
		t.Fatalf("read ai: %v", err)
	}
	if ai.Prompt != prompt {
		t.Fatalf("提示词应当逐字保留:\n got %q\nwant %q", ai.Prompt, prompt)
	}
}

func TestSetValueLeavesNoTempFileAndUses0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.yaml")
	if err := SetValue(path, "ai.base_url", "https://api.openai.com/v1"); err != nil {
		t.Fatalf("set base url: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "note.yaml" {
		t.Fatalf("目录里只该有 note.yaml，得到 %v", entries)
	}

	if runtime.GOOS == "windows" {
		// Windows 上 chmod 不生效，权限按 daemon/state_test 的先例跳过
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("配置文件权限应当是 0600，得到 %o", mode)
	}
}

func TestReadAIMissingFileIsNotAnError(t *testing.T) {
	ai, found, err := ReadAI(filepath.Join(t.TempDir(), "note.yaml"))
	if err != nil {
		t.Fatalf("文件不存在不该报错：%v", err)
	}
	if found {
		t.Fatal("文件不存在时 found 应当为 false")
	}
	if !reflect.DeepEqual(ai, AI{}) {
		t.Fatalf("应当返回零值，得到 %+v", ai)
	}
}

func TestLoadReadsAIAndRejectsBadValues(t *testing.T) {
	path := writeConfig(t, `ai:
  base_url: https://api.deepseek.com/v1
  api_key: sk-abcdefghijklmnop
  prompt: 请把内容整理成笔记 {{content}}
`)
	cfg, _, err := Load([]string{"--config", path})
	if err != nil {
		t.Fatalf("load ai config: %v", err)
	}
	if cfg.AI.BaseURL != "https://api.deepseek.com/v1" ||
		cfg.AI.APIKey != "sk-abcdefghijklmnop" ||
		cfg.AI.Prompt != "请把内容整理成笔记 {{content}}" {
		t.Fatalf("ai 三项没读出来: %+v", cfg.AI)
	}

	// 手改出坏地址时要在启动阶段就报错，而不是等真的去调接口
	bad := writeConfig(t, "ai:\n  base_url: api.openai.com\n")
	if _, _, err := Load([]string{"--config", bad}); err == nil {
		t.Fatal("非法 base_url 应当让 Load 报错")
	} else if !strings.Contains(err.Error(), "ai.base_url") {
		t.Fatalf("错误信息应当指出是 ai.base_url，得到 %v", err)
	}
}

func TestMaskSecret(t *testing.T) {
	cases := []struct {
		name   string
		secret string
		want   string
	}{
		{"空串", "", ""},
		{"太短", "sk-123", "********"},
		{"刚好够长", "sk-123456789", "sk-…6789"},
		{"常见长度", "sk-abcdefghijklmnopqrstuvwxyz", "sk-…wxyz"},
	}
	for _, tc := range cases {
		if got := MaskSecret(tc.secret); got != tc.want {
			t.Fatalf("%s：MaskSecret(%q) = %q, want %q", tc.name, tc.secret, got, tc.want)
		}
	}

	// 掩码里不能出现完整密钥
	secret := "sk-abcdefghijklmnopqrstuvwxyz"
	if strings.Contains(secret, MaskSecret(secret)) {
		t.Fatalf("掩码结果不该是原文的子串：%q", MaskSecret(secret))
	}
}

func TestSetValuesWritesAllInOneGo(t *testing.T) {
	path := writeConfig(t, "server:\n  addr: 127.0.0.1:9090\n")

	err := SetValues(path, []KeyValue{
		{Key: "ai.base_url", Value: "https://api.example.com/v1"},
		{Key: "ai.api_key", Value: "sk-abcdefghijkl"},
		{Key: "ai.prompt", Value: "整理成笔记\n" + PromptVariable},
	})
	if err != nil {
		t.Fatalf("set values: %v", err)
	}

	ai, _, err := ReadAI(path)
	if err != nil {
		t.Fatalf("read ai: %v", err)
	}
	if ai.BaseURL != "https://api.example.com/v1" || ai.APIKey != "sk-abcdefghijkl" || ai.Prompt != "整理成笔记\n"+PromptVariable {
		t.Fatalf("三项应当一起写入: %+v", ai)
	}

	// 其中一项不合法时整次写入作废，文件保持上一版
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if err := SetValues(path, []KeyValue{
		{Key: "ai.base_url", Value: "https://changed.example.com/v1"},
		{Key: "ai.api_key", Value: "sk-contains space"},
	}); err == nil {
		t.Fatal("有非法值时应当整次拒绝")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("部分失败不该留下半截写入:\n before:\n%s\n after:\n%s", before, after)
	}
}

func TestSplitModels(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a,b", []string{"a", "b"}},
		{" a , b ,, ", []string{"a", "b"}},
		{"a\nb", []string{"a", "b"}},
		{"a,a,b", []string{"a", "b"}},
		{"", nil},
	}
	for _, c := range cases {
		if got := SplitModels(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitModels(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
	if got := JoinModels([]string{"a", "b"}); got != "a, b" {
		t.Fatalf("JoinModels = %q", got)
	}
}

func TestValidateModels(t *testing.T) {
	if err := ValidateModels(nil); err != nil {
		t.Fatalf("空列表应当合法: %v", err)
	}
	if err := ValidateModels([]string{"gpt-4o-mini", "deepseek-chat"}); err != nil {
		t.Fatalf("正常列表: %v", err)
	}
	if err := ValidateModels([]string{"gpt 4o"}); err == nil {
		t.Fatal("带空白应当被拒")
	}
	if err := ValidateModels([]string{strings.Repeat("m", MaxAIModelLen+1)}); err == nil {
		t.Fatal("超长模型名应当被拒")
	}
	many := make([]string, MaxAIModels+1)
	for i := range many {
		many[i] = fmt.Sprintf("m%d", i)
	}
	if err := ValidateModels(many); err == nil {
		t.Fatal("超过 MaxAIModels 应当被拒")
	}
}

func TestValidatePromptRequiresPlaceholder(t *testing.T) {
	if err := ValidatePrompt(""); err != nil {
		t.Fatalf("空串（使用默认提示词）应当合法: %v", err)
	}
	if err := ValidatePrompt("整理成笔记\n" + PromptVariable); err != nil {
		t.Fatalf("带变量应当合法: %v", err)
	}
	err := ValidatePrompt("整理成笔记")
	if err == nil {
		t.Fatal("缺 {{content}} 应当被拒")
	}
	if !strings.Contains(err.Error(), PromptVariable) || !strings.Contains(err.Error(), "留空") {
		t.Fatalf("错误文案要点名变量并给出恢复默认的办法: %v", err)
	}
}

func TestDefaultAIPromptIsUsable(t *testing.T) {
	if !strings.Contains(DefaultAIPrompt, PromptVariable) {
		t.Fatal("默认提示词必须含变量")
	}
	if n := utf8.RuneCountInString(DefaultAIPrompt); n > MaxAIPromptRunes {
		t.Fatalf("默认提示词 %d 字超出上限", n)
	}
	if got := PromptOrDefault(""); got != DefaultAIPrompt {
		t.Fatal("空提示词应当回落到默认")
	}
	if got := PromptOrDefault("自定义" + PromptVariable); got != "自定义"+PromptVariable {
		t.Fatal("非空应当原样")
	}
	if PromptDisplay("") != "（默认提示词）" || ModelsDisplay(nil) != "（未配置）" {
		t.Fatal("空值展示文案不对")
	}
	if ModelsDisplay([]string{"a", "b"}) != "a, b" {
		t.Fatal("非空模型列表应当逐项展示")
	}
}

func TestSetModelsWritesSequenceAndKeepsComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.yaml")
	seed := "# 我的注释\nserver:\n  addr: 127.0.0.1:8080\nai:\n  base_url: https://api.example.com/v1\n"
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SetValues(path, []KeyValue{
		{Key: "ai.models", Value: "gpt-4o-mini, deepseek-chat"},
		{Key: "ai.prompt", Value: "整理成笔记\n" + PromptVariable},
	}); err != nil {
		t.Fatalf("SetValues: %v", err)
	}

	ai, found, err := ReadAI(path)
	if err != nil || !found {
		t.Fatalf("ReadAI: %v found=%v", err, found)
	}
	if !reflect.DeepEqual(ai.Models, []string{"gpt-4o-mini", "deepseek-chat"}) {
		t.Fatalf("models 往返不一致: %#v", ai.Models)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "# 我的注释") {
		t.Fatal("注释必须保留")
	}
	if !strings.Contains(string(data), "models:\n    - gpt-4o-mini") {
		t.Fatalf("models 应当写成块序列:\n%s", data)
	}

	// 清空：写成 []，再读回是空列表
	if err := SetValue(path, "ai.models", ""); err != nil {
		t.Fatalf("清空 models: %v", err)
	}
	ai, _, err = ReadAI(path)
	if err != nil {
		t.Fatalf("清空后读取: %v", err)
	}
	if len(ai.Models) != 0 {
		t.Fatalf("清空后应为空: %#v", ai.Models)
	}
}

func TestSetModelsRejectsBadValueWithoutTouchingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.yaml")
	seed := "ai:\n  base_url: https://api.example.com/v1\n"
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	err := SetValue(path, "ai.models", "a, b c")
	var invalid ValueError
	if !errors.As(err, &invalid) {
		t.Fatalf("应当是 ValueError，得到 %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != seed {
		t.Fatal("校验失败不得改动文件")
	}
}

// 启动时的严格校验：提示词缺变量、models 写成标量都要当场拒绝。
func TestLoadValidatesAI(t *testing.T) {
	t.Run("提示词缺变量", func(t *testing.T) {
		path := writeConfig(t, "ai:\n  prompt: 整理成笔记\n")
		if _, _, err := Load([]string{"--config", path}); err == nil {
			t.Fatal("启动时必须拒绝缺 {{content}} 的提示词")
		}
	})
	t.Run("models 写成标量", func(t *testing.T) {
		path := writeConfig(t, "ai:\n  models: gpt-4o\n")
		if _, _, err := Load([]string{"--config", path}); err == nil {
			t.Fatal("models 是标量时应当报错而不是猜成列表")
		}
	})
	t.Run("正常列表", func(t *testing.T) {
		path := writeConfig(t, "ai:\n  base_url: https://api.example.com/v1\n  models:\n    - a\n    - b\n")
		cfg, _, err := Load([]string{"--config", path})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !reflect.DeepEqual(cfg.AI.Models, []string{"a", "b"}) {
			t.Fatalf("models 没读进来: %#v", cfg.AI.Models)
		}
	})
}
