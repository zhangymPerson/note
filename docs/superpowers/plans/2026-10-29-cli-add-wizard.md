# CLI 改造（`note` 打印帮助 + `note add` 向导）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 裸 `note` 打印 `--help` 内容；新增 `note add`——bubbletea 向导依次选分类（可就地新建）、填标题、写正文（多行，照 `gum write`），保存为明文笔记。

**Architecture:** 交互模型放在 `internal/cli/add.go`（一个 `tea.Program`、`step` 字段切步、`bubbles` 的 `textinput`/`textarea` 承担输入），只通过 `AddDeps` 三个函数接触外部，`internal/cli` 不 import `internal/store`；`cmd/note` 用 store 实现 `AddDeps` 并打印结果。原 `internal/cli/menu.go` 删除，裸 `note` 变纯帮助输出。

**Tech Stack:** Go 标准库 + `github.com/charmbracelet/bubbletea v1.3.10`（已有）+ `github.com/charmbracelet/bubbles`（新增，`textarea` / `textinput`）。

**Spec:** `docs/superpowers/specs/2026-10-29-cli-add-wizard-design.md`

## Global Constraints

- 校验口径与 store 完全一致，一律按**码点**（`utf8.RuneCountInString`）：标题 `1..200`、正文 `0..2000`、分类名 `1..30`。
- `internal/cli` **不得** import `internal/store` 或 `net/http`；持久层只经 `AddDeps` 注入。
- 唯一新增依赖是 `github.com/charmbracelet/bubbles`（纯 Go、CGO 关闭不变）；不引入 `lipgloss` 直接依赖（它是 bubbles 的间接依赖）。
- 退出码：成功 `0`、运行失败 `1`、参数错误 `2`、用户放弃 `130`。
- 中文文案直接照抄本计划中的字符串，不自行改写。
- 裸 `note` 的行为从「进交互菜单」改为「等价 `note --help`」，属破坏性变更，必须在 CHANGELOG 的 `Changed` 里写清迁移方式。
- 本次不动数据库结构、不动 Web 端任何行为、不碰加解密。
- `note add` 只创建明文笔记（`cipher` 为空），不读主密码。

## Review Focus

- **stdin 不是终端**（`echo x | note add`、CI）：立即报错退出 1，不挂起、不把管道当正文。→ Task 3 `TestRunAddRefusesNonTerminalStdin`
- **分类名 30 码点边界**：恰好 30 码点（含中文）能建，31 码点拒绝且停在原步。→ Task 2 `TestValidateCategoryNameRuneBoundaries`
- **正文 2000 码点边界**：恰好 2000（含 emoji）能保存，2001 被拒且不落库。→ Task 3 `TestContentRuneLimitBlocksSave`
- **中途放弃**：`Esc`/`Ctrl+C` 任何一步放弃都不产生笔记，退出码 130。→ Task 3 `TestEscAbandonsWithoutSaving`
- **过滤到空列表**：输入无匹配的过滤串后按 `Enter` 不能 panic、不能选中不存在的分类，光标停在「+ 新建分类…」。→ Task 2 `TestEmptyFilterResultFallsOnNewCategoryRow`

---

### Task 1: `note add` 的命令解析与帮助文本

**Files:**

- Modify: `internal/cli/parse.go`
- Modify: `internal/cli/usage.go`
- Test: `internal/cli/parse_test.go`
- Test: `internal/cli/usage_test.go`

**Interfaces:**

- Consumes: 无（沿用现有 `Parse` / `Usage` / `UsageFor`）
- Produces:
  - `CommandAdd`（`Command` 新枚举值，位于 `CommandInfo` 之后、`CommandHelp` 之前）
  - `UsageFor("add")` 返回 `addUsage()` 文本
  - `Parse` 行为：`note add` → `(CommandAdd, Options{}, nil)`；`note add -h` → `(CommandHelp, Options{Args: []string{"add"}}, nil)`；`note add foo` → 错误

- [ ] **Step 1: 写失败的解析测试**

`internal/cli/parse_test.go` 追加三例：

