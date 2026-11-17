package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// StreamFunc 是流式增量回调：每拿到一段新的正文片段调用一次。
// 返回错误即中止本次调用（浏览器断开、用户点「停止」、写响应失败等）。
type StreamFunc func(delta string) error

// OptimizeStream 与 Optimize 等价，只是要求上游边生成边返回：
// 每收到一段增量就交给 onDelta，最后仍返回剥好围栏的完整结果。
//
// 为什么值得分两条路：网页最怕的是「点了按钮盯着转圈」——同样的生成时长，
// 首字能在 1 秒内出现，体验完全不同。一次性结果排版更好，所以 CLI 的
// Ctrl+O 继续走 Optimize，两条管线共用同一份 prepare / 错误映射。
func (c *Client) OptimizeStream(ctx context.Context, req Request, onDelta StreamFunc) (Result, error) {
	prompt := BuildPromptWithCustom(c.Prompt, req.Content, req.CustomPrompt)
	parsed, model, err := c.chatStream(ctx, req.Model, prompt, onDelta)
	return c.finish(parsed, model, err)
}

// chatStream 发一次 stream:true 的请求并逐段解析 SSE，返回累积的正文与模型。
//
// 兼容性兜底：有的网关会无视 stream:true，直接回一段普通 JSON。发现响应不是
// event-stream 就按非流式解析、把整段内容交给 onDelta 一次，而不是报「空结果」。
func (c *Client) chatStream(ctx context.Context, modelName, prompt string, onDelta StreamFunc) (parsedCompletion, string, error) {
	body, endpoint, model, err := c.prepare(modelName, prompt, true)
	if err != nil {
		return parsedCompletion{}, "", err
	}

	// 空闲看门狗：长时间没有下一段就主动掐掉上游，别让用户干等到总超时。
	// 取消的是子 ctx，所以浏览器断开（父 ctx 取消）仍会被认成 client_gone。
	idleCtx, stopIdle := context.WithCancel(ctx)
	defer stopIdle()
	watch := newIdleWatch(stopIdle, c.idleTimeout())
	defer watch.stop()

	client := c.httpClient(true)
	httpReq, err := http.NewRequestWithContext(c.trace(idleCtx), http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return parsedCompletion{}, "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(httpReq)
	if err != nil {
		if watch.fired() {
			return parsedCompletion{}, "", IdleError{Model: model, Idle: watch.idle}
		}
		return parsedCompletion{}, "", wrapTransportError(err, model, effectiveTimeout(client))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		return parsedCompletion{}, "", UpstreamError{
			Status:  resp.StatusCode,
			Message: truncateRunes(errorMessage(raw, resp.StatusCode)),
		}
	}

	// 每收到一段正文就重置看门狗：只要还在出字，慢就不是问题。
	watched := func(delta string) error {
		watch.touch()
		if onDelta == nil {
			return nil
		}
		return onDelta(delta)
	}

	var (
		parsed  parsedCompletion
		readErr error
	)
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		parsed, _, readErr = readWholeCompletion(resp.Body, model, watched)
	} else {
		parsed, _, readErr = readSSE(idleCtx, resp.Body, model, watched)
	}
	if readErr != nil && watch.fired() {
		// 是看门狗掐的，不是上游自己断的：给它一个能说人话的错误。
		return parsedCompletion{}, "", IdleError{Model: model, Idle: watch.idle}
	}
	return parsed, model, readErr
}

// idleWatch 是「多久没有下一段输出就不能忍了」的计时器。
// 到点只做一件事：把传入的 cancel 调掉，让挂在这个 ctx 上的请求立刻返回。
// touch 在每段正文到达时重置，所以它衡量的是「沉默时长」而不是「总时长」。
type idleWatch struct {
	timer *time.Timer
	idle  time.Duration
	once  atomic.Bool
}

func newIdleWatch(cancel context.CancelFunc, idle time.Duration) *idleWatch {
	w := &idleWatch{idle: idle}
	w.timer = time.AfterFunc(idle, func() {
		w.once.Store(true)
		cancel()
	})
	return w
}

func (w *idleWatch) touch()      { w.timer.Reset(w.idle) }
func (w *idleWatch) stop()       { w.timer.Stop() }
func (w *idleWatch) fired() bool { return w.once.Load() }

// readWholeCompletion 处理「上游没按流式回」的情况：整段 JSON 解析后一次性回调。
func readWholeCompletion(body io.Reader, model string, onDelta StreamFunc) (parsedCompletion, string, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxResponseBytes+1))
	if err != nil {
		return parsedCompletion{}, "", fmt.Errorf("read response: %w", err)
	}
	if len(raw) > maxResponseBytes {
		return parsedCompletion{}, "", ErrTooLarge
	}
	parsed, err := parseChatCompletion(raw)
	if err != nil {
		return parsedCompletion{}, "", err
	}
	if !parsed.Truncated && parsed.Content != "" && onDelta != nil {
		if err := onDelta(parsed.Content); err != nil {
			return parsedCompletion{}, "", err
		}
	}
	return parsed, model, nil
}

// readSSE 逐行解析 SSE：只认 data: 行，忽略注释/心跳/坏行与 [DONE] 之后的尾巴。
func readSSE(ctx context.Context, body io.Reader, model string, onDelta StreamFunc) (parsedCompletion, string, error) {
	var (
		content    strings.Builder
		truncated  bool
		scanner    = bufio.NewScanner(body)
		readFailed error
	)
	// 单行上限给到响应总量上限：一个 chunk 里塞满整段正文也不该被截断。
	scanner.Buffer(make([]byte, 0, 64<<10), maxResponseBytes)

	for scanner.Scan() {
		payload, ok := ssePayload(scanner.Text())
		if !ok {
			continue
		}
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			// 单行坏掉不该毁掉整次生成：跳过就是了，真没内容最后会报 ErrEmptyResult。
			continue
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason == finishReasonLength {
				truncated = true
			}
			if choice.Delta.Content == "" {
				continue
			}
			content.WriteString(choice.Delta.Content)
			if onDelta != nil {
				if err := onDelta(choice.Delta.Content); err != nil {
					return parsedCompletion{}, "", err
				}
			}
		}
		// 每行都看一眼 ctx：写响应失败的场景由 onDelta 负责，这里兜住客户端断开。
		if err := ctx.Err(); err != nil {
			readFailed = err
			break
		}
	}
	if readFailed != nil {
		return parsedCompletion{}, "", readFailed
	}
	if err := scanner.Err(); err != nil {
		return parsedCompletion{}, "", wrapTransportError(err, model, DefaultTimeout)
	}
	return parsedCompletion{Content: content.String(), Truncated: truncated}, model, nil
}

// ssePayload 从一行 SSE 里取出 data 内容；注释行（以 : 开头）、空行与 event/id/retry
// 等字段都不算正文。
func ssePayload(line string) (string, bool) {
	line = strings.TrimRight(line, "\r")
	if line == "" || strings.HasPrefix(line, ":") {
		return "", false
	}
	payload, ok := strings.CutPrefix(line, "data:")
	if !ok {
		return "", false
	}
	return strings.TrimSpace(payload), true
}
