# CLI 阅读路径：`note list` 可过滤列表 + `note show` 终端渲染

- 日期：2026-10-29
- 状态：待实施（方向已确认：bubbles/list + 全量内存过滤 + 内嵌 glamour）
- 相关：`docs/PRD.md`（CLI 一节）、`docs/TECH_DESIGN.md`（CLI 一节）、`docs/superpowers/specs/2026-10-29-cli-add-wizard-design.md`（上一版 CLI 改造）、`internal/cli`、`internal/store`

## 1. 背景与目标

v1.9 起 CLI 只能**写**不能**读**：`note add` 能把笔记写进库，但想看一条笔记只能开浏览器，`note add` 末尾打印的链接还要求 `note web` 正在运行。CLI 的阅读路径是缺的。

能读的范围由架构决定：明文笔记（`note add` 写的、以及 Web 端未加密的）正文在库里就是明文 TEXT，CLI 直接读得到；加密笔记只有密文 BLOB 与公盐，主密码只存在于浏览器内存，服务端零密码学（TECH_DESIGN 1.2），CLI **永远**解不开——这不是本次要绕过的限制，而是本次要如实表达的事实。

目标：

1. 新增 `note list`：进一个 `gum filter` 观感的列表，敲字即模糊筛选（fuzzy），`↑/↓` 选，`Enter` 直接预览，`Esc`/`q` 退出。
2. 新增 `note show <id>`：把一条笔记渲染成终端 Markdown——用 glow 的渲染内核 `glamour` 内嵌实现，而不是调用外部 `glow` 可执行文件。
3. 非 TTY（管道 / CI / 重定向）自动降级：`note list` 输出对齐的纯文本一行一条，`note show` 输出无 ANSI 的纯文本正文。默认情况下两条命令都能安全进管道；唯一的例外是用户显式给了 `--style`/`--width`，此时按显式值走（与 glow 同规则，见 §3.3）。
4. 把 `note add` 分类步的手写过滤列表也换成同一个组件（见 §9），消掉项目里两套选择器并存的状态。

非目标（明确不做）：

- 不做 `note edit` / `note delete` / `note new`：写路径已由 `note add` 与 Web 端覆盖。
- 不做加密笔记的 CLI 解密：不引入任何口令输入，不在 CLI 或服务端新增任何密码学代码。
- 不做 `note list --category` / `--json` / 分页参数：列表一次性读进内存（见 §2 决定 2）。
- 不做 glow 的浏览器/仓库浏览能力（GitHub/GitLab 拉取、stash、文件树 TUI）。
- 不碰 Web 端与任何 HTTP API；本次不动 `internal/web`。
- 不在 CLI 侧写活动表（不记 `search`，也不记 `note_view`，见 §6）。

## 2. 方案对比（已确认）

三个决定点各自比较过，结论是 1A + 2A + 3A。

### 决定 1：过滤组件

| 方案                                            | 观感与能力                                                                          | 代价                                                                                                         | 结论     |
| ----------------------------------------------- | ----------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ | -------- |
| **A. `bubbles/list`**                           | 自带 fuzzy 匹配（`sahilm/fuzzy`）、分页、状态栏与帮助栏，就是 `gum filter` 那类观感 | 与 `note add` 里手写的过滤列表观感不一致，项目里会同时存在两种选择器；`bubbles` 已是对接依赖，只多用一个子包 | **采用** |
| B. 把 `note add` 的 `filterCategories` 抽成组件 | 观感统一、零新组件                                                                  | 子串匹配（非 fuzzy）；要泛化已稳定的向导代码，回归面更大                                                     | 否       |
| C. 直接用 `sahilm/fuzzy` 手写模型（gum 的做法） | 完全可控                                                                            | 等于重写分页、状态栏、窗口尺寸、窄终端处理                                                                   | 否       |

采用 A，并在模型里补一个 `gum filter` 的细节：**任意可见字符即进过滤态**（等价于先按 `/`），不必先敲 `/`。这条对 `note list` 与 `note add` 分类步都成立，两处共用同一个封装（见 §9）。

### 决定 2：数据来源

| 方案                                      | 代价                                                                                                                                             | 结论     |
| ----------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | -------- |
| **A. 进列表时一次性读进内存，本地过滤**   | 内存占用与库大小成正比（个人库量级：几千条 × 几百字节 ≈ MB 以内）；换来零延迟、零额外查询                                                        | **采用** |
| B. 过滤串变化时查库（`ListNotes` 的 `Q`） | **会污染搜索历史**：`store.ListNotes` 在 `Q` 非空时调 `RecordSearch` 写 `activities` 表，逐键查库等于把每次按键记成一次搜索；且拿不到 fuzzy 排序 | 否       |

### 决定 3：渲染内核