```go
func TestParseAddCommand(t *testing.T) {
	cmd, opts, err := Parse([]string{"add"})
	if err != nil || cmd != CommandAdd {
		t.Fatalf("Parse(add) = %v, %v; want CommandAdd", cmd, err)
	}
	if len(opts.Args) != 0 {
		t.Fatalf("add 不带参数，Args 应为空，得到 %v", opts.Args)
	}
}

func TestParseAddHelpFlag(t *testing.T) {
	cmd, opts, err := Parse([]string{"add", "-h"})
	if err != nil || cmd != CommandHelp {
		t.Fatalf("Parse(add -h) = %v, %v; want CommandHelp", cmd, err)
	}
	if len(opts.Args) != 1 || opts.Args[0] != "add" {
		t.Fatalf("帮助主题应为 [add]，得到 %v", opts.Args)
	}
}

func TestParseAddRejectsExtraArgs(t *testing.T) {
	if _, _, err := Parse([]string{"add", "foo"}); err == nil {
		t.Fatal("note add 不接受多余参数，应报错")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli -run 'TestParseAdd' -v`
Expected: FAIL（`undefined: CommandAdd`，或 `add` 被当作未知命令）

- [ ] **Step 3: 改 `parse.go`**

在 `Command` 枚举里加 `CommandAdd`（注释：`// CommandAdd 走交互向导新建一条明文笔记。`）；在 `case arg == "web" || arg == "info"` 分支旁新增 `add` 分支：取 `rest := args[i+1:]`，`containsHelpFlag(rest)` 为真则 `opts.Args = []string{"add"}` 并返回 `CommandHelp`；`len(rest) > 0` 时报错 `note add 不接受参数（用 note add --help 查看用法）`；否则返回 `CommandAdd`。同步更新文件头 `Parse` 的形态注释（加 `note add → CommandAdd` 一行）。

- [ ] **Step 4: 写失败的使用说明测试**

`internal/cli/usage_test.go` 追加：

```go
func TestUsageListsAddCommand(t *testing.T) {
	if !strings.Contains(Usage(), "add") {
		t.Fatal("总帮助的命令表里缺少 add")
	}
}

func TestUsageForAddDescribesThreeSteps(t *testing.T) {
	text := UsageFor("add")
	for _, want := range []string{"分类", "标题", "正文", "Ctrl+D"} {
		if !strings.Contains(text, want) {
			t.Fatalf("addUsage 缺少 %q", want)
		}
	}
}
```

- [ ] **Step 5: 跑测试确认失败**

Run: `go test ./internal/cli -run 'TestUsage(List|ForAdd)' -v`
Expected: FAIL（`addUsage` 不存在 / `add` 不在总帮助里）

- [ ] **Step 6: 改 `usage.go`**

- `Usage()` 命令表在 `info` 之后加一行：`  add      新建一条笔记（交互向导：选分类 → 标题 → 正文）`。
- `Usage()` 末尾把「不带命令运行 note 进入交互菜单。」改为「不带命令运行 note 等价于 `note help`（打印本帮助）。」。
- 新增 `func addUsage() string`，内容固定：

```
用法: note add [全局参数]

在交互式终端里依次完成三步，保存为一条明文笔记：
  1. 选择分类（输入即过滤；列表末尾可当场新建分类）
  2. 输入标题（1-200 字符）
  3. 输入正文（0-2000 码点，支持换行）

键位:
  分类步    ↑/↓ 选择 · 输入过滤 · Enter 确认 · Esc 放弃
  标题步    Enter 下一步 · Esc 放弃
  正文步    Enter 换行 · Ctrl+D 完成 · Ctrl+E 用 $EDITOR 编辑 · Esc 放弃

全局参数:
  -c, --config <路径>    配置文件路径
  --db <路径>            SQLite 数据库路径
  -h, --help             显示本帮助

note add 不接受其它参数；脚本化创建笔记请用 POST /api/notes。
stdin 不是终端时报错退出，不会挂起等待输入。
```

- `UsageFor` 的 `switch` 加 `case "add": return addUsage()`。

