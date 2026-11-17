package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"note/internal/config"
)

// 为什么需要这份「入参快照」：一次超时/报错只留一句「模型 X 在 60 秒内没有响应」，
// 排查时最想知道的是「到底发了什么、发多大、用哪个模型、超时设了多少」。
//
// 同时必须守住项目硬约束：日志里不得出现笔记正文、渲染后的提示词、自定义提示词与
// api_key。所以正文一类的内容只以「码点/字节数 + sha256 前 12 位」出现——既能确认
// 「两次是不是同一份正文」「提示词有没有被自定义要求改写」，又不泄露一个字。

// maxErrorChainDepth 是错误链最多展开几层：层层包装时日志不该比正文还长。
const maxErrorChainDepth = 5

// RequestInfo 是一次调用的入参快照，字段与真正发出去的请求一一对应。
type RequestInfo struct {
	Endpoint    string
	BaseURL     string
	Host        string
	Model       string
	Models      []string
	Stream      bool
	Temperature float64
	MaxTokens   int

	Messages    int
	MessageRole string

	// PromptKind 说明提示词从哪来：builtin（内置默认）/ custom（note.yaml 的 ai.prompt）。
	PromptKind  string
	PromptRunes int
	PromptBytes int
	PromptSHA   string

	ContentRunes int
	ContentBytes int
	ContentSHA   string

	CustomPrompt      bool
	CustomPromptRunes int
	CustomPromptSHA   string

	APIKeyMasked string

	// HTTPClient 说明这次用的是共享实例还是调用方注入的（测试）。
	HTTPClient            string
	ClientTimeout         time.Duration // 0 表示没设总超时（流式）
	ResponseHeaderTimeout time.Duration
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ProxyFromEnv          bool
}

// Describe 给出这次调用会发出去的每一项入参，供调用方在失败时写进日志。
// 它不做校验、不报错（配置残缺也照常输出），因为日志不能成为第二个故障点。
func (c *Client) Describe(req Request, stream bool) RequestInfo {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = c.DefaultModel()
	}
	prompt := BuildPromptWithCustom(c.Prompt, req.Content, req.CustomPrompt)

	info := RequestInfo{
		Endpoint:     strings.TrimRight(strings.TrimSpace(c.BaseURL), "/") + "/chat/completions",
		BaseURL:      c.BaseURL,
		Host:         c.Host(),
		Model:        model,
		Models:       c.Models,
		Stream:       stream,
		Temperature:  0.2,
		MaxTokens:    maxCompletionTokens,
		Messages:     1,
		MessageRole:  "user",
		PromptKind:   promptKind(c.Prompt),
		PromptRunes:  utf8.RuneCountInString(prompt),
		PromptBytes:  len(prompt),
		PromptSHA:    sha12(prompt),
		ContentRunes: utf8.RuneCountInString(req.Content),
		ContentBytes: len(req.Content),
		ContentSHA:   sha12(req.Content),
		APIKeyMasked: config.MaskSecret(c.APIKey),
		ProxyFromEnv: proxyFromEnv(),
	}

	if custom := strings.TrimSpace(req.CustomPrompt); custom != "" {
		info.CustomPrompt = true
		info.CustomPromptRunes = utf8.RuneCountInString(custom)
		info.CustomPromptSHA = sha12(custom)
	}

	if c.HTTP != nil {
		info.HTTPClient = "injected"
		info.ClientTimeout = c.HTTP.Timeout
		return info
	}
	info.ResponseHeaderTimeout = sharedTransport.ResponseHeaderTimeout
	info.TLSHandshakeTimeout = sharedTransport.TLSHandshakeTimeout
	info.DialTimeout = dialTimeout
	if stream {
		info.HTTPClient = "shared-stream"
		// 流式刻意不设总超时（生成本来就是持续输出），日志里写 off 而不是 0。
		info.ClientTimeout = 0
	} else {
		info.HTTPClient = "shared"
		info.ClientTimeout = sharedClient.Timeout
	}
	return info
}