| 方案                                  | 代价                                                                                                                                                                         | 结论     |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------- |
| **A. 内嵌 `glamour` v1.0.0**          | 新增 goldmark / chroma / bluemonday / reflow 等纯 Go 依赖，并把 lipgloss 从 v1.1.0 抬到伪版本（见 §5）；换来自带样式、代码高亮、表格与 emoji，`note show` 与列表共用一套观感 | **采用** |
| B. 调外部 `glow` 可执行文件           | 零依赖，但要求用户自己装 glow（Windows 上未必有）、每次落临时文件再 exec、错误与分页行为都不受控                                                                             | 否       |
| C. 用 lipgloss 手写极简 Markdown 渲染 | 零新依赖，但标题/列表/代码块/表格全要自己实现，Markdown 覆盖度注定残缺                                                                                                       | 否       |

**glow 本体不能当库用**：`github.com/charmbracelet/glow@v1.5.1` 的仓库根包是 `package main`（cobra 子命令 + `charm` 云同步依赖），Go 无法 import。可复用的只有它用的渲染内核 `github.com/charmbracelet/glamour`。因此「引入 glow」在本项目里的正确落点是 **glamour**。

## 3. 命令面与交互规格

### 3.1 `note list`（交互模式）

```
note list · 12 篇

  > 会议纪要                                工作 · 2026-10-29 14:03
    先说结论，再说细节…
    读书笔记                                未分类 · 2026-10-27 09:11
    加密的那条                              私密 · 2026-10-26 22:40 · 加密

↑/↓ 选择 · 输入即筛选 · Enter 预览 · Esc 退出                            1/1
```

- 数据来自 `store.ListNotesAll`（见 §4），按 `updated_at DESC, id DESC` 排序，与 Web 端活动列表同序。
- 每一行两行高：第一行是标题（选中项加粗、前缀洋红游标），右对齐压暗的 `分类 · 本地时间`；第二行是明文摘要（前 140 码点，`store` 已经截好），加密笔记没有摘要行，改为在右对齐信息里追加 `· 加密`。
- 摘要进列表前先**折叠所有空白为单个空格**（库里存的是原始正文，可能带换行、`#` 标题号、`-` 列表符），再按**显示宽度**（`uniseg`）截到行宽。这一步是必须的：不然带换行的摘要会把「两行高」撑坏，分页和 `bubbles/list` 的视口都跟着错。
- 时间格式与 Web 端同口径（`web/src/views/NoteListView.svelte` 的 `shownAt`）：把 UTC 秒级文本按本机时区显示到分钟 `YYYY-MM-DD HH:MM`，解析失败则原样显示 UTC 串。
- **输入即筛选**：任意可见字符进入过滤态并转发给 `bubbles/list` 的过滤输入框（等价于先按 `/`）；`Backspace` 删字符；清空后列表复原。`/` 仍可显式进入过滤态。
- **fuzzy 目标 = 标题 + 分类**（`未分类` 时用字面量「未分类」）。摘要**不**进 fuzzy 目标：否则随便一个词都会因为摘要命中而把列表搅乱，而 CLI 找笔记的主线是「按标题找」。
- 匹配片段按 `note add` 的 `styleMatch` 高亮（`bubbles/list` 的 `DefaultFilter` 会给出匹配下标）。
- **键位照 `gum filter`**：`↑`/`↓`（以及 `ctrl+k`/`ctrl+j`）移动，`Enter` 选中，`Esc` 退出，`Ctrl+C` 随时退出。**不绑定 `q`/`j`/`k`**：过滤态下它们就是普通字符，不能和导航抢字母（`note add` 的分类步已经踩过这个坑，那里是靠改用 `ctrl+k`/`ctrl+j` 绕开的）。
- `Esc` 是两段式：正在过滤时先清空过滤串回到全量列表，过滤串已空时再按才退出。`bubbles/list` 的默认行为正是如此，照用不另写逻辑。
- 底部状态栏显示 `当前页/总页`；帮助栏显示键位。
- 选中后**退出 TUI**，把预览打印到 stdout（与 `note show` 走同一个渲染函数）。选中明文笔记退出 0，选中加密笔记退出 4（见 §3.3）。TUI 内不做二次预览窗口：保持「TUI 只负责选」这一条边界，想再看另一条就重跑 `note list`（列表是内存数据，重跑是瞬时的）。

### 3.2 `note list`（非交互输出）

stdin 或 stdout 不是终端时（管道、重定向、CI）自动降级，不报错——这一点**有意区别于** `note add`（`note add` 没有非交互语义，非终端直接报错退出 1）。用 `--plain` 可在真终端强制走这条路径。

```
9f2c1a3b-4d5e-4f60-8a71-2b3c4d5e6f70  会议纪要            工作      2026-10-29T14:03:05Z
b1c2d3e4-5f60-4a71-8b92-3c4d5e6f7081  加密的那条 [加密]   私密      2026-10-26T22:40:11Z
```

