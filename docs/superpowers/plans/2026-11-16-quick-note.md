# 速记（快速笔记）：网页速记卡片 + AI 速记 + CLI `note add <内容>`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans

- 规格：`docs/superpowers/specs/2026-11-16-quick-note-design.md`（已批准）
- 版本：v1.18（里程碑 M15）
- 目标：让「记一条小笔记」从 3 步向导 / 无网页入口变成 1 步；`note add <内容>` 直接落库；`--ai` 走 AI 整理＋预览确认

## Goal

「速记」分类（id=2）目前只有 `note set/get/del` 可写，网页无入口，`note add` 三步向导太重。本计划实现：

1. **Web 首页速记卡片**：查询区下方正文文本框＋「速记」「AI 速记」按钮；「速记」直接落「速记」分类（标题自动 `速记-<UTC 秒级>`）；「AI 速记」先 AI 整理＋生成概括标题，预览确认后落库。
2. **CLI**：`note add <内容>` 直接记录（脚本化，退出 0）；`note add <内容> --ai` 进 bubbletea 单屏预览确认（Enter 保存 / Esc 放弃 130）；无参 `note add` 三步向导保持不变（向后兼容 AC-54 已批准行为之外的部分）。
3. **后端**：`store.CreateQuickNote` + `internal/ai.QuickNote` + 独立端点 `POST /api/quick` / `POST /api/ai/quick`（不扩展既有 `POST /api/notes` 与 `POST /api/ai/optimize`）。

## Architecture

```text
网页 HomeView 速记卡片                    CLI note add <内容> / --ai
   │  POST /api/quick                       │ cmd/note/runAdd 注入 deps
   │  POST /api/ai/quick                    ▼
   ▼                               internal/cli quick_add.go（bubbletea 预览）
internal/web handlers_quick.go        │
   │ 每请求 config.ReadAI             │ RunAddQuick → CreateQuick
   ▼                                 ▼
internal/store.CreateQuickNote      internal/store.CreateQuickNote
   │  title 空 → 速记-<UTC 秒级>，撞秒顺延 ≤5 次
   ▼
internal/ai.QuickNote（唯一对外 HTTP 出口，复用 Optimize 管线）
```

新增文件：`internal/store/quick.go`、`internal/ai/quick.go`、`internal/web/handlers_quick.go`、`internal/cli/quick_add.go`、`web/src/views/HomeView.test.ts`。修改文件：`internal/ai/client.go`（抽共用管线）、`internal/web/server.go`（路由）、`web/src/lib/notes.ts`、`web/src/lib/ai.ts`、`web/src/views/HomeView.svelte`、`internal/cli/parse.go`、`internal/cli/usage.go`、`cmd/note/main.go`、`cmd/note/add.go`、`docs/PRD.md`、`docs/TECH_DESIGN.md`、`CHANGELOG.md`。

## Tech Stack

不变：Go `net/http` + `ServeMux`、SQLite（modernc）、Svelte 5 + TS、bubbletea。数据库**不加迁移**（速记分类 v1.15 已建）。

## Spec

### S1 后端

**`internal/store/quick.go`**（新增）：

```go
// CreateQuickNote 创建一条速记，固定落在「速记」分类（QuickCategoryID）。
// title 为空 → 自动生成 速记-<UTC 秒级>，同秒撞名自动顺延下一秒重试（≤5 次，
// ErrNoteTitleExists 与 ErrNoteTitleInTrash 都算）；title 非空 → 单次创建，撞名原样返回。
func (s *Store) CreateQuickNote(ctx context.Context, content, title string) (Note, error)
```

- 内部 `createQuickNoteAt(ctx, content, title string, now time.Time)` 可注入时钟；公开方法包 `time.Now()`。
- 校验（不符 → `ErrInvalidNote`）：content trim 后非空且 `utf8.RuneCountInString ≤ 2000`；title 非空时 `≤ 200` 码点。
- 自动标题：`"速记-" + now.Add(time.Duration(i)*time.Second).UTC().Format(timeLayoutUTC)`，`i = 0..4`；每次调用 `s.CreateNote(ctx, NoteInput{Title: t, Content: &content, CategoryID: &catID})`（`catID := int64(QuickCategoryID)`），命中撞名错误继续，5 次未成返回最后一次错误。
- 复用 `CreateNote` 的落库列（`kind='note'`/`remark=''`/`rev=1`）与 `recordActivityTx(ActivityNoteCreate)`——热力图/统计自动计入。

**`internal/ai/quick.go`**（新增）+ `internal/ai/client.go`（重构）：

- 从 `Optimize` 抽共用管线 `func (c *Client) chat(ctx context.Context, model, prompt string) (raw []byte, resolvedModel string, err error)`：配置与模型校验（Configured/Missing/ModelError）、endpoint 拼接 `TrimRight(base_url,"/")+"/chat/completions"`、Bearer、`Temperature: 0.2`、`LimitReader(maxResponseBytes+1)`、状态码检查（UpstreamError/TimeoutError/ErrTooLarge）、返回原始响应体。
- `Optimize` 改为：先 `BuildPromptWithCustom`，再 `chat`，再解析 choices + `StripFence` + 空结果检查（行为不变，现有测试全绿）。
- 新类型：

```go
type QuickNoteResult struct { Content, Title, Model string }

// QuickResultError 表示 AI 速记结果不符合预期（缺字段/超长等），Reason 面向用户。
type QuickResultError struct{ Reason string }
func (e QuickResultError) Error() string { return e.Reason }

func (c *Client) QuickNote(ctx context.Context, req Request) (QuickNoteResult, error)
```

- 内建提示词常量 `defaultQuickPrompt`（JSON 指令：只输出 `{"title":…,"content":…}`，title 一句 ≤30 字概括，content 条理清晰的 Markdown、不编造）；正文**直接追加**在指令之后（`buildQuickPrompt`），不展开任何变量——速记提示词不是 `ai.prompt` 模板变体。
- `chat` → `StripFence` → `json.Unmarshal` 到 `{Title, Content}` → 校验：title 非空且 ≤60 码点、content 非空且 ≤2000 码点；任一不符 → `QuickResultError{Reason}`（**不截断**）。
- 错误语义与 Optimize 一致：ErrNotConfigured / ModelError / TimeoutError / UpstreamError / ErrBadResponse（传输层不变）。

**`internal/web/handlers_quick.go`**（新增）+ `server.go` 路由：

- `POST /api/quick`：body `{content string, title string}`（title 可省），`quickMaxBytes = 64 << 10`；校验 content 非空 ≤2000（400 `fields.content`）、title 非空 ≤200（400 `fields.title`）；`store.CreateQuickNote`；错误走 `writeNoteStoreError`（409 `title_conflict`/`title_in_trash` 文案与既有分类内撞名一致）；成功 201 + 完整 Note。
- `POST /api/ai/quick`：body `{content string}`；校验同速记（400）；每请求 `config.ReadAI(cfg.ConfigPath)`，未配置 → 409 `ai_not_configured`（消息含 `client.Missing()`，与 optimize 同文案）；`optimizeTimeout = 60s` 上下文；`client.QuickNote`；传输层错误复用 `writeOptimizeError`；`QuickResultError` → 502 `ai_bad_response` 带 `Reason`（比 optimize 的通用文案更明确——设计 §5.9 验收 6「明确报错」）；成功 200 `{title, content, model}`。
- 两者都是 POST → 自动过 CSRF 双提交；`writeJSON` 已统一 `Cache-Control: no-store`；中间件链 `recoverPanics → csrfProtect → hostGuard → securityHeaders → accessLog` 自动继承。