- [ ] **Step 7: 跑测试确认通过**

Run: `go test ./internal/cli -v && go build ./...`
Expected: 全 PASS，编译通过（此时 `menu.go` 仍在，裸 `note` 行为未变）

- [ ] **Step 8: 提交**

```bash
git add internal/cli/parse.go internal/cli/parse_test.go internal/cli/usage.go internal/cli/usage_test.go
git commit -m "feat(cli): 新增 note add 的命令解析与帮助文本"
```

---

### Task 2: 向导模型——分类步（过滤 + 就地新建分类）

**Files:**

- Create: `internal/cli/add.go`
- Test: `internal/cli/add_test.go`
- Modify: `internal/cli/usage.go`（仅在 Step 6 发现漏字时）

**Interfaces:**

- Consumes: `CommandAdd`（Task 1，不在本任务使用）
- Produces（后续任务与 `cmd/note` 依赖，名字与类型不得改）：

```go
type Category struct {
	ID        int64
	Name      string
	Count     int
	IsDefault bool // 未分类：落库时 category_id 存 NULL
}

type AddNote struct {
	Title      string
	Content    string
	CategoryID *int64 // nil 表示未分类
}

type NoteRef struct {
	ID           string
	Title        string
	CategoryName string
}

type AddDeps struct {
	Categories     func(ctx context.Context) ([]Category, error)
	CreateCategory func(ctx context.Context, name string) (Category, error)
	CreateNote     func(ctx context.Context, in AddNote) (NoteRef, error)
}

const (
	maxAddCategoryRunes = 30
	maxAddTitleRunes    = 200
	maxAddContentRunes  = 2000
)

func filterCategories(all []Category, query string) []Category
func validateCategoryName(name string) error // 空/超 30 码点返回错误，文案见下
```

- [ ] **Step 1: 写失败的测试**

`internal/cli/add_test.go`（`newTestAddModel` 是本任务产出的测试助手，接收假 `AddDeps`）：

```go
func TestFilterCategoriesMatchesIgnoringCase(t *testing.T) {
	all := []Category{{ID: 1, Name: "未分类", IsDefault: true}, {ID: 2, Name: "工作"}, {ID: 3, Name: "读书笔记"}}
	if got := filterCategories(all, "读"); len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("过滤「读」应只剩读书笔记，得到 %+v", got)
	}
	if got := filterCategories(all, "工作"); len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("过滤「工作」应只剩工作，得到 %+v", got)
	}
	if got := filterCategories(all, ""); len(got) != 3 {
		t.Fatalf("空过滤应返回全部，得到 %d 条", len(got))
	}
}

func TestValidateCategoryNameRuneBoundaries(t *testing.T) {
	ok := strings.Repeat("笔", 30)
	if err := validateCategoryName(ok); err != nil {
		t.Fatalf("30 码点应通过，得到 %v", err)
	}
	if err := validateCategoryName(strings.Repeat("笔", 31)); err == nil {
		t.Fatal("31 码点应被拒绝")
	}
	if err := validateCategoryName("   "); err == nil {
		t.Fatal("空白分类名应被拒绝")
	}
}

func TestNewCategorySuccessReturnsToCategoryAndSelectsIt(t *testing.T) {
	deps := fakeDeps()
	m := newTestAddModel(deps)
	m = pressKeys(m, "ctrl+n") // 见 Step 3：这组用例统一用 "down" 跳到「+ 新建分类」行也行
	...
}
```

三个分类步用例的**断言**（写法自定，必须覆盖）：

1. `TestNewCategorySuccessReturnsToCategoryAndSelectsIt`：`step` 从 `stepNewCategory` 回到 `stepCategory`，`m.categories` 含新分类，`m.selected()` 的 `ID` 等于 `CreateCategory` 返回的 ID，`m.createErr == ""`。
2. `TestNewCategoryRejectionKeepsStep`：`CreateCategory` 返回错误（用 `errors.New("分类「工作」已存在")`）时，`step` 仍为 `stepNewCategory`，`m.createErr` 非空，`m.categories` 长度不变。
3. `TestEmptyFilterResultFallsOnNewCategoryRow`：过滤串无匹配（`filterCategories` 结果为空）时 `m.cursor` 落在「+ 新建分类…」行（`m.cursor == len(m.filtered)`），此时按 `Enter` 进入 `stepNewCategory` 且不 panic。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli -run 'TestFilter|TestValidateCategory|TestNewCategory|TestEmptyFilter' -v`
Expected: FAIL（`undefined: filterCategories` 等）

- [ ] **Step 3: 实现 `add.go` 的分类步**

结构固定为：

```go
type addStep int

