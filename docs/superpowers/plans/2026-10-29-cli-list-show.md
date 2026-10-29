# CLI 阅读路径（`note list` / `note show` / `note add` 分类步统一选择器）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 CLI 能读笔记——`note list` 进一个 fuzzy 筛选列表、`note show <id>` 在终端渲染 Markdown，并把 `note add` 分类步的手写过滤列表换成同一个选择器。

**Architecture:** 新增 `internal/markdown`（唯一触碰 glamour 的薄封装）与 `internal/cli/pick.go`（包一层 `bubbles/list`，补上 gum filter 的键位语义）。`internal/cli` 继续只做纯逻辑与交互，通过 `ListDeps`/`ShowDeps` 注入数据，`cmd/note` 负责开库、探测终端、打印。store 侧只加一个不分页的 `ListNotesAll`，与既有 `ListNotes` 共用查询构造。

**Tech Stack:** Go 1.26（`CGO_ENABLED=0`）、bubbletea v1.3.10、bubbles v1.0.0（新增用 `list` 子包）、lipgloss（会被抬到伪版本）、glamour v1.0.0、`github.com/charmbracelet/x/term`、`text/tabwriter`、`github.com/rivo/uniseg`。

**Spec:** `docs/superpowers/specs/2026-10-29-cli-list-show-design.md`

## Global Constraints

- Go 版本按 `go.mod`（`go 1.26.0`）；构建全程 `CGO_ENABLED=0`（`justfile` 已 export）。
- 只允许新增这两个直接依赖：`github.com/charmbracelet/glamour v1.0.0`、`github.com/charmbracelet/x/term`；`bubbles/list` 来自已有依赖。`github.com/charmbracelet/lipgloss` 会被 glamour 抬到 `v1.1.1-0.20250404203927-76690c660834`，不得手工降级或另选版本。
- `internal/cli` 不得 import `internal/store`；`internal/markdown` 不得 import `internal/cli` 或 `internal/store`。
- 退出码固定：0 正常（含 `note list` 未选择就退出）、1 运行时失败、2 参数错误、4 命中加密笔记。3 已被 `note web status` 占用，不得复用。
- 时间戳一律 UTC 秒级文本 `2006-01-02T15:04:05Z`；`note list --plain` 原样输出它，不做时区转换（只有 TUI 行按本机时区显示到分钟）。
- 非 TTY 输出不得含 ANSI 转义；唯一例外是用户显式给了 `--style`。
- `note list` 非交互路径与 `--plain` 在结果为空时不得输出任何字节（`wc -l` 必须是 0）。
- 不改 `internal/web`、不加 HTTP 接口、不改 `sql/` 下任何迁移文件。
- 每步改完跑 `just test` 与 `just check`；CHANGELOG 只记已经验证通过的事实（AGENTS.md 规则 1-3）。
- 注释与用户可见文案用中文；不写 emoji；注释只解释「为什么」，不写「做了什么」。

## Review Focus

以下 6 类是最容易出事、而单看某个任务的测试不一定覆盖的地方。每条都在下面的任务里指定了归属测试。

1. **过滤态下的按键路由**：`bubbles/list` 的过滤是模态的，`Tab`/`Shift+Tab` 会把它推进 `FilterApplied` 态、字母键在浏览态是导航键、空格是 `KeySpace` 而不是 `KeyRunes`。任何一条漏掉都会让「敲字即筛选」失效或让 `Esc` 语义漂移。→ Task 3、Task 8。
2. **加密笔记的输出形态与退出码**：TTY/非 TTY、`--raw` 三种组合下的头部与提示行，以及必须为 4 的退出码。→ Task 6、Task 7。
3. **管道污染**：空结果输出零字节、非 TTY 不打印头部、默认不带 ANSI。→ Task 5、Task 6。
4. **列表行高被内容撑坏**：库里存的是原始正文，摘要可能含换行、`#`、`-`、宽字符与 emoji。→ Task 5。
5. **`note add` 回归**：lipgloss 抬版本后的观感，以及新建分类后光标是否落在新分类上。→ Task 1（既有用例）、Task 8。
6. **大库**：全量读入是设计选择，必须证明它真的不分页（`ListNotes` 每页只有 20 条）。→ Task 2。

---

### Task 1: 依赖落地与 `internal/markdown` 渲染包

**Files:**
- Modify: `go.mod`、`go.sum`
- Create: `internal/markdown/render.go`
- Test: `internal/markdown/render_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `markdown.Options{Width int; Style string}`、`markdown.Render(src string, opts Options) (string, error)`

- [ ] **Step 1: 拉起依赖并确认既有测试仍然全绿**

```bash
go get github.com/charmbracelet/glamour@v1.0.0
go get github.com/charmbracelet/x/term
go mod tidy
go test ./internal/cli/... ./internal/store/...
```

Expected: 全部 PASS。`go.mod` 里 `github.com/charmbracelet/lipgloss` 变成 `v1.1.1-0.20250404203927-76690c660834`（glamour 的 `ansi/table.go` 要求）。若 `internal/cli` 有用例失败，说明 lipgloss 抬版本改了观感，先停下来报告，不要改测试去迁就。

- [ ] **Step 2: 写失败的测试**

```go
package markdown

import (
	"strings"
	"testing"
)