- 列：`id`、`标题`、`分类`（未分类时为空）、`updated_at`（**原样 UTC 秒级文本**，可排序、可被脚本消费）。用 `text/tabwriter` 对齐。
- 加密笔记在标题后追加 `[加密]`。
- 无表头、无 ANSI、无摘要：这是给管道用的格式。
- `--limit N`（默认 0 = 全部）只影响这条路径。
- **空结果时一个字节都不输出**（退出 0）：`note list --plain | wc -l` 必须是 0 行，提示文案不能污染管道。空库的提示只在交互路径下出现（见 §6）。

### 3.3 `note show <id>`

TTY（真终端）：

```
会议纪要
工作 · 2026-10-29 14:03 · 9f2c1a3b-4d5e-4f60-8a71-2b3c4d5e6f70
──────────────────────────────────────────────────────────────
<glamour 渲染后的正文>
```

- 头部三行（标题加粗、元信息压暗、一条分隔线）**只在 TTY 下打印**。**理由**：标题与正文在库里是两个字段，不打印头部就永远看不到标题；而非 TTY 下打印头部会破坏管道语义，所以两者取不同的形态。
- 正文用 `glamour` 渲染，宽度取终端宽度（上限 120），风格默认 `auto`（glamour 按终端背景在 dark/light 间选）。
- 回收站里的笔记照常渲染，头部元信息追加 `· 回收站`。

非 TTY：不打印头部，只输出正文；风格与宽度按下面的规则解析（默认就是纯文本、宽度 80）。

**风格与宽度的解析规则**（`resolveStyle` / `resolveWidth`，与 glow 的 `--style` 规则同源）：

- 非 TTY 且用户**没有**显式给 `--style` → 强制 `notty`（纯文本、无 ANSI）。
- 用户显式给了 `--style` → 按显式值走，即使 stdout 是管道（例如 `note show x | less -R` 想看彩色）。**这是「安全进管道」这条目标的唯一例外**，`--style dark` 打进管道会带 ANSI，属于用户明确要求。
- 宽度：`--width` 显式给了就用它（参数层已经保证 20..1000）；否则取 `max(20, 终端宽)`（上限 120）；取不到（非 TTY）用 80。

`--raw`：只输出库里存的原始 Markdown（不渲染、也不加头部），便于接给别的渲染器。

**「头部只在 TTY 下出现」这条规则对加密笔记同样适用**（`ShowResult.Header` 在非 TTY 下恒为空）：加密笔记没有正文可输出，它的提示行放在 `Body` 里，所以 `note show <加密笔记 id> | cat` 输出的是**一行提示**（无 ANSI），配合退出码 4 让脚本能分辨「这不是正文」。

加密笔记（TTY）：

```
加密的那条
私密 · 2026-10-26 22:40 · b1c2d3e4-5f60-4a71-8b92-3c4d5e6f7081
──────────────────────────────────────────────────────────────
这条笔记是加密的，CLI 没有主密码，无法解密；请在 note web 里解锁查看。
```

非 TTY 下同一场景只有最后那一行提示。`--raw` 对加密笔记没有明文可输出，因此与普通路径同形（TTY 下带头部、非 TTY 下只有提示行），退出码 4。

### 3.4 参数

| 命令             | 参数                                                                                                                                             |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `note list`      | `-c, --config <路径>`、`--db <路径>`、`--trash`（只列回收站）、`--plain`（强制纯文本输出）、`--limit <N>`（非交互输出条数，0=全部）、`-h/--help` |
| `note show <id>` | `<id>`（必填）、`-c, --config <路径>`、`--db <路径>`、`--style <名>`、`--width <N>`、`--raw`、`-h/--help`                                        |

- `--style` 取值限定为 glamour 内置风格名：`auto`（默认）、`dark`、`light`、`notty`、`ascii`、`pink`、`dracula`、`tokyo-night`。其它值报错退出 2（项目惯例：非法参数不静默忽略）。
- `--width` 取值 `20..1000`，越界或非数字报错退出 2；默认 0 = 自动（终端宽，上限 120，取不到时 80）。
- `note show` 缺 `<id>` 时报错退出 2，错误信息里提示「不知道 id 就先跑 note list 筛选」。
- `note list` 与 `note show` 只把 `-c/--config`、`--db` 透传给 `config.Load`，其余参数由 `cli.Parse` 自己消费并从 `opts.Args` 中摘掉，因此 `cmd/note` 的 `loadConfig` 不需要改动。

### 3.5 退出码

