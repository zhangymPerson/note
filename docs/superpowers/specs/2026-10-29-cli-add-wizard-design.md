# CLI 改造：`note` 打印帮助 + `note add` 交互式新建笔记

> 后续变更：本文 §3.2（分类步的输入即过滤、子串高亮、`j`/`k` 当导航键）与 §3.3（新建分类后的定位）已被 `docs/superpowers/specs/2026-10-29-cli-list-show-design.md` §9 取代——分类步改用 `bubbles/list` 包出的共用 `picker`（fuzzy 匹配、`j`/`k` 是过滤字符、过滤框只在过滤态出现）。其余章节仍然有效。

- 日期：2026-10-29
- 状态：待实施（设计已与需求方确认方向：分类步允许当场新建分类）
- 相关：`docs/PRD.md`（CLI 一节）、`docs/TECH_DESIGN.md`（CLI 一节）、`internal/cli`

## 1. 背景与目标

现在裸 `note` 会进一个只有三个选项的 bubbletea 菜单（启动 Web 服务 / 系统信息 / 退出）。菜单本身价值不大——两个动作都有对等的子命令，且菜单把「用户不知道有哪些命令」这件事藏了起来。

同时，纯 CLI 用户目前**无法新建笔记**：写笔记必须开 `note web` 用浏览器。加密笔记对服务端是密文字节，CLI 没有主密码，这条路注定只能写明文笔记——但明文笔记是主体，`note add` 能覆盖绝大多数场景。

目标：

1. 裸 `note` 直接输出 `note --help` 的内容（与 `note help` 等价），退出码 0。
2. 新增 `note add`：bubbletea 交互向导，依次 **选择分类 → 标题 → 正文**，正文支持换行（照 `gum write` 的实现方式），保存后落库并打印结果。
3. 删除原来的裸 `note` 交互菜单（`internal/cli/menu.go`）。

非目标（明确不做）：

- 不做 `note list` / `note show` / `note edit` / `note delete`——本次只补「新增」这一个动作。
- 不做非交互形态（`note add --title … --content …`）；需要脚本化时走 `POST /api/notes`。
- 不做「保存后继续写第二条」的循环录入。
- 不碰加密：`note add` 只创建明文笔记，不读主密码、不产生 `cipher`。
- 不改 Web 端任何行为。

## 2. 方案对比

| 方案                                                      | 多行编辑                                                                                   | 代价                                                                                                                            | 结论     |
| --------------------------------------------------------- | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------- | -------- |
| **A. `bubbles/textarea` + `textinput`（gum 同款）**       | 成熟：软换行、宽字符、光标、粘贴、`ctrl+e` 外接 `$EDITOR`                                  | 新增一个依赖 `github.com/charmbracelet/bubbles`（bubbletea 的官方组件库，纯 Go、无 CGO；已实测可下载 v0.21.0 并 `go get` 成功） | **采用** |
| B. 手写多行编辑器                                         | 要自己实现软换行 / CJK 宽度 / 光标移动 / 粘贴 / 历史                                       | 约等于重写 `gum write`，几百行且必然有边角 bug                                                                                  | 否       |
| C. 三个独立 `tea.Program`（一步一个程序，切步时退出再起） | 每步都是全屏程序，切步清屏；错误提示（重名分类、空标题）要么退出整步要么停在无法回显的状态 | `gum` 的 `sequence` 也是单进程多步                                                                                              | 否       |

采用 **A + 单进程状态机**：一个 `tea.Program`，模型内部用 `step` 字段切步，切步时把焦点交给对应的组件（`textinput` / `textarea`）。

## 3. 交互规格

### 3.1 三步 + 一个子流程

```
选择分类（1/3） ──[+ 新建分类]──▶ 新建分类（子流程，Esc 回到分类）
      │
      ▼
   标题（2/3）
      │
      ▼
   正文（3/3） ── Ctrl+D ──▶ 保存并退出
```

- 顶部固定一行状态：`note add · 选择分类（1/3）`（新建分类步显示 `note add · 新建分类`）。
- 底部固定一行键位提示，随步变化。
- 每步之间不清屏（单进程，整屏重绘），因此上一步的输入不会丢。

### 3.2 第 1 步：选择分类

```
note add · 选择分类（1/3）

  > 未分类                    3 篇
    工作                      5 篇
    读书笔记                  0 篇
    + 新建分类…

输入即过滤 · ↑/↓ 选择 · Enter 确认 · Esc 放弃
```

- 列表来自 `store.ListCategories`（按 id 升序，`未分类` 天然在首位），每行右侧显示篇数。
- **输入即过滤**：敲任意可见字符进过滤串，按名字做不区分大小写的子串匹配；`Backspace` 删字符，过滤串清空后列表复原。过滤时把过滤串显示在标题行右侧：`… （1/3）  过滤：工▌`。
- `↑`/`↓`（以及 `ctrl+k`/`ctrl+j`，避免与过滤用的 `j`/`k` 冲突）移动；`Enter` 选中。
- 列表末尾常驻 `+ 新建分类…`，它不受过滤影响（过滤后仍然可见，方便 `↓` 到底选中）。
- `Esc` 放弃整个向导。

