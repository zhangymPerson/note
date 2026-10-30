# AI 美化笔记 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让「新建笔记」时能用已配置的 OpenAI 兼容接口把正文的 Markdown 格式美化一遍——网页一个按钮、`note add` 一个快捷键，结果先预览、用户确认才替换正文。

**Architecture:** 模型是列表（`ai.models`，默认取第一个），提示词是模板（笔记在 `{{content}}` 处代入）；唯一的对外 HTTP 出口是新的 `internal/ai` 包，它由 Go 服务端的 `POST /api/ai/optimize` 与 CLI 的 `note add` 共同使用（密钥不出机器）。网页在编辑器正文下方展开「AI 优化结果」面板，CLI 在正文步之后进 `stepOptimized` 预览屏；两边都在用户点「替换 / Enter」之前不动原文。

**Tech Stack:** Go 1.26 标准库 `net/http`（客户端与服务端都用它）、bubbletea/bubbles（`textarea`、`viewport`）、Svelte 5 + TypeScript、Vitest、`internal/markdown`（glamour）。

**Spec:** `docs/superpowers/specs/2026-10-30-ai-note-optimize-design.md`

## Global Constraints

- 正文上限 2000 **码点**（`utf8.RuneCountInString` / `Array.from(text).length`），标题 200、分类名 30；SQL 与前端口径一致。
- `api_key` 明文只允许存在于 `note.yaml`（文件 `0600`、目录 `0700`）；任何 API 响应、网页、日志只有掩码。
- 密钥、正文、渲染后的提示词一律不进日志；日志只允许 `model` / `host` / `status` / 耗时 / 上游错误摘要。
- 配置写入只有 `config.SetValues` 一个入口（`yaml.Node` 级改写保留注释 → 编码 → 严格 `decode` 复核 → 临时文件 + rename）。
- 含正文的响应必须 `Cache-Control: no-store`（`writeJSON` 已统一设置）；状态变更与外部副作用类 API 必须过 CSRF 双提交，唯一豁免是 `/api/shutdown`。
- `{@html}` 只允许出现在 `web/src/components/Markdown.svelte`，输入先过 DOMPurify。
- 时间戳格式 `2006-01-02T15:04:05Z`（本功能不新增任何时间字段）。
- 数据库结构本功能**不改**，不新增迁移。
- 提示词模板变量只有一个：`{{content}}`（`config.PromptVariable`）。
- 全仓库只有 `internal/ai` 允许发起对外 HTTP 请求。
- 合入前 `just test` 与 `just check` 必须全绿；`CHANGELOG.md` 只在验证通过后写。

## Review Focus

- 上游把正文包在 ` ``` ` / `~~~` 围栏里返回 → 「替换」后编辑器/库里出现多余围栏（Task 3 `StripFence`）。
- 用户正文里本来就有 `{{content}}` 字样 → 二次展开或丢字（Task 3 `BuildPrompt`）。
- `base_url` 末尾带 `/` → 拼出 `//chat/completions`（Task 3 URL 拼接）。
- 结果是中文/emoji 恰好在 2000 码点边界 → 网页「替换」与 CLI `Enter` 必须按码点判超长，不许把 codepoint 截断当字符（Task 7、Task 8）。
- `note.yaml` 被手写成 `models: gpt-4o`（标量）或 `models: []` → 启动/列表必须给出指向该键的错误或「未配置」，不许 panic（Task 1）。
- 上游 500 / 超时 / 未配置 → 网页与 CLI 都必须原文不动且错误可见，不许静默失败（Task 6、Task 7、Task 8）。
- 请求在途时用户继续改正文 → 迟到的结果不许覆盖新输入（网页禁用「替换」并提示；CLI 锁编辑）。

---

### Task 1: 配置层——模型列表、提示词模板与默认提示词

**Files:**

- Modify: `internal/config/ai.go`
- Modify: `internal/config/config.go`（`fileConfig.AI` 增 `Models`，`Load` 拷进 `Config.AI`）
- Test: `internal/config/ai_test.go`、`internal/config/config_test.go`

**Interfaces:**

- Consumes: 现有 `config.SetValues` / `config.ReadAI` / `config.Normalize` / `config.ValueError`。
- Produces（后续任务全部依赖这些确切名字与类型）：

```go
type AI struct {
    BaseURL string
    APIKey  string
    Models  []string
    Prompt  string
}

const (
    MaxAIBaseURLLen  = 500
    MaxAIKeyLen      = 512
    MaxAIPromptRunes = 2000
    MaxAIModels      = 10
    MaxAIModelLen    = 200
    PromptVariable   = "{{content}}"
)

const DefaultAIPrompt = "…" // 见 Step 3，含 PromptVariable

func SplitModels(text string) []string   // 逗号/换行拆分、trim、去空、去重（保留首现顺序）
func JoinModels(models []string) string  // ", " 连接
func ValidateModel(model string) error   // 空串视为合法「未设置」
func ValidateModels(models []string) error
func PromptOrDefault(prompt string) string // 空 → DefaultAIPrompt
func PromptDisplay(prompt string) string   // 空 → "（默认提示词）"
func ModelsDisplay(models []string) string // 空 → "（未配置）"
```

- [ ] **Step 1: 写失败测试**

在 `internal/config/ai_test.go` 追加：

```go
func TestSplitModels(t *testing.T) {
    cases := []struct{ in string; want []string }{
        {"a,b", []string{"a", "b"}},
        {" a , b ,, ", []string{"a", "b"}},
        {"a\nb", []string{"a", "b"}},
        {"a,a,b", []string{"a", "b"}},
        {"", nil},
    }
    for _, c := range cases {
        got := SplitModels(c.in)
        if !reflect.DeepEqual(got, c.want) {
            t.Errorf("SplitModels(%q) = %#v, want %#v", c.in, got, c.want)
        }
    }
}

func TestValidateModels(t *testing.T) {
    if err := ValidateModels(nil); err != nil { t.Fatalf("空列表应当合法: %v", err) }
    if err := ValidateModels([]string{"gpt-4o-mini", "deepseek-chat"}); err != nil { t.Fatalf("正常列表: %v", err) }
    if err := ValidateModels([]string{"gpt 4o"}); err == nil { t.Fatal("带空白应当被拒") }
    if err := ValidateModels([]string{strings.Repeat("m", MaxAIModelLen+1)}); err == nil { t.Fatal("超长模型名应当被拒") }
    many := make([]string, MaxAIModels+1)
    for i := range many { many[i] = fmt.Sprintf("m%d", i) }
    if err := ValidateModels(many); err == nil { t.Fatal("超过 MaxAIModels 应当被拒") }
}

func TestValidatePromptRequiresPlaceholder(t *testing.T) {
    if err := ValidatePrompt(""); err != nil { t.Fatalf("空串（使用默认提示词）应当合法: %v", err) }
    if err := ValidatePrompt("整理成笔记\n" + PromptVariable); err != nil { t.Fatalf("带变量应当合法: %v", err) }
    err := ValidatePrompt("整理成笔记")
    if err == nil { t.Fatal("缺 {{content}} 应当被拒") }
    if !strings.Contains(err.Error(), PromptVariable) || !strings.Contains(err.Error(), "留空") {
        t.Fatalf("错误文案要点名变量并给出恢复默认的办法: %v", err)
    }
}

func TestDefaultAIPromptIsUsable(t *testing.T) {
    if !strings.Contains(DefaultAIPrompt, PromptVariable) { t.Fatal("默认提示词必须含变量") }
    if n := utf8.RuneCountInString(DefaultAIPrompt); n > MaxAIPromptRunes { t.Fatalf("默认提示词 %d 字超出上限", n) }
    if got := PromptOrDefault(""); got != DefaultAIPrompt { t.Fatal("空提示词应当回落到默认") }
    if got := PromptOrDefault("自定义" + PromptVariable); got != "自定义"+PromptVariable { t.Fatal("非空应当原样") }
    if PromptDisplay("") != "（默认提示词）" || ModelsDisplay(nil) != "（未配置）" { t.Fatal("空值展示文案不对") }
}

func TestSetModelsWritesSequenceAndKeepsComments(t *testing.T) {
    path := filepath.Join(t.TempDir(), "note.yaml")
    seed := "# 我的注释\nserver:\n  addr: 127.0.0.1:8080\nai:\n  base_url: https://api.example.com/v1\n"
    if err := os.WriteFile(path, []byte(seed), 0o600); err != nil { t.Fatal(err) }

    if err := SetValues(path, []KeyValue{
        {Key: "ai.models", Value: "gpt-4o-mini, deepseek-chat"},
        {Key: "ai.prompt", Value: "整理成笔记\n" + PromptVariable},
    }); err != nil { t.Fatalf("SetValues: %v", err) }

    ai, found, err := ReadAI(path)
    if err != nil || !found { t.Fatalf("ReadAI: %v found=%v", err, found) }
    if !reflect.DeepEqual(ai.Models, []string{"gpt-4o-mini", "deepseek-chat"}) {
        t.Fatalf("models 往返不一致: %#v", ai.Models)
    }
    data, _ := os.ReadFile(path)
    if !strings.Contains(string(data), "# 我的注释") { t.Fatal("注释必须保留") }
    if !strings.Contains(string(data), "models:\n    - gpt-4o-mini") {
        t.Fatalf("models 应当写成块序列:\n%s", data)
    }

    // 清空：写成 []，再读回是空列表
    if err := SetValue(path, "ai.models", ""); err != nil { t.Fatalf("清空 models: %v", err) }
    ai, _, _ = ReadAI(path)
    if len(ai.Models) != 0 { t.Fatalf("清空后应为空: %#v", ai.Models) }
}

func TestSetModelsRejectsBadValueWithoutTouchingFile(t *testing.T) {
    path := filepath.Join(t.TempDir(), "note.yaml")
    seed := "ai:\n  base_url: https://api.example.com/v1\n"
    if err := os.WriteFile(path, []byte(seed), 0o600); err != nil { t.Fatal(err) }
    err := SetValue(path, "ai.models", "a, b c")
    var invalid ValueError
    if !errors.As(err, &invalid) { t.Fatalf("应当是 ValueError，得到 %v", err) }
    data, _ := os.ReadFile(path)
    if string(data) != seed { t.Fatal("校验失败不得改动文件") }
}
```