| 码  | 场景                                                                                                                   |
| --- | ---------------------------------------------------------------------------------------------------------------------- |
| 0   | 正常结束，包含 `note list` 里用户按 `Esc` 未选择就退出                                                                 |
| 1   | 运行时失败：开库失败、`note show` 的 id 不存在、渲染失败、写 stdout 失败                                               |
| 2   | 参数错误：未知参数、`show` 缺 id、`--style`/`--width`/`--limit` 取值非法（沿用现有惯例）                               |
| 4   | 命中加密笔记：`note show <id>` 或 `note list` 里选中的是加密笔记。不是故障，单独编码让脚本能区分「看不了」与「没找到」 |

（3 已被 `note web status` 占用，表示「未运行」。）

## 4. 结构变更

| 文件                               | 动作 | 说明                                                                                                             |
| ---------------------------------- | ---- | ---------------------------------------------------------------------------------------------------------------- |
| `internal/markdown/render.go`      | 新增 | `Render(src string, opts Options) (string, error)`：唯一的 glamour 触点，纯函数、可单测                          |
| `internal/cli/list.go`             | 新增 | `ListItem`/`ListDeps`/`RunList`/`FormatListPlain`，bubbletea 模型与 delegate                                     |
| `internal/cli/show.go`             | 新增 | `NoteDetail`/`ShowDeps`/`BuildShow`/`resolveStyle`/`resolveWidth`/`ErrNoteEncrypted`                             |
| `internal/cli/pick.go`             | 新增 | 共用选择器：包一层 `bubbles/list`，补 gum 键位语义与 fuzzy 高亮（`note list` 与 `note add` 分类步都用它，见 §9） |
| `internal/cli/add.go`              | 改   | 分类步换成 `picker`；删掉手写的过滤/高亮/游标逻辑（见 §9.5）                                                     |
| `internal/cli/add_test.go`         | 改   | 分类步用例重写（键位与模型结构都变了）                                                                           |
| `internal/cli/list_test.go`        | 新增 | 模型级用例（喂 `tea.KeyMsg`、喂窗口尺寸）、`FormatListPlain` 对齐、fuzzy 目标                                    |
| `internal/cli/show_test.go`        | 新增 | `resolveStyle`/`resolveWidth` 全组合、头部拼接、加密笔记路径、`--raw`                                            |
| `internal/markdown/render_test.go` | 新增 | `notty` 风格下的确定性输出（标题、列表、代码块各一例）                                                           |
| `internal/cli/parse.go`            | 改   | 新增 `CommandList`/`CommandShow`、`ListOptions`/`ShowOptions`、两条命令的参数解析与校验                          |
| `internal/cli/parse_test.go`       | 改   | 两条命令的正常/非法参数用例（未知参数、缺 id、`--style` 非法、`--limit` 非法）                                   |
| `internal/cli/usage.go`            | 改   | 总帮助命令表加 `list`/`show`；新增 `listUsage()`、`showUsage()` 与 `UsageFor` 分支                               |
| `internal/cli/usage_test.go`       | 改   | 两条命令在总帮助与主题帮助里都出现                                                                               |
| `internal/store/note.go`           | 改   | 抽出共用的 WHERE/ORDER 构造与行扫描；新增 `ListNotesAll(ctx, deleted bool)`                                      |
| `internal/store/note_test.go`      | 改   | `ListNotesAll` 的顺序、回收站过滤、加密笔记无摘要、不写活动表                                                    |
| `cmd/note/list.go`                 | 新增 | `runList`：装配 `ListDeps`；选中后复用 `previewNote`                                                             |
| `cmd/note/show.go`                 | 新增 | `runShow` 与 `previewNote`（`note list` 选中后也走它）                                                           |
| `cmd/note/main.go`                 | 改   | `run` 里分发 `CommandList`/`CommandShow`                                                                         |
| `go.mod` / `go.sum`                | 改   | 新增 `glamour`、`golang.org/x/term`；lipgloss 升到伪版本                                                         |

`internal/cli` 仍然**不** import `internal/store`：两条命令都走依赖注入（`ListDeps` / `ShowDeps`），与 `AddDeps` 同一套路，`cmd/note` 用 store 实现、测试注入假实现。

关键类型与签名：

```go
// internal/cli/list.go
type ListItem struct {
    ID           string
    Title        string
    CategoryName string
    Summary      string // 明文笔记前 140 码点；加密笔记为空
    Encrypted    bool
    Trashed      bool
    UpdatedAt    string // UTC 秒级文本，原样来自 store
}

// FilterTarget 是 fuzzy 匹配目标：标题 + 分类（未分类时为「未分类」）。
func (i ListItem) FilterTarget() string

type ListDeps struct {
    Notes func(ctx context.Context) ([]ListItem, error) // deleted 由 RunList 的 opts.Trash 决定
}

// ListResult 是 note list 的结果：Text 非空表示非交互路径已经算好了要打印的文本，
// Selected 非空表示用户在 TUI 里选中了一条；两者互斥，都为空表示空库（非交互路径）或用户直接退出。
// cli 不写 stdout，I/O 留给 cmd/note（与 RunAdd 返回 NoteRef 而不自己打印是同一分工）。
type ListResult struct {
    Text     string
    Selected *ListItem
}

// RunList 跑 note list：进 TUI 还是走纯文本路径由它自己探测
// （stdout 与 stdin 都是终端且未给 --plain 才进 TUI，与 RunAdd 探测 stdin 同一套 isTerminal 辅助函数）。
func RunList(ctx context.Context, deps ListDeps, opts ListOptions) (ListResult, error)

// FormatListPlain 渲染非交互输出（tabwriter 对齐）；limit <= 0 表示全部；空输入返回空串。
func FormatListPlain(items []ListItem, limit int) string
```