### 3.3 第 1.5 步：新建分类（子流程）

```
note add · 新建分类

  > 读书笔记▌
    1/30

Enter 创建 · Esc 返回分类列表
```

- 单个 `textinput`，`CharLimit = 30`（与 Web 端 `maxCategoryNameRunes` 一致）。
- `Enter` 调 `CreateCategory`；失败**不退出这一步**，在输入框下方红色回显：
  - 空名 → `分类名不能为空`
  - 已存在（`store.ErrCategoryExists`）→ `分类「X」已存在`
  - 其它错误 → 原样显示错误文本
- 成功后：新分类加进列表并成为当前选中项，回到分类步（不再要求用户再选一次）。
- `Esc` 回分类列表（不放弃整个向导）。

### 3.4 第 2 步：标题

```
note add · 标题（2/3）

  > 会议纪要▌
    4/200

Enter 下一步 · Esc 放弃
```

- 单个 `textinput`，`CharLimit = 200`（与 `store.maxTitleRunes` 一致）。
- `Enter` 时 trim 后为空 → 输入框下方回显 `标题不能为空`，停在本步。
- `Esc` 放弃整个向导。

### 3.5 第 3 步：正文（照 `gum write`）

```
note add · 正文（3/3）  分类：工作 · 标题：会议纪要

  > 1. 先说结论
    2. 再说细节
    ▌

  23/2000 码点   Enter 换行 · Ctrl+D 完成 · Ctrl+E 用 $EDITOR 编辑 · Esc 放弃
```

- 单个 `bubbles/textarea`：宽度取 `min(终端宽度-4, 80)`，高度 10 行（终端过窄时退化为 `表宽-4`，最小 20）。
- `Enter` 插入换行（textarea 默认行为，不做"回车提交"）。
- `Ctrl+D` 完成：把内容交给保存流程。
- `Ctrl+E` 用 `$EDITOR`/`$VISUAL` 打开编辑（`tea.ExecProcess` + 临时文件；两者都没设置时在 footer 回显 `未设置 $EDITOR，无法外接编辑`，不是错误退出）。
- 实时显示码点数（`utf8.RuneCountInString`，与 `store.normalizeBody` 同一口径）。
- 超过 2000 码点时：计数变红并显示 `超出 N 个码点`，此时 `Ctrl+D` 被拒（底部回显 `正文最多 2000 码点`），可以继续删改。
- `Esc` 放弃整个向导。

### 3.6 保存与输出

成功后退出 TUI，在标准输出打印：

```
已新建笔记「会议纪要」
  分类  工作
  ID    9f2c1a3b-…
  链接  http://127.0.0.1:8080/notes/9f2c1a3b-…（需 note web 正在运行）
```

（链接用 `cfg.Addr` 拼；`--remote`/`0.0.0.0` 时如实反映配置值，不再自作聪明替换。）

失败（如 `SQLITE_BUSY`、校验失败）打印 `新建笔记失败: <原因>` 并以退出码 1 结束。

## 4. 结构变更

| 文件                         | 动作     | 说明                                                                                             |
| ---------------------------- | -------- | ------------------------------------------------------------------------------------------------ |
| `internal/cli/add.go`        | 新增     | 向导模型、`AddDeps`、`RunAdd`、`filterCategories` 等纯函数                                       |
| `internal/cli/add_test.go`   | 新增     | 模型级用例（喂 `tea.KeyMsg`）                                                                    |
| `internal/cli/menu.go`       | **删除** | 裸 `note` 不再进菜单                                                                             |
| `internal/cli/menu_test.go`  | **删除** | 同上                                                                                             |
| `internal/cli/parse.go`      | 改       | 新增 `CommandAdd`；`note add [-h]` 解析；`CommandMenu` 语义改为「无子命令 → 打印帮助」           |
| `internal/cli/parse_test.go` | 改       | `note add` / `note add -h` / `note add --bogus` 三例                                             |
| `internal/cli/usage.go`      | 改       | `Usage()` 命令表加 `add`、删「不带命令进入交互菜单」那句；新增 `addUsage()` 与 `UsageFor("add")` |
| `internal/cli/usage_test.go` | 改       | `add` 在总帮助与主题帮助里都出现                                                                 |
| `cmd/note/main.go`           | 改       | `CommandMenu` → 打印 `cli.Usage()`；新增 `CommandAdd` → `runAdd(globalConfigPath, args)`         |
| `go.mod` / `go.sum`          | 改       | 新增 `github.com/charmbracelet/bubbles`                                                          |