在同一文件（或 `config_test.go`，沿用既有位置）追加启动校验用例：

```go
func TestLoadRejectsPromptWithoutPlaceholder(t *testing.T) {
    path := filepath.Join(t.TempDir(), "note.yaml")
    data := "ai:\n  prompt: 整理成笔记\n"
    if err := os.WriteFile(path, []byte(data), 0o600); err != nil { t.Fatal(err) }
    if _, _, err := Load([]string{"--config", path}); err == nil {
        t.Fatal("启动时必须拒绝缺 {{content}} 的提示词")
    }
}

func TestLoadAcceptsModelsList(t *testing.T) {
    path := filepath.Join(t.TempDir(), "note.yaml")
    data := "ai:\n  base_url: https://api.example.com/v1\n  models:\n    - a\n    - b\n"
    if err := os.WriteFile(path, []byte(data), 0o600); err != nil { t.Fatal(err) }
    cfg, _, err := Load([]string{"--config", path})
    if err != nil { t.Fatalf("Load: %v", err) }
    if !reflect.DeepEqual(cfg.AI.Models, []string{"a", "b"}) { t.Fatalf("models 没读进来: %#v", cfg.AI.Models) }
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/config/ -run 'TestSplitModels|TestValidateModels|TestValidatePromptRequiresPlaceholder|TestDefaultAIPromptIsUsable|TestSetModels|TestLoadRejectsPromptWithoutPlaceholder|TestLoadAcceptsModelsList' -v`
Expected: 编译失败（`undefined: SplitModels` 等）。

- [ ] **Step 3: 实现**

`internal/config/ai.go`：

1. `AI` 结构体加 `Models []string`；常量块加 `MaxAIModels = 10`、`MaxAIModelLen = 200`、`PromptVariable = "{{content}}"`。
2. 默认提示词常量（逐字照抄，raw string）：

```go
// DefaultAIPrompt 是「用 AI 美化笔记」的内置提示词模板。
// 用户的笔记在 PromptVariable 处代入；note.yaml 里 ai.prompt 留空时用它。
const DefaultAIPrompt = `你是 Markdown 笔记专家。请美化下面这段笔记的 Markdown 格式，让它更好读、更有条理。

【笔记原文】
{{content}}
【笔记原文结束】

要求：
1. 不改变核心内容：不删减要点、不编造事实、不改动原意与结论。
2. 可以调整：标题层级、段落划分、列表与表格、代码块语言标注、加粗与斜体、标点与全半角、以及描述性的措辞。
3. 目标是「扫一眼就能读懂」：每个小节有说清主题的标题，并列的信息改成列表，结论与关键事实放在前面。
4. 只输出优化后的 Markdown 正文：不要解释、不要前言或结语，也不要用代码块把整篇包起来。`
```