```go
// internal/cli/show.go
type NoteDetail struct {
    ID           string
    Title        string
    CategoryName string
    Encrypted    bool
    Trashed      bool
    UpdatedAt    string
    Content      string // 明文正文；加密笔记为空
}

// ErrNoteEncrypted 表示这条笔记是加密的：CLI 没有主密码，无法渲染正文。
var ErrNoteEncrypted = errors.New("note is encrypted")

type ShowDeps struct {
    Note func(ctx context.Context, id string) (NoteDetail, error)
}

// ShowResult 是 note show 的纯计算结果，由 cmd/note 打印。
type ShowResult struct {
    Header string // TTY 下的元信息头部；非 TTY 为空
    Body   string // 渲染后的正文（--raw 时为原始 Markdown）
}

// BuildShow 读一条笔记并算出要打印的内容；tty/termWidth 由调用方探测后传入。
// 加密笔记不是失败：deps.Note 照常返回 NoteDetail{Encrypted: true}（Content 为空），
// BuildShow 也照常填好 ShowResult（头部 + 解密提示），同时返回 ErrNoteEncrypted；
// 调用方先打印 ShowResult，再把该错误映射成退出码 4。
func BuildShow(ctx context.Context, deps ShowDeps, opts ShowOptions, tty bool, termWidth int) (ShowResult, error)

// resolveStyle 非 TTY 且用户未显式指定 --style 时返回 "notty"（与 glow 同规则），否则返回 style（空串按 "auto"）。
func resolveStyle(style string, tty bool) string

// resolveWidth 返回渲染宽度：width > 0 时用它；否则用终端宽（上限 120）；termWidth <= 0 时用 80。
func resolveWidth(width, termWidth int) int
```

```go
// internal/markdown/render.go
package markdown

// Options 是渲染参数。Width <= 0 时不调用 glamour 的 WithWordWrap（沿用它的默认 80，避免 WordWrap=0 触发 glamour 内部宽度为 0 的路径）；Style 空串等价 "auto"。
type Options struct {
    Width int
    Style string
}

// Render 把 Markdown 渲染成终端文本；Style == "notty" 时输出纯文本（无 ANSI）。
func Render(src string, opts Options) (string, error)
```

```go
// internal/store/note.go（新增；ListNotes 的 WHERE/ORDER 与行扫描抽出来共用）
// ListNotesAll 返回全部（deleted=true 时只返回回收站）笔记列表项，不分页、不写活动表。
func (s *Store) ListNotesAll(ctx context.Context, deleted bool) ([]NoteListItem, error)
```

`ListNotesAll` 的实现要求：与 `ListNotes` 共用同一个 WHERE 子句构造（含「未分类 = `category_id IS NULL`」这条口径）与同一段行扫描逻辑；不带 `LIMIT`/`OFFSET`，不做 `COUNT(*)`，不调 `RecordSearch`（`Q` 恒为空），顺序与 `ListNotes` 一致（回收站按 `deleted_at DESC`，活动列表按 `updated_at DESC, id DESC`）。

## 5. 依赖变更

| 依赖                                | 变化                  | 说明                                                                                                                         |
| ----------------------------------- | --------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `github.com/charmbracelet/glamour`  | 新增 `v1.0.0`（直接） | glow 的渲染内核；纯 Go、无 CGO，`CGO_ENABLED=0` 不受影响                                                                     |
| `github.com/charmbracelet/bubbles`  | 已依赖，新增用 `list` | `list` 的 fuzzy 来自 `sahilm/fuzzy v0.1.1`，随 `bubbles` 的 go.mod 进来                                                      |
| `github.com/charmbracelet/x/term`   | 已依赖（间接转直接）  | `note show` 探测终端宽度；bubbletea 已经把它带进图里，不再另引 `golang.org/x/term`（后者随 glamour 间接进来，不直接 import） |
| `github.com/charmbracelet/lipgloss` | `v1.1.0` → 伪版本     | glamour 的 `ansi/table.go` 要求 `v1.1.1-0.20250404203927-76690c660834`；`note add` 的观感必须重跑用例 + 手工确认             |