### S2 前端

- `web/src/lib/notes.ts`：`createQuickNote(content: string, title?: string): Promise<NoteMeta>` → `postJSON("/api/quick", title ? {content, title} : {content})`。
- `web/src/lib/ai.ts`：`aiQuickNote(content: string): Promise<{title, content, model}>` → `postJSON("/api/ai/quick", {content})`（payload 只带 content，仿 `optimizeNote`）。
- `HomeView.svelte`：搜索 form 下方插速记卡片：
  - 状态：`quickText`、`quickBusy`、`quickMessage`、`quickError`、`aiBusy`、`aiPreview: {title, content, model} | null`、`aiTitle`（可编辑）、`aiError`。
  - 「速记」：`createQuickNote(quickText.trim())` → 成功清空文本框、`quickMessage = 已记入速记「标题」`、`refreshStats()`（热力图/统计刷新）；409 显示后端文案。
  - 「AI 速记」：`aiQuickNote(quickText)` → 预览面板（标题输入框 `bind:value={aiTitle}`、`<Markdown source={aiContent}/>`、模型名）＋「保存」「返回修改」；「保存」用 `aiPreview` 锁定的内容（不读文本框当前值）`createQuickNote(aiContent, aiTitle)`；「返回修改」关预览、正文留在输入框；AI 错误内联显示。
  - 正文空 → 两按钮 disabled；AI 未配置（409 `ai_not_configured`）显示补齐提示（可发现、不静默失败）。
- 新建 `web/src/views/HomeView.test.ts`（仿 `NoteEditorView.test.ts` 的 `vi.hoisted` 批量 mock 模式）：渲染 → 输入正文 → 「速记」→ 断言 `createQuickNote` 调用与清空/`refreshStats`；AI 预览 → 改标题 → 「保存」→ 断言 payload；「返回修改」不落库；409 显示错误。

### S3 CLI

**`internal/cli/parse.go`**：

- `Options` 加 `Add AddOptions`；`type AddOptions struct{ Content string; AI bool }`。
- 新增 `parseAddArgs(rest []string, cfg *ConfigOptions) (AddOptions, error)` 替换 `validateConfigOnlyArgs("add", …)`：
  - `-c/--config/--db` 透传（沿用 `addConfigFlags`）；
  - `--ai` 置位；第一个位置参数 = 正文，第二个位置参数 → 错误；
  - 无位置参数 → `Content=""`（进向导）；有位置参数但 trim 空 → 错误「正文不能为空」；
  - `--ai` 且正文空 → 错误；正文 `utf8.RuneCountInString > 2000` → 错误（均退出 2）。

**`internal/cli/quick_add.go`**（新增）：

```go
type QuickAddDeps struct {
    CreateQuick func(ctx context.Context, content, title string) (cliRef, error) // NoteRef
    QuickNote   func(ctx context.Context, content string) (QuickNoteResult, error) // nil = 未配置
    AIModel     string
    AIHost      string
}
func RunAddQuick(ctx context.Context, deps QuickAddDeps, content string, ai bool) (NoteRef, bool, error)
```

- 非 AI：`CreateQuick(ctx, content, "")` → ref, saved=true。正文校验在 parse 已做，此处兜底（`maxQuickContentRunes`）。
- AI：`quickAddModel` bubbletea 单屏：
  - Init：`QuickNote == nil` → status「还没配置 AI：note config set ai.base_url=… ai.api_key=… ai.models=…」，等 Esc 放弃 130；
  - 有配置：status「正在用 {AIModel} 整理…（正文会发送到 {AIHost}）」→ command → `quickNoteFinishedMsg{Title, Content, Model, Err}`；
  - 成功 → 预览屏（`internal/markdown.Render(content)` + 标题只读 + 模型名）；Enter → `CreateQuick(ctx, content, title)` → saved=true；Esc → saved=false（130）；Ctrl+O 重新整理；
  - 失败 → status 截断错误摘要（`truncateRunes` 口径），留在原地可重试。
- `RunAddQuick` 由 `RunAdd`（现有入口）分派：content 为空走 `runAddWizard`（不变）。

**`cmd/note/main.go` / `cmd/note/add.go`**：

- `runAdd(globalConfigPath string, args []string, addOpts cli.AddOptions)`；`CommandAdd` 分派处传 `opts.Add`。
- `runAdd`：content 为空 → 现有向导路径；否则组装 `QuickAddDeps`（`CreateQuick = st.CreateQuickNote` 适配 NoteRef；AI 三项与 `addDepsWithAI` 同口径）→ `cli.RunAddQuick`：
  - 非 AI 成功：stderr「已记入速记「{title}」」（+ 链接/ID，仿 `printAddedNote` 风格），stdout 干净，退出 0；
  - AI 且 `!IsTerminal(os.Stdin)` → `ErrNotTerminal` 退出 1；放弃 → stderr「已放弃，未新建笔记」退出 130；
  - 运行错误退出 1、用法错误退出 2（parse 层）。

**`internal/cli/usage.go`**：

- `addUsage()` 与总帮助 `add` 行补充：`note add <内容>` 直接记录、`note add <内容> --ai` AI 整理（预览确认）、无参进三步向导；删除「note add 不接受其它参数」句。
- `internal/cli/parse_test.go` 的 `TestParseAddRejectsExtraArgs` 重写：`add foo` 现为合法（Content="foo"），`add foo bar` 仍报错。
- `internal/cli/usage_test.go` 的 `TestUsageForAddDescribesThreeSteps` 追加快速记法断言。

### S4 文档

- `docs/PRD.md` → v1.18：修订记录；新增 FR（网页速记卡片 / AI 速记预览 / `note add <内容>` / `--ai`）；路由表加两行；AC-54 改写（`note add foo` 现为记录一条速记、退出 0）；新 AC；里程碑 M15。
- `docs/TECH_DESIGN.md`：§1.2 安全不变量补速记口径；§8 端点表加 `POST /api/quick` / `POST /api/ai/quick`；目录树与 AI 段补速记提示词（内建常量、不是 `ai.prompt` 变体）。
- `CHANGELOG.md` `[Unreleased]`：按仓库三段式（做了什么 / 关键决策 / 验证）写，**只在 just test 与 just check 全绿后写**。
- 设计文档已批准，本计划文件随实现入库。

## Global Constraints

- 数据库结构不变，**不加迁移**。
- 状态变更类 API（POST）过 CSRF 双提交；含正文响应 `Cache-Control: no-store`（`writeJSON` 已统一）。
- `internal/ai` 是全项目唯一对外 HTTP 出口；`POST /api/ai/quick` 服务端代发（api_key 不出机器，每请求 `config.ReadAI`）；`internal/cli` 不 import ai/config。
- 日志/`/api/info`/响应体不得出现正文、AI 标题、渲染后提示词、`api_key`；`api_key` 明文只存在于 `note.yaml`。
- 长度口径一律 `utf8.RuneCountInString`：正文 2000、标题 200、AI 标题 60；AI 结果**不截断**、明确报错、不落库。
- 时间戳 UTC 秒级 `2006-01-02T15:04:05Z`（自动标题与 `created_at` 同 `nowUTC()` 口径）。
- CLI：成功提示写 stderr、stdout 干净；用法错误退出 2、运行错误 1、Esc/Ctrl+C 放弃 130；`IsTerminal(os.Stdin)` 判终端。
- 反目标：同秒互相覆盖（禁止）——自动标题撞名必须顺延重试。
- 合入前 `just test`（Go 全包 + vitest）与 `just check`（go vet + svelte-check）必须全绿；CHANGELOG 只在验证通过后写。