3. `aiKeys` 追加 `"ai.models"`（放在 `ai.base_url` 之后、`ai.prompt` 之前）。
4. 新增纯函数 `SplitModels` / `JoinModels` / `ValidateModel` / `ValidateModels` / `PromptOrDefault` / `PromptDisplay` / `ModelsDisplay`。`SplitModels` 用 `strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' })` 再 trim、跳过空串、按 `map[string]bool` 去重并保序。
5. `ValidatePrompt` 在长度检查后加：`if prompt != "" && !strings.Contains(prompt, PromptVariable) { return fmt.Errorf("ai.prompt 里必须包含 %s（用户的笔记在这里代入）；要恢复默认提示词，把值留空", PromptVariable) }`。
6. `validateValue` 的 `"ai.models"` 分支：`return ValidateModels(SplitModels(value))`；`normalizeValue` 对 `ai.models` 走 `strings.TrimSpace`（沿用默认分支即可，不需特判，因为它不换行敏感）。
7. `ReadAI` 返回 `Models: fileCfg.AI.Models`；`ValidateAI` 加 `ValidateModels`。
8. 写入：把 `setNode` 的「沿途建映射」部分抽成 `ensureMapping(doc *yaml.Node, key string) (*yaml.Node, string, error)`（返回父映射节点与叶子键名）；`setNode` 调用它写标量；新增 `setModelsNode(doc, models []string) error` 用同一个 `ensureMapping` 写 `SequenceNode`（`Tag: "!!seq"`，每项 `stringNode`；空列表写 `Content: nil` 的空序列，YAML 输出即 `models: []`）。`SetValues` 里按键分派：`ai.models` → `setModelsNode(doc, SplitModels(item.Value))`，其余 → `setNode`。
9. `config.go`：`fileConfig.AI` 加 `Models []string \`yaml:"models"\``；`Load`里`cfg.AI = AI{BaseURL: …, APIKey: …, Models: fileCfg.AI.Models, Prompt: …}`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/config/ -v`
Expected: PASS（含既有用例）。

- [ ] **Step 5: 提交**

```bash
git add internal/config/ai.go internal/config/config.go internal/config/ai_test.go
git commit -m "feat(config): AI 模型列表与提示词模板（{{content}} 变量、内置默认提示词）"
```

---

### Task 2: `note config` 展示四项与模板帮助

**Files:**

- Modify: `cmd/note/config.go`（`cliAIItems`）
- Modify: `internal/cli/config.go`（`FormatConfigList` 末行帮助、`configUsage`）
- Test: `internal/cli/config_test.go`、`internal/cli/usage_test.go`

**Interfaces:**

- Consumes: Task 1 的 `config.ModelsDisplay` / `config.PromptDisplay` / `ai.Models`；既有 `cli.ConfigItem`。
- Produces: `note config list` 的第四行 `ai.models`，无新导出函数。

- [ ] **Step 1: 写失败测试**

`internal/cli/config_test.go` 追加：

```go
func TestFormatConfigListShowsModelsAndDefaultPromptPlaceholder(t *testing.T) {
    out := FormatConfigList("/tmp/note.yaml", true, []ConfigItem{
        {Key: "ai.base_url", Value: "https://api.example.com/v1"},
        {Key: "ai.models", Value: "（未配置）"},
        {Key: "ai.prompt", Value: "（默认提示词）"},
    })
    for _, want := range []string{"ai.models", "（未配置）", "（默认提示词）", "{{content}}"} {
        if !strings.Contains(out, want) {
            t.Fatalf("列表输出缺少 %q:\n%s", want, out)
        }
    }
}
```

`internal/cli/usage_test.go` 追加一条断言（沿用既有 `configUsage` 用例的写法）：

```go
if !strings.Contains(configUsage(), "ai.models") || !strings.Contains(configUsage(), "{{content}}") {
    t.Fatal("note config 帮助要提到 ai.models 与提示词变量")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run 'TestFormatConfigListShowsModelsAndDefaultPromptPlaceholder|TestUsage' -v`
Expected: FAIL（缺 `{{content}}` 文案）。

- [ ] **Step 3: 实现**

- `internal/cli/config.go`：`FormatConfigList` 末尾两行改成

```go
b.WriteString("\n用 note config set <键>=<值> 写入；值留空表示清空该项。\n")
b.WriteString("ai.prompt 里用 {{content}} 代表笔记正文；留空表示使用内置默认提示词。\n")
b.WriteString("这四项供 OpenAI 兼容接口使用，密钥在命令行与网页里都只显示掩码。\n")
```

`configUsage()` 在 `ai.prompt` 那一行补 `（用 {{content}} 代表笔记正文，留空=默认提示词）`，并在 `ai.api_key` 后加一行 `ai.models` 的说明（`逗号分隔的模型列表，第一个为默认`）。

- `cmd/note/config.go` 的 `cliAIItems` 改为四项，顺序 `base_url` / `api_key` / `models` / `prompt`：

```go
{Key: "ai.models", Value: config.ModelsDisplay(ai.Models)},
{Key: "ai.prompt", Value: config.PromptDisplay(ai.Prompt)},
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ -run 'TestFormatConfigList|TestFormatConfigSet|TestUsage' -v`，再 `go test ./cmd/note/ -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add cmd/note/config.go internal/cli/config.go internal/cli/config_test.go internal/cli/usage_test.go
git commit -m "feat(cli): note config 展示模型列表与默认提示词"
```

---

### Task 3: `internal/ai`——唯一的外部网络出口

**Files:**

- Create: `internal/ai/client.go`
- Create: `internal/ai/client_test.go`

**Interfaces:**

- Consumes: Task 1 的 `config.AI`（`Models` / `Prompt`）、`config.DefaultAIPrompt`、`config.PromptVariable`。
- Produces:

```go
package ai

type Client struct {
    BaseURL string
    APIKey  string
    Models  []string
    Prompt  string
    HTTP    *http.Client // nil → 60s 超时
}

type Result struct{ Content, Model string }

func NewClient(cfg config.AI) *Client
func (c *Client) Configured() bool      // BaseURL/APIKey/Models 都齐
func (c *Client) DefaultModel() string  // Models[0]，空列表返回 ""
func (c *Client) Host() string          // url.Parse(BaseURL).Host；解析失败返回 BaseURL
func (c *Client) Missing() []string     // 缺的键名：["ai.base_url","ai.api_key","ai.models"]
func (c *Client) Optimize(ctx context.Context, content, model string) (Result, error)

func BuildPrompt(tpl, content string) string
func StripFence(text string) string

var ErrNotConfigured = errors.New("ai is not configured")
var ErrBadResponse = errors.New("upstream returned unexpected json")
var ErrTooLarge = errors.New("upstream response too large")
var ErrEmptyResult = errors.New("upstream returned empty content")

type ModelError struct{ Model string }      // 请求的模型不在 Models 里
type UpstreamError struct{ Status int; Message string }
```

- [ ] **Step 1: 写失败测试**

`internal/ai/client_test.go`（`httptest`，全部用注入的 `c.HTTP`，不碰真网络）：

````go
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
    t.Helper()
    srv := httptest.NewServer(handler)
    t.Cleanup(srv.Close)
    return &Client{BaseURL: srv.URL + "/v1", APIKey: "sk-test", Models: []string{"m1", "m2"}, HTTP: srv.Client()}, srv
}

func TestOptimizeSendsRenderedPrompt(t *testing.T) {
    var gotPath, gotAuth, gotModel, gotMessage string
    c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
        gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
        var body struct{ Model string; Messages []struct{ Role, Content string } }
        json.NewDecoder(r.Body).Decode(&body)
        gotModel, gotMessage = body.Model, body.Messages[0].Content
        io.WriteString(w, `{"choices":[{"message":{"content":"```markdown\n# 标题\n```"}}]}`)
    })
    c.Prompt = "把这段整理好：\n" + config.PromptVariable

    res, err := c.Optimize(context.Background(), "原始正文", "")
    if err != nil { t.Fatalf("Optimize: %v", err) }
    if gotPath != "/v1/chat/completions" { t.Fatalf("path = %q", gotPath) }
    if gotAuth != "Bearer sk-test" { t.Fatalf("auth = %q", gotAuth) }
    if gotModel != "m1" { t.Fatalf("默认应当用第一个模型，得到 %q", gotModel) }
    if gotMessage != "把这段整理好：\n原始正文" { t.Fatalf("提示词未渲染: %q", gotMessage) }
    if res.Content != "# 标题" { t.Fatalf("围栏应当剥掉: %q", res.Content) }
    if res.Model != "m1" { t.Fatalf("res.Model = %q", res.Model) }
}

func TestOptimizeRejectsUnknownModel(t *testing.T) {
    c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
        t.Error("模型不在列表时不该发请求")
    })
    _, err := c.Optimize(context.Background(), "x", "nope")
    var bad ModelError
    if !errors.As(err, &bad) || bad.Model != "nope" { t.Fatalf("err = %v", err) }
}

func TestOptimizeUpstreamErrorKeepsStatusAndTruncates(t *testing.T) {
    long := strings.Repeat("坏", 500)
    c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusUnauthorized)
        fmt.Fprintf(w, `{"error":{"message":%q}}`, long)
    })
    _, err := c.Optimize(context.Background(), "x", "")
    var up UpstreamError
    if !errors.As(err, &up) { t.Fatalf("err = %v", err) }
    if up.Status != http.StatusUnauthorized { t.Fatalf("status = %d", up.Status) }
    if n := utf8.RuneCountInString(up.Message); n > 200 { t.Fatalf("摘要 %d 字，应当截断到 200", n) }
}

func TestOptimizeFailsOnBadJSONEmptyChoicesAndOversize(t *testing.T) {
    cases := []struct{ name string; body string }{
        {"坏 JSON", `not json`},
        {"没有 choices", `{"choices":[]}`},
        {"正文为空", `{"choices":[{"message":{"content":"   \n"}}]}`},
        {"响应过大", `{"choices":[{"message":{"content":"` + strings.Repeat("x", 1<<20) + `"}}]}`},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, tc.body) })
            if _, err := c.Optimize(context.Background(), "x", ""); err == nil { t.Fatal("应当报错") }
        })
    }
}

func TestOptimizeRespectsContextAndTrailingSlash(t *testing.T) {
    c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/v1/chat/completions" { t.Errorf("path = %q", r.URL.Path) }
        time.Sleep(200 * time.Millisecond) // 让 ctx 先到期
        io.WriteString(w, `{"choices":[{"message":{"content":"x"}}]}`)
    })
    c.BaseURL += "/" // 末尾斜杠不许拼出 //
    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
    defer cancel()
    if _, err := c.Optimize(ctx, "x", ""); !errors.Is(err, context.DeadlineExceeded) {
        t.Fatalf("应当返回 ctx 错误，得到 %v", err)
    }
}

func TestBuildPrompt(t *testing.T) {
    if BuildPrompt("", "笔记") != strings.Replace(config.DefaultAIPrompt, config.PromptVariable, "笔记", 1) {
        t.Fatal("空模板应当使用默认提示词")
    }
    if got := BuildPrompt("前缀"+config.PromptVariable, "含 {{content}} 的正文"); got != "前缀含 {{content}} 的正文" {
        t.Fatalf("正文里的变量不许二次展开: %q", got)
    }
    if strings.Count(BuildPrompt("x"+config.PromptVariable, "{{content}} {{content}}"), "{{content}}") != 2 {
        t.Fatal("正文原样保留")
    }
}

func TestStripFence(t *testing.T) {
    cases := []struct{ in, want string }{
        {"```markdown\n# 标题\n```", "# 标题"},
        {"~~~\n# 标题\n~~~", "# 标题"},
        {"```\n# 标题\n```\n", "# 标题"},
        {"# 标题", "# 标题"},
        {"```\n一\n```\n```\n二\n```", "```\n一\n```\n```\n二\n```"}, // 多段围栏不剥
    }
    for _, c := range cases {
        if got := StripFence(c.in); got != c.want { t.Errorf("StripFence(%q) = %q, want %q", c.in, got, c.want) }
    }
}

func TestConfiguredAndMissing(t *testing.T) {
    c := NewClient(config.AI{})
    if c.Configured() { t.Fatal("空配置不该算已配置") }
    if got := strings.Join(c.Missing(), ","); got != "ai.base_url,ai.api_key,ai.models" { t.Fatalf("missing = %s", got) }
    full := NewClient(config.AI{BaseURL: "https://api.example.com/v1", APIKey: "k", Models: []string{"m"}})
    if !full.Configured() || full.DefaultModel() != "m" { t.Fatal("完整配置") }
    if full.Host() != "api.example.com" { t.Fatalf("host = %q", full.Host()) }
}
````

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/ai/ -v`
Expected: 编译失败（包不存在）。

- [ ] **Step 3: 实现 `internal/ai/client.go`**

请求侧：`url := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"`；body 用匿名结构体 `{Model string `json:"model"`; Messages []struct{Role, Content string} `json:"messages"`; Temperature float64 `json:"temperature"`; Stream bool `json:"stream"``}`，`temperature: 0.2`、`stream: false`、`messages: [{role:"user", content: BuildPrompt(c.Prompt, content)}]`；头 `Authorization: Bearer`、`Content-Type`、`Accept: application/json`。

响应侧：`http.Client` 为 nil 时用 `&http.Client{Timeout: 60 * time.Second}`；`io.LimitReader(resp.Body, 1<<20)` 读满则 `ErrTooLarge`；非 2xx → 尝试解析 `{"error":{"message":...}}`，拿不到就用 `http.StatusText`，按 200 码点截断，包成 `UpstreamError`。

后处理：`StripFence` 只在「去空白后整段以 ``` 或 ~~~ 开头，且同一围栏符恰好在结尾闭合一次」时剥掉首行与末行；`strings.TrimSpace` 收尾。`Optimize` 返回前若 `StripFence` 结果为空 → `ErrEmptyResult`。`ModelError`/`UpstreamError` 实现 `Error()`，`UpstreamError.Error()` 形如 `AI 接口返回 401：<message>`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/ai/ -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/ai/client.go internal/ai/client_test.go
git commit -m "feat(ai): 新增 OpenAI 兼容的笔记优化客户端（模板渲染、围栏剥离、限流与超时）"
```

---

### Task 4: `GET/PUT /api/ai-config` 支持模型列表与默认提示词

**Files:**

- Modify: `internal/web/handlers_ai.go`
- Modify: `internal/web/errors.go`（`writeValidationError` 的字段优先顺序表加 `"models"`）
- Test: `internal/web/handlers_ai_test.go`

**Interfaces:**

- Consumes: Task 1 的 `config.SplitModels` / `ValidateModels` / `DefaultAIPrompt` / `PromptVariable`。
- Produces: 响应结构新增三个字段，`PUT` 请求体新增 `models`（逗号分隔字符串），前端 Task 5/6 依赖：

```go
type aiConfigResponse struct {
    BaseURL      string   `json:"base_url"`
    Models       []string `json:"models"`
    Prompt       string   `json:"prompt"`
    DefaultPrompt string  `json:"default_prompt"`
    PromptVariable string `json:"prompt_variable"`
    APIKeySet    bool     `json:"api_key_set"`
    APIKeyMasked string   `json:"api_key_masked"`
    Path         string   `json:"path"`
}
// 请求：{ base_url, models string, prompt, api_key *string }
```

