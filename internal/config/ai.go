package config

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// AI 是配置文件里的 AI 三项（OpenAI 兼容接口）。空串表示未设置。
type AI struct {
	BaseURL string
	APIKey  string
	Prompt  string
}

const (
	// MaxAIBaseURLLen 是 base_url 的字节上限：正常地址远小于它，超了多半是粘错。
	MaxAIBaseURLLen = 500
	// MaxAIKeyLen 是 api_key 的字节上限。
	MaxAIKeyLen = 512
	// MaxAIPromptRunes 与正文同口径（2000 码点）：提示词最终会拼进正文一起用。
	MaxAIPromptRunes = 2000
)

// ValueError 是某个配置项的值不合法（未知键、地址或密钥不合规）。
// 调用方据此按「用法错误」而不是「服务故障」处理：命令行退出码 2、
// 网页返回 400 字段级提示。
type ValueError struct{ Err error }

func (e ValueError) Error() string { return e.Err.Error() }
func (e ValueError) Unwrap() error { return e.Err }

// ErrUnknownKey 表示配置文件里已经有严格解析不认识的键：写回会把它们带出去，
// 所以宁可拒绝写入，让用户先手动修正（web 层据此返回 409 而不是 500）。
var ErrUnknownKey = errors.New("config file has unknown keys")

// aiKeys 是允许通过 note config set 写入的键（点分形式）与它在 YAML 里的层级。
// 只有这几项能从命令行改：配置文件里的其它键要么由安装流程写入，
// 要么由服务自己维护，手改比命令行更安全。
var aiKeys = []string{"ai.base_url", "ai.api_key", "ai.prompt"}

// KnownKeys 返回可写的配置键，供帮助与错误提示使用。
func KnownKeys() []string {
	return append([]string(nil), aiKeys...)
}

// IsKnownKey 判断 key 是否是 note config set 接受的键。
func IsKnownKey(key string) bool {
	for _, known := range aiKeys {
		if known == key {
			return true
		}
	}
	return false
}

// IsSecretKey 判断某个键的值是不是不该回显的密钥。
func IsSecretKey(key string) bool {
	return key == "ai.api_key"
}

// ReadAI 只读配置文件本身的 AI 三项：不合并命令行与内置默认值，也不做地址校验，
// 供 note config list 与 GET /api/ai-config 使用。
// 文件不存在时返回零值与 found=false（首次配置前查看配置不算错误）。
func ReadAI(path string) (AI, bool, error) {
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return AI{}, false, nil
	case err != nil:
		return AI{}, false, fmt.Errorf("read config %s: %w", path, err)
	}

	fileCfg, err := decode(data)
	if err != nil {
		return AI{}, true, fmt.Errorf("parse config %s: %w", path, err)
	}
	return AI{
		BaseURL: fileCfg.AI.BaseURL,
		APIKey:  fileCfg.AI.APIKey,
		Prompt:  fileCfg.AI.Prompt,
	}, true, nil
}

// KeyValue 是一次要写入的配置项（点分键 + 值）。
type KeyValue struct {
	Key   string
	Value string
}

// SetValue 写入单项；value 为空串表示清空该项。
func SetValue(path, key, value string) error {
	return SetValues(path, []KeyValue{{Key: key, Value: value}})
}