传递依赖（全部纯 Go）：`goldmark`、`goldmark-emoji`、`chroma/v2`、`bluemonday`、`douceur`、`gorilla/css`、`regexp2`、`reflow`、`termenv`、`x/net`、`x/text`。

上述版本已在本机验证可解析：`go mod download github.com/charmbracelet/glamour@v1.0.0` 与 `.../glow@v1.5.1` 都成功（`GOPROXY` 为 `https://goproxy.cn`），glamour 的依赖树已逐个确认全是纯 Go，`CGO_ENABLED=0` 的构建不会破。

## 6. 边界与错误

| 场景                     | 行为                                                                                                                                                      |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 库为空（0 条笔记）       | 交互路径：不进 TUI，stdout 打印「还没有笔记，用 note add 新建一条」，退出 0；非交互路径（非 TTY 或 `--plain`）：**不输出任何内容**，退出 0                |
| `--trash` 且回收站为空   | 同上，交互路径文案改为「回收站是空的」                                                                                                                    |
| 摘要含换行或宽字符       | 列表行里的摘要折叠空白为单个空格、按显示宽度截断，保证每项恒为两行高（见 §3.1）                                                                           |
| 加密笔记                 | 列表里照常出现（右对齐信息追加 `· 加密`，无摘要行）；`note show` 或列表里选中它都打印元信息 + 解密提示，退出 4                                            |
| 回收站笔记               | `note list` 默认不含；`--trash` 只列回收站；`note show` 照常渲染并在头部标注 `· 回收站`                                                                   |
| stdin 或 stdout 非 TTY   | `note list` → 纯文本一行一条；`note show` → `notty` 风格、不打印头部。两者都不报错                                                                        |
| `note show` 的 id 不存在 | stderr 打印 `找不到笔记 <id>`，退出 1（`store.ErrNoteNotFound`）                                                                                          |
| 终端过窄（< 40 列）      | 列表交给 `bubbles/list` 自行处理；`note show` 宽度取 `max(20, 终端宽)`                                                                                    |
| 正文为空                 | 渲染出空正文，头部照常打印，不报错                                                                                                                        |
| 上万条笔记               | 一次性读进内存（见 §2 决定 2），不设上限；个人库量级在 MB 以内                                                                                            |
| 正文含 ANSI 转义序列     | 原样输出，终端自行解释；不净化（数据源是本机用户自己的笔记）。记录为已知行为，见 §8                                                                       |
| 活动统计                 | CLI 两条命令都**不**写 `activities`（不记 `search`、不记 `note_view`）：`note list` 是本地过滤没有查询词可记，`note show` 的翻看不该刷屏 Web 端的查看统计 |

安全不变量不受影响：本次只读库，不解密、不落盘、不新增任何输出面（打印的都是用户自己的明文正文），`{@html}`、CSRF、`/api/shutdown` 等约定一概不动。

## 7. 文档与 CHANGELOG

- `docs/PRD.md`：修订说明加 v1.10；FR-1.11（`note list`）、FR-1.12（`note show`）；命令表加两行；参数表加 `--trash`/`--plain`/`--limit`/`--style`/`--width`/`--raw`；AC-55（列表交互与降级）、AC-56（渲染与加密拒绝）；M9 v1.10 增量；`internal/cli` 模块说明补一句。
- `docs/superpowers/specs/2026-10-29-cli-add-wizard-design.md`：§3.2（分类步的输入即过滤、子串高亮、`j`/`k` 语义）与 §3.3（新建分类后的定位）被本文件 §9 取代，在该文档顶部加一行指向本文件，不重写历史。
- `docs/TECH_DESIGN.md`：目录树补 `list.go`/`show.go`/`internal/markdown`；依赖表加 glamour 一行；命令表加两行；新增 ADR-25（引入 glamour 而非外部 glow；列表全量内存过滤，因为 `ListNotes` 的 `Q` 会经 `RecordSearch` 污染搜索历史）。
- `README.md`：CLI 一节补 `note list` / `note show` 两条用法。
- `CHANGELOG.md`：`[Unreleased]` 的 Added 记两条命令与 glamour 依赖，Changed 记 lipgloss 版本抬升。

## 8. 留给实施者的部分

