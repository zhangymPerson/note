// Package ai 是对外的唯一出口：用 note.yaml 里的 OpenAI 兼容配置调用
// /chat/completions，把一段 Markdown 笔记交给模型润色格式。
//
// 这里刻意不依赖 web 与 cli：网页（服务端代发，密钥不出机器）与
// note add（不要求 note web 在跑）共用同一份实现。
// 日志由调用方负责：本包不打印任何东西，也不把正文写进错误信息。
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"note/internal/config"
)

// Client 是一次调用的全部上下文。零值不可用：请用 NewClient 构造。
type Client struct {
	BaseURL string
	APIKey  string
	Models  []string
	Prompt  string
	HTTP    *http.Client // nil → 60s 超时的默认客户端
}

// Result 是成功调用的结果：Content 已剥掉整段代码围栏，Model 是实际用的模型。
type Result struct{ Content, Model string }

const (
	// maxResponseBytes 是上游响应的读取上限：正常回复几 KB，超过就是异常。
	maxResponseBytes = 1 << 20
	// maxErrorMessageRunes 限制回显给用户与日志的上游错误摘要长度。
	maxErrorMessageRunes = 200
	defaultTimeout       = 60 * time.Second
	// MaxCustomPromptRunes 是「自定义提示词」的码点上限：它是本次调用的补充约束，
	// 不是第二份提示词模板（后者是 ai.prompt，上限 2000）。
	MaxCustomPromptRunes = 500
)

var (
	// ErrNotConfigured 表示 base_url / api_key / models 没配齐。
	ErrNotConfigured = errors.New("ai is not configured")
	// ErrBadResponse 表示上游返回的不是能用的 chat/completions 结果。
	ErrBadResponse = errors.New("upstream returned unexpected json")
	// ErrTooLarge 表示上游响应超过读取上限。
	ErrTooLarge = errors.New("upstream response too large")
	// ErrEmptyResult 表示上游回了空内容（例如只剩围栏或全空白）。
	ErrEmptyResult = errors.New("upstream returned empty content")
)

// chatMessage / chatRequest 是发往上游的请求体。字段上的 json tag 不是装饰：
// OpenAI 兼容服务的 role/content 键名大小写敏感，没有 tag 时 encoding/json 会
// 发成 Role/Content，真正的服务会直接 400。
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

// ModelError 表示请求的模型不在 ai.models 里：切换模型是显式选择，不猜。
type ModelError struct{ Model string }

func (e ModelError) Error() string {
	return fmt.Sprintf("模型 %q 不在 ai.models 里，请先在配置里加上它", e.Model)
}

// TimeoutError 表示上游在超时时间内没有返回。
//
// 为什么要包一层：net/http 的原始错误长这样
// `Post "https://gw.example.com/v1/chat/completions": context deadline exceeded`，
// 命令行把它当状态行抛出来、网页原样回给用户，都等于没说「接下来能做什么」。
// 这里统一成「模型 X 在 N 秒内没有响应，可以稍后重试、换一个模型…」，
// 并用 Unwrap 保留 context.DeadlineExceeded，让调用方仍能判定 504。
type TimeoutError struct {
	Model    string
	Duration time.Duration
	Err      error
}

func (e TimeoutError) Error() string {
	where := "AI 接口"
	if e.Model != "" {
		where = fmt.Sprintf("模型 %s", e.Model)
	}
	return fmt.Sprintf("%s 在 %s内没有响应，可以稍后重试、换一个模型，或检查接口地址是否可用",
		where, formatTimeout(e.Duration))
}

func (e TimeoutError) Unwrap() error { return e.Err }

// formatTimeout 把超时时长写成人话：整秒用「60 秒」，毫秒级用「200 毫秒」，
// 其余情况退回 Duration.String()（例如 1.5 秒不会变成「1 秒」）。
func formatTimeout(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d%time.Second == 0:
		return fmt.Sprintf("%d 秒", int64(d/time.Second))
	case d%time.Millisecond == 0:
		return fmt.Sprintf("%d 毫秒", int64(d/time.Millisecond))
	default:
		return d.String() + " "
	}
}