// SetValues 在一次原子替换里写入多项：全部通过校验才会落盘，
// 中途出错文件保持原样。流程是读现有 YAML（保留其它键与注释）→ 逐项改节点 →
// 同目录临时文件 + rename → 再按严格规则解析一遍，确认服务能加载。
func SetValues(path string, values []KeyValue) error {
	if len(values) == 0 {
		return nil
	}

	normalized := make([]KeyValue, 0, len(values))
	for _, item := range values {
		if !IsKnownKey(item.Key) {
			return ValueError{fmt.Errorf("未知配置项 %q（可用: %s）", item.Key, strings.Join(aiKeys, ", "))}
		}
		value := normalizeValue(item.Key, item.Value)
		if err := validateValue(item.Key, value); err != nil {
			return ValueError{err}
		}
		normalized = append(normalized, KeyValue{Key: item.Key, Value: value})
	}

	doc := &yaml.Node{}
	if data, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(data, doc); err != nil {
			return fmt.Errorf("parse config %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read config %s: %w", path, err)
	}

	for _, item := range normalized {
		if err := setNode(doc, item.Key, item.Value); err != nil {
			return err
		}
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	// 写出去之后必须还能被严格解析：note.yaml 的约定是未知键即报错，
	// 与其等下次启动才炸，不如现在就把错误拦在写入这一步。
	if _, err := decode(buf.Bytes()); err != nil {
		return fmt.Errorf("%w（%v）", ErrUnknownKey, err)
	}
	return writeAtomic(path, buf.Bytes())
}

// Normalize 把某个键的值处理成写入时的样子：地址与密钥去掉首尾空白
// （粘贴常带换行），提示词原样保留。web 层在校验前也用它，
// 免得「命令行能写的值、网页却报错」。
func Normalize(key, value string) string {
	return normalizeValue(key, value)
}

// normalizeValue 只做「粘贴时混进首尾空白」的清理：地址与密钥不该带换行，
// 提示词原样保留（换行是它的一部分）。
func normalizeValue(key, value string) string {
	if key == "ai.prompt" {
		return value
	}
	return strings.TrimSpace(value)
}

// validateValue 校验单个键的值，与 ValidateAI 是同一套口径。
func validateValue(key, value string) error {
	switch key {
	case "ai.base_url":
		return ValidateBaseURL(value)
	case "ai.api_key":
		return ValidateAPIKey(value)
	case "ai.prompt":
		return ValidatePrompt(value)
	}
	return nil
}

// ValidateAI 校验配置里的 AI 三项：启动加载、note config set 与
// PUT /api/ai-config 共用，避免「命令行写得进去、服务却起不来」。
func ValidateAI(ai AI) error {
	if err := ValidateBaseURL(ai.BaseURL); err != nil {
		return err
	}
	if err := ValidateAPIKey(ai.APIKey); err != nil {
		return err
	}
	return validateValue("ai.prompt", ai.Prompt)
}

// ValidateBaseURL 校验 ai.base_url：空串是合法的「未设置」。
func ValidateBaseURL(raw string) error {
	if len(raw) > MaxAIBaseURLLen {
		return fmt.Errorf("ai.base_url 太长了，请控制在 %d 字节以内", MaxAIBaseURLLen)
	}
	if raw == "" {
		return nil
	}

	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("ai.base_url 必须是 http(s) 绝对地址，例如 https://api.openai.com/v1")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("ai.base_url 不要带查询串或锚点，接口路径写在路径部分")
	}
	return nil
}

// ValidatePrompt 校验 ai.prompt：空串表示「不追加提示词」。
func ValidatePrompt(prompt string) error {
	if n := utf8.RuneCountInString(prompt); n > MaxAIPromptRunes {
		return fmt.Errorf("ai.prompt 请控制在 %d 字以内，当前 %d 字", MaxAIPromptRunes, n)
	}
	return nil
}

// ValidateAPIKey 校验 ai.api_key：空串是合法的「未设置」。
func ValidateAPIKey(key string) error {
	if len(key) > MaxAIKeyLen {
		return fmt.Errorf("ai.api_key 太长了，请控制在 %d 字节以内", MaxAIKeyLen)
	}
	if key == "" {
		return nil
	}
	if strings.IndexFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("ai.api_key 不能包含空白或控制字符（多半是粘贴时混进了换行）")
	}
	return nil
}

// MaskSecret 把密钥压成「前 3 + … + 后 4」的展示形态；短于 12 个字符时只给星号，
// 免得遮了跟没遮一样。空串原样返回（调用方用它表示「未设置」）。
func MaskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	runes := []rune(secret)
	if len(runes) < 12 {
		return strings.Repeat("*", 8)
	}
	return string(runes[:3]) + "…" + string(runes[len(runes)-4:])
}

// setNode 在 YAML 文档里按点分路径写入一个字符串值，沿途缺的映射自动创建。
func setNode(doc *yaml.Node, key, value string) error {
	parts := strings.Split(key, ".")
	if doc.Kind == 0 {
		doc.Kind = yaml.DocumentNode
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}

	node := doc.Content[0]
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("配置文件的顶层不是映射，无法写入 %s", key)
	}
	for _, part := range parts[:len(parts)-1] {
		child := mappingValue(node, part)
		switch {
		case child == nil:
			child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			node.Content = append(node.Content, stringNode(part), child)
		case child.Kind != yaml.MappingNode:
			return fmt.Errorf("配置项 %q 不是映射，无法写入 %s", part, key)
		}
		node = child
	}

	leaf := stringNode(value)
	if strings.Contains(value, "\n") {
		// 多行提示词用字面量块，手改 YAML 时也能读
		leaf.Style = yaml.LiteralStyle
	}
	setMappingEntry(node, parts[len(parts)-1], leaf)
	return nil
}

func stringNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// setMappingEntry 覆盖已有的值节点（键节点连同注释留在原位），没有就追加。
func setMappingEntry(node *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1] = value
			return
		}
	}
	node.Content = append(node.Content, stringNode(key), value)
}

// writeAtomic 用同目录临时文件 + rename 原子替换配置文件，权限 0600：
// 中断只会留下一个待清理的临时文件，不会出现半截 YAML。
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".note-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config %s: %w", path, err)
	}
	return nil
}