- [ ] **Step 1: 写失败测试**

`internal/web/handlers_ai_test.go` 的 `aiConfigResp` 加 `Models []string`、`DefaultPrompt string`、`PromptVariable string`，并追加：

```go
func TestAIConfigAPIModelsAndDefaultPrompt(t *testing.T) {
    handler, cfg := newTestHandler(t)
    token := csrfTokenFor(t, handler)

    rec := apiRequest(t, handler, http.MethodPut, "/api/ai-config", token,
        `{"base_url":"https://api.example.com/v1","models":"gpt-4o-mini, deepseek-chat","prompt":"整理：{{content}}"}`)
    if rec.Code != http.StatusOK { t.Fatalf("PUT = %d: %s", rec.Code, rec.Body) }
    body := decodeAIConfig(t, rec.Body.Bytes())
    if !reflect.DeepEqual(body.Models, []string{"gpt-4o-mini", "deepseek-chat"}) { t.Fatalf("models = %#v", body.Models) }
    if body.DefaultPrompt != config.DefaultAIPrompt || body.PromptVariable != config.PromptVariable {
        t.Fatal("响应要带上默认提示词与变量名，供前端展示")
    }
    ai, _, _ := config.ReadAI(cfg.ConfigPath)
    if len(ai.Models) != 2 { t.Fatalf("没落盘: %#v", ai.Models) }
}

func TestAIConfigAPIPromptPlaceholderField(t *testing.T) {
    handler, _ := newTestHandler(t)
    token := csrfTokenFor(t, handler)
    rec := apiRequest(t, handler, http.MethodPut, "/api/ai-config", token,
        `{"base_url":"https://api.example.com/v1","models":"m","prompt":"没有变量"}`)
    if rec.Code != http.StatusBadRequest { t.Fatalf("GET = %d, want 400: %s", rec.Code, rec.Body) }
    if !strings.Contains(rec.Body.String(), `"prompt"`) { t.Fatalf("应当是字段级错误: %s", rec.Body) }
}

func TestAIConfigAPIModelsValidation(t *testing.T) {
    handler, _ := newTestHandler(t)
    token := csrfTokenFor(t, handler)
    rec := apiRequest(t, handler, http.MethodPut, "/api/ai-config", token,
        `{"base_url":"https://api.example.com/v1","models":"a, b c","prompt":""}`)
    if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"models"`) {
        t.Fatalf("非法模型名应当字段级 400: %d %s", rec.Code, rec.Body)
    }
}
```

（第一个用例里那段「故意 400」的写法请改成先断言 400 再发第二次 PUT，两段各自显式断言。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/web/ -run 'TestAIConfigAPI' -v`
Expected: FAIL（字段不存在 / 400 未生效）。

- [ ] **Step 3: 实现**

- `aiConfigResponse` / `aiConfigRequest` 按上面的接口块加字段（请求侧 `Models string`）。
- `handlePutAIConfig`：`modelsText := config.Normalize("ai.models", req.Models)`；`models := config.SplitModels(modelsText)`；校验 `ValidateModels(models)` 并把错误塞进 `fields["models"]`；`values` 追加 `{Key: "ai.models", Value: modelsText}`。
- `newAIConfigResponse` 填 `Models: ai.Models`（空时给 `[]string{}`，JSON 输出 `[]` 而不是 `null`）、`DefaultPrompt: config.DefaultAIPrompt`、`PromptVariable: config.PromptVariable`。
- `internal/web/errors.go` 的字段优先顺序 slice 在 `"prompt"` 后加 `"models"`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/web/ -v`
Expected: PASS（既有 AI 用例一起绿）。

- [ ] **Step 5: 提交**

```bash
git add internal/web/handlers_ai.go internal/web/errors.go internal/web/handlers_ai_test.go
git commit -m "feat(web): AI 配置接口支持模型列表与默认提示词（密钥仍只回掩码）"
```

---

### Task 5: 信息页 AI 卡片——模型列表输入 + 恢复默认提示词

**Files:**

- Modify: `web/src/lib/ai-config.ts`
- Modify: `web/src/views/InfoView.svelte`
- Test: `web/src/lib/ai-config.test.ts`、`web/src/views/InfoView.test.ts`

**Interfaces:**

- Consumes: Task 4 的响应字段 `models` / `default_prompt` / `prompt_variable`。
- Produces: `AIConfig`（新增 `models: string[]`、`default_prompt: string`、`prompt_variable: string`）、`AIConfigPayload`（新增 `models: string`）；给 Task 6/7 的编辑器提供 `getAIConfig()` 的返回形状。

- [ ] **Step 1: 写失败测试**

`web/src/lib/ai-config.test.ts`：先给 `CONFIGURED` 补三个字段（`models: ["gpt-4o-mini", "deepseek-chat"]`、`default_prompt: DEFAULT`、`prompt_variable: "{{content}}"`，`DEFAULT` 是含 `{{content}}` 的常量），再追加：

```ts
it("保存时把模型列表作为逗号串提交", async () => {
  mockEndpoints({ put: () => json(CONFIGURED) });
  await saveAIConfig({
    base_url: "https://api.example.com/v1",
    models: "a, b",
    prompt: "",
  });
  expect(requests[0].body).toEqual({
    base_url: "https://api.example.com/v1",
    models: "a, b",
    prompt: "",
  });
});

it("默认提示词与变量名来自服务端", async () => {
  mockEndpoints({ get: () => json(CONFIGURED) });
  const cfg = await getAIConfig();
  expect(cfg.default_prompt).toBe(DEFAULT);
  expect(cfg.prompt_variable).toBe("{{content}}");
  expect(cfg.models).toEqual(["gpt-4o-mini", "deepseek-chat"]);
});
```

`web/src/views/InfoView.test.ts`：先把 `beforeEach` 里的 `CONFIGURED` 补上三个新字段，并**把既有 `toHaveBeenCalledWith({base_url, prompt})` 改成带上 `models`**（严格相等断言会因此失败）；再追加：

```ts
it("模型列表输入框回填列保存", async () => {
  const element = await render();
  expect((element.querySelector("#ai-models") as HTMLInputElement).value).toBe(
    "gpt-4o-mini, deepseek-chat",
  );

  typeInto(element.querySelector("#ai-models") as HTMLInputElement, "m1, m2");
  submit(element.querySelector("#ai-models")!);
  await vi.waitFor(() => expect(mocks.saveAIConfig).toHaveBeenCalled());
  expect(mocks.saveAIConfig).toHaveBeenCalledWith(
    expect.objectContaining({ models: "m1, m2" }),
  );
});

it("「恢复默认提示词」把文本框填成默认模板且不落盘", async () => {
  const element = await render();
  const restore = Array.from(element.querySelectorAll("button")).find((b) =>
    b.textContent?.includes("恢复默认提示词"),
  );
  expect(restore).toBeDefined();
  restore!.dispatchEvent(new Event("click", { bubbles: true }));
  flushSync();

  expect(
    (element.querySelector("#ai-prompt") as HTMLTextAreaElement).value,
  ).toBe(CONFIGURED.default_prompt);
  expect(mocks.saveAIConfig).not.toHaveBeenCalled();
});

it("提示词缺 {{content}} 时标红服务端错误", async () => {
  const { ApiRequestError } = await import("../lib/api");
  mocks.saveAIConfig.mockRejectedValue(
    new ApiRequestError("validation_failed", "提示词不合规", {
      prompt: "ai.prompt 里必须包含 {{content}}",
    }),
  );
  const element = await render();
  submit(element.querySelector("#ai-prompt")!);
  await vi.waitFor(() =>
    expect(element.textContent).toContain("必须包含 {{content}}"),
  );
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `npm --prefix web test -- ai-config InfoView`
Expected: FAIL（类型/字段不存在、断言不成立）。

- [ ] **Step 3: 实现**

- `ai-config.ts`：两个接口按上面加字段；注释改成「AI 四项」（`base_url` / `api_key` / `models` / `prompt`）。
- `InfoView.svelte`：新增 `let aiModels = $state('')`、`let aiDefaultPrompt = $state('')`、`let aiPromptVariable = $state('{{content}}')`；加载时 `aiModels = config.models.join(', ')`、`aiDefaultPrompt = config.default_prompt`、`aiPromptVariable = config.prompt_variable`；保存 payload 加 `models: aiModels`。
- 卡片里在「API 密钥」与「提示词」之间插入「模型」输入框（`id="ai-models"`，placeholder `gpt-4o-mini, deepseek-chat`），提示「逗号分隔，第一个为默认模型。」+ `aiFields.models` 标红。
- 提示词 textarea 下：提示改为「用 `{aiPromptVariable}` 代表笔记正文（必填）；留空表示使用内置默认提示词。当前 {aiPromptRunes} 字。」，并加按钮 `恢复默认提示词`（`type="button"`，`onclick={() => (aiPrompt = aiDefaultPrompt)}`，禁用条件 `aiBusy`）。

- [ ] **Step 4: 跑测试确认通过**

Run: `npm --prefix web test -- ai-config InfoView`，再 `npm --prefix web run check`
Expected: PASS，0 错误。

- [ ] **Step 5: 提交**

```bash
git add web/src/lib/ai-config.ts web/src/lib/ai-config.test.ts web/src/views/InfoView.svelte web/src/views/InfoView.test.ts
git commit -m "feat(web): 信息页 AI 卡片支持模型列表与恢复默认提示词"
```

---

### Task 6: `POST /api/ai/optimize`

**Files:**

- Create: `internal/web/handlers_optimize.go`
- Modify: `internal/web/server.go`（注册路由）
- Test: `internal/web/optimize_api_test.go`

**Interfaces:**

- Consumes: Task 1 的 `config.ReadAI`、Task 3 的 `ai.NewClient` / `Client.Configured` / `Missing` / `DefaultModel` / `Host` / `Optimize` / `ModelError` / `UpstreamError`。
- Produces: 端点契约，前端 Task 7 依赖：

```text
POST /api/ai/optimize   CSRF 必需
req  {"content":"…","model":"可选"}
200  {"content":"优化后的 markdown","model":"实际使用的模型"}
400  validation_failed（fields: content / model）
409  ai_not_configured
502  ai_upstream_failed | ai_bad_response
504  ai_timeout
```

- [ ] **Step 1: 写失败测试**

`internal/web/optimize_api_test.go`：

````go
func fakeUpstream(t *testing.T, content string) *httptest.Server {
    t.Helper()
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/v1/chat/completions" { t.Errorf("path = %q", r.URL.Path) }
        io.WriteString(w, `{"choices":[{"message":{"content":`+strconv.Quote(content)+`}}]}`)
    }))
    t.Cleanup(srv.Close)
    return srv
}