const (
	stepCategory addStep = iota
	stepNewCategory
	stepTitle
	stepContent
)

type addModel struct {
	deps    AddDeps
	step    addStep
	all     []Category
	filtered []Category
	filter  string
	cursor  int // 0..len(filtered)（末尾一行是「+ 新建分类…」）
	create  textinput.Model
	createErr string
	title   textinput.Model
	titleErr string
	body    textarea.Model
	status  string // 底部一次性提示（保存失败等）
	ref     NoteRef
	saved   bool
	width   int
	height  int
}
```

要点：

- `filterCategories`：`strings.Contains(strings.ToLower(c.Name), strings.ToLower(strings.TrimSpace(query)))`，保持原顺序。
- `validateCategoryName`：trim 后空 → `fmt.Errorf("分类名不能为空")`；`utf8.RuneCountInString > 30` → `fmt.Errorf("分类名最多 30 个字符")`。
- `Update` 的分类步：`↑`/`↓` 与 `ctrl+k`/`ctrl+j` 移动（`j`/`k` 保留给过滤输入）；`Backspace` 删过滤串末字符并重算 `filtered`、把 `cursor` 夹到 `[0, len(filtered)]`；可打印字符（`msg.Type == tea.KeyRunes`）追加到过滤串；`Enter` 时若 `cursor == len(filtered)` 进 `stepNewCategory`（并把 `create` 复位为空、聚焦），否则记录所选分类进 `m.selectedID` 并进 `stepTitle`。
- 新建分类步：`Enter` → `validateCategoryName` → 失败填 `createErr`；成功调 `deps.CreateCategory`，失败填 `createErr`（原样 `err.Error()`），成功则 `append(m.all, c)`、清空过滤串、把 `cursor` 指到该分类、回 `stepCategory`。`Esc` 回 `stepCategory`（清空 `createErr`，不算放弃向导）。
- `View` 按设计文档 3.2 / 3.3 的版面渲染：标题行 `note add · 选择分类（1/3）`（有过滤时右侧 `  过滤：<query>▌`），每行 `  > 名称` + 右侧 `N 篇`，末尾恒显 `    + 新建分类…`（选中时前缀 `> `），底部键位提示 `输入即过滤 · ↑/↓ 选择 · Enter 确认 · Esc 放弃`。
- `newAddModel(deps AddDeps) addModel`：初始化 `textinput`（`CharLimit: maxAddCategoryRunes`, `Prompt: "  > "`）与 `textarea`，`create.Placeholder = "分类名"`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli -run 'TestFilter|TestValidateCategory|TestNewCategory|TestEmptyFilter' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/cli/add.go internal/cli/add_test.go
git commit -m "feat(cli): note add 的分类步（即时过滤 + 就地新建分类）"
```

---

### Task 3: 向导模型——标题步、正文步、保存与放弃

**Files:**

- Modify: `internal/cli/add.go`
- Test: `internal/cli/add_test.go`

**Interfaces:**

- Consumes: `AddDeps` / `AddNote` / `NoteRef` / `addModel`（Task 2）
- Produces:
  - `func RunAdd(ctx context.Context, deps AddDeps) (NoteRef, bool, error)`——返回值 `(保存的笔记, 是否已保存, 错误)`；用户放弃 → `(NoteRef{}, false, nil)`
  - `var ErrNotTerminal = errors.New(...)`（文案 `note add 需要在交互式终端里运行`）
  - `func buildAddNote(m addModel) AddNote`（纯函数：未分类 → `CategoryID == nil`）