// UpstreamError 是上游的非 2xx 响应：保留状态码与截断后的摘要。
type UpstreamError struct {
	Status  int
	Message string
}

func (e UpstreamError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("AI 接口返回 %d", e.Status)
	}
	return fmt.Sprintf("AI 接口返回 %d：%s", e.Status, e.Message)
}

// NewClient 从配置构造客户端；HTTP 留空，由 Optimize 按需补默认值。
func NewClient(cfg config.AI) *Client {
	return &Client{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Models:  cfg.Models,
		Prompt:  cfg.Prompt,
	}
}

// Configured 表示三项都齐了（base_url、api_key 至少一个模型）。
func (c *Client) Configured() bool {
	return len(c.Missing()) == 0
}

// Missing 返回还缺哪几项，供网页与命令行给出具体提示。
func (c *Client) Missing() []string {
	var missing []string
	if strings.TrimSpace(c.BaseURL) == "" {
		missing = append(missing, "ai.base_url")
	}
	if strings.TrimSpace(c.APIKey) == "" {
		missing = append(missing, "ai.api_key")
	}
	if len(c.Models) == 0 {
		missing = append(missing, "ai.models")
	}
	return missing
}

// DefaultModel 是下拉框的初始值：列表的第一个。
func (c *Client) DefaultModel() string {
	if len(c.Models) == 0 {
		return ""
	}
	return c.Models[0]
}

// Host 是网页上「数据会发到哪里」的主机名；解析不出来就原样返回。
func (c *Client) Host() string {
	parsed, err := url.Parse(c.BaseURL)
	if err != nil || parsed.Host == "" {
		return c.BaseURL
	}
	return parsed.Host
}

// Request 是一次优化的入参：正文、模型（空串表示列表第一个）与本次的自定义提示词。
// CustomPrompt 是编辑器里那一栏「自定义提示词」的内容，只对这一次调用生效，
// 不写回 note.yaml；空串表示没有额外要求。
type Request struct {
	Content      string
	Model        string
	CustomPrompt string
}

// Optimize 渲染提示词并发起一次调用。req.Content 原样代入模板，不截断、不改写
// （长度由调用方预检）。
func (c *Client) Optimize(ctx context.Context, req Request) (Result, error) {
	if !c.Configured() {
		return Result{}, ErrNotConfigured
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = c.DefaultModel()
	} else if !c.hasModel(model) {
		return Result{}, ModelError{Model: model}
	}

	payload := chatRequest{Model: model, Temperature: 0.2}
	payload.Messages = []chatMessage{{
		Role:    "user",
		Content: BuildPromptWithCustom(c.Prompt, req.Content, req.CustomPrompt),
	}}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, fmt.Errorf("encode request: %w", err)
	}

	// 末尾斜杠不参与拼接：base_url 写成 .../v1 或 .../v1/ 都一样。
	endpoint := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return Result{}, wrapTransportError(err, model, effectiveTimeout(client))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return Result{}, fmt.Errorf("read response: %w", err)
	}
	if len(raw) > maxResponseBytes {
		return Result{}, ErrTooLarge
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Result{}, UpstreamError{
			Status:  resp.StatusCode,
			Message: truncateRunes(errorMessage(raw, resp.StatusCode)),
		}
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Result{}, ErrBadResponse
	}
	if len(parsed.Choices) == 0 {
		return Result{}, ErrBadResponse
	}

	content2 := StripFence(parsed.Choices[0].Message.Content)
	if content2 == "" {
		return Result{}, ErrEmptyResult
	}
	return Result{Content: content2, Model: model}, nil
}

// wrapTransportError 只给「等太久」这一种失败换文案：其它传输错误（DNS、连接被拒、
// 证书问题）原样返回，它们的原文比我们编的摘要更有用。
func wrapTransportError(err error, model string, timeout time.Duration) error {
	var netErr net.Error
	if !errors.Is(err, context.DeadlineExceeded) && !(errors.As(err, &netErr) && netErr.Timeout()) {
		return err
	}
	if errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		// 用户自己取消（网页关页、Ctrl+C）不是「超时」，别误导成上游有问题。
		return err
	}
	return TimeoutError{Model: model, Duration: timeout, Err: err}
}