- 列表行的具体留白、游标与右对齐信息的宽度分配：按 §3.1 的观感自行把握，与 `note add` 的配色变量（`internal/cli/add_style.go`）保持一致。
- `bubbles/list` 的 `Title`/`ShowStatusBar`/`ShowHelp`/`SetShowTitle` 组合：目标是 §3.1 那个形态，具体开关自选。
- glamour 的 `WithPreservedNewLines()`、`WithBaseURL()`、`WithEmoji()` 等选项是否启用：默认全不启用，除非观感测试表明需要（glow 启用了前两个，但那是因为它要处理文件与远程 URL）。
- 列表高亮的实现：统一按 §9.5 走——用 `MatchesForItem(index)` 的命中下标配 `styleMatch`，**不再**用 `note add` 原来的 `matchRange` 子串高亮（那个函数连同它的用例一起删）。
- 已知行为（不做处理，但要在 PR 描述里点明）：正文里的 ANSI 转义序列会被原样输出。
- `picker` 与两个调用方（`note list`、`note add` 分类步）的具体接口形状：`picker` 自己持有 `list.Model` 并把「未处理」的按键交回外层，还是外层拿 `list.Model` 调共享辅助函数，二选一即可；但 §9.3 的四条键位规则与 §9.4 的观感是硬要求。

## 9. `note add` 分类步换成同一个选择器

### 9.1 为什么

`note add` 的分类步是手写的：`filterCategories` 做子串匹配、`highlight` 做子串高亮、光标自己管。`note list` 用 `bubbles/list`（fuzzy）。两套选择器并存正是 §2 决定 1 里 A 方案列的代价，本次一并消掉：分类步改用同一个组件。

### 9.2 一个必须先知道的前提：`bubbles/list` 的过滤是模态的

读 `bubbles@v1.0.0/list` 源码确认（不是猜的）：

- 过滤框只在 `filterState == Filtering` 时渲染（`titleView`），浏览态不显示。
- 浏览态敲可见字符**不会**进过滤态：`handleBrowsing` 只认绑定的键，未绑定的按键丢给 delegate，不启动过滤。要进过滤态必须按 `/`（`KeyMap.Filter`）。
- 过滤态下 `handleFiltering` 只处理 `CancelWhileFiltering`（Esc）与 `AcceptWhileFiltering`（Enter），**其余按键全部转发给过滤输入框**，因此过滤态里 `↑`/`↓` 不会移动光标（被 textinput 吃掉）。
- `SetFilterState(Filtering)` 会先 `GoToStart()`，光标被拉到顶部。
- `SetItems` 在非 Unfiltered 状态下会重新跑过滤，所以过滤串能跨 `SetItems` 保留；`ResetFilter()` 才会回到 Unfiltered（`SetItems` 不会）。
- `DefaultFilter` 是 fuzzy（`sahilm/fuzzy`），`Model.MatchesForItem(index)` 给出命中下标，够做逐字符高亮。
- `updateKeybindings` 只对绑定做 `SetEnabled`，**不改键位**，所以我们自己重绑的键不会被它覆盖。
- 默认键位里字母键很多：`CursorUp` = `up`/`k`，`CursorDown` = `down`/`j`，`PrevPage` = `left`/`h`/`pgup`/`b`/`u`，`NextPage` = `right`/`l`/`pgdown`/`f`/`d`，`GoToStart` = `home`/`g`，`GoToEnd` = `end`/`G`，`Quit` = `q`/`esc`，`ForceQuit` = `ctrl+c`。

也就是说，`bubbles/list` 的默认交互是 vim 式模态（`/` 进、Enter 出），**不是** `gum filter` 那种「一直能打字、方向键一直能选」。要拿到 gum 的观感，必须在外层补四条键位规则并重绑一批字母键。

### 9.3 共用封装：`internal/cli/pick.go`

两处都包同一个 `picker`，把规则实现一次。

**重绑（构造时做一次，`updateKeybindings` 不会覆盖）**：

| 绑定         | 改成                                 |
| ------------ | ------------------------------------ |
| `CursorUp`   | `up`、`ctrl+k`（去掉 `k`）           |
| `CursorDown` | `down`、`ctrl+j`（去掉 `j`）         |
| `PrevPage`   | `pgup`（去掉 `left`/`h`/`b`/`u`）    |
| `NextPage`   | `pgdown`（去掉 `right`/`l`/`f`/`d`） |
| `GoToStart`  | `home`（去掉 `g`）                   |
| `GoToEnd`    | `end`（去掉 `G`）                    |
| `Quit`       | 禁用：退出由外层模型决定             |
| `ForceQuit`  | 禁用：`ctrl+c` 由外层模型自己处理    |

重绑的理由：只要还有一个字母键被绑成导航，它就无法作为过滤字符输入（`j`/`k`/`h`/`l`/`b`/`u`/`f`/`d`/`g`/`G` 都在默认绑定里）。这一改的副作用是 `note add` 分类步**不再有「过滤为空时 `j`/`k` 当上下移动」**的行为，但该行为从未写进帮助文案（帮助一直只写 `↑/↓ 选择`），且 `gum filter` 本来就没有它。

**四条键位规则**（在把按键交给 `list.Update` 之前判断）：