- [ ] **Step 1: 写失败的测试（4 例 + 2 例边界）**

```go
func TestContentSavesWithSelectedCategory(t *testing.T)      // 选「工作」→ 正文按 ctrl+d → 捕获 AddNote.CategoryID 指向 2
func TestContentSavesDefaultAsNilCategory(t *testing.T)       // 选「未分类」→ CategoryID == nil
func TestContentRuneLimitBlocksSave(t *testing.T)             // 2000 码点（含 emoji）可保存；2001 拒绝且 CreateNote 未被调用
func TestEscAbandonsWithoutSaving(t *testing.T)               // 正文步按 esc → saved=false、CreateNote 未被调用
func TestRunAddRefusesNonTerminalStdin(t *testing.T)          // go test 下 stdin 不是终端 → RunAdd 返回 ErrNotTerminal，秒返回
func TestBuildAddNoteDropsDefaultCategoryID(t *testing.T)     // IsDefault 的分类 → nil；其它 → 指针等于其 ID
```

标题步：

```go
func TestTitleStepRequiresNonEmpty(t *testing.T) {
	m := newTestAddModel(fakeDeps())
	m = m.pickCategory(t, "工作")       // 进入 stepTitle
	m = typeText(m, "   ")
	m = pressKeys(m, "enter")
	if m.step != stepTitle {
		t.Fatalf("空标题应停在标题步，当前 %v", m.step)
	}
	if m.titleErr == "" {
		t.Fatal("空标题应给出回显")
	}
	m = typeText(m, "会议纪要")
	m = pressKeys(m, "enter")
	if m.step != stepContent {
		t.Fatalf("合法标题应进入正文步，当前 %v", m.step)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/cli -run 'TestContent|TestTitle|TestEsc|TestRunAdd|TestBuildAddNote' -v`
Expected: FAIL

- [ ] **Step 3: 实现标题 / 正文 / 保存 / 放弃**

要点：

- 标题步：`Enter` → `strings.TrimSpace(titleInput.Value())` 为空则 `titleErr = "标题不能为空"` 并停留；否则 `titleErr = ""`、进 `stepContent`、`body.Focus()`。
- 正文步：`textarea` 宽度 `min(m.width-4, 80)`、下限 20；高度 10（`m.height < 14` 时 5）。`Ctrl+D` → 用 `utf8.RuneCountInString(body.Value()) > maxAddContentRunes` 判定，超限则 `status = "正文最多 2000 码点"` 且不保存；否则调用 `deps.CreateNote`（参数由 `buildAddNote` 产生，`Content` 原样保留换行、不 trim），失败 `status = "新建笔记失败: " + err.Error()`、停在 `stepContent`；成功记录 `m.ref`、`m.saved = true`、返回 `tea.Quit`。
- `Ctrl+E`：`os.Getenv("EDITOR")` 空则 `status = "未设置 $EDITOR，无法外接编辑"`；否则写临时文件（`os.CreateTemp("", "note-add-*.md")`）、`tea.ExecProcess(exec.Command(editor, path), …)`，回调里把文件内容读回 `body.SetValue` 并删临时文件。
- `Esc` / `Ctrl+C`（任何步，`stepNewCategory` 除外——那里 `Esc` 只回上一步）：`m.saved = false`、返回 `tea.Quit`。
- `RunAdd`：

```go
func RunAdd(ctx context.Context, deps AddDeps) (NoteRef, bool, error) {
	if !isTerminal(os.Stdin) {
		return NoteRef{}, false, ErrNotTerminal
	}
	cats, err := deps.Categories(ctx)
	if err != nil {
		return NoteRef{}, false, err
	}
	m := newAddModel(deps)
	m.all = cats
	m.filtered = cats
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return NoteRef{}, false, err
	}
	fm, _ := final.(addModel)
	return fm.ref, fm.saved, nil
}
```