// effectiveTimeout 是这次调用真正生效的上限：客户端自带 60 秒，
// 调用方也可以塞一个自己的 HTTP 客户端（测试就是这么把超时压到毫秒的）。
func effectiveTimeout(c *http.Client) time.Duration {
	if c != nil && c.Timeout > 0 {
		return c.Timeout
	}
	return defaultTimeout
}

func (c *Client) hasModel(model string) bool {
	for _, candidate := range c.Models {
		if candidate == model {
			return true
		}
	}
	return false
}

// BuildPrompt 把正文代入模板；空模板用内置默认提示词。
// 只替换第一个变量，正文里再出现 {{content}} 也原样保留（不二次展开）。
func BuildPrompt(tpl, content string) string {
	tpl = config.PromptOrDefault(tpl)
	if !strings.Contains(tpl, config.PromptVariable) {
		return tpl + "\n\n" + content
	}
	return strings.Replace(tpl, config.PromptVariable, content, 1)
}

// ValidateCustomPrompt 校验本次的自定义提示词：空串合法（不说话就是没要求），
// 超长拒绝。它和 ai.prompt 是两种东西，所以校验口径也分开（500 码点 vs 2000）。
func ValidateCustomPrompt(custom string) error {
	if n := utf8.RuneCountInString(custom); n > MaxCustomPromptRunes {
		return fmt.Errorf("自定义提示词请控制在 %d 字以内，当前 %d 字", MaxCustomPromptRunes, n)
	}
	return nil
}

// BuildPromptWithCustom 在 BuildPrompt 的结果之后追加本次的额外要求。
// 追加在模板末尾并写明「不得违反上面任何一条」，以免「用 mermaid 展示」这类补充
// 把「只输出优化后的 Markdown 正文」「不超 2000 字符」冲掉；额外要求里的
// {{content}} 同样不展开（正文只在前面的模板里化入一次）。
func BuildPromptWithCustom(tpl, content, custom string) string {
	base := BuildPrompt(tpl, content)
	custom = strings.TrimSpace(custom)
	if custom == "" {
		return base
	}
	return base + "\n\n【本次额外要求】（在满足上面全部要求的前提下执行，仍然只输出优化后的 Markdown 正文）\n" + custom
}

// StripFence 剥掉模型「顺手」包上的整段代码围栏（``` 或 ~~~）。
// 只在围栏恰好从头包到尾、且内部没有第二组同种围栏时剥，避免吃掉正文里的代码块。
func StripFence(text string) string {
	s := strings.TrimSpace(text)
	firstLineEnd := strings.Index(s, "\n")
	if firstLineEnd < 0 {
		return s
	}
	first := strings.TrimSpace(s[:firstLineEnd])
	marker := ""
	for _, candidate := range []string{"```", "~~~"} {
		if strings.HasPrefix(first, candidate) {
			marker = candidate
			break
		}
	}
	if marker == "" {
		return s
	}

	lastLineStart := strings.LastIndex(s, "\n")
	if lastLineStart < 0 || strings.TrimSpace(s[lastLineStart+1:]) != marker {
		return s
	}
	inner := s[firstLineEnd+1 : lastLineStart]
	for _, line := range strings.Split(inner, "\n") {
		if strings.TrimSpace(line) == marker {
			return s // 内部还有同种围栏：说明这是正文里的代码块，别动
		}
	}
	return strings.TrimSpace(inner)
}

// errorMessage 从上游错误体里取 message；取不到就用状态码的标准文案。
func errorMessage(raw []byte, status int) string {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err == nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return strings.TrimSpace(parsed.Error.Message)
	}
	return http.StatusText(status)
}

// truncateRunes 按码点截断，避免把多字节字符切坏。
func truncateRunes(text string) string {
	if utf8.RuneCountInString(text) <= maxErrorMessageRunes {
		return text
	}
	runes := []rune(text)
	return string(runes[:maxErrorMessageRunes-1]) + "…"
}