func configureAI(t *testing.T, cfg config.Config, baseURL, models string) {
    t.Helper()
    data := "ai:\n  base_url: " + baseURL + "\n  api_key: sk-test\n"
    if models != "" { data += "  models:\n" + models }
    if err := os.MkdirAll(filepath.Dir(cfg.ConfigPath), 0o700); err != nil { t.Fatal(err) }
    if err := os.WriteFile(cfg.ConfigPath, []byte(data), 0o600); err != nil { t.Fatal(err) }
}

func TestOptimizeAPIReturnsContentAndModel(t *testing.T) {
    handler, cfg := newTestHandler(t)
    upstream := fakeUpstream(t, "```markdown\n# 标题\n```")
    configureAI(t, cfg, upstream.URL+"/v1", "    - m1\n    - m2\n")
    token := csrfTokenFor(t, handler)

    rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, `{"content":"原始正文"}`)
    if rec.Code != http.StatusOK { t.Fatalf("POST = %d: %s", rec.Code, rec.Body) }
    if rec.Header().Get("Cache-Control") != "no-store" { t.Fatal("含正文的响应必须 no-store") }
    var body struct{ Content, Model string }
    json.Unmarshal(rec.Body.Bytes(), &body)
    if body.Content != "# 标题" || body.Model != "m1" { t.Fatalf("body = %+v", body) }
    if strings.Contains(rec.Body.String(), "sk-test") { t.Fatal("响应里不许出现密钥") }
}

func TestOptimizeAPIModelOverride(t *testing.T) {
    handler, cfg := newTestHandler(t)
    var got string
    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var b struct{ Model string }
        json.NewDecoder(r.Body).Decode(&b)
        got = b.Model
        io.WriteString(w, `{"choices":[{"message":{"content":"x"}}]}`)
    }))
    t.Cleanup(upstream.Close)
    configureAI(t, cfg, upstream.URL+"/v1", "    - m1\n    - m2\n")
    token := csrfTokenFor(t, handler)

    rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, `{"content":"c","model":"m2"}`)
    if rec.Code != http.StatusOK || got != "m2" { t.Fatalf("code=%d model=%q", rec.Code, got) }

    rec = apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, `{"content":"c","model":"nope"}`)
    if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"model"`) {
        t.Fatalf("未知模型应当 400 字段级: %d %s", rec.Code, rec.Body)
    }
}

func TestOptimizeAPINotConfigured(t *testing.T) {
    handler, _ := newTestHandler(t)
    token := csrfTokenFor(t, handler)
    rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, `{"content":"c"}`)
    if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "ai_not_configured") {
        t.Fatalf("未配置应当 409: %d %s", rec.Code, rec.Body)
    }
    if !strings.Contains(rec.Body.String(), "ai.models") { t.Fatal("文案要点名缺哪几项") }
}

func TestOptimizeAPIValidation(t *testing.T) {
    handler, cfg := newTestHandler(t)
    configureAI(t, cfg, "http://127.0.0.1:1/v1", "    - m1\n")
    token := csrfTokenFor(t, handler)
    for name, payload := range map[string]string{
        "空正文": `{"content":"   "}`,
        "超长正文": `{"content":"` + strings.Repeat("字", 2001) + `"}`,
    } {
        t.Run(name, func(t *testing.T) {
            rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, payload)
            if rec.Code != http.StatusBadRequest { t.Fatalf("code = %d: %s", rec.Code, rec.Body) }
        })
    }
}

func TestOptimizeAPIUpstreamFailure(t *testing.T) {
    handler, cfg := newTestHandler(t)
    upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusInternalServerError)
        io.WriteString(w, `{"error":{"message":"boom"}}`)
    }))
    t.Cleanup(upstream.Close)
    configureAI(t, cfg, upstream.URL+"/v1", "    - m1\n")
    token := csrfTokenFor(t, handler)

    rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", token, `{"content":"c"}`)
    if rec.Code != http.StatusBadGateway { t.Fatalf("code = %d: %s", rec.Code, rec.Body) }
    if strings.Contains(rec.Body.String(), "sk-test") { t.Fatal("上游错误里不许带密钥") }
}

func TestOptimizeAPIRequiresCSRF(t *testing.T) {
    handler, _ := newTestHandler(t)
    rec := apiRequest(t, handler, http.MethodPost, "/api/ai/optimize", "", `{"content":"c"}`)
    if rec.Code != http.StatusForbidden { t.Fatalf("无 CSRF 应当 403，得到 %d", rec.Code) }
}
````

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/web/ -run 'TestOptimizeAPI' -v`
Expected: FAIL（404/405，路由不存在）。

- [ ] **Step 3: 实现**

`internal/web/handlers_optimize.go`：

```go
const optimizeMaxBytes = 64 << 10
const optimizeTimeout = 60 * time.Second

type optimizeRequest struct {
    Content string `json:"content"`
    Model   string `json:"model"`
}
type optimizeResponse struct {
    Content string `json:"content"`
    Model   string `json:"model"`
}
```

`handleOptimizeNote` 顺序：

1. `decodeJSON(w, r, &req, optimizeMaxBytes)`。
2. 校验：`strings.TrimSpace(req.Content) == ""` → `fields["content"] = "正文不能为空"`；`utf8.RuneCountInString(req.Content) > maxContentRunes` → `fields["content"] = fmt.Sprintf("正文请控制在 %d 字以内，当前 %d 字", maxContentRunes, n)`；有错 → `writeValidationError`。
3. 现读配置：`aiCfg, found, err := config.ReadAI(app.cfg.ConfigPath)`（err → 500 `internal`）；`client := ai.NewClient(aiCfg)`；`!client.Configured()` → 409 `ai_not_configured`，消息 `"AI 还没配置好：缺 " + strings.Join(client.Missing(), "、") + "。用 note config set <键>=<值> 补上，或在信息页的 AI 配置卡片里填。"`。
4. 模型：`model := req.Model`；为空就用 `client.DefaultModel()`；显式给了但不在 `client.Models` → `fields["model"] = fmt.Sprintf("模型 %q 不在 ai.models 里", model)` → `writeValidationError`。
5. 调用：`ctx, cancel := context.WithTimeout(r.Context(), optimizeTimeout)`；`res, err := client.Optimize(ctx, req.Content, model)`。
6. 错误映射（`slog.Error("optimize note", "host", client.Host(), "model", model, "status", …, "error", err)`，**不记正文**）：
   - `errors.Is(err, context.DeadlineExceeded)` → 504 `ai_timeout`「AI 接口超时（60 秒），可以重试或换一个模型」；
   - `ai.ModelError` → 400 字段级 `model`；
   - `ai.UpstreamError` → 502 `ai_upstream_failed`，消息 `fmt.Sprintf("AI 接口返回 %d：%s", up.Status, up.Message)`；
   - `ai.ErrBadResponse` / `ai.ErrEmptyResult` / `ai.ErrTooLarge` → 502 `ai_bad_response`「AI 接口返回了无法识别的内容」；
   - 其它（含 `ErrNotConfigured` 的兜底） → 502 `ai_upstream_failed`。
7. 成功：`writeJSON(w, http.StatusOK, optimizeResponse{Content: res.Content, Model: res.Model})`。

`server.go` 在 `PUT /api/ai-config` 之后加：

```go
mux.HandleFunc("POST /api/ai/optimize", app.handleOptimizeNote)
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/web/ -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/web/handlers_optimize.go internal/web/server.go internal/web/optimize_api_test.go
git commit -m "feat(web): 新增 POST /api/ai/optimize（服务端代发，密钥不出机器）"
```

---

### Task 7: 网页编辑器「用 AI 优化格式」

**Files:**

- Modify: `web/src/lib/api.ts`（导出 `postJSON`）
- Create: `web/src/lib/ai.ts`、`web/src/lib/ai.test.ts`
- Modify: `web/src/views/NoteEditorView.svelte`
- Create: `web/src/views/NoteEditorView.test.ts`（该文件目前**不存在**，照 `NoteListView.test.ts` 的挂载 + `vi.mock` 套路新建）

**Interfaces:**

- Consumes: Task 4 的 `AIConfig`（`models`、`api_key_set`）、Task 6 的端点。
- Produces:

```ts
// web/src/lib/ai.ts
export interface OptimizeResult {
  content: string;
  model: string;
}
export function optimizeNote(
  content: string,
  model?: string,
): Promise<OptimizeResult>;
export function defaultModel(models: string[]): string; // models[0] ?? ""
export function hostOf(baseURL: string): string; // URL().host，解析失败原样返回
```

- [ ] **Step 1: 写失败测试**

`web/src/lib/ai.test.ts`：

```ts
import { afterEach, describe, expect, it, vi } from "vitest";
import { defaultModel, hostOf, optimizeNote } from "./ai";

let requests: { url: string; init: RequestInit }[] = [];

function stubFetch(result: unknown, status = 200): void {
  requests = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      requests.push({ url, init });
      return new Response(JSON.stringify(result), {
        status,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ai", () => {
  it("optimizeNote 省略模型时不带 model 字段", async () => {
    stubFetch({ content: "# 优化后", model: "m1" });
    const result = await optimizeNote("正文");
    expect(result).toEqual({ content: "# 优化后", model: "m1" });
    const optimize = requests.find((r) => r.url === "/api/ai/optimize");
    expect(optimize?.init.method).toBe("POST");
    expect(JSON.parse(String(optimize?.init.body))).toEqual({
      content: "正文",
    });
  });

  it("optimizeNote 传入模型时带上 model 字段", async () => {
    stubFetch({ content: "x", model: "m2" });
    await optimizeNote("正文", "m2");
    const optimize = requests.find((r) => r.url === "/api/ai/optimize");
    expect(JSON.parse(String(optimize?.init.body))).toEqual({
      content: "正文",
      model: "m2",
    });
  });

  it("defaultModel 取第一个，hostOf 取主机", () => {
    expect(defaultModel([])).toBe("");
    expect(defaultModel(["a", "b"])).toBe("a");
    expect(hostOf("https://api.example.com/v1")).toBe("api.example.com");
    expect(hostOf("不是地址")).toBe("不是地址");
  });
});
```

`web/src/views/NoteEditorView.test.ts`（新建）：先按 `NoteListView.test.ts` 的方式 `vi.mock` 掉 `../lib/notes`（`listCategories`、`getMasterPassword`、`getNote`、`createNote`、`updateNote`、`MAX_CONTENT_CODEPOINTS` 保留真实值、`countCodePoints` 保留真实值）、`../lib/crypto`、`../lib/template`、`../lib/draft`（`take` 返回 `null`，`save`/`clear` 空函数）、`../lib/session-key`（`$hasKey` 需要 store，用 `readable(false)`；`sessionKey.current` 返回 `null`）、`../lib/router`（`navigate`），再 `vi.mock("../lib/ai-config")` 与 `vi.mock("../lib/ai")`；然后写：

```ts
const AI_ON = {
  base_url: "https://api.example.com/v1",
  models: ["m1", "m2"],
  prompt: "",
  default_prompt: "整理：{{content}}",
  prompt_variable: "{{content}}",
  api_key_set: true,
  api_key_masked: "sk-…abcd",
  path: "/tmp/note.yaml",
};

function aiButton(element: HTMLElement): HTMLButtonElement {
  const button = Array.from(element.querySelectorAll("button")).find(
    (b) =>
      b.textContent?.includes("用 AI 优化格式") ||
      b.textContent?.includes("优化中"),
  );
  if (!button) throw new Error("找不到 AI 优化按钮");
  return button as HTMLButtonElement;
}

function contentBox(element: HTMLElement): HTMLTextAreaElement {
  return Element.querySelector("#note-content") as HTMLTextAreaElement;
}

function buttonWith(element: HTMLElement, label: string): HTMLButtonElement {
  const button = Array.from(element.querySelectorAll("button")).find((b) =>
    b.textContent?.includes(label),
  );
  if (!button) throw new Error("找不到按钮: " + label);
  return button as HTMLButtonElement;
}

it("未配置 AI 时按钮禁用并指向信息页", async () => {
  mocks.getAIConfig.mockResolvedValue({ ...AI_ON, api_key_set: false });
  const element = await renderEditor();
  expect(aiButton(element).disabled).toBe(true);
  expect(element.querySelector('a[href="/info"]')).not.toBeNull();
});

it("优化成功后预览出现，「替换正文」写入正文", async () => {
  mocks.getAIConfig.mockResolvedValue({ ...AI_ON });
  mocks.optimizeNote.mockResolvedValue({ content: "# 优化后", model: "m1" });
  const element = await renderEditor();
  typeInto(contentBox(element), "原始正文");

  aiButton(element).dispatchEvent(new Event("click", { bubbles: true }));
  await vi.waitFor(() => expect(element.textContent).toContain("# 优化后"));
  expect(mocks.optimizeNote).toHaveBeenCalledWith("原始正文", "m1");

  buttonWith(element, "替换正文").dispatchEvent(
    new Event("click", { bubbles: true }),
  );
  flushSync();
  expect(contentBox(element).value).toBe("# 优化后");
});

it("优化期间正文被改动时禁止替换", async () => {
  mocks.getAIConfig.mockResolvedValue({ ...AI_ON });
  let resolve!: (value: unknown) => void;
  mocks.optimizeNote.mockImplementation(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  const element = await renderEditor();
  typeInto(contentBox(element), "原始正文");
  aiButton(element).dispatchEvent(new Event("click", { bubbles: true }));

  typeInto(contentBox(element), "原始正文改过了");
  resolve({ content: "# 优化后", model: "m1" });
  await vi.waitFor(() => expect(element.textContent).toContain("请重新优化"));
  expect(buttonWith(element, "替换正文").disabled).toBe(true);
});

it("结果超 2000 码点时禁止替换", async () => {
  mocks.getAIConfig.mockResolvedValue({ ...AI_ON });
  mocks.optimizeNote.mockResolvedValue({
    content: "😀".repeat(2001),
    model: "m1",
  });
  const element = await renderEditor();
  typeInto(contentBox(element), "原始正文");
  aiButton(element).dispatchEvent(new Event("click", { bubbles: true }));
  await vi.waitFor(() => expect(element.textContent).toContain("2001"));
  expect(buttonWith(element, "替换正文").disabled).toBe(true);
});

it("优化失败时显示错误且正文不变", async () => {
  mocks.getAIConfig.mockResolvedValue({ ...AI_ON });
  mocks.optimizeNote.mockRejectedValue(new Error("AI 接口返回 500：boom"));
  const element = await renderEditor();
  typeInto(contentBox(element), "原始正文");
  aiButton(element).dispatchEvent(new Event("click", { bubbles: true }));

  await vi.waitFor(() => expect(element.textContent).toContain("boom"));
  expect(contentBox(element).value).toBe("原始正文");
  expect(element.textContent).toContain("重新优化");
});

it("只有一个模型时不渲染模型下拉", async () => {
  mocks.getAIConfig.mockResolvedValue({ ...AI_ON, models: ["m1"] });
  const element = await renderEditor();
  expect(element.querySelector("#ai-model")).toBeNull();
});
```

（`renderEditor` / `typeInto` / `mocks` 是该文件自己的 helper，按 `NoteListView.test.ts` 的 `render` / `beforeEach` 写法实现；`getMasterPassword` 返回 `{configured: true, salt_b64: "salt", question: ""}`，`getTemplate` 返回 `{content: ""}`。上面 `contentBox(element)` 的形参名打错了大小写的话随手改；正常应当叫 `Element`／`element` 一致即可。）

- [ ] **Step 2: 跑测试确认失败**

Run: `npm --prefix web test -- ai.test NoteEditorView`
Expected: FAIL。

- [ ] **Step 3: 实现**

- `api.ts`：`export async function postJSON<T>(path: string, payload: unknown): Promise<T> { return sendJSON<T>("POST", path, payload); }`。
- `web/src/lib/ai.ts`：三个函数按接口块实现；`optimizeNote` 用 `model ? { content, model } : { content }`。
- `NoteEditorView.svelte`：
  - 状态：`aiModels`、`aiConfigured`、`aiHost`、`aiModel`、`aiBusy`、`aiResult`、`aiResultModel`、`aiError`、`aiRequestContent`。
  - `loadFormChrome` 里用 `getAIConfig().catch(() => null)` 并行取配置（失败不影响编辑器），填 `aiModels`、`aiModel = defaultModel(...)`、`aiHost = hostOf(base_url)`、`aiConfigured = api_key_set && base_url !== '' && models.length > 0`。
  - 派生：`canOptimize = aiConfigured && !aiBusy && !loading && content.trim() !== ''`；`aiStale = aiResult !== '' && content !== aiRequestContent`；`aiTooLong = aiResult !== '' && countCodePoints(aiResult) > MAX_CONTENT_CODEPOINTS`；`canReplace = aiResult !== '' && !aiStale && !aiTooLong`。
  - `optimize()`：置 `aiBusy`、清 `aiResult`/`aiError`、`aiRequestContent = content`，`await optimizeNote(content, aiModel || undefined)`，成功填 `aiResult`/`aiResultModel`，失败 `aiError = fail(error)`，`finally` 复位 `aiBusy`。
  - 正文标签右侧那排加按钮（`type="button"`，class 沿用现有 `linklike`）：未配置时 `disabled` 并显示「去信息页配置 AI」链接；已配置时文案 `{aiBusy ? '优化中…' : '用 AI 优化格式'}`。旁边在 `aiModels.length > 1` 时渲染 `<select bind:value={aiModel} disabled={aiBusy}>`。
  - 按钮下方常驻 `<p class="muted">正文会发送到 {aiHost}</p>`；`encrypt` 为真（或 `cipher !== ''`）时改文案为「正文会以明文发送到 {aiHost}」。
  - textarea 之后渲染结果面板（`aiError` 优先）：错误 + 「重新优化」；否则 `aiResult !== ''` 时显示标题「AI 优化结果 · {aiResultModel}」、`{countCodePoints(aiResult)} / {MAX_CONTENT_CODEPOINTS}` 计数（超限用现有 `.counter-over` 类）、`<Markdown source={aiResult} />`、「替换正文」（`disabled={!canReplace}`）、「放弃」，以及 `aiStale`/`aiTooLong` 时对应提示。
  - 「替换正文」：`content = aiResult; dirty = true; aiResult = ''; aiError = ''`。