## Review Focus（易错点）

- `{{content}}` 字面量：速记提示词是**内建常量**，正文直接追加，不展开 `config.PromptVariable`，也不与 `ai.prompt` 合并校验。
- base_url 尾部斜杠：复用 `TrimRight(base_url,"/")` 拼 `chat/completions`，别手写双斜杠。
- 同秒撞名：普通模式 ≤5 次顺延（`ErrNoteTitleExists`/`ErrNoteTitleInTrash` 都算）；AI 模式（title 非空）单次创建、撞名原样返回 409。
- 旧断言更新：`TestParseAddRejectsExtraArgs`（`add foo` 合法化）、`TestUsageForAddDescribesThreeSteps`、PRD AC-54。
- AI 输出非 JSON / 围栏包裹（`json…`）/ 字段超长：`StripFence` + 校验 → `QuickResultError`（web 502 带 Reason，不截断）。
- `Optimize` 重构后既有 ai 测试必须全绿（行为不变）。
- 码点边界：正文恰 2000、标题恰 200/60 通过；2001/201/61 拒绝。
- 前端「保存」用预览锁定的 `aiPreview` 内容，不读输入框当前值；`aiTitle` 只在预览面板可编辑。
- 网页 AI 未配置 409 文案与 CLI 提示都要可发现（给出 `note config set` 指引）。

---

## Task 1: store `CreateQuickNote`

**Files:** 新增 `internal/store/quick.go`；测试 `internal/store/quick_test.go`

**Interfaces:**

- Consumes: `CreateNote`（note.go）、`NoteInput`、`ErrInvalidNote`、`ErrNoteTitleExists`/`ErrNoteTitleInTrash`、`timeLayoutUTC`（activity.go）、`QuickCategoryID`（category.go）、`recordActivityTx`。
- Produces: `func (s *Store) CreateQuickNote(ctx context.Context, content, title string) (Note, error)`

- [ ] **Step 1: 写失败测试** `internal/store/quick_test.go`（用现有 `noteTestStore(t)` + `configureMaster(t, st)`）：

```go
func TestCreateQuickNoteAutoTitle(t *testing.T) {
    st := noteTestStore(t)
    n, err := st.CreateQuickNote(context.Background(), "买牛奶和鸡蛋", "")
    if err != nil { t.Fatal(err) }
    if n.CategoryID != int64(QuickCategoryID) { t.Fatalf("category = %d", n.CategoryID) }
    if !regexp.MustCompile(`^速记-\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`).MatchString(n.Title) {
        t.Fatalf("标题格式不符: %q", n.Title)
    }
    if n.Content != "买牛奶和鸡蛋" { t.Fatal("正文不符") }
}

func TestCreateQuickNoteRetriesSameSecond(t *testing.T) {
    st := noteTestStore(t)
    fixed := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
    // 先占住 0 秒的标题（含回收站路径也要测：一条正常 + 一条回收站）
    if _, err := st.CreateNote(context.Background(), NoteInput{
        Title: "速记-" + fixed.Format(timeLayoutUTC), Content: strPtr("占用"), CategoryID: int64Ptr(QuickCategoryID),
    }); err != nil { t.Fatal(err) }
    n, err := st.createQuickNoteAt(context.Background(), "新速记", "", fixed)
    if err != nil { t.Fatal(err) }
    want := "速记-" + fixed.Add(time.Second).Format(timeLayoutUTC)
    if n.Title != want { t.Fatalf("title = %q, want %q", n.Title, want) }
}

func TestCreateQuickNoteExplicitTitleConflict(t *testing.T) {
    st := noteTestStore(t)
    if _, err := st.CreateNote(context.Background(), NoteInput{
        Title: "我写过的", Content: strPtr("旧"), CategoryID: int64Ptr(QuickCategoryID),
    }); err != nil { t.Fatal(err) }
    _, err := st.CreateQuickNote(context.Background(), "新内容", "我写过的")
    if !errors.Is(err, ErrNoteTitleExists) { t.Fatalf("err = %v", err) }
}

func TestCreateQuickNoteValidation(t *testing.T) {
    st := noteTestStore(t)
    for _, tc := range []struct{ name, content, title string }{
        {"空正文", "  ", ""},
        {"正文超长", strings.Repeat("😀", maxContentRunes+1), ""},
        {"标题超长", "内容", strings.Repeat("字", maxTitleRunes+1)},
    } {
        if _, err := st.CreateQuickNote(context.Background(), tc.content, tc.title); !errors.Is(err, ErrInvalidNote) {
            t.Fatalf("%s: err = %v", tc.name, err)
        }
    }
}

func TestCreateQuickNoteRecordsActivity(t *testing.T) {
    st := noteTestStore(t)
    st.CreateQuickNote(context.Background(), "活动", "")
    // stats 里 ActivityNoteCreate 计数 +1（仿 note_test.go 既有断言）
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/store/ -run TestCreateQuickNote -v`
Expected: FAIL（`CreateQuickNote` 未定义）。`maxContentRunes`/`maxTitleRunes` 是 note.go 同包未导出常量（2000/200），quick.go 直接复用，无需新常量。

- [ ] **Step 3: 实现 `internal/store/quick.go`**

```go
// CreateQuickNote 固定落「速记」分类；title 空 → 自动 速记-<UTC 秒级>，
// 同秒撞名顺延下一秒重试（≤5 次）；title 非空 → 单次创建。
func (s *Store) CreateQuickNote(ctx context.Context, content, title string) (Note, error) {
    return s.createQuickNoteAt(ctx, content, title, time.Now())
}

func (s *Store) createQuickNoteAt(ctx context.Context, content, title string, now time.Time) (Note, error) {
    content = strings.TrimSpace(content)
    if content == "" || utf8.RuneCountInString(content) > maxContentRunes {
        return Note{}, ErrInvalidNote
    }
    title = strings.TrimSpace(title)
    if title != "" && utf8.RuneCountInString(title) > maxTitleRunes {
        return Note{}, ErrInvalidNote
    }
    catID := int64(QuickCategoryID)
    if title == "" {
        for i := 0; i < quickTitleRetries; i++ {
            t := "速记-" + now.Add(time.Duration(i)*time.Second).UTC().Format(timeLayoutUTC)
            n, err := s.CreateNote(ctx, NoteInput{Title: t, Content: &content, CategoryID: &catID})
            if err == nil { return n, nil }
            if !errors.Is(err, ErrNoteTitleExists) && !errors.Is(err, ErrNoteTitleInTrash) { return Note{}, err }
        }
        return Note{}, fmt.Errorf("速记自动标题连续 %d 秒被占用: %w", quickTitleRetries, ErrNoteTitleExists)
    }
    return s.CreateNote(ctx, NoteInput{Title: title, Content: &content, CategoryID: &catID})
}
```