（`isTerminal` 沿用 `menu.go` 的实现；Task 4 删 `menu.go` 时把它移到 `add.go`。）

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/cli -v`
Expected: 全 PASS（含 Task 2 的用例）

- [ ] **Step 5: 提交**

```bash
git add internal/cli/add.go internal/cli/add_test.go
git commit -m "feat(cli): note add 的标题步、正文步与保存/放弃流程"
```

---

### Task 4: `cmd/note` 接线，删除旧菜单

**Files:**

- Modify: `cmd/note/main.go`
- Delete: `internal/cli/menu.go`、`internal/cli/menu_test.go`
- Modify: `internal/store/category.go`（导出 `DefaultCategoryID`）

**Interfaces:**

- Consumes: `cli.CommandAdd`、`cli.RunAdd`、`cli.AddDeps`、`cli.Category`、`cli.AddNote`、`cli.NoteRef`
- Produces: `store.DefaultCategoryID`（导出别名，值等于现有未导出的 `defaultCategoryID`）

- [ ] **Step 1: 加依赖并确认能编译**

```bash
go get github.com/charmbracelet/bubbles@v0.21.0
go mod tidy
go build ./...
```

Expected: 编译成功（若报版本不兼容，退到 `v0.20.0` 并在提交信息里注明）。把 `bubbles` 从 indirect 挪到 `go.mod` 的第一个 `require` 块（手工整理，`go mod tidy` 不会自动分组）。

- [ ] **Step 2: 导出默认分类 id**

`internal/store/category.go` 里 `defaultCategoryID` 之上加：

```go
// DefaultCategoryID 是内置「未分类」分类的 id；Web 端把它落库成 category_id = NULL，
// CLI 也用同一判断，避免两边对"未分类"的理解分叉。
const DefaultCategoryID = defaultCategoryID
```

- [ ] **Step 3: 改 `cmd/note/main.go`**

- `switch` 里 `case cli.CommandMenu:` 的原有分支改为 `fmt.Print(cli.Usage())` 并 `return nil`（删掉 `runMenu` 整段）。
- 新增 `case cli.CommandAdd:` → `return runAdd(globalConfigPath, args)`。
- 新增：

```go
// runAdd 打开库并跑 note add 向导；用户放弃（saved=false）时以 130 退出。
func runAdd(globalConfigPath string, args []string) error { … }
```

要点：`loadConfig(globalConfigPath, args)` → `store.Open(cfg.DBPath)`（`defer st.Close()`）→ 构造 `cli.AddDeps` 三个函数（`Categories` 里 `IsDefault: c.ID == store.DefaultCategoryID`；`CreateNote` 里 `Content: &in.Content`、`CategoryID: in.CategoryID`）→ `ref, saved, err := cli.RunAdd(ctx, deps)`；`err != nil && errors.Is(err, cli.ErrNotTerminal)` → 返回该错误（由 main 统一打到 stderr、退出码 1）；`!saved` → 打印 `已放弃，未新建笔记` 到 stderr 并返回 `errCancelled`（新增哨兵，`main` 里映射退出码 130）；否则打印设计文档 3.6 的四行输出。

- [ ] **Step 4: 删除旧菜单并确保构建与测试通过**

```bash
git rm internal/cli/menu.go internal/cli/menu_test.go
go build ./... && go vet ./... && go test ./...
```

Expected: 全绿（`isTerminal` 已移入 `add.go`，无残留引用）

- [ ] **Step 5: 手工验收（真终端）**

1. `just build` → 在 Git Bash 里跑 `./note.exe`：输出 `note --help` 全文，退出码 0（`echo $?`）。
2. `./note.exe add`：三步走完（选「未分类」→ 标题「向导验收」→ 正文三行含换行 → `Ctrl+D`），打印四行结果；`./note.exe web` 起服务后 Web 端能看到该笔记且正文换行保留。
3. 再跑一次 `./note.exe add`，分类步就地新建「CLI 验收」分类 → 保存后该分类出现在 Web 端侧栏。
4. `echo hi | ./note.exe add`：立刻报错退出 1，不挂起。
5. `./note.exe add -h` 与 `./note.exe help add` 输出同一份帮助。
6. `./note.exe add foo`：退出码 2。
7. 向导中途 `Esc`：打印「已放弃，未新建笔记」，`echo $?` 为 130，库中笔记数不变。

- [ ] **Step 6: 提交**

```bash
git add cmd/note/main.go internal/store/category.go go.mod go.sum
git commit -m "feat(note): note add 接线；裸 note 改为打印帮助并移除交互菜单"
```

---

### Task 5: 文档与总闸门

**Files:**

- Modify: `AGENTS.md:11`
- Modify: `docs/PRD.md`（修订说明、目录树 `cli/` 注释、CLI 命令表、里程碑）
- Modify: `docs/TECH_DESIGN.md`（目录树 113 行、依赖表 201 行、CLI 命令表 227 行、CLI 小节 1524 行、ADR-20 1649 行 + 新 ADR）
- Modify: `CHANGELOG.md`
- Modify: `README.md`（仅在确有菜单说明时）

- [ ] **Step 1: 按设计文档第 7 节逐条改文档**

关键替换（照抄，别改口径）：

- `AGENTS.md`：`裸 note 进交互菜单（bubbletea）` → `裸 note 等价于 note --help（打印帮助）；note add 走 bubbletea 向导新建明文笔记（选分类 → 标题 → 正文）`。
- `docs/TECH_DESIGN.md:113`（目录树）：`menu.go # bubbletea 交互菜单…` → `add.go # note add 向导（bubbletea + bubbles；模型可单测；非终端直接报错）`。
- `docs/TECH_DESIGN.md:201`（依赖表）：行标题 `CLI 菜单` → `CLI 向导`，包列补 `bubbles`（textarea/textinput），用途列写 `note add 三步向导`。
- `docs/TECH_DESIGN.md:227`：`note` 行改为「打印帮助（等价 note --help）」，并新增 `note add` 行。
- `docs/TECH_DESIGN.md:1524` 与 `1649`（ADR-20）：把"bubbletea 只承担裸 note 的交互菜单"改写为"bubbletea 只承担 `note add` 向导"，ADR-20 的迁移说明补一句裸 `note` 已改为帮助输出；新增 ADR：`CLI 多行正文用 bubbles/textarea 而非手写编辑器`（理由：软换行/CJK 宽度/粘贴/外接编辑器；代价：多一个纯 Go 依赖）。
- `docs/PRD.md`：修订说明加 `v1.9：CLI 改造——裸 note 打印帮助、新增 note add 交互向导（选分类/标题/正文，正文支持换行）`；CLI 命令表加 `note add` 一行并把 `note` 一行改为帮助；里程碑表补一条 M9（或并入既有版本行）。
- `CHANGELOG.md` `[Unreleased]`：`### Changed`（裸 `note` 不再进菜单，迁移：用 `note web start`）+ `### Added`（`note add` 三步向导、就地新建分类、`Ctrl+E` 外接编辑器、`note add -h`）+ `### Removed`（`internal/cli/menu.go`）。

- [ ] **Step 2: 跑总闸门**

```bash
just test && just check && just format
git diff --stat   # format 后若只动了排序/空行则照常提交
```

Expected: `just test` Go 全绿 + 前端全绿；`just check` 0 error 0 warning。

- [ ] **Step 3: 提交**

```bash
git add -A && git commit -m "docs: 补充 note add 向导与裸 note 行为变更"
```

---

## Self-Review 记录

- **Spec 覆盖**：§1 目标 1（裸 `note` 帮助）→ Task 4 Step 3；目标 2（三步向导）→ Task 2/3；目标 3（删菜单）→ Task 4 Step 4；§3.2–3.5 键位 → Task 2/3；§3.6 输出 → Task 4 Step 3；§5 边界表 → Review Focus 五条；§7 文档 → Task 5。
- **依赖顺序**：Task 1 不触碰 `menu.go`，Task 2/3 只加新文件，Task 4 才切换 `main` 并删菜单——每个任务结束时 `go build ./...` 都能过。
- **类型一致**：`AddDeps` / `AddNote` / `NoteRef` / `Category` 在 Task 2 定义一次，Task 3/4 只引用，不再改名。