- [ ] **Step 4: 跑测试确认通过**

Run: `npm --prefix web test -- ai.test NoteEditorView`，再 `npm --prefix web run check`
Expected: PASS，0 错误。

- [ ] **Step 5: 提交**

```bash
git add web/src/lib/api.ts web/src/lib/ai.ts web/src/lib/ai.test.ts web/src/views/NoteEditorView.svelte web/src/views/NoteEditorView.test.ts
git commit -m "feat(web): 编辑器新增「用 AI 优化格式」按钮与结果预览"
```

---

### Task 8: CLI `note add` 的 `Ctrl+O`

**Files:**

- Modify: `internal/cli/add.go`
- Modify: `cmd/note/add.go`
- Test: `internal/cli/add_test.go`

**Interfaces:**

- Consumes: Task 3 的 `ai.NewClient` / `Configured` / `DefaultModel` / `Host` / `Optimize`；`internal/markdown.Render`。
- Produces:

```go
type OptimizeResult struct{ Content, Model string }

type AddDeps struct {
    Categories     func(ctx context.Context) ([]Category, error)
    CreateCategory func(ctx context.Context, name string) (Category, error)
    CreateNote     func(ctx context.Context, in AddNote) (NoteRef, error)
    Optimize       func(ctx context.Context, content string) (OptimizeResult, error) // nil = 未配置
    AIModel        string // 预览屏与状态行展示用
    AIHost         string
}
```

- [ ] **Step 1: 写失败测试**

`internal/cli/add_test.go` 追加（复用文件里既有的 `newTestAddModel` / `pickCategory` / `typeText` / `press` / `pressCmd` / `feed`；`keyMsgFor` 需补一档 `case "ctrl+o": msg = tea.KeyMsg{Type: tea.KeyCtrlO}`）：

```go
// enterContentStep 是本地 helper：走到正文步并填好正文。
func enterContentStep(t *testing.T) addModel {
    t.Helper()
    m := newTestAddModel(&fakeBackend{})
    m = pickCategory(t, m, "工作")
    m = typeText(m, "标题")
    m = press(m, "enter")
    m = typeText(m, "原正文")
    return m
}

func TestContentStepCtrlOWithoutAIShowsHint(t *testing.T) {
    m := enterContentStep(t)
    m = press(m, "ctrl+o")
    if !strings.Contains(m.status, "note config set ai.models") {
        t.Fatalf("未配置时应给出补配置的提示: %q", m.status)
    }
    if m.body.Value() != "原正文" { t.Fatal("正文不该变") }
}

func TestCtrlOAdoptsOptimizedContent(t *testing.T) {
    m := enterContentStep(t)
    m.deps.AIModel, m.deps.AIHost = "m1", "api.example.com"
    m.deps.Optimize = func(context.Context, string) (OptimizeResult, error) {
        return OptimizeResult{Content: "# 优化后", Model: "m1"}, nil
    }

    m, cmd := pressCmd(m, "ctrl+o")
    if m.step != stepContent || !m.aiBusy { t.Fatalf("应当进入等待: step=%v busy=%v", m.step, m.aiBusy) }
    if !strings.Contains(m.status, "api.example.com") { t.Fatalf("状态行要披露目标主机: %q", m.status) }
    if cmd == nil { t.Fatal("应当返回一个执行优化的命令") }

    // 等待期间编辑被忽略
    if got := typeText(m, "x"); got.body.Value() != "原正文" { t.Fatal("等待期间不该编辑正文") }

    m = feed(m, cmd()) // optimizeFinishedMsg → 预览步
    if m.step != stepOptimized { t.Fatalf("step = %v", m.step) }

    m = press(m, "enter")
    if m.step != stepContent || m.body.Value() != "# 优化后" { t.Fatalf("应采用结果: %q", m.body.Value()) }
    if !strings.Contains(m.status, "Ctrl+D") { t.Fatalf("采用后应提示保存: %q", m.status) }
}

func TestPreviewEscKeepsOriginal(t *testing.T) {
    m := enterContentStep(t)
    m.deps.AIModel, m.deps.AIHost = "m1", "h"
    m.deps.Optimize = func(context.Context, string) (OptimizeResult, error) {
        return OptimizeResult{Content: "# 优化后", Model: "m1"}, nil
    }
    m, cmd := pressCmd(m, "ctrl+o")
    m = feed(m, cmd())
    m = press(m, "esc")
    if m.step != stepContent || m.body.Value() != "原正文" { t.Fatalf("Esc 应保留原文: step=%v body=%q", m.step, m.body.Value()) }
}

func TestOptimizeErrorKeepsOriginalAndStays(t *testing.T) {
    m := enterContentStep(t)
    m.deps.AIModel, m.deps.AIHost = "m1", "h"
    m.deps.Optimize = func(context.Context, string) (OptimizeResult, error) {
        return OptimizeResult{}, errors.New("AI 接口返回 500：boom")
    }
    m, cmd := pressCmd(m, "ctrl+o")
    m = feed(m, cmd())
    if m.step != stepContent || m.body.Value() != "原正文" { t.Fatal("失败不该改正文也不该换步") }
    if !strings.Contains(m.status, "boom") { t.Fatalf("应回显上游原因: %q", m.status) }
}

func TestPreviewRefusesTooLongResult(t *testing.T) {
    m := enterContentStep(t)
    m.deps.AIModel, m.deps.AIHost = "m1", "h"
    m.deps.Optimize = func(context.Context, string) (OptimizeResult, error) {
        return OptimizeResult{Content: strings.Repeat("😀", maxAddContentRunes+1), Model: "m1"}, nil
    }
    m, cmd := pressCmd(m, "ctrl+o")
    m = feed(m, cmd())
    m = press(m, "enter")
    if m.step != stepOptimized { t.Fatal("超限结果应留在预览步") }
    if m.body.Value() != "原正文" { t.Fatal("超限时不该采用") }
    if !strings.Contains(m.status, "2000") { t.Fatalf("应提示超限: %q", m.status) }
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run 'TestContentStepCtrlO|TestCtrlOAdopts|TestPreviewEsc|TestOptimizeError|TestPreviewRefuses' -v`
Expected: FAIL（编译失败：`OptimizeResult` / `stepOptimized` 未定义）。

- [ ] **Step 3: 实现 `internal/cli/add.go`**

1. `addStep` 末尾加 `stepOptimized`。
2. `addModel` 加字段 `aiBusy bool`、`aiResult string`、`aiModel string`、`aiRendered string`、`aiRequestContent string`、`aiView viewport.Model`（`github.com/charmbracelet/bubbles/viewport`，不需要新依赖）。
3. 消息类型 `optimizeFinishedMsg struct { Content, Model string; Err error }`。
4. `Update` 的 `tea.KeyMsg` 分派加 `case stepOptimized: return m.updateOptimizedStep(msg)`；`optimizeFinishedMsg` 分支：`m.aiBusy = false`；有错 → 回 `stepContent`，`m.status = "AI 优化失败: " + err`；否则填 `aiResult`/`aiModel`，用 `internal/markdown.Render(content, markdown.Options{Width: m.listWidth()})` 渲染（出错就退回原文）写入 `m.aiRendered`，`m.aiView = viewport.New(m.listWidth(), m.previewHeight()); m.aiView.SetContent(m.aiRendered)`，`m.step = stepOptimized`。
5. `updateContentStep` 加 `case "ctrl+o": return m.startOptimize()`：

```go
func (m addModel) startOptimize() (tea.Model, tea.Cmd) {
    if m.deps.Optimize == nil {
        m.status = "还没配置 AI：note config set ai.base_url=… ai.api_key=… ai.models=…"
        return m, nil
    }
    if strings.TrimSpace(m.body.Value()) == "" {
        m.status = "正文是空的，先写点东西再优化"
        return m, nil
    }
    if utf8.RuneCountInString(m.body.Value()) > maxAddContentRunes {
        m.status = fmt.Sprintf("正文最多 %d 码点，先删掉一些再优化", maxAddContentRunes)
        return m, nil
    }
    m.aiBusy, m.aiResult, m.aiRequestContent = true, "", m.body.Value()
    m.status = fmt.Sprintf("正在用 %s 优化…（正文会发送到 %s）", m.deps.AIModel, m.deps.AIHost)
    content, optimize := m.body.Value(), m.deps.Optimize
    return m, func() tea.Msg {
        res, err := optimize(context.Background(), content)
        return optimizeFinishedMsg{Content: res.Content, Model: res.Model, Err: err}
    }
}
```