（常量：`quickTitleRetries = 5`。`maxContentRunes`/`maxTitleRunes` 已在 note.go 同包定义，直接复用。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/store/ -run 'TestCreateQuick|TestNote' -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/store/quick.go internal/store/quick_test.go
git commit -m "feat(store): CreateQuickNote 固定落速记分类，自动标题撞秒顺延重试"
```

---

## Task 2: `internal/ai.QuickNote`

**Files:** 修改 `internal/ai/client.go`；新增 `internal/ai/quick.go`；测试 `internal/ai/quick_test.go`

**Interfaces:**

- Consumes: `Client`（Configured/Missing/DefaultModel/Host）、`Request`、`StripFence`、错误集。
- Produces: `QuickNoteResult{Content, Title, Model}`、`QuickResultError{Reason}`、`func (c *Client) QuickNote(ctx, Request) (QuickNoteResult, error)`。

- [ ] **Step 1: 写失败测试** `internal/ai/quick_test.go`（仿 `client_test.go` 的 `newTestClient`）：

````go
func TestQuickNoteSendsBuiltinPrompt(t *testing.T) {
    var got struct{ model, prompt string }
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var body struct{ Model, Messages string } // 用 map 取 content
        json.NewDecoder(r.Body).Decode(&body)
        w.Header().Set("Content-Type", "application/json")
        fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"title\":\"待办\",\"content\":\"- 买牛奶\"}"}}]}`)
    }))
    c := newTestClient(t, srv)
    c.BaseURL, c.APIKey, c.Models = srv.URL+"/v1", "k", "m1"
    res, err := c.QuickNote(context.Background(), Request{Content: "买牛奶"})
    if err != nil { t.Fatal(err) }
    if res.Title != "待办" || res.Content != "- 买牛奶" || res.Model != "m1" { t.Fatalf("res = %+v", res) }
    // 断言请求体里含速记指令且不含 ai.prompt 模板（内容直接追加）
}

func TestQuickNoteStripsFence(t *testing.T) { /* 响应为 ```json {...} ``` → 结果正常 */ }

func TestQuickNoteBadJSON(t *testing.T) {
    // 响应 "不是 JSON" → errors.Is(err, ErrBadResponse)
}

func TestQuickNoteInvalidTitleAndContent(t *testing.T) {
    for _, resp := range []string{
        `{"choices":[{"message":{"content":"{\"title\":\"\",\"content\":\"x\"}"}}]}`,
        `{"choices":[{"message":{"content":"{\"title\":\"` + strings.Repeat("字", 61) + `\",\"content\":\"x\"}"}}]}`,
        `{"choices":[{"message":{"content":"{\"title\":\"t\",\"content\":\"\"}"}}]}`,
        `{"choices":[{"message":{"content":"{\"title\":\"t\",\"content\":\"` + strings.Repeat("😀", 2001) + `\"}"}}]}`,
    } {
        // 每个都断言 errors.As(err, &QuickResultError{}) 且 Reason 非空
    }
}

func TestQuickNoteReusesErrorTypes(t *testing.T) {
    // 401 → UpstreamError；未配置（Models 空）→ ErrNotConfigured；超体积 → ErrTooLarge
}
````

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/ai/ -run TestQuickNote -v`
Expected: FAIL（`QuickNote` 未定义）。

- [ ] **Step 3: 重构 `client.go` + 实现 `quick.go`**

1. `client.go`：把 `Optimize` 里「校验配置与模型 → 拼 payload → 发请求 → 读响应 → 状态码/体积检查」抽成私有 `chat(ctx, model, prompt string) (raw []byte, model string, err error)`，返回原始响应体；`Optimize` 改为 `BuildPromptWithCustom` → `chat` → 解析 choices → `StripFence` → 空检查。**行为不变**，跑 `go test ./internal/ai/` 确认既有测试全绿再继续。
2. `quick.go`：

```go
// defaultQuickPrompt 是速记专用提示词（内建常量）。要求模型只输出一个 JSON 对象。
const defaultQuickPrompt = `你是本地笔记应用的速记整理助手。把下面的速记原文整理成一条简明、条理清晰的 Markdown 笔记。
要求：
1. 只输出一个 JSON 对象，不要任何其它文字、代码块或注释。
2. 格式必须是 {"title":"…","content":"…"}。
3. title 是一句不超过 30 字的概括标题。
4. content 是整理后的 Markdown 正文，保留要点、不编造、不超过 2000 字。
5. JSON 内的换行用 \n 转义。`

func buildQuickPrompt(content string) string {
    return defaultQuickPrompt + "\n\n【速记原文】\n" + content
}

func (c *Client) QuickNote(ctx context.Context, req Request) (QuickNoteResult, error) {
    raw, model, err := c.chat(ctx, req.Model, buildQuickPrompt(req.Content))
    if err != nil { return QuickNoteResult{}, err }
    raw = StripFence(raw)
    var out struct {
        Title   string `json:"title"`
        Content string `json:"content"`
    }
    if err := json.Unmarshal(raw, &out); err != nil {
        return QuickNoteResult{}, fmt.Errorf("AI 速记返回的不是 JSON 对象: %w", ErrBadResponse)
    }
    out.Title = strings.TrimSpace(out.Title)
    out.Content = strings.TrimSpace(out.Content)
    switch {
    case out.Title == "":
        return QuickNoteResult{}, QuickResultError{Reason: "AI 速记返回的标题是空的，已放弃保存（不截断）"}
    case utf8.RuneCountInString(out.Title) > maxQuickTitleRunes:
        return QuickNoteResult{}, QuickResultError{Reason: fmt.Sprintf("AI 速记的标题超过 %d 码点，已放弃保存（不截断）", maxQuickTitleRunes)}
    case out.Content == "":
        return QuickNoteResult{}, QuickResultError{Reason: "AI 速记返回的正文是空的，已放弃保存（不截断）"}
    case utf8.RuneCountInString(out.Content) > maxQuickContentRunes:
        return QuickNoteResult{}, QuickResultError{Reason: fmt.Sprintf("AI 速记的正文超过 %d 码点，已放弃保存（不截断）", maxQuickContentRunes)}
    }
    return QuickNoteResult{Title: out.Title, Content: out.Content, Model: model}, nil
}
```

（常量：`maxQuickTitleRunes = 60`、`maxQuickContentRunes = 2000`，放 `quick.go`。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/ai/ -v`
Expected: 新测试 + 既有 Optimize 测试全 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/ai/client.go internal/ai/quick.go internal/ai/quick_test.go
git commit -m "feat(ai): QuickNote 内建速记提示词，输出 JSON 标题+正文，不截断只报错"
```

---

## Task 3: web 端点 `POST /api/quick` + `POST /api/ai/quick`

**Files:** 新增 `internal/web/handlers_quick.go`；修改 `internal/web/server.go`；测试 `internal/web/quick_api_test.go`

**Interfaces:**

- Consumes: `store.CreateQuickNote`、`ai.QuickNote`、`config.ReadAI`、`writeNoteStoreError`、`writeOptimizeError`、`optimizeTimeout`、`decodeJSON`。
- Produces: `handleQuickNote` / `handleAIQuickNote`，注册到 `server.go` 路由。

- [ ] **Step 1: 写失败测试** `internal/web/quick_api_test.go`（用 `newTestHandler(t)` + `apiRequest` + `csrfTokenFor` + `configureAI(t, cfg, baseURL, models)` + `fakeUpstream`）：

```go
func TestQuickNoteCreatesWithAutoTitle(t *testing.T) {
    h, _ := newTestHandler(t)
    // POST /api/quick {content:"买牛奶"} + CSRF 头 → 201，body.title 匹配 ^速记-…Z$，
    // body.category_id == 2；再 GET /api/notes 能查到
}