func TestRenderNottyHasNoEscapeSequences(t *testing.T) {
	out, err := Render("# 会议纪要\n\n- 先说结论\n- 再说细节\n", Options{Style: "notty", Width: 80})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("notty 风格不该出现 ANSI 转义：%q", out)
	}
	if !strings.Contains(out, "会议纪要") || !strings.Contains(out, "先说结论") {
		t.Fatalf("正文被吞了：%q", out)
	}
}

func TestRenderKeepsCodeBlockContent(t *testing.T) {
	out, err := Render("```go\nfmt.Println(1)\n```\n", Options{Style: "notty", Width: 80})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "fmt.Println(1)") {
		t.Fatalf("代码块内容丢失：%q", out)
	}
}

func TestRenderRejectsUnknownStyle(t *testing.T) {
	if _, err := Render("hi\n", Options{Style: "nope"}); err == nil {
		t.Fatal("未知风格应当报错，而不是静默回退")
	}
}

func TestRenderTrimsTrailingNewlines(t *testing.T) {
	out, err := Render("一句话\n", Options{Style: "notty", Width: 80})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.HasSuffix(out, "\n") {
		t.Fatalf("Render 返回的文本不该带结尾换行（由调用方决定怎么打印）：%q", out)
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./internal/markdown/ -v`
Expected: 编译失败，`undefined: Render` / `undefined: Options`。

- [ ] **Step 4: 实现 `internal/markdown/render.go`**

签名固定为 `func Render(src string, opts Options) (string, error)`，`Options{Width int; Style string}`。实现要点：

- `opts.Style` 为空串时按 `"auto"` 处理；风格名直接交给 `glamour.WithStandardStyle`，让未知风格名以 error 形式冒出来（不要自己维护一份风格名白名单，白名单在 `cli` 的参数校验里）。
- `opts.Width > 0` 时才调用 `glamour.WithWordWrap(opts.Width)`。**不要**传 0：glamour 的 `ansi.BlockStack.Width` 在 `WordWrap` 小于缩进+边距时返回 0，会走到它内部宽度为 0 的路径。
- 返回前 `strings.TrimRight(out, "\n")`（末尾换行由调用方决定）。
- 包注释说明：这是全项目唯一 import glamour 的地方，`note show` 与列表预览都从这里出。

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/markdown/ -v`
Expected: PASS（4 个用例）。

- [ ] **Step 6: 提交**

```bash
git add go.mod go.sum internal/markdown
git commit -m "feat(cli): 引入 glow 的渲染内核 glamour 并提供 markdown 渲染封装"
```

---

### Task 2: `store.ListNotesAll`（共用查询构造 + 全量读取）

**Files:**
- Modify: `internal/store/note.go`
- Test: `internal/store/note_test.go`

**Interfaces:**
- Consumes: 既有 `NoteFilter`、`NoteListItem`、`listSelectSQL`、`truncateRunes`、`summaryRunes`
- Produces: `func (s *Store) ListNotesAll(ctx context.Context, deleted bool) ([]NoteListItem, error)`；内部 `buildNoteQuery(filter NoteFilter) (clause, orderBy string, args []any)`、`scanNoteItems(rows *sql.Rows) ([]NoteListItem, error)`

- [ ] **Step 1: 写失败的测试**

```go
func TestListNotesAllReturnsEveryNote(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 60; i++ { // 远超 ListNotes 的每页 20 条
		body := fmt.Sprintf("正文 %d", i)
		if _, err := st.CreateNote(ctx, NoteInput{Title: fmt.Sprintf("笔记 %02d", i), Content: &body}); err != nil {
			t.Fatalf("CreateNote: %v", err)
		}
	}
	all, err := st.ListNotesAll(ctx, false)
	if err != nil {
		t.Fatalf("ListNotesAll: %v", err)
	}
	if len(all) != 60 {
		t.Fatalf("ListNotesAll 必须不分页，得到 %d 条", len(all))
	}
}

func TestListNotesAllOrdersByUpdatedAtDesc(t *testing.T) {
	// 建三条，逐条 UpdateNote 改 updated_at 顺序，断言结果按 updated_at DESC, id DESC
}

func TestListNotesAllTrashOnlyWhenDeleted(t *testing.T) {
	// 建两条，SoftDeleteNote 其中一条：
	// ListNotesAll(ctx, false) 只含未删的那条；ListNotesAll(ctx, true) 只含被删的那条
}

func TestListNotesAllEncryptedHasNoSummary(t *testing.T) {
	// 用 NoteInput{CipherB64: ..., KDFParams: ...} 建加密笔记
	// 断言 item.Encrypted 为 true 且 item.Content 为 nil（与 ListNotes 同口径）
}

func TestListNotesAllDoesNotRecordActivity(t *testing.T) {
	// 建几条后记录 activities 的 count，调 ListNotesAll，断言 count 不变
	// （ListNotes 在 Q 非空时会 RecordSearch，这里必须不写）
}
```

`newTestStore(t)` 沿用 `internal/store` 既有测试里的建库辅助；若现有测试用的是别的名字，沿用那个。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/store/ -run TestListNotesAll -v`
Expected: 编译失败，`st.ListNotesAll undefined`。

- [ ] **Step 3: 抽出共用部分并实现 `ListNotesAll`**

把 `ListNotes` 里的 WHERE 构造（含 `deleted_at IS NULL` / `IS NOT NULL` 分支、`Q` 的 LIKE、`category_id` 与「未分类 = `category_id IS NULL`」口径）与 ORDER BY 选择抽成 `buildNoteQuery(filter NoteFilter) (clause, orderBy string, args []any)`，把 `rows` 扫描循环抽成 `scanNoteItems(rows *sql.Rows) ([]NoteListItem, error)`（保留「加密笔记不写摘要、明文只回前 140 码点」那段）。`ListNotes` 改为调用这两个函数，行为必须完全不变（包括 `titleRank` 的占位符顺序：WHERE 参数在前、`titleRank` 的参数在后、`LIMIT`/`OFFSET` 最后）。

`ListNotesAll` 用 `buildNoteQuery(NoteFilter{Deleted: deleted})`，单条 `SELECT` + `ORDER BY`，不带 `LIMIT`/`OFFSET`，不做 `COUNT(*)`，不调 `RecordSearch`。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/store/ -v`
Expected: 新增 5 个用例 PASS，既有用例（含 `ListNotes` 的分页与搜索）全部仍然 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/store/note.go internal/store/note_test.go
git commit -m "feat(store): 增加不分页的笔记全量查询，供 CLI 列表本地筛选"
```

---

### Task 3: `picker`：`bubbles/list` 的 gum 化封装

**Files:**
- Create: `internal/cli/pick.go`
- Test: `internal/cli/pick_test.go`

**Interfaces:**
- Consumes: `github.com/charmbracelet/bubbles/list`、`internal/cli/add_style.go` 的 `styleMatch`/`styleCursor`/`styleSelected`/`styleMuted`
- Produces:
  - `type pickItem interface { list.Item; Title() string; Description() string }`（`list.Item` 要求 `FilterValue() string`）
  - `type picker struct { list list.Model }`
  - `func newPicker(items []list.Item, width, height int, delegate list.ItemDelegate, filter list.FilterFunc) picker`
  - `func (p picker) update(msg tea.Msg) (picker, bool, tea.Cmd)`（第二个返回值 `handled=false` 表示这条消息要交回外层模型：`Enter` 确认、未过滤时的 `Esc`、`ctrl+c` 三种）
  - `func (p picker) selected() list.Item`
  - `func (p picker) view() string`
  - `func pinnedLastFilter(pinnedIndex int) list.FilterFunc`
  - `func highlightMatches(name string, matched []int) string`

- [ ] **Step 1: 写失败的测试**

```go
func TestPickerTypingEntersFilterModeForEveryLetter(t *testing.T) {
	// 对 "j" "k" "h" "l" "b" "u" "f" "d" "g" "G" " " 各喂一次 tea.KeyMsg
	// 断言：list.FilterState() == list.Filtering，且 list.FilterValue() 里出现了那个字符
}

func TestPickerArrowsMoveWhileFiltering(t *testing.T) {
	// 进入过滤态后连按两次 "down"，断言 list.Index() 从 0 变成 2
	// （bubbles 默认在过滤态把方向键转发给 textinput，必须被 picker 拦住）
}

func TestPickerEnterReturnsSelectedImmediately(t *testing.T) {
	// 过滤出唯一一条后按 "enter"：断言 handled=true、返回的选中项是它
	// 且 list.FilterState() 不是 list.FilterApplied
}

func TestPickerEscClearsFilterThenReportsUnhandled(t *testing.T) {
	// 过滤态按 "esc" → handled=true 且回到 list.Unfiltered
	// 未过滤时再按 "esc" → handled=false（由外层决定放弃/退出）
}

func TestPickerTabIsSwallowed(t *testing.T) {
	// 过滤态按 "tab" 与 "shift+tab"：断言 handled=true 且 FilterState 仍不是 FilterApplied
	// （Tab 会触发 bubbles 的 AcceptWhileFiltering，进了那个态 Esc 语义就漂了）
}

func TestPickerPinnedItemSurvivesFiltering(t *testing.T) {
	// 三项 ["未分类","工作","+ 新建分类…"]，pinnedLastFilter(2)
	// 输入一个只命中 "工作" 的过滤串：断言可见项是 ["工作", "+ 新建分类…"]，占位项仍在最后
}

func TestPickerQuitKeysAreDisabled(t *testing.T) {
	// 未过滤时按 "q" 与 "ctrl+c"：断言 handled=true（被 picker 当普通字符/交回外层处理），
	// 不会产生 tea.Quit
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/cli/ -run TestPicker -v`
Expected: 编译失败，`undefined: newPicker` 等。

- [ ] **Step 3: 实现 `internal/cli/pick.go`**

构造时做这些事（`list.New` 之后、返回之前）：

- `SetShowTitle(false)`、`SetShowStatusBar(false)`、`SetShowHelp(false)`、`SetShowPagination(false)`、`SetFilteringEnabled(true)`、`SetShowFilter(true)`。**注意**：过滤框挂在 list 的 title 区（`titleView` 里 `showFilter && filterState == Filtering` 才渲染），隐藏标题文字不影响它。
- 重绑键位（`updateKeybindings` 只改 enabled、不覆盖键位，所以构造时重绑一次即可）：`CursorUp` → `up`/`ctrl+k`；`CursorDown` → `down`/`ctrl+j`；`PrevPage` → `pgup`；`NextPage` → `pgdown`；`GoToStart` → `home`；`GoToEnd` → `end`；`Quit` 与 `ForceQuit` → 空绑定（`key.NewBinding()`，退出交给外层）。理由：默认绑定里 `j`/`k`/`h`/`l`/`b`/`u`/`f`/`d`/`g`/`G` 都是导航键，只要留一个，那个字母就没法当过滤字符输入。
- `list.Filter = filter`（默认 `list.DefaultFilter`）。

`update` 的顺序（在交给 `p.list.Update` 之前）：

1. `tea.KeyMsg` 且是 `tab`/`shift+tab` → `handled=true`，丢弃（防 `FilterApplied`）。
2. 未过滤态且是 `tea.KeyRunes` 或 `tea.KeySpace`（空格在 bubbletea 里是 `KeySpace`，不是 `KeyRunes`）或 `/` → `SetFilterState(list.Filtering)`，再转发给 list。
3. 过滤态且是 `up`/`down`/`ctrl+k`/`ctrl+j` → 自己调 `p.list.CursorUp()` / `CursorDown()`，不转发。
4. 过滤态且是 `enter` → `handled=false`（外层取 `p.selected()` 做确认）。
5. 未过滤态且是 `esc` → `handled=false`。
6. 其余 → 转发给 `p.list.Update`。

`pinnedLastFilter(pinnedIndex)`：对去掉 `pinnedIndex` 之后的 targets 跑 `list.DefaultFilter`，再把 `list.Rank{OriginalIndex: pinnedIndex}` 追加到结果末尾（占位项永远可见、永远在最后）。

`highlightMatches`：把 `matched` 下标处的字符用 `styleMatch` 染色，其余原样（参考 `note add` 现在的 `highlight`，但输入是下标集合而不是子串）。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/cli/ -run TestPicker -v`
Expected: 7 个用例 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/cli/pick.go internal/cli/pick_test.go
git commit -m "feat(cli): 抽出共用选择器，给 bubbles/list 补上 gum filter 的键位语义"
```

---

### Task 4: 命令面：`note list` / `note show` 的参数解析与帮助

**Files:**
- Modify: `internal/cli/parse.go`、`internal/cli/parse_test.go`、`internal/cli/usage.go`、`internal/cli/usage_test.go`

**Interfaces:**
- Consumes: 既有 `Parse`、`valueFlags`、`addConfigFlags`、`containsHelpFlag`、`Usage`、`UsageFor`
- Produces:
  - `CommandList`、`CommandShow`（加到 `Command` 枚举）
  - `type ListOptions struct { Trash bool; Plain bool; Limit int }`
  - `type ShowOptions struct { ID string; Style string; Width int; Raw bool }`
  - `Options` 新增字段 `List ListOptions`、`Show ShowOptions`
  - `Usage()` 命令表新增两行；`listUsage()`、`showUsage()`，并接进 `UsageFor`

- [ ] **Step 1: 写失败的测试**

```go
func TestParseListFlags(t *testing.T) {
	// note list --trash --plain --limit 5 --db /tmp/x.db
	// → CommandList；opts.List == ListOptions{Trash: true, Plain: true, Limit: 5}
	//   且 opts.Args == ["--db", "/tmp/x.db"]（配置参数留给 config.Load）
}

func TestParseShowFlags(t *testing.T) {
	// note show 9f2c1a3b --style dark --width 100 --raw
	// → CommandShow；opts.Show == ShowOptions{ID: "9f2c1a3b", Style: "dark", Width: 100, Raw: true}
}

func TestParseListRejectsUnknownFlag(t *testing.T)     { /* note list --nope → err */ }
func TestParseListRejectsBadLimit(t *testing.T)        { /* --limit 0x / --limit -1 → err */ }
func TestParseShowRequiresID(t *testing.T)             { /* note show → err，错误信息里出现 note list */ }
func TestParseShowRejectsBadStyle(t *testing.T)        { /* --style nope → err */ }
func TestParseShowRejectsBadWidth(t *testing.T)        { /* --width 5 / --width abc → err */ }
func TestParseListAndShowHelp(t *testing.T)            { /* note list -h / note show -h → CommandHelp，topic 为对应命令 */ }
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/cli/ -run "TestParseList|TestParseShow" -v`
Expected: 编译失败（`CommandList` 未定义）。

- [ ] **Step 3: 实现解析与帮助**

- `list`/`show` 的参数分两类：`-c/--config`、`--db` 留在 `opts.Args` 里透传给 `config.Load`（复用 `addConfigFlags`）；其余开关由 `cli.Parse` 消费并从 `opts.Args` 摘掉。这样 `cmd/note` 的 `loadConfig` 一行都不用改。
- 取值校验：`--limit` 必须是非负整数；`--width` 必须在 20..1000；`--style` 必须属于 `auto`/`dark`/`light`/`notty`/`ascii`/`pink`/`dracula`/`tokyo-night`。非法值一律返回 error（项目惯例：多余/非法参数直接报错，不静默忽略）。
- `note show` 缺 id 时返回 error，文案里要出现 `note list`，例如：`note show 需要一个笔记 id（先用 note list 筛选，见 note show --help）`。
- `Usage()` 命令表加 `list`、`show` 两行；`listUsage()`/`showUsage()` 写清参数、退出码（含「命中加密笔记退出 4」）与两个示例；`UsageFor` 加 `list`/`show` 分支。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/cli/ -v`
Expected: 新增用例 PASS，既有 `parse_test.go`/`usage_test.go` 用例全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/cli/parse.go internal/cli/parse_test.go internal/cli/usage.go internal/cli/usage_test.go
git commit -m "feat(cli): 增加 note list 与 note show 的参数解析与帮助"
```

---

### Task 5: `note list`：纯文本输出与交互列表

**Files:**
- Create: `internal/cli/list.go`
- Test: `internal/cli/list_test.go`

**Interfaces:**
- Consumes: Task 3 的 `picker`、`newPicker`、`pinnedLastFilter`、`highlightMatches`；Task 4 的 `ListOptions`；`internal/cli/add.go` 的 `isTerminal`、`header`、`help`
- Produces:
  - `type ListItem struct { ID, Title, CategoryName, Summary string; Encrypted, Trashed bool; UpdatedAt string }`
  - `func (i ListItem) FilterTarget() string`
  - `type ListDeps struct { Notes func(ctx context.Context, deleted bool) ([]ListItem, error) }`
  - `type ListResult struct { Text string; Selected *ListItem }`
  - `func RunList(ctx context.Context, deps ListDeps, opts ListOptions) (ListResult, error)`
  - `func FormatListPlain(items []ListItem, limit int) string`
  - `func listRowSummary(content string, width int) string`
  - 内部：`type listModel struct { ... }`，带 `Init`/`Update`/`View`

- [ ] **Step 1: 写失败的测试**

```go
func TestFormatListPlainAlignsColumns(t *testing.T) {
	items := []ListItem{
		{ID: "9f2c1a3b-4d5e-4f60-8a71-2b3c4d5e6f70", Title: "会议纪要", CategoryName: "工作", UpdatedAt: "2026-10-29T14:03:05Z"},
		{ID: "b1c2d3e4-5f60-4a71-8b92-3c4d5e6f7081", Title: "加密的那条", CategoryName: "私密", Encrypted: true, UpdatedAt: "2026-10-26T22:40:11Z"},
	}
	out := FormatListPlain(items, 0)
	// 断言：两行；第一行含 id/标题/分类/原样 UTC 时间；
	// 第二行标题后是 "[加密]"；输出不含 "\x1b["；列以 tabwriter 对齐（同一列起点相同）
}

func TestFormatListPlainLimit(t *testing.T)          { /* 3 条 + limit 2 → 2 行 */ }
func TestFormatListPlainEmptyIsEmptyString(t *testing.T) { /* nil / 空切片 → ""，一个字节都不输出 */ }
func TestListRowSummaryCollapsesWhitespace(t *testing.T) {
	// "# 标题\n\n第一行\n第二行" → 单行、无换行、无连续空格
	// 长 CJK 串按显示宽度截断（uniseg），不按码点
}

func TestRunListPlainPathReturnsText(t *testing.T) {
	// 用假的 deps 返回 2 条，把 stdin/stdout 都换成非终端（测试进程里天然如此）
	// 断言 res.Text 非空、res.Selected == nil、deps 收到的 deleted == opts.Trash
}

func TestRunListEmptyLibraryInteractiveHint(t *testing.T) {
	// 空列表 + 交互路径：res.Text 含「还没有笔记」；--trash 时含「回收站是空的」
	// 非交互路径（--plain）：res.Text == ""
}

func TestListModelEnterSelectsHighlightedItem(t *testing.T) {
	// 直接驱动 listModel：喂字符筛选、按 "enter"
	// 断言 done=true 且 selected.ID 是被筛出来的那条
}

func TestListModelEscWithoutSelectionQuits(t *testing.T) {
	// 未过滤时按 "esc" → done=true 且 selected == nil
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/cli/ -run "TestFormatListPlain|TestListRowSummary|TestRunList|TestListModel" -v`
Expected: 编译失败。

- [ ] **Step 3: 实现 `internal/cli/list.go`**

- `ListItem.FilterTarget()`：`标题 + " " + 分类名`，分类名为空时用字面量 `未分类`。摘要**不**进 fuzzy 目标（避免噪声）。
- 行渲染（自定义 `list.ItemDelegate`）：两行高，第一行标题（选中加粗 + 游标）+ 右对齐压暗的 `分类 · 本机时间（YYYY-MM-DD HH:MM）`，加密笔记在右对齐信息里追加 `· 加密`；第二行摘要（加密笔记没有）。时间格式化与 Web 端 `shownAt` 同口径：解析失败原样显示 UTC 串。
- `listRowSummary`：先把所有空白折叠成单个空格，再按显示宽度（`uniseg`）截断。这是 Review Focus 第 4 条的实现点。
- `FormatListPlain`：`text/tabwriter` 四列（id、标题（加密加 `[加密]`）、分类、`updated_at` 原样），`limit > 0` 时截断，空输入返回 `""`。
- `RunList`：先 `deps.Notes(ctx, opts.Trash)`；`interactive := !opts.Plain && isTerminal(os.Stdout) && isTerminal(os.Stdin)`；空列表时交互路径返回 `Text` = 提示（`--trash` 用另一句），非交互路径返回空 `Text`；非交互路径返回 `FormatListPlain(...)`；交互路径跑 `tea.NewProgram` 的 `listModel`，选中返回 `Selected`，`Esc` 未选中返回空 `ListResult`。
- `listModel.Update` 把 `tea.KeyMsg` 交给 `picker.update`：`handled=true` 就继续；`Enter` 时取 `picker.selected()` 映射回 `ListItem` 并 `tea.Quit`；未过滤的 `Esc` 直接 `tea.Quit`（不选择）；`ctrl+c` 同理。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/cli/ -v`
Expected: 新增用例 PASS，既有用例 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/cli/list.go internal/cli/list_test.go
git commit -m "feat(cli): note list 支持模糊筛选列表与非交互纯文本输出"
```

---

### Task 6: `note show`：渲染、头部与风格/宽度解析

**Files:**
- Create: `internal/cli/show.go`
- Test: `internal/cli/show_test.go`

**Interfaces:**
- Consumes: Task 1 的 `markdown.Render`/`markdown.Options`；Task 4 的 `ShowOptions`；`internal/cli/add_style.go` 的样式变量
- Produces:
  - `type NoteDetail struct { ID, Title, CategoryName, UpdatedAt, Content string; Encrypted, Trashed bool }`
  - `var ErrNoteEncrypted = errors.New("note is encrypted")`
  - `type ShowDeps struct { Note func(ctx context.Context, id string) (NoteDetail, error) }`
  - `type ShowResult struct { Header, Body string }`
  - `func BuildShow(ctx context.Context, deps ShowDeps, opts ShowOptions, tty bool, termWidth int) (ShowResult, error)`
  - `func resolveStyle(style string, tty bool) string`
  - `func resolveWidth(width, termWidth int) int`

- [ ] **Step 1: 写失败的测试**

```go
func TestResolveStyleMatrix(t *testing.T) {
	// ("", true) → "auto"; ("", false) → "notty"
	// ("dark", false) → "dark"（显式指定优先，即使非 TTY）
	// ("notty", true) → "notty"
}

func TestResolveWidthMatrix(t *testing.T) {
	// (0, 200) → 120（上限）；(0, 60) → 60；(0, 10) → 20（下限）；(0, 0) → 80；(40, 200) → 40
}

func TestBuildShowTTYHasHeader(t *testing.T) {
	// deps 返回一条明文笔记；tty=true
	// Header 含标题、分类、本机时间、id；Body 不含 ANSI 时也不能为空
}

func TestBuildShowNonTTYHasNoHeader(t *testing.T) { /* tty=false → Header == "" */ }
func TestBuildShowTrashedMarksHeader(t *testing.T) { /* Trashed=true → Header 含「回收站」 */ }
func TestBuildShowRawSkipsRendering(t *testing.T) {
	// --raw：Body 等于原始 Markdown（含 "# " 之类的记号），Header 为 ""（即使 tty=true）
}

func TestBuildShowEncryptedReturnsHintAndError(t *testing.T) {
	// Encrypted=true 的笔记：err 必须是 ErrNoteEncrypted；
	// Body 含「加密」且不含正文；tty=true 时 Header 仍填好；tty=false 时 Header 为 ""
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/cli/ -run "TestResolveStyle|TestResolveWidth|TestBuildShow" -v`
Expected: 编译失败。

- [ ] **Step 3: 实现 `internal/cli/show.go`**

- `resolveStyle`：显式风格优先；否则 TTY 用 `auto`、非 TTY 用 `notty`（与 glow 同规则）。
- `resolveWidth`：显式宽度优先（参数层已保证 ≥20）；否则 `max(20, 终端宽)` 且上限 120；终端宽取不到时 80。
- `BuildShow`：调 `deps.Note`；加密笔记**不是**错误路径的一部分——照常拼好 `Header`（tty 时）与提示行 `Body`，然后返回 `ErrNoteEncrypted`；明文笔记走 `markdown.Render(Content, markdown.Options{Width: resolveWidth(...), Style: resolveStyle(...)})`；`--raw` 时 `Body` = 原始 `Content`、`Header` = `""`；`Trashed` 时头部元信息追加 `· 回收站`。头部固定三行：标题（加粗）、`分类 · 本机时间 · id`（压暗）、分隔线（长度取 `min(60, 终端宽)`）。
- 非 TTY 下 `Header` 恒为 `""`。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/cli/ -v`
Expected: 新增用例 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/cli/show.go internal/cli/show_test.go
git commit -m "feat(cli): note show 用 glamour 渲染单条笔记，加密笔记明确拒绝"
```

---

### Task 7: `cmd/note` 接线

**Files:**
- Create: `cmd/note/list.go`、`cmd/note/show.go`、`cmd/note/show_test.go`
- Modify: `cmd/note/main.go`

**Interfaces:**
- Consumes: Task 5 的 `cli.RunList`/`ListResult`/`ListItem`、Task 6 的 `cli.BuildShow`/`ShowOptions`/`ShowResult`/`ErrNoteEncrypted`、既有 `loadConfig`、`store.Open`、`store.GetNote`、`exitError`
- Produces:
  - `func runList(globalConfigPath string, args []string) error`
  - `func runShow(globalConfigPath string, args []string) error`
  - `func renderNote(ctx context.Context, st *store.Store, id string, opts cli.ShowOptions, tty bool, out io.Writer) error`

- [ ] **Step 1: 写失败的测试**

```go
func TestRenderNotePrintsRenderedBody(t *testing.T) {
	// 用临时库 + store.CreateNote 建一条明文笔记
	// renderNote(..., tty=false, out=&buf) → nil；buf 含正文，不含 "\x1b["，不含标题头部
}

func TestRenderNoteEncryptedMapsToExitCode4(t *testing.T) {
	// 建一条加密笔记（NoteInput{CipherB64, KDFParams}）
	// err 必须是 exitError(4)；buf 里含「加密」提示
}

func TestRenderNoteMissingNoteIsError1(t *testing.T) {
	// err 非 nil 且不是 exitError；错误信息含「找不到笔记」
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./cmd/note/ -v`
Expected: 编译失败。

- [ ] **Step 3: 实现**

- `cmd/note/show.go`：`runShow` 加载配置、开库、探测 `tty := isTerminal(os.Stdout)` 与终端宽度（`github.com/charmbracelet/x/term` 的 `GetSize`），调 `renderNote(..., os.Stdout)`；`renderNote` 负责 `cli.BuildShow` → 先打印 `Header`（非空时 + 换行）再打印 `Body`（+ 换行）→ 再把 `ErrNoteEncrypted` 映射成 `exitError(4)`（其余错误原样返回）。映射成 4 之前**必须**先把 `ShowResult` 打印出去。
- `cmd/note/list.go`：`runList` 加载配置、开库、`cli.RunList`；`Text` 非空就写到 stdout；`Selected` 非空就调 `renderNote(ctx, st, item.ID, cli.ShowOptions{}, tty, os.Stdout)`（**默认风格与宽度**，即跟随终端）；两者都空就直接返回 nil（用户未选择，退出 0）。`ListDeps.Notes` 用 `st.ListNotesAll(ctx, deleted)` 映射成 `[]cli.ListItem`（分类名空串表示未分类）。
- `cmd/note/main.go`：`run` 的 switch 加 `case cli.CommandList: return runList(opts.ConfigPath, opts.Args)` 与 `case cli.CommandShow: return runShow(opts.ConfigPath, opts.Args)`。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./cmd/note/ -v && go build ./...`
Expected: 3 个用例 PASS，构建通过。

- [ ] **Step 5: 冒烟（真二进制）**

```bash
just build
./note.exe --db /tmp/note-smoke.db list --plain        # 空库：无输出，退出 0
./note.exe --db /tmp/note-smoke.db list --trash --plain # 无输出，退出 0
./note.exe --db /tmp/note-smoke.db show nope            # 「找不到笔记」，退出 1
./note.exe --db /tmp/note-smoke.db show -h              # 帮助，退出 0
./note.exe --db /tmp/note-smoke.db list -h              # 帮助，退出 0
```

真终端里再跑一次 `note add` 建一条笔记，然后 `note list`（敲字筛选、`Enter` 预览、`Esc` 退出）与 `note show <id>`；非终端环境验证不了 TUI，这一步按 ADR-20 的既有做法人工确认。

- [ ] **Step 6: 提交**

```bash
git add cmd/note
git commit -m "feat(cli): 接线 note list 与 note show"
```

---

### Task 8: `note add` 分类步换成 `picker`

**Files:**
- Modify: `internal/cli/add.go`、`internal/cli/add_test.go`、`internal/cli/usage.go`（核对键位文案）

**Interfaces:**
- Consumes: Task 3 的 `picker`/`newPicker`/`pinnedLastFilter`/`highlightMatches`
- Produces: `addModel` 用 `picker` 替换 `filter`/`filtered`/`filterInput`/`applyFilter`；`Category` 与 `AddDeps` 的对外形状不变

- [ ] **Step 1: 改/写失败的测试**

```go
func TestAddCategoryStepFuzzyFilters(t *testing.T) {
	// 分类 ["未分类","工作","读书笔记"]，喂 "读"
	// 断言可见项只剩 ["读书笔记", "+ 新建分类…"]
}

func TestAddCategoryStepKeepsCreateRow(t *testing.T) {
	// 喂一个谁都匹配不上的串：可见项必须只剩 ["+ 新建分类…"]，仍可按 Enter 进新建子流程
}

func TestAddCategoryStepEscAbandonsOnlyWhenFilterEmpty(t *testing.T) {
	// 过滤中按 esc → 回到未过滤、向导未退出（saved 状态不变、step 仍为 stepCategory）
	// 未过滤时按 esc → saved=false、返回 tea.Quit
}

func TestAddCategoryStepNewCategoryBecomesSelected(t *testing.T) {
	// 走新建分类子流程成功创建 "读书笔记" 后：
	// 断言过滤串已清空、当前选中项是新建的分类、step 回到 stepCategory
}

func TestAddCategoryStepEnterPicksCategory(t *testing.T) {
	// Enter 命中分类 → step 变 stepTitle 且 chosen 是它
	// Enter 命中 "+ 新建分类…" → step 变 stepNewCategory
}
```

同时删掉针对 `filterCategories`/`matchRange`/`highlight`/`cursorMark` 的用例。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/cli/ -run TestAddCategoryStep -v`
Expected: 编译失败或断言失败。

- [ ] **Step 3: 改 `add.go`**

- `addModel` 用 `picker` 替掉 `filter`/`filtered`/`filterInput`/`applyFilter`；`newAddModel` 里不再建过滤用的 `textinput`。
- 分类步的 `Update`：`tea.KeyMsg` 交给 `picker.update`；`handled=true` 就返回它的 cmd；`Enter` 时取 `picker.selected()`：占位项 → `startNewCategory()`，分类 → 记 `chosen` 并进标题步；未过滤的 `Esc` → `saved=false` + `tea.Quit`（现有行为）。
- `viewCategory`：`header("选择分类", "1/3")` + `picker.view()` + 现有 `help(...)`（键位文案不含 `j`/`k`）。
- delegate 自己画行（分类名 + 右对齐 `N 篇`）与游标，用 `highlightMatches` 配 `styleMatch` 做高亮。
- 自定义 `Filter` 用 `pinnedLastFilter(len(all))`，保证 `+ 新建分类…` 永不被过滤掉。
- 新建分类成功后：`SetItems`（新分类追加在末尾）→ `ResetFilter()`（`SetItems` 不会清过滤态）→ `list.Select(indexOfCategory(all, created.ID))` → `chosen = &created`、`step = stepCategory`。
- 删掉 `filterCategories`、`matchRange`、`highlight`、`cursorMark`；保留 `indexOfCategory`、`validateCategoryName`、`styleMatch` 等。
- `tea.WindowSizeMsg` 里要跟着 `list.SetSize`。
- 核对 `usage.go` 的 `addUsage()`：键位行仍是 `↑/↓ 选择 · 输入过滤 · Enter 确认 · Esc 放弃`（不得出现 `j`/`k`），并补一句「输入即筛选（fuzzy）」。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/cli/ -v && just check`
Expected: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/cli/add.go internal/cli/add_test.go internal/cli/usage.go
git commit -m "refactor(cli): note add 分类步改用与 note list 相同的选择器"
```

---

### Task 9: 文档与 CHANGELOG

**Files:**
- Modify: `docs/PRD.md`、`docs/TECH_DESIGN.md`、`README.md`、`CHANGELOG.md`、`docs/superpowers/specs/2026-10-29-cli-add-wizard-design.md`

**Interfaces:**
- Consumes: 前面所有任务的最终行为
- Produces: 无代码接口

- [ ] **Step 1: 先跑全量验证，确认有资格写文档**

Run: `just test && just check`
Expected: Go 测试、前端 Vitest、`go vet`、`svelte-check`/`tsc` 全部通过。未通过就不要往下走。

- [ ] **Step 2: 更新文档**

- `docs/PRD.md`：修订说明加 v1.10；新增 FR-1.11（`note list`：fuzzy 筛选、非交互纯文本降级）与 FR-1.12（`note show`：终端渲染、加密笔记拒绝并退出 4）；命令表加两行；参数表加 `--trash`/`--plain`/`--limit`/`--style`/`--width`/`--raw`；新增 AC-55（列表交互与降级、空结果零字节）与 AC-56（渲染、加密拒绝、非 TTY 无头部）；M9 v1.10 增量；`internal/cli` 模块说明补上两条命令与共用选择器。
- `docs/TECH_DESIGN.md`：目录树补 `internal/cli/list.go`/`show.go`/`pick.go` 与 `internal/markdown`；依赖表加 glamour 一行；命令表加两行；新增 ADR-25（引入 glamour 而非外部 glow；列表全量内存过滤，因为 `ListNotes` 的 `Q` 会经 `RecordSearch` 污染搜索历史）。
- `docs/superpowers/specs/2026-10-29-cli-add-wizard-design.md`：顶部加一行指向本设计文档，注明 §3.2/§3.3 已被取代。
- `README.md`：CLI 一节补 `note list` 与 `note show` 用法各一行。
- `CHANGELOG.md`：`[Unreleased]` 的 Added 记两条命令、共用选择器与 glamour 依赖；Changed 记 `note add` 分类步改为 fuzzy 筛选、lipgloss 版本抬升。

- [ ] **Step 3: 提交**

```bash
git add docs README.md CHANGELOG.md
git commit -m "docs: 记录 CLI 阅读路径（note list / note show）与分类步改造"
```

---

## Self-Review

- **Spec coverage**：§1 目标 1-4 → Task 5（list）、Task 6/7（show）、Task 8（分类步统一）。§3.1-3.5 → Task 4/5/6/7。§4 文件表 → 每个文件都有归属任务（`pick.go` 在 Task 3）。§5 依赖 → Task 1。§6 边界 → Task 2/5/6/7 的用例。§7 文档 → Task 9。§9 → Task 3/8。
- **Step scan**：每个任务以「写失败测试 → 跑出失败 → 实现 → 跑通过 → 提交」闭环；签名与文件路径都写死，未替实施者决定实现细节的部分（delegate 留白、`picker` 内部字段）明确留在 §8 或本文「Interfaces」之外。
- **Type consistency**：`ListOptions`/`ShowOptions`（Task 4）被 Task 5/6 消费；`ListResult{Text, Selected}`（Task 5）被 Task 7 消费；`picker.update` 的三返回值形态在 Task 3 定义、Task 5/8 消费；`markdown.Options`（Task 1）被 Task 6 消费。
- **Review Focus**：6 条各有所属用例（1 → Task 3/8；2 → Task 6/7；3 → Task 5/6；4 → Task 5；5 → Task 1/8；6 → Task 2）。
- **Proportion**：计划以签名、测试名与断言为主，没有抄实现体；两个唯一给出实现要点的函数（`pinnedLastFilter`、`listRowSummary`）是规格里未固定、又容易被做错的算法。