// Attrs 把快照摊成 slog 的 key/value 序列（顺序稳定，便于人读）。
func (info RequestInfo) Attrs() []any {
	attrs := []any{
		"endpoint", info.Endpoint,
		"host", info.Host,
		"base_url", info.BaseURL,
		"model", info.Model,
		"models", info.Models,
		"stream", info.Stream,
		"temperature", info.Temperature,
		"max_tokens", info.MaxTokens,
		"messages", info.Messages,
		"message_role", info.MessageRole,
		"prompt_kind", info.PromptKind,
		"prompt_runes", info.PromptRunes,
		"prompt_bytes", info.PromptBytes,
		"prompt_sha256", info.PromptSHA,
		"content_runes", info.ContentRunes,
		"content_bytes", info.ContentBytes,
		"content_sha256", info.ContentSHA,
		"custom_prompt", info.CustomPrompt,
		"custom_prompt_runes", info.CustomPromptRunes,
	}
	if info.CustomPrompt {
		attrs = append(attrs, "custom_prompt_sha256", info.CustomPromptSHA)
	}
	attrs = append(attrs,
		"api_key", info.APIKeyMasked,
		"http_client", info.HTTPClient,
		"client_timeout_ms", timeoutMillis(info.ClientTimeout),
		"response_header_timeout_ms", timeoutMillis(info.ResponseHeaderTimeout),
		"dial_timeout_ms", timeoutMillis(info.DialTimeout),
		"tls_handshake_timeout_ms", timeoutMillis(info.TLSHandshakeTimeout),
		"proxy_from_env", info.ProxyFromEnv,
	)
	return attrs
}

// ErrorAttrs 把一个错误展开成日志字段：给人看的整句、类型、以及底层原因链。
// 错误链能直接回答「是超时？是上游 5xx？还是 DNS 失败被包了一层」。
func ErrorAttrs(err error) []any {
	if err == nil {
		return nil
	}
	chain := make([]string, 0, maxErrorChainDepth)
	for cur := err; cur != nil && len(chain) < maxErrorChainDepth; cur = errors.Unwrap(cur) {
		chain = append(chain, fmt.Sprintf("%s: %s", typeName(cur), truncateRunes(cur.Error())))
	}
	attrs := []any{
		"error", truncateRunes(err.Error()),
		"error_type", typeName(err),
		"error_chain", chain,
	}
	return attrs
}

// typeName 取错误的动态类型名；包装类错误（*fmt.wrapError）对排查没意义，
// 因此优先取链上第一个非包装类型的名字。
func typeName(err error) string {
	name := fmt.Sprintf("%T", err)
	if name == "*fmt.wrapError" || name == "*errors.errorString" {
		// 继续往里看一层：包装错误的类型名只会误导人。
		if inner := errors.Unwrap(err); inner != nil {
			return typeName(inner)
		}
	}
	return strings.TrimPrefix(name, "*")
}

func promptKind(tpl string) string {
	if strings.TrimSpace(tpl) == "" {
		return "builtin"
	}
	return "custom"
}

// sha12 是内容指纹（sha256 前 12 位十六进制）：用于确认「两次是不是同一份正文 /
// 提示词有没有被改写」，同时不泄露内容本身。
func sha12(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:12]
}

func timeoutMillis(d time.Duration) any {
	if d <= 0 {
		return "off"
	}
	return d.Milliseconds()
}

// proxyFromEnv 只说「配没配代理」，不输出代理地址：地址里可能带凭据。
func proxyFromEnv() bool {
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

// Timeline 记录一次调用里网络各阶段的时间点（相对调用开始），用于回答
// 「60 秒没响应，是连都没连上，还是连上了模型不出字」。
//
// 只在调用方显式设置 Client.Timeline 时采集：默认零开销。
type Timeline struct {
	start     time.Time
	gotConn   time.Time
	reused    bool
	dnsDone   time.Time
	connDone  time.Time
	tlsDone   time.Time
	wroteReq  time.Time
	firstByte time.Time
}

// NewTimeline 开始一次采集。
func NewTimeline() *Timeline {
	return &Timeline{start: time.Now()}
}

// Attrs 摊平成日志字段；没走到的阶段给 "off"，别用 0 假装「瞬间完成」。
func (t *Timeline) Attrs() []any {
	if t == nil {
		return nil
	}
	return []any{
		"dns_ms", sinceMillis(t.start, t.dnsDone),
		"connect_ms", sinceMillis(t.start, t.connDone),
		"tls_ms", sinceMillis(t.start, t.tlsDone),
		"got_conn_ms", sinceMillis(t.start, t.gotConn),
		"conn_reused", t.reused,
		"wrote_request_ms", sinceMillis(t.start, t.wroteReq),
		"first_byte_ms", sinceMillis(t.start, t.firstByte),
	}
}

func sinceMillis(start, at time.Time) any {
	if at.IsZero() {
		return "off"
	}
	return at.Sub(start).Milliseconds()
}

// trace 把采集钩子挂到 ctx 上；Timeline 为 nil 时原样返回 ctx。
func (c *Client) trace(ctx context.Context) context.Context {
	if c.Timeline == nil {
		return ctx
	}
	t := c.Timeline
	if t.start.IsZero() {
		t.start = time.Now()
	}
	return httptraceWith(ctx, t)
}