func TestQuickNoteWithExplicitTitle(t *testing.T) { /* {content, title} → 201 用给定标题 */ }

func TestQuickNoteValidation(t *testing.T) {
    // content 空 / content 超 2000 / title 超 200 → 400 validation_failed + fields
}

func TestQuickNoteRequiresCSRF(t *testing.T) {
    // 不带 X-CSRF-Token → 403 csrf_token_missing
}

func TestQuickNoteTitleConflict(t *testing.T) { /* 显式 title 已存在 → 409 title_conflict */ }

func TestAIQuickNoteUnconfigured(t *testing.T) {
    // 未写 note.yaml → 409 ai_not_configured，消息含 ai.Missing() 文案
}

func TestAIQuickNoteHappyPath(t *testing.T) {
    cfg := configureAI(t, cfg, fakeUpstream(t, `{"choices":[{"message":{"content":"{\"title\":\"t\",\"content\":\"c\"}"}}]}`), "m1")
    // POST /api/ai/quick {content:"x"} → 200 {title:"t",content:"c",model:"m1"}；响应 Cache-Control 含 no-store
}

func TestAIQuickNoteDoesNotPersist(t *testing.T) {
    // AI 调用后 GET /api/notes 数量不变（只预览不落库）
}

func TestAIQuickNoteBadResultReason(t *testing.T) {
    // 上游返回 title 空 → 502 ai_bad_response，body.message 含「标题是空的」
}

func TestAIQuickNoteValidation(t *testing.T) { /* content 空/超长 → 400 */ }
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/web/ -run 'TestQuickNote|TestAIQuick' -v`
Expected: FAIL（handler 未注册 → 404）。

- [ ] **Step 3: 实现 `handlers_quick.go` + 注册路由**

1. 常量 `quickMaxBytes = 64 << 10`。
2. `handleQuickNote`：`decodeJSON` → 校验（content trim 非空 ≤2000 → `writeValidationError` `fields.content`；title 非空 ≤200 → `fields.title`）→ `store.CreateQuickNote` → 错误 `writeNoteStoreError`（复用，409 文案自动一致）→ `writeJSON(w, 201, note)`。
3. `handleAIQuickNote`：`decodeJSON` → content 校验同上 → `aiCfg, err := config.ReadAI(cfg.ConfigPath)`（err → 500）→ `client := ai.NewClient(aiCfg)`；`!client.Configured()` → `writeError(w, 409, "ai_not_configured", client.Missing())`（与 optimize 同文案）→ `ctx, cancel := context.WithTimeout(r.Context(), optimizeTimeout)` → `res, err := client.QuickNote(ctx, ai.Request{Content: content})` → 错误：`errors.As(err, &ai.QuickResultError{})` → `writeError(w, 502, "ai_bad_response", qe.Reason)`；否则 `writeOptimizeError(w, err)` → `writeJSON(w, 200, map[string]string{"title":…, "content":…, "model":…})`。
4. `server.go` 路由表加两行（POST 区，仿 `/api/ai/optimize`）：

```go
mux.HandleFunc("POST /api/quick", s.handleQuickNote)
mux.HandleFunc("POST /api/ai/quick", s.handleAIQuickNote)
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/web/ -v`
Expected: 新测试 + 既有全 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/web/handlers_quick.go internal/web/quick_api_test.go internal/web/server.go
git commit -m "feat(web): POST /api/quick 与 /api/ai/quick（AI 先预览不落库）"
```

---

## Task 4: 前端 lib `createQuickNote` / `aiQuickNote`

**Files:** 修改 `web/src/lib/notes.ts`、`web/src/lib/ai.ts`；测试 `web/src/lib/notes.test.ts`、`web/src/lib/ai.test.ts`

**Interfaces:**

- Consumes: `postJSON`（api.ts）、`NoteMeta`（notes.ts）、`ApiRequestError`。
- Produces: `createQuickNote(content: string, title?: string): Promise<NoteMeta>`；`aiQuickNote(content: string): Promise<QuickNoteResult>`（`QuickNoteResult = {title, content, model}`）。

- [ ] **Step 1: 写失败测试**

`notes.test.ts` 追加（仿 `createNote` 的 mock 模式）：

```ts
it("createQuickNote omits title when absent", async () => {
  // mock postJSON 捕获 payload → createQuickNote("买牛奶") → payload === {content:"买牛奶"}
});
it("createQuickNote sends title when given", async () => {
  // createQuickNote("买牛奶", "待办") → payload === {content:"买牛奶", title:"待办"}
});
```

`ai.test.ts` 追加：`aiQuickNote("买牛奶")` → `postJSON("/api/ai/quick", {content:"买牛奶"})` 且返回 `{title,content,model}`（`vi.hoisted` mock 里已有 postJSON 桩，注意复用 `sendJSON` 的 mock 方式）。

- [ ] **Step 2: 跑测试确认失败**

Run: `npm --prefix web test -- notes ai`
Expected: FAIL（函数不存在）。

- [ ] **Step 3: 实现**

- `notes.ts`：`export async function createQuickNote(content: string, title?: string): Promise<NoteMeta>` → `postJSON<NoteMeta>("/api/quick", title && title.trim() !== "" ? {content, title} : {content})`。
- `ai.ts`：`export interface QuickNoteResult { title: string; content: string; model: string }`；`export async function aiQuickNote(content: string): Promise<QuickNoteResult>` → `postJSON<QuickNoteResult>("/api/ai/quick", {content})`（仿 `optimizeNote`，无 model 字段）。

- [ ] **Step 4: 跑测试确认通过**