1. **可见字符即进过滤态**：未在过滤态时收到 `tea.KeyRunes` 或 `tea.KeySpace`（`bubbletea` 把单独的空白键投成 `KeySpace` 而不是 `KeyRunes`，漏了它空格就会被吃掉），或收到 `/`，先 `SetFilterState(Filtering)` 再把这条按键转发给 list，字符进过滤框、fuzzy 重算。
2. **过滤态里方向键仍然移动**：`↑`/`↓`/`ctrl+k`/`ctrl+j` 由我们调 `list.CursorUp()` / `CursorDown()`，不转发（转发就被 textinput 吃掉）。
3. **`Enter` 立即确认**：不转发给 `AcceptWhileFiltering`（那会进 `FilterApplied` 态、过滤框消失、要再按一次），直接取 `list.SelectedItem()` 交给调用方。
4. **`Esc` 两段式**：过滤态 `Esc` 交给 list 的 `CancelWhileFiltering`（回到 Unfiltered、过滤框消失）；未过滤时 `Esc` 由调用方决定（`note add` 放弃向导、`note list` 退出）。这与 §3.1 的描述一致，也保住了 `note add` 现有文档里「`Esc` 放弃」的语义（只是多一层清空）。

另外必须**吞掉 `tab` 与 `shift+tab`**：`AcceptWhileFiltering` 的默认绑定是 `enter`/`tab`/`shift+tab`/`ctrl+k`/`up`/`ctrl+j`/`down`，规则 2 只拦了方向键那几个，不吞 Tab 的话一次 Tab 就会把列表推进 `FilterApplied` 态（过滤框消失、过滤仍生效），那个态下 `ClearFilter` 分支会让 `Esc` 变成「只清过滤」而不是退出——规则 4 的不变式就破了。吞掉 Tab（向导里 Tab 本来没有含义）后 `FilterApplied` 真的不可达，`ClearFilter` 绑定也就不会被触发。

### 9.4 分类步的目标形态

```
note add · 选择分类（1/3）

  > 未分类                    3 篇
    工作                      5 篇
    读书笔记                  0 篇
    + 新建分类…

输入 过滤 · ↑/↓ 选择 · Enter 确认 · Esc 放弃
```

- 头部与底部**仍由向导自己画**（`header("选择分类", "1/3")` 与 `help(...)`）；list 的 `Title`/`StatusBar`/`Help`/`Pagination` 全关。分类只有个位数，分页与状态栏是噪声，底部出现两套键位提示更是错乱。过滤框挂在 list 的 title 区，隐藏标题文字不影响它。
- 行 = 分类名（选中加粗）+ 右侧压暗的 `N 篇`，用自定义 delegate 画（`DefaultDelegate` 的样式体系与 `add_style.go` 不同，自己写更省事）。
- 高亮用 `MatchesForItem` 的命中下标配 `styleMatch`（现有配色），不再用 `matchRange` 子串高亮。
- **末行 `+ 新建分类…` 在任何过滤串下都可见**（旧设计文档 §3.2 的硬要求）：它是一个占位 item，靠自定义 `list.Filter`（`FilterFunc func(string, []string) []Rank`）保证——先对分类跑 `DefaultFilter`，再把占位项无条件追加到最后。这是本节最容易做错的一处。
- `Enter` 命中占位项 → 进「新建分类」子流程；命中分类 → 进标题步。
- 新建分类成功后：`SetItems` 重建（新分类追加在末尾），然后 `ResetFilter()` 清过滤串（否则 `SetItems` 会带着旧过滤串重算、过滤框也还在），再 `list.Select(indexOfCategory(m.all, created.ID))` 把光标落到新分类上，它成为当前选中项——与现有行为一致。
- `tea.WindowSizeMsg` 时 `list.SetSize` 要跟着走（向导已经在处理这个消息）。

### 9.5 要删/要留

| 位置                                                                    | 处置                                                                   |
| ----------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `addModel.filter`/`filtered`/`filterInput`/`applyFilter`                | 删：过滤串与结果都归 `picker`                                          |
| `filterCategories`                                                      | 删（连同用例）：fuzzy 由 `list.DefaultFilter` 提供                     |
| `matchRange`/`highlight`                                                | 删：改用 `MatchesForItem` 的下标 + `styleMatch`                        |
| `cursorMark`                                                            | 删：游标由 delegate 自己画                                             |
| `indexOfCategory`                                                       | 留：新建分类后定位光标仍要用它                                         |
| `styleMatch`/`styleCursor`/`styleSelected`/`styleMuted`/`help`/`header` | 留：delegate 与向导其余步继续用                                        |
| `add_test.go` 里走分类步的用例                                          | 改：键位与模型结构都变了，断言改为对 `picker` 的可见项与选中项         |
| `addUsage()` 的键位行（`usage.go`）                                     | 核对：仍是 `↑/↓ 选择 · 输入过滤 · Enter 确认 · Esc 放弃`，不含 `j`/`k` |