6. 等待期间锁编辑：`updateContentStep` 开头 `if m.aiBusy { return m, nil }`（放在 `ctrl+c`/`esc` 之后，保证仍能中断退出）。`ctrl+d` 同样被这条挡住。
7. `updateOptimizedStep`：`enter` → 若 `utf8.RuneCountInString(m.aiResult) > maxAddContentRunes` 则 `m.status = "优化结果超过 2000 码点，不能直接采用；按 Esc 保留原文"` 留在原步；否则 `m.body.SetValue(m.aiResult)`、`m.body.CursorEnd()`、`m.step = stepContent`、`m.status = "已采用 AI 优化结果，Ctrl+D 保存"`、`m.aiResult = ""`。`esc` → `m.step = stepContent`、`m.status = ""`。`ctrl+o` → `return m.startOptimize()`。其余消息交给 `m.aiView.Update(msg)`。
8. `layout()` 里同步 viewport 尺寸：`m.aiView.Width, m.aiView.Height = width, m.previewHeight()`（`newAddModel` 里先 `m.aiView = viewport.New(width, m.previewHeight())` 建一次，之后只改尺寸，内容不会被清掉——直接 new 会丢 keymap 之外的内容）。新增 `func (m addModel) previewHeight() int { h := m.height - 8; if h < 6 { h = 6 }; return h }`。
9. `View()` 加 `case stepOptimized: return m.viewOptimized()`；help 行加 `{"Ctrl+O", "用 AI 优化"}`。`viewOptimized` 内容：`header("AI 优化结果", "")`、一行 `styleMuted.Render(fmt.Sprintf("%s · %d → %d 码点", m.aiModel, utf8.RuneCountInString(m.aiRequestContent), utf8.RuneCountInString(m.aiResult)))`、`m.aiView.View()`、`help([][2]string{{"Enter", "采用"}, {"Esc", "保留原文"}, {"Ctrl+O", "重新优化"}})`。

- [ ] **Step 4: 实现 `cmd/note/add.go` 装配**

```go
client := ai.NewClient(cfg.AI)
deps := addDeps(st)
deps.AIModel, deps.AIHost = client.DefaultModel(), client.Host()
if client.Configured() {
    deps.Optimize = func(ctx context.Context, content string) (cli.OptimizeResult, error) {
        res, err := client.Optimize(ctx, content, "")
        if err != nil { return cli.OptimizeResult{}, err }
        return cli.OptimizeResult{Content: res.Content, Model: res.Model}, nil
    }
}
ref, saved, err := cli.RunAdd(context.Background(), deps)
```

（`addDeps(st)` 现在是纯 `cli.AddDeps`，在 `runAdd` 里补齐 AI 三项。）

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/cli/ ./cmd/note/ -v`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/cli/add.go internal/cli/add_test.go cmd/note/add.go
git commit -m "feat(cli): note add 支持 Ctrl+O 用 AI 优化正文（预览后 Enter 采用）"
```

---

### Task 9: 文档、CHANGELOG 与端到端冒烟

**Files:**

- Modify: `docs/PRD.md`（v1.14：新增 5.9、路由表、AC-60、M13、偏离表一行）
- Modify: `docs/TECH_DESIGN.md`（1.2 不变量、目录树、AI 四项与模板、端点表、网页/CLI 段、测试矩阵）
- Modify: `AGENTS.md`（硬性约定两条）
- Modify: `CHANGELOG.md`（`[Unreleased] / Added`）

**Interfaces:**

- Consumes: 前八个任务的最终实现（文档里写的名字必须与代码一致）。
- Produces: 无代码产出。

- [ ] **Step 1: 全量验证（先跑，再写文档）**

Run: `just test`，然后 `just check`
Expected: Go 全包 ok、Vitest 全绿、`go vet` 无输出、`svelte-check` 0 错 0 警。测试没过就**不要**往下写「已完成」。

- [ ] **Step 2: 用假上游做端到端冒烟**

在仓库外的临时目录放一个假上游（不要提交进仓库）：

````go
// %TEMP%/fake-ai/main.go
package main

import ("fmt";"net/http")

func main() {
    http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
        fmt.Println("path:", r.URL.Path, "auth:", r.Header.Get("Authorization")[:7])
        w.Header().Set("Content-Type", "application/json")
        fmt.Fprint(w, `{"choices":[{"message":{"content":"```markdown\n# 优化后的标题\n\n- 要点一\n- 要点二\n```"}}]}`)
    })
    http.ListenAndServe("127.0.0.1:18081", nil)
}
````

`go run` 起来后，用临时配置文件 `note -c <tmp>/note.yaml config set ai.base_url=http://127.0.0.1:18081/v1`、`note config set ai.api_key=sk-test`、`note config set ai.models=m1,m2`、`note config set ai.prompt=`（留空=默认）。

逐项确认并记录到 CHANGELOG：

1. `note config list` 显示四项，`ai.models` 为 `m1, m2`、`ai.prompt` 为`（默认提示词）`。
2. `note web` 起服务后，用浏览器在编辑器里点「用 AI 优化格式」→ 面板出现、计数正确 → 「替换正文」→ 正文变成 `# 优化后的标题` → 保存成功；切到模型 `m2` 时假上游打印的请求体 `model` 是 `m2`。
3. `ai.prompt` 改成不带 `{{content}}` 的值：`note config set ai.prompt=没有变量` 退出 2；网页保存 400 且提示标红。
4. 信息页点「恢复默认提示词」→ 文本框填成默认模板 → 保存后 `GET /api/ai-config` 的 `prompt` 等于默认模板。
5. 关掉假上游 → 网页显示 502 错误且正文不变；`note add` 里 `Ctrl+O` 的 status 显示错误且正文不变。
6. `note add` 真终端：写正文 → `Ctrl+O` → 预览屏（渲染后的 Markdown、`n → m 码点`）→ `Enter` 采用 → `Ctrl+D` 保存 → `note show <id>` 是优化后的内容。
7. 检索安全不变量：`note.log`、终端输出、响应体里搜不到 `sk-test` 与正文；`curl -i` 该端点有 `Cache-Control: no-store`。

- [ ] **Step 3: 写文档**

- PRD：版本 `v1.14`，修订说明加一段；新增 `### 5.9 AI 美化笔记（v1.14）`（FR-7.5 网页按钮/FR-7.6 CLI `Ctrl+O`/FR-7.7 模型列表默认第一项/FR-7.8 提示词模板与恢复默认/FR-7.9 预览后确认、明文外发须明示）；路由表加 `POST /api/ai/optimize` 与 `/api/ai-config` 的新字段；验收加 AC-60（照上一步 1–7 写）；里程碑表加 M13；2.x 偏离表加一行「`ai.prompt` 空串语义：v1.13 不追加 → v1.14 使用默认模板」。
- TECH_DESIGN：1.2 安全不变量加「唯一外部网络出口是 `internal/ai`，正文只在用户触发时外发，密钥/正文/提示词不进日志」与「含正文的优化响应 `no-store`」；目录树加 `internal/ai/`、`internal/web/handlers_optimize.go`、`web/src/lib/ai.ts`；AI 段从三项改四项并写模板变量与默认提示词来源；端点表加一行；网页与 CLI 段落按实现补写；测试矩阵补新文件。
- AGENTS.md：硬性约定加两条——「对外 HTTP 请求只有 `internal/ai` 一处」与「提示词模板变量 `{{content}}`、默认提示词 `config.DefaultAIPrompt` 是唯一来源；`ai.prompt` 非空必须含变量，空=默认」。
- CHANGELOG `[Unreleased] / Added`：按仓库既有密度写「做了什么 / 关键决策 / 测试与冒烟结果」，只写已跑通的事实，并注明测试与冒烟的实测数字。

- [ ] **Step 4: 收尾验证**

Run: `just test`、`just check`
Expected: 全绿（写文档不引入代码变更，但要复跑确认工作区干净可交付）。

- [ ] **Step 5: 提交**

```bash
git add docs/PRD.md docs/TECH_DESIGN.md AGENTS.md CHANGELOG.md docs/superpowers/specs/2026-10-30-ai-note-optimize-design.md docs/superpowers/plans/2026-10-30-ai-note-optimize.md
git commit -m "docs: 记录 AI 美化笔记（PRD v1.14、TECH_DESIGN、AGENTS、CHANGELOG）"
```

---

## Self-Review

- **Spec coverage**：§5.1 配置（Task 1/2/4/5）、§5.2 `internal/ai`（Task 3）、§5.3 端点（Task 6）、§5.4 网页（Task 7）、§5.5 CLI（Task 8）、§5.6 安全（Task 3/6/7/8 的实现 + Task 9 冒烟 7）、§5.8 文档（Task 9）、§5.9 验收（Task 9）——无遗漏章节。
- **Step scan**：每个 Step 都可独立判定（测试名 + 断言、签名、命令、期望输出）；未出现「处理边界情况」这类空话。
- **Type consistency**：`OptimizeResult`（CLI）/`ai.Result`（Go）/`OptimizeResult`（TS）三者字段均为 `content`+`model`；`aiConfigResponse.Models/PromptVariable/DefaultPrompt` 与前端 `models`/`prompt_variable`/`default_prompt` 对应；`stepOptimized`、`aiView`、`previewHeight` 只在 Task 8 内使用。
- **Review Focus**：围栏（Task 3）、正文含变量（Task 3）、尾斜杠（Task 3）、码点边界（Task 7/8）、手写 YAML 标量/空列表（Task 1）、上游失败（Task 6/7/8）、迟到结果（Task 7/8）——每条都落到具体任务的测试。
- **Proportion**：计划约为设计文档两倍长，主要由测试代码构成（本仓库既有风格即如此），没有替实现者写业务函数体。