Run: `npm --prefix web test -- notes ai`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add web/src/lib/notes.ts web/src/lib/notes.test.ts web/src/lib/ai.ts web/src/lib/ai.test.ts
git commit -m "feat(web): lib 新增 createQuickNote 与 aiQuickNote"
```

---

## Task 5: HomeView 速记卡片 + AI 预览 + `HomeView.test.ts`

**Files:** 修改 `web/src/views/HomeView.svelte`；新增 `web/src/views/HomeView.test.ts`

**Interfaces:**

- Consumes: `createQuickNote`、`aiQuickNote`、`refreshStats`、`<Markdown/>`（`web/src/components/Markdown.svelte`）。
- Produces: 首页速记卡片（正文框＋「速记」「AI 速记」）、AI 预览面板（可编辑标题、Markdown 正文、模型名、「保存」「返回修改」）。

- [ ] **Step 1: 写失败测试** `HomeView.test.ts`（仿 `NoteEditorView.test.ts` 的 `vi.hoisted` 批量 mock：`listNotes`/`loadStats`/`createQuickNote`/`aiQuickNote`/`getSettings`/`navigate` + `flushSync`/`mount`/`unmount`）：

```ts
it("速记按钮直接落库并清空输入、刷新统计", async () => {
  // 输入 "买牛奶" → 点「速记」→ createQuickNote("买牛奶") 被调；输入框清空；loadStats 再次调用
});
it("AI 速记进入预览，改标题后保存用锁定的内容", async () => {
  // aiQuickNote resolve {title:"待办", content:"- 买牛奶", model:"m1"}
  // 预览出现：标题输入框值 "待办"、正文渲染、模型名 "m1"
  // 改标题为 "今日待办" → 点「保存」→ createQuickNote("- 买牛奶", "今日待办")（content 是预览锁定的）
});
it("返回修改不落库且正文保留", async () => {
  // AI 预览 → 点「返回修改」→ createQuickNote 未被调；输入框仍是原文
});
it("AI 未配置显示补齐提示", async () => {
  // aiQuickNote reject ApiRequestError{code:"ai_not_configured"} → 显示提示文案
});
it("正文为空时按钮禁用", async () => {
  /* 两按钮 disabled */
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `npm --prefix web test -- HomeView`
Expected: FAIL（测试文件不存在 / 组件无速记卡片）。

- [ ] **Step 3: 实现 `HomeView.svelte`**

1. 状态（`$state`）：`quickText`、`quickBusy`、`quickMessage`、`quickError`、`aiBusy`、`aiPreview`（`null | {title, content, model}`）、`aiTitle`、`aiError`；`let aiContent = $state("")` 等。
2. 搜索 form 之后插卡片（样式沿用现有卡片/`linklike` 类）：
   - `<textarea bind:value={quickText} maxlength={MAX_CONTENT_CODEPOINTS} placeholder="记一条速记…"/>`；
   - 「速记」`disabled={quickBusy || !quickText.trim()}` → `quick()`：
     ```ts
     quickBusy = true;
     quickError = quickMessage = "";
     try {
       const n = await createQuickNote(quickText.trim());
       quickText = "";
       quickMessage = `已记入速记「${n.title}」`;
       refreshStats();
     } catch (e) {
       quickError = fail(e);
     } finally {
       quickBusy = false;
     }
     ```
   - 「AI 速记」`disabled={aiBusy || !quickText.trim()}` → `quickAI()`：`aiPreview = null; aiError = ""; aiBusy = true;` → `const r = await aiQuickNote(quickText.trim())` → `aiPreview = r; aiTitle = r.title;`（`catch` → `aiError = fail(e)`，含 `ai_not_configured` 时文案「还没配置 AI，去信息页填写 ai.base_url / ai.api_key / ai.models」）；`finally aiBusy = false`。
   - AI 预览面板（`aiPreview` 非空）：标题输入框 `<input bind:value={aiTitle} maxlength={60}/>`、`<Markdown source={aiPreview.content}/>`、`模型：{aiPreview.model}`、「保存」→ `saveAI()`：
     ```ts
     quickBusy = true;
     try {
       const n = await createQuickNote(aiPreview.content, aiTitle.trim());
       quickText = "";
       aiPreview = null;
       quickMessage = `已记入速记「${n.title}」`;
       refreshStats();
     } catch (e) {
       quickError = fail(e);
     } finally {
       quickBusy = false;
     }
     ```
     「返回修改」→ `aiPreview = null`（正文保留在 `quickText`）。
3. `quickMessage`/`quickError` 用现有 muted/error 样式内联显示。

- [ ] **Step 4: 跑测试确认通过**

Run: `npm --prefix web test -- HomeView`，再 `npm --prefix web run check`
Expected: PASS、0 错 0 警。

- [ ] **Step 5: 提交**

```bash
git add web/src/views/HomeView.svelte web/src/views/HomeView.test.ts
git commit -m "feat(web): 首页速记卡片（直接速记 + AI 速记预览确认）"
```

---

## Task 6: CLI 解析 `note add <内容>` / `--ai`

**Files:** 修改 `internal/cli/parse.go`、`internal/cli/parse_test.go`、`internal/cli/usage.go`、`internal/cli/usage_test.go`

**Interfaces:**

- Consumes: `Options`、`addConfigFlags`、`ConfigOptions`。
- Produces: `AddOptions{Content string; AI bool}` 挂到 `Options.Add`；`parseAddArgs`。

- [ ] **Step 1: 写失败测试**

`parse_test.go` 改/加：

```go
// 改写既有 TestParseAddRejectsExtraArgs（add foo 现在合法）：
func TestParseAddContent(t *testing.T) {
    opts, cmd, err := parseArgs("add", "买牛奶")
    // cmd == CommandAdd，opts.Add.Content == "买牛奶"，opts.Add.AI == false
}
func TestParseAddContentWithAI(t *testing.T) {
    opts, _, err := parseArgs("add", "买牛奶", "--ai")
    // opts.Add.Content == "买牛奶"，opts.Add.AI == true
}
func TestParseAddAIWithoutContent(t *testing.T) { /* "add --ai" → 错误 */ }
func TestParseAddEmptyContent(t *testing.T) { /* "add", "" → 错误 */ }
func TestParseAddTooLongContent(t *testing.T) { /* strings.Repeat("😀", 2001) → 错误 */ }
func TestParseAddExtraPositional(t *testing.T) { /* "add", "a", "b" → 错误 */ }
func TestParseAddNoArgKeepsWizard(t *testing.T) { /* "add" → Content == "" */ }
```

`usage_test.go` 的 `TestUsageForAddDescribesThreeSteps` 追加：addUsage 含 `note add <正文>`、`--ai`、速记自动标题说明。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run 'TestParseAdd|TestUsageForAdd' -v`
Expected: FAIL（`add foo` 仍被旧校验拒绝）。

- [ ] **Step 3: 实现**

1. `Options` 加 `Add AddOptions`；`type AddOptions struct { Content string; AI bool }`（放 parse.go）。
2. 替换 `validateConfigOnlyArgs("add", …)` 为 `parseAddArgs(rest []string, cfg *ConfigOptions) (AddOptions, error)`：

```go
func parseAddArgs(rest []string, cfg *ConfigOptions) (AddOptions, error) {
    var o AddOptions
    for i := 0; i < len(rest); i++ {
        arg := rest[i]
        switch {
        case arg == "--ai":
            o.AI = true
        case arg == "-c" || arg == "--config" || arg == "--db":
            if i+1 >= len(rest) { return o, fmt.Errorf("缺少 %s 的参数", arg) }
            if err := setConfigPath(cfg, arg, rest[i+1]); err != nil { return o, err }
            i++
        case strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "--db="):
            if err := setConfigPath(cfg, arg[:strings.IndexByte(arg, '=')], strings.TrimPrefix(arg[strings.IndexByte(arg, '=')+1:], "=")); err != nil { return o, err }
        default:
            if strings.HasPrefix(arg, "-") { return o, fmt.Errorf("未知参数 %s", arg) }
            if o.Content != "" { return o, fmt.Errorf("note add 只接受一个正文参数（多词请用引号）") }
            o.Content = arg
        }
    }
    if o.Content != "" {
        if strings.TrimSpace(o.Content) == "" { return o, fmt.Errorf("正文不能为空") }
        if utf8.RuneCountInString(o.Content) > maxQuickContentRunes {
            return o, fmt.Errorf("正文最多 %d 码点", maxQuickContentRunes)
        }
    } else if o.AI {
        return o, fmt.Errorf("note add --ai 需要正文：note add <正文> --ai")
    }
    return o, nil
}
```

（若 parse.go 已有 `setConfigPath` 模式就复用；没有则仿 `validateConfigOnlyArgs` 里 `-c` 的处理。`maxQuickContentRunes = 2000` 已在 `internal/cli/quick.go`，parse.go 同包可直接用。）

3. `usage.go`：`addUsage()` 开头加两行快速记法（`note add <正文>` / `note add <正文> --ai`），删除「note add 不接受其它参数；脚本化创建笔记请用 POST /api/notes」；总帮助 `add` 行改为「新建笔记：无参数交互向导；带正文直接记速记；--ai 用 AI 整理」。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ -v`
Expected: 全 PASS（含改写后的旧断言）。

- [ ] **Step 5: 提交**

```bash
git add internal/cli/parse.go internal/cli/parse_test.go internal/cli/usage.go internal/cli/usage_test.go
git commit -m "feat(cli): note add <内容> 直接记速记，--ai 标记 AI 整理"
```

---

## Task 7: CLI 速记向导 `quick_add.go` + `cmd/note` 装配

**Files:** 新增 `internal/cli/quick_add.go` + `internal/cli/quick_add_test.go`；修改 `cmd/note/main.go`、`cmd/note/add.go`

**Interfaces:**

- Consumes: `RunAdd` 分派、`markdown.Render`、`IsTerminal`（cmd/note）、`st.CreateQuickNote`。
- Produces: `QuickAddDeps`、`RunAddQuick(ctx, deps, content, ai) (NoteRef, bool, error)`。

- [ ] **Step 1: 写失败测试** `internal/cli/quick_add_test.go`（仿 `add_test.go` 的 `newTestAddModel`/`press`/`feed`/`keyMsgFor` 模式，独立建 `quickAddModel`）：

```go
func TestRunAddQuickWithoutAI(t *testing.T) {
    var gotTitle string
    deps := QuickAddDeps{CreateQuick: func(_ context.Context, content, title string) (NoteRef, error) {
        gotTitle = title; return NoteRef{ID: "id1", Title: "速记-x", CategoryName: "速记"}, nil
    }}
    ref, saved, err := RunAddQuick(context.Background(), deps, "买牛奶", false)
    // saved == true、gotTitle == ""、err == nil
}

func TestQuickAddAIWithoutConfigShowsHint(t *testing.T) {
    m := newTestQuickAddModel(t, QuickAddDeps{QuickNote: nil, AIModel: "", AIHost: ""})
    // View() 含「还没配置 AI」；按 Esc → saved == false
}

func TestQuickAddAIAdoptsPreview(t *testing.T) {
    deps := QuickAddDeps{
        QuickNote: func(_ context.Context, c string) (QuickNoteResult, error) {
            return QuickNoteResult{Title: "待办", Content: "- 买牛奶", Model: "m1"}, nil
        },
        CreateQuick: func(_ context.Context, content, title string) (NoteRef, error) {
            // 断言 content == "- 买牛奶" && title == "待办"
            return NoteRef{ID: "id2", Title: "待办", CategoryName: "速记"}, nil
        },
        AIModel: "m1", AIHost: "api.example.com",
    }
    m := newTestQuickAddModel(t, deps)
    // Init 后 feed 完成的 command → 预览屏（标题只读、渲染正文、模型名）
    m = press(m, "enter") // 保存
    // saved == true；CreateQuick 被调一次
}

func TestQuickAddAIEscAbandons(t *testing.T) { /* Esc → saved == false、CreateQuick 未调 */ }

func TestQuickAddAIErrorShowsAndRetries(t *testing.T) {
    // QuickNote 返回 error("上游 500") → status 含错误；再 Ctrl+O 重试成功
}

func TestQuickAddAIEmptyContentRefused(t *testing.T) { /* content 空 → 错误（兜底） */ }
```

（`QuickNoteResult` 在 cli 包定义：`type QuickNoteResult struct{ Title, Content, Model string }`，由 `cmd/note` 从 `ai.QuickNote` 适配。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli/ -run 'TestRunAddQuick|TestQuickAddAI' -v`
Expected: FAIL（未定义）。

- [ ] **Step 3: 实现 `internal/cli/quick_add.go`**

1. 类型与分派：

```go
type QuickNoteResult struct{ Title, Content, Model string }
type QuickAddDeps struct {
    CreateQuick func(ctx context.Context, content, title string) (NoteRef, error)
    QuickNote   func(ctx context.Context, content string) (QuickNoteResult, error) // nil = 未配置
    AIModel     string
    AIHost      string
}
func RunAddQuick(ctx context.Context, deps QuickAddDeps, content string, ai bool) (NoteRef, bool, error) {
    content = strings.TrimSpace(content)
    if content == "" { return NoteRef{}, false, fmt.Errorf("正文不能为空") }
    if utf8.RuneCountInString(content) > maxQuickContentRunes {
        return NoteRef{}, false, fmt.Errorf("正文最多 %d 码点", maxQuickContentRunes)
    }
    if !ai {
        ref, err := deps.CreateQuick(ctx, content, "")
        return ref, err == nil, err
    }
    p := tea.NewProgram(quickAddModel{deps: deps, content: content, width: 80})
    m, err := p.Run()
    if err != nil { return NoteRef{}, false, err }
    qm := m.(quickAddModel)
    if qm.saved { return qm.ref, true, nil }
    return NoteRef{}, false, nil // 未保存（Esc 放弃）→ cmd 按既有约定退出 130
}
```

2. `quickAddModel`：字段 `deps`/`content`/`width`/`step`（`stepQuickWorking`/`stepQuickPreview`）/`status`/`busy`/`title`/`resultContent`/`model`/`rendered`/`view viewport.Model`/`saved`/`abandoned`/`err`；消息 `quickNoteFinishedMsg{Title, Content, Model string; Err error}`。
3. Init：`deps.QuickNote == nil` → `status = "还没配置 AI：note config set ai.base_url=… ai.api_key=… ai.models=…"`（等 Esc）；否则 `busy = true`、`status = fmt.Sprintf("正在用 %s 整理…（正文会发送到 %s）", deps.AIModel, deps.AIHost)` → `func() tea.Msg { r, err := deps.QuickNote(ctx, content); return quickNoteFinishedMsg{…} }`。
4. Update：`esc`（任意步）→ `abandoned = true`、`return m, tea.Quit`；`quickNoteFinishedMsg`：`busy=false`；`Err != nil` → `status = "AI 整理失败: " + truncate(err, …)`（留在原步，可 Ctrl+O 重试）；成功 → `title/resultContent/model` 填好，`markdown.Render(resultContent, markdown.Options{Width: m.width-…})`，`step = stepQuickPreview`、`status = ""`；预览步 `enter` → `saved=true; ref=deps.CreateQuick(ctx, resultContent, title)` → `tea.Quit`；`ctrl+o` → 重新 Init 整理。
5. View：工作步 = status + 帮助行（Esc 放弃）；预览步 = `header("AI 速记预览", model)` + 标题行（只读）+ `rendered` + help（`Enter 保存` / `Esc 放弃` / `Ctrl+O 重新整理`）。

**Step 3b: `cmd/note/add.go` 装配**：

```go
// runAdd 分派
if addOpts.Content == "" {
    // 现有向导路径（不变）
} else {
    return runAddQuick(ctx, st, cfg, addOpts)
}
```

`runAddQuick`：`!cli.IsTerminal(os.Stdin)` 且 `addOpts.AI` → `ErrNotTerminal` → 退出 1；deps：

```go
deps := cli.QuickAddDeps{
    CreateQuick: func(ctx context.Context, content, title string) (cli.NoteRef, error) {
        n, err := st.CreateQuickNote(ctx, content, title)
        if err != nil { return cli.NoteRef{}, err }
        return cli.NoteRef{ID: n.ID, Title: n.Title, CategoryName: st.MustCategoryName(n.CategoryID)}, nil
    },
}
if addOpts.AI {
    client := ai.NewClient(cfg.AI)
    deps.AIModel, deps.AIHost = client.DefaultModel(), client.Host()
    if client.Configured() {
        deps.QuickNote = func(ctx context.Context, content string) (cli.QuickNoteResult, error) {
            res, err := client.QuickNote(ctx, ai.Request{Content: content})
            if err != nil { return cli.QuickNoteResult{}, err }
            return cli.QuickNoteResult{Title: res.Title, Content: res.Content, Model: res.Model}, nil
        }
    }
}
ref, saved, err := cli.RunAddQuick(ctx, deps, addOpts.Content, addOpts.AI)
if err != nil {
    if errors.Is(err, cli.ErrNotTerminal) { fmt.Fprintln(os.Stderr, err); return exitError(1) }
    return err // 运行错误 → 退出 1
}
if !saved {
    fmt.Fprintln(os.Stderr, "已放弃，未新建笔记")
    return exitError(130)
}
fmt.Fprintf(os.Stderr, "已记入速记「%s」%s\n", ref.Title, linkFor(ref.ID))
```

（非 AI 成功提示走 stderr；stdout 干净。`cli.IsTerminal` 判断参考 `cmd/note/add.go` 现有 `IsTerminal(os.Stdin)`。`main.go`：`case cli.CommandAdd: return runAdd(opts.ConfigPath, opts.Args, opts.Add)`。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli/ ./cmd/note/ -v`
Expected: 全 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/cli/quick_add.go internal/cli/quick_add_test.go cmd/note/main.go cmd/note/add.go
git commit -m "feat(cli): note add <内容> 直落速记，--ai 预览确认后保存"
```

---

## Task 8: 端到端冒烟 + 文档（PRD v1.18 / TECH_DESIGN / CHANGELOG）

**Files:** 修改 `docs/PRD.md`、`docs/TECH_DESIGN.md`、`CHANGELOG.md`（不提交假上游脚本）

**Interfaces:**

- Consumes: 前七个任务的最终实现（文档里的名字必须与代码一致）。
- Produces: 无代码产出。

- [ ] **Step 1: 全量验证（先跑，再写文档）**

Run: `just test`，然后 `just check`
Expected: Go 全包 ok、Vitest 全绿、`go vet` 无输出、`svelte-check` 0 错 0 警。测试没过就**不要**往下写「已完成」。

- [ ] **Step 2: 用假上游做端到端冒烟**

仓库外临时目录放假上游（仿 optimize 计划 Task 9 Step 2，响应改成 `{"choices":[{"message":{"content":"{\"title\":\"待办\",\"content\":\"- 买牛奶\"}"}}]}`），`go run` 起来后用临时 `-c` 配置 `ai.base_url=http://127.0.0.1:18081/v1`、`ai.api_key=sk-test`、`ai.models=m1`。逐项确认并记入 CHANGELOG：

1. 网页首页：输入正文 → 点「速记」→ 卡片提示「已记入速记「速记-…」」，热力图/统计刷新；标题自动、落在「速记」分类。
2. 网页「AI 速记」→ 预览面板（标题可编辑、Markdown 正文、模型名 m1）→ 改标题 → 「保存」→ 落库；「返回修改」→ 正文保留、不落库。
3. `note add "买牛奶"`：stderr 提示、stdout 干净、退出 0；`note show` 可见。
4. `note add "买牛奶" --ai`：真终端预览（标题只读、渲染正文）→ Enter 保存 / Esc 退出 130。
5. 关掉假上游：网页 AI 速记显示 502 且不落库；`note add … --ai` status 显示错误、正文不变。
6. 未配 AI：网页按钮提示补齐；`note add … --ai` status 提示 `note config set …`。
7. 安全检索：`note.log`、终端输出、`curl -i` 响应体搜不到 `sk-test` 与正文；`POST /api/quick`、`POST /api/ai/quick` 响应含 `Cache-Control: no-store`；`/api/info` 不含速记正文。

- [ ] **Step 3: 写文档**

- PRD：`v1.18`，修订记录加一段；新增 FR（速记卡片 / AI 速记预览 / `note add <内容>` / `--ai`）；路由表加两行；**改写 AC-54**（`echo hi | note add` 直接建速记退出 0；`note add foo` 建速记；向导 Esc 130）；新增 AC（自动标题不覆盖、AI 预览确认、未配 AI 提示）；里程碑 M15。
- TECH_DESIGN：§1.2 不变量补速记口径（正文/标题/提示词不进日志）；§8 端点表加两行并注明 `POST /api/ai/quick` 不落库；目录树补 `internal/store/quick.go`、`internal/ai/quick.go`、`internal/web/handlers_quick.go`、`internal/cli/quick_add.go`；AI 段注明速记提示词为 `internal/ai` 内建常量、不是 `ai.prompt` 变体。
- CHANGELOG `[Unreleased] / Added`：按仓库既有密度写「做了什么 / 关键决策 / 验证」，含冒烟实测结果；只写已跑通的事实。

- [ ] **Step 4: 收尾验证**

Run: `just test`、`just check`
Expected: 全绿（写文档不引入代码变更，但要复跑确认工作区干净可交付）。

- [ ] **Step 5: 提交**

```bash
git add docs/PRD.md docs/TECH_DESIGN.md CHANGELOG.md docs/superpowers/specs/2026-11-16-quick-note-design.md docs/superpowers/plans/2026-11-16-quick-note.md
git commit -m "docs: 记录速记功能（PRD v1.18、TECH_DESIGN、CHANGELOG、设计文档与计划）"
```

---

## Self-Review

- **Spec coverage**：S1 后端（Task 1/2/3）、S2 前端（Task 4/5）、S3 CLI（Task 6/7）、S4 文档（Task 8）——无遗漏章节。
- **Step scan**：每个 Step 都可独立判定（测试名 + 断言、签名、命令、期望输出）；未出现「处理边界情况」这类空话。
- **Type consistency**：`store.CreateQuickNote` ↔ `web handleQuickNote` ↔ `notes.ts createQuickNote`（content/title）；`ai.QuickNote` ↔ `ai.ts aiQuickNote`（title/content/model）；`cli.QuickNoteResult` 字段与 `ai.QuickNoteResult` 对应（cmd/note 适配）。
- **Review Focus**：`{{content}}` 字面量（Task 2 提示词追加）、base_url 尾斜杠（Task 2 复用 chat）、同秒撞名（Task 1 重试 + Task 3 409）、旧断言（Task 6）、围栏/坏 JSON/超长（Task 2/3）、码点边界（Task 1/2/6/7）、前端锁定预览内容（Task 5）——每条都落到具体任务的测试。
- **Proportion**：计划与设计文档篇幅相当（设计约 600 行 / 本计划约 700 行），测试代码占主体，符合本仓库既有风格；没有替实现者写业务函数体（quick_add 的 view 细节留给实现）。