`internal/cli` 仍然**不 import `internal/store`**：向导只通过 `AddDeps` 里三个函数与外界打交道，由 `cmd/note` 用 store 实现，测试注入假实现。

```go
// 向导看到的最小数据形状（不含 store 类型，避免 cli 依赖持久层）。
type Category struct {
    ID        int64
    Name      string
    Count     int
    IsDefault bool // 未分类：落库时 category_id 存 NULL，与 Web 端一致
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

// RunAdd 跑完整向导；返回 saved=false 表示用户放弃（Esc/Ctrl+C）。
func RunAdd(ctx context.Context, deps AddDeps) (NoteRef, bool, error)
```

`cmd/note` 侧的实现就是薄胶水：`ListCategories` → 映射成 `cli.Category`（`IsDefault: c.ID == store.DefaultCategoryID`），`CreateNote` → `store.NoteInput{Title, Content: &body, CategoryID: in.CategoryID}`。

## 5. 错误与边界

| 场景                      | 行为                                                                                     |
| ------------------------- | ---------------------------------------------------------------------------------------- |
| stdin 不是终端（管道/CI） | 不进 TUI，直接 `note add 需要在交互式终端里运行` 到 stderr，退出码 1；**不**读管道当正文 |
| 用户 `Esc` / `Ctrl+C`     | 退出 TUI，stderr 打印 `已放弃，未新建笔记`，退出码 130（脚本里不会被当成成功）           |
| 数据库不存在              | `store.Open` 建库 + 跑迁移（与 `note info` 一致），继续向导                              |
| Web 服务正开着            | WAL + `busy_timeout(5000)` 已就绪，CLI 直接写库；`SQLITE_BUSY` 时报错退出 1              |
| 库里有加密笔记            | 不影响：只新建明文笔记，列表里不区分                                                     |
| `note add 多余参数`       | 参数错误，退出码 2，提示「note add 不接受参数（用 note add --help 查看说明）」           |
| 终端极窄/极矮             | 宽度下限 20，高度下限 3；textarea 内部自行滚动，不崩                                     |

## 6. 测试策略

先红后绿（TDD）：

1. `parse_test.go`：`note add` → `CommandAdd`；`note add -h` → `CommandHelp` 且 `Args == ["add"]`；`note add foo` → 错误。
2. `usage_test.go`：`Usage()` 含 `add`；`UsageFor("add")` 含三步与 `Ctrl+D`。
3. `add_test.go`（喂消息，不起真 TTY）：
   - 分类过滤：输入 `工` 后列表只剩「工作」；`Backspace` 后复原；`+ 新建分类` 恒在末尾。
   - 新建分类：空名/超 30 码点/重名三种拒绝且 `step` 不变并带回显；成功后回分类步且新分类被选中。
   - 标题：空标题停留本步；正常标题进正文步。
   - 正文：`Ctrl+D` 触发一次 `CreateNote`，捕获到的 `AddNote` 为 `CategoryID == nil`（默认分类）或指向所选分类；超过 2000 码点时 `Ctrl+D` 不保存并回显。
   - `Esc`：`RunAdd` 返回 `saved=false`，`CreateNote` 未被调用。
4. 手工验收（真终端）：`note add` 走完三步在 Web 端看到笔记与分类；`note`（裸）输出帮助；管道里 `echo x | note add` 报错不挂起。

## 7. 文档更新

- `AGENTS.md` 第 11 行：裸 `note` 的行为改为「等价于 `note --help`」，补一句 `note add` 的定位（bubbletea 向导，只写明文笔记）。
- `docs/PRD.md`：CLI 命令表加 `note add`，`note` 一行改写；能力清单补一条「CLI 新增笔记」。
- `docs/TECH_DESIGN.md`：目录树（`menu.go` → `add.go`）、依赖表（加 bubbles）、CLI 命令表、`§` CLI 小节补三步规格与 `AddDeps` 分层、ADR 追加一条（用 bubbles 而非手写 textarea）。
- `CHANGELOG.md`：`[Unreleased]` 的 Added / Changed（裸 `note` 行为变更属 Changed，写清是破坏性变更）。
- `README.md`：若有菜单相关说明同步（当前 README 未提菜单，仅确认）。

## 8. 风险

- **`bubbles` 版本兼容**：v0.21.0 与 `bubbletea v1.3.10` 搭配需实测；若 `go build` 报不兼容就退到 v0.20.x（同一 API 面）。
- **Windows 终端下的 `ctrl+e` 外接编辑器**：`tea.ExecProcess` 在 Windows 上可用，但真机无 `$EDITOR` 时只能验证回显分支；POSIX 行为靠代码审查。
- **裸 `note` 行为变更**：破坏性——原来点菜单能启动服务，现在需要 `note web start`。写进 CHANGELOG 的 Changed 并给出一行迁移说明。
- **TUI 无法自动化**：与菜单时代相同，模型逻辑靠 `add_test.go` 覆盖，真终端行为靠手工验收记录。
