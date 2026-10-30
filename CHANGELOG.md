# Changelog

本项目所有值得关注的变更都记录在此文件中。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。只记录影响需求、架构、构建或使用方式的关键变更，不记录日常格式化与零散改动。

## [Unreleased]

### Added

- `note list` 与 `note show`：CLI 终于能读笔记（实现过程与关键逻辑）：
  - **`note list`**：`internal/cli/list.go` 用 `bubbles/list` 做一个可模糊筛选的列表（输入即筛选、`↑/↓` 选择、`Enter` 预览、`Esc` 退出）；`Enter` 之后**退出 TUI 再打印预览**，与 `note show` 共用同一条渲染路径。stdin 或 stdout 不是终端时（管道、重定向、CI）自动降级为 `text/tabwriter` 对齐的纯文本（`id  标题  分类  updated_at`，时间原样输出 UTC 秒级文本），结果为空时**一个字节都不输出**，因此 `note list --plain | wc -l` 是 0 行。
  - **`note list` 的数据来源**：新增的 `store.ListNotesAll`（与既有 `ListNotes` 共用 WHERE/ORDER 组装与行扫描，但不分页、不做 COUNT、不写活动表）。列表一次性读进内存而不是逐键查库，理由很实在：`ListNotes` 在查询词非空时会 `RecordSearch`，逐键查库会把搜索历史刷爆。
  - **`note show <id>`**：`internal/cli/show.go` 加新增的 `internal/markdown` 包（全项目唯一 import glamour 的地方）渲染单条笔记。真终端下先打印三行头部（标题、`分类 · 本机时间 · id`、分隔线）再渲染正文；非终端不打印头部、默认 `notty` 风格（无 ANSI），只有显式 `--style` 才按显式值走。`--width` 取 20–1000，默认按终端宽度（上限 120），取不到时 80。
  - **加密笔记如实拒绝**：CLI 没有主密码，`note show`（以及 `note list` 里选中它）打印元信息与「请在 note web 里解锁查看」的提示并以退出码 4 结束——不是故障，但让脚本能区分「看不了」与「没找到」。
  - **共用选择器**：`internal/cli/pick.go` 给 `bubbles/list` 补上 gum filter 的键位语义（可见字符即进过滤态、过滤态里方向键仍能移动、`Enter` 立即确认、`Esc` 两段式：先清过滤再退出），并重绑掉默认绑定里会抢字母的导航键（`j`/`k`/`h`/`l`/`b`/`u`/`f`/`d`/`g`/`G`）、禁掉 list 自己的退出键、吞掉会破坏 `Esc` 不变式的 `tab`。`note list` 与 `note add` 的分类步都用它，项目里不再有两套选择器。

- `note add`：终端里的三步交互向导，新建一条明文笔记（实现过程与关键逻辑）：
  - **模型与分层**：`internal/cli/add.go` 是单进程 `tea.Program`，用 `step` 字段在三步之间切换、切步不清屏（`gum sequence` 同思路，避免"每步退出再起一个程序"造成错误提示无处回显）。向导只通过 `AddDeps` 的三个函数（`Categories` / `CreateCategory` / `CreateNote`）接触外部，`internal/cli` 因而**不 import `internal/store`**——交互逻辑可以用假实现直接喂 `tea.KeyMsg` 测，不必起 TTY；`cmd/note/add.go` 用 store 实现这三个函数，`store.DefaultCategoryID` 为「未分类」的判断提供单一来源（落库仍是 `category_id = NULL`，与 Web 端一致）。
  - **分类步**（v1.10 起已改为共用选择器，见下）——列表来自 `store.ListCategories`（含各分类篇数），输入任意字符即筛选（现为 fuzzy 匹配）；`↑/↓` 移动；列表末尾常驻「+ 新建分类…」且不受过滤影响，选中后进「新建分类」子流程：1–30 码点校验（与 Web 端 `maxCategoryNameRunes` 同口径）、重名与其它错误原地回显且不退出该步、成功后新分类进入列表并被选中，`Esc` 只回上一步而非放弃整个向导。
  - **标题步**：`textinput`，`CharLimit` 200，`Enter` 时 trim 后为空则原地回显「标题不能为空」。
  - **正文步**：照 `gum write` 用 `bubbles/textarea`——`Enter` 换行、`Ctrl+D` 完成、`Ctrl+E` 用 `$EDITOR`/`$VISUAL` 打开（临时文件 + `tea.ExecProcess`，未设置变量时只回显提示）、`Esc` 放弃；实时显示码点数，超过 2000 码点时 `Ctrl+D` 被拒并提示，可继续删改（与 `store.maxContentRunes` 同一 `utf8.RuneCountInString` 口径）。
  - **结果与退出码**：成功打印标题、分类、ID 与 `http://<addr>/notes/<id>` 链接（地址取自配置，需 `note web` 在跑）；用户放弃（`Esc`/`Ctrl+C`）打印「已放弃，未新建笔记」并以 130 退出；stdin 不是终端时立刻报错退出 1，**不**读管道当正文；`note add` 的其它参数与多余参数退出 2。
  - **视觉（gum 同款观感）**：命中过滤词的片段用强调色加粗高亮（现为 `highlightMatches`，按 fuzzy 给的命中下标染色；旧版的 `matchRange` 子串高亮已删）；选中行 `> ` 洋红 + 名字加粗；篇数、计数器、底部帮助、步骤号统一用灰色 `AdaptiveColor` 压暗，错误回显用红色；标题/正文档同样换成洋红提示符，正文的 `textarea` 去掉默认的 `┃ ` 前缀，外面套一层 gum write 那样的圆角细边框（边框色同灰色辅助文字，左右各留一列内边距，边框宽度固定不随内容长短跳动），正文框上方加一行 header：加粗的「新建笔记」+ 压暗的分类与已填标题上下文。配色随终端亮/暗主题切换（`lipgloss.AdaptiveColor`），非终端环境自动退化为无色纯文本。
  - **分类步的按键约定**（v1.10 起已改为共用选择器，见下）——过滤框只在过滤态可见；当时 `j`/`k` 在过滤为空时当上下移动（`gum filter` 的习惯），已输入过滤词后则如实进入过滤串；`↑/↓` 与 `ctrl+k/j`（以及 `ctrl+p/n`）始终用于移动。
  - **依赖**：新增 `github.com/charmbracelet/bubbles`（`textarea`/`textinput`）与 `github.com/charmbracelet/lipgloss`（配色；纯 Go、CGO 仍关闭；`go mod tidy` 解析到 bubbles v1.0.0 / lipgloss v1.1.0，并与 `bubbletea v1.3.10` 编译通过）。
  - **测试**：`internal/cli` 侧 27 项新用例覆盖过滤与匹配高亮区间、过滤框接管输入、30/200/2000 码点边界、空标题、重名与非重名回显、`Esc` 不落库、保存失败停在正文步、非终端拒跑、`-c/--db` 放行与缺值报错；`go vet` 与 `go test ./...`（9 包、顶层 175 个用例、含子测试 182 项，0 失败）全绿。
  - **已验证 / 未验证的边界**：真二进制验证了裸 `note` 打印帮助退出 0、`note add -h` 与 `note help add` 输出一致、`note add foo` 退出 2、管道与 `/dev/null` 喂 stdin 立刻报错退出 1 且不挂起、`--db` 指向新路径会自动建库迁移。**交互三步（含 `Ctrl+E` 外接编辑器）没做自动化**：`go test` 的 stdin 不是终端、`winpty` 在本机无 TTY 环境同样拒绝（`stdin is not a tty`），与 ADR-20 记录的限制相同，仍需真终端人工验收。

### Changed

- **`note add` 的分类步改用与 `note list` 同一个选择器**：过滤从「不区分大小写的子串匹配」升级为 fuzzy 匹配；行渲染与 `note list` 同一套（游标 + 名字 + 右对齐篇数，`ansi.Truncate` 夹到列表宽度）；`j`/`k` 不再是导航键（它们是过滤字符）；过滤框只在开始输入后出现（旧版一直显示一个「过滤分类…」占位框）。旧的 `filterCategories` / `matchRange` / `highlight` / `cursorMark` 一并删除。
- **依赖**：新增 `github.com/charmbracelet/glamour`（glow 的渲染内核；glow 本体是 `package main`，无法当库引用）与 `github.com/charmbracelet/x/term`；`github.com/charmbracelet/lipgloss` 被 glamour 抬到 `v1.1.1-0.20250404203927-76690c660834`。新依赖全是纯 Go，`CGO_ENABLED=0` 不变。
- **退出码**：`note list` / `note show` 引入 `4`（命中加密笔记，不是故障）；`note web status` 的 `3` 不变，两者语义不同。
- **裸 `note` 从交互菜单改为打印帮助**（等价 `note help`，退出 0），`internal/cli/menu.go` 与 `menu_test.go` 删除。这是破坏性变更：原先靠菜单「启动 Web 服务（后台）」的用户改用 `note web start`，菜单里的「系统信息」改用 `note info`。理由是只有三个选项的菜单把"到底有哪些命令"藏了起来，而帮助文本能直接列全。
- `note help` / `-h` 的命令表新增 `add` 一行；`usage.go` 新增 `addUsage()` 主题帮助（`note add -h` 与 `note help add` 输出同一份，三步与键位照抄设计文档）。
- `README.md` 的快速开始与前端开发命令改为 `note web`（原来写的是裸 `./note`，本就与「裸 `note` 进菜单」对不上），并补一句 `note add` 的入口说明。

### Removed

- `internal/cli/menu.go`（bubbletea 交互菜单）与 `internal/cli/menu_test.go`：功能由 `note add` 取代，`isTerminal` 移到 `add.go` 供向导使用。

- 新建笔记预填一份可编辑的 markdown 骨架（`note-example.md`，实现过程与关键逻辑）：
  - **骨架放在文件系统里**：`internal/template` 新包负责 `note-example.md` 的一切——`Path(configPath)` 是纯函数（`dir(note.yaml)/note-example.md`），`Default()` 返回 `go:embed` 进二进制的 `default.md`，`Ensure()` 只在文件缺失时写入默认骨架（**已存在绝不覆盖**），`Load()` 在文件缺失/不可读时回退内置默认并标记 `fromDefault`，`Save()` 走同目录临时文件 + `rename` 原子替换（`0644`：内容不含正文与密码，配置目录本身 `0700`）。
  - **初始化时机**：`cmd/note` 的 `serve(cfg)`（前台与后台子进程共用）在开库前调用 `Ensure`，创建成功打一行 info；配置目录只读时只 `slog.Warn`，服务照常启动——模板缺失不该让服务起不来。
  - **API**：新增 `GET /api/template` 与 `PUT /api/template`，响应统一为 `{content, path, source, default_content}`（`source` 为 `file` 或 `default`，`default_content` 给页面「恢复默认」按钮使用，前端不再另存一份默认值）；`application` 增加 `templatePath` 字段，由 `template.Path(cfg.ConfigPath)` 推导。PUT 走既有 `csrfProtect` 双提交校验、未扩大 `/api/shutdown` 的豁免；长度上限复用正文的 `maxContentRunes`（2000 码点），超限 400 `validation_failed` + `fields.content`——模板会原样变成笔记正文，允许更长等于制造「新建完就存不下」。写盘失败返回 500 且文案不含路径细节。
  - **回跳路径守卫**：`/template?next=` 与既有的 `/master-password?next=` 共用新的 `lib/nav.ts#safeNextPath`（只接受以 `/` 开头的站内绝对路径，拒绝 `//host` 与 `javascript:`）——外部地址会让 `history.pushState` 抛错、按钮看起来失效。正文标签行把「编辑模板」与计数器包进 `.field-label-actions`，避免三列 `space-between` 把按钮悬在行中间。
  - **前端入口**：新增 `lib/template.ts`（`getTemplate` / `saveTemplate` / `MAX_TEMPLATE_CODEPOINTS`，PUT 由 `lib/api.ts` 自动带 `X-CSRF-Token`）与 `views/TemplateView.svelte`（路由 `/template`：textarea + 复用 `Markdown.svelte` 的预览，300ms 防抖，显示模板文件真实路径，保存/恢复默认/返回）。「恢复默认」只把内置骨架填进 textarea、**不落盘**，误点一下不该覆盖用户改过的文件。
  - **新建笔记预填**：`NoteEditorView` 在新建且没有可恢复草稿时拉一次模板填进正文，`dirty` 保持 `false`（骨架不是用户输入，不该让 `beforeunload` 弹窗）；拉取失败静默降级为空正文。正文标签行新增「编辑模板」按钮（仅新建时显示），点击先用 `editorDraft` 存草稿再跳 `/template?next=/notes/new`，返回时恢复——复用「跳去设置主密码」那套交棒机制。另加 `restoredDraft` 守卫：用户把正文清空后去改模板再回来，空草稿优先于骨架，不会被重新灌一遍。
  - 不做的事：不碰数据库（无迁移）、不加模板变量、不改加密流程、不加 CLI 子命令；模板内容只在文本插值与 `Markdown.svelte` 里出现，`{@html}` 仍只有那一处。
  - TDD：`internal/template/template_test.go` 6 例（路径纯函数、默认骨架含 markdown 标记、`Ensure` 只写一次且不覆盖自定义内容、`Load` 缺失回退/存在直读、`Save` 原子替换无临时文件残留；权限断言按 `internal/daemon/state_test.go` 先例在 Windows 上跳过）、`internal/web/template_api_test.go` 5 例（缺失回退 `source=default`、PUT 后 GET 读回与落盘位置、超 2000 码点 400 且不写文件、空模板写成 0 字节、无令牌 PUT 403 而 GET 不需要 CSRF）、前端 `lib/template.test.ts` 4 例（GET、PUT 带令牌与 body、400 的 `fields.content` 透传、上限与正文一致）、`router.test.ts` 新增 `/template` 与带查询串两条断言。
  - 验证：`just test`（Go 7 包 156 项 + 前端 16 文件 137 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；真实二进制 + Edge headless 用 `.run/template-verify.mjs` 跑 10 项全 PASS（启动补出文件、模板页载入的就是文件内容、预览渲染、路径行、`<script>` 与 `javascript:` 均被净化、恢复默认只回填不落盘且给出「还要点保存」提示、保存后文件内容更新；脚本与库状态无关，连跑两次都 ALL PASS），`.run/template-editor-verify.mjs` 跑 6 项全 PASS（新建笔记正文预填模板而标题留空、编辑模板跳转带 `next=`、返回后标题与正文草稿都在、保存后详情页显示手写正文）。

- 删除改为可恢复的软删除 + 回收站（P0，实现过程与关键逻辑）：
  - **迁移** `sql/0006_note_soft_delete.sql`：`ALTER TABLE notes ADD COLUMN deleted_at TEXT`（UTC 秒级文本，`NULL` = 未删除，存量行升级后全是 `NULL`），并建两个部分索引 `idx_notes_active`（`updated_at DESC WHERE deleted_at IS NULL`）与 `idx_notes_deleted_at`（`deleted_at DESC WHERE deleted_at IS NOT NULL`），让默认列表与回收站列表各走一条窄索引；`user_version` 升到 6。不碰既有迁移与 `notes` 的 CHECK——软删除行仍保留 `content` 或 `cipher`，校验照旧成立。
  - **store 层**：`DeleteNote` 拆成 `SoftDeleteNote`（写 `deleted_at`）、`RestoreNote`（清 `deleted_at`）、`PurgeNote`（真 `DELETE`，事务提交后 `PRAGMA wal_checkpoint(TRUNCATE)`，必须在事务外才能生效）。三者的写入点都在事务内顺带记一条活动事件（`note_delete` / `note_restore` / `note_purge`），失败路径因为回滚不留事件；`RestoreNote`/`PurgeNote` 用 `classifyMissingNote` 区分「不存在」（404 `not_found`）与「存在但不在回收站」（409 `not_in_trash`）。
  - **语义取舍**：软删除与恢复都不动 `updated_at`、不 `rev + 1`——删了又恢复不该让笔记在按更新时间倒序的列表里跳到最前；恢复保留原 `category_id`。`GetNote` 仍读得到回收站笔记（详情页要显示恢复/彻底删除），但 `UpdateNote` 一律按 404 拒绝，编辑入口不会误写回收站笔记。
  - **读路径口径**：`ListNotes` 改签名 `ListNotes(ctx, NoteFilter{Q, CategoryID, Page, Deleted})`，默认只看 `deleted_at IS NULL`；`Deleted` 为真时按 `deleted_at DESC` 排序且不套用「标题命中排前」（回收站是按扔掉时间看的维护列表）。分类计数只算未删除笔记，但分类删除仍会把**所有**笔记（含回收站里的）的 `category_id` 置 `NULL`——外键不认软删除，否则删分类会留下悬挂引用。详情页的 `note_view` 只在笔记未删除时记录，回收站里的读取不算「使用」。
  - **API**：`DELETE /api/notes/{id}` 默认软删除，`?purge=1` 彻底删除（仅对回收站里的笔记生效）；新增 `POST /api/notes/{id}/restore` 返回完整笔记；`GET /api/notes?deleted=1` 列出回收站。`ErrNoteNotInTrash` 映射为 409 `not_in_trash`（「这条笔记不在回收站」）。恢复走 `postJSON`，因此照旧带 CSRF 双提交令牌。
  - **加密笔记的解锁门禁（前端门禁，守住服务端零密码学）**：列表与回收站里加密行的「删除 / 彻底删除」按钮直接 `disabled` 并用 `title` 说明要去详情解锁；详情页在 `awaitingKey`（没有会话密钥或 GCM 解密失败）时不渲染删除按钮，只显示「解锁后才能删除这条加密笔记」。回收站里加密行的「恢复」不设门禁——恢复不是读取明文。忘记密码页的「删除笔记」改成「软删除 + 立即彻底删除」：那条笔记谁也解不开，留在回收站只会变成一条既解不开也清不掉的残留。
  - **前端**：`lib/notes.ts` 增加 `purgeNote` / `restoreNote`、`deleted` 查询参数与 `deleted_at` 字段；新增 `views/TrashView.svelte`（`/trash`：移入时间、原分类、恢复、彻底删除、分页、空状态）与顶栏「回收站」入口；`App.svelte` 注册路由与 `document.title`；`NoteDetailView` 增加回收站提示条与「恢复 / 彻底删除 / 返回回收站」动作组；`lib/stats.ts` 把 `note_purge` 折进「删除笔记」柱、`note_restore` 单列一行。
  - TDD：`internal/store/note_trash_test.go`（默认读路径全部隐藏、回收站可见、恢复保留分类与时间戳、`restore`/`purge` 的 409/404 边界、重复删除 404、三类事件各一条、只剩回收站笔记的分类可删且恢复后落到「未分类」）、`store_test.go` 的 `TestMigration0006SoftDeleteColumn`、`internal/web/notes_api_test.go` 的 `TestNotesAPIRecycleBinFlow`（详情带 `deleted_at`、默认列表不出现、`deleted=1` 列出、恢复回原分类、`not_in_trash` 409、purge 后 404、无令牌 403）、`web/src/views/TrashView.test.ts` 5 例（jsdom 组件测试）、`lib/notes.test.ts` / `lib/stats.test.ts` / `router/router.test.ts` 补充。
  - 验证：`just test`（Go 8 包 152 项 + 前端 15 文件 132 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；真实二进制 + Edge headless 用 `.run/trash-verify.mjs` 跑 44 项全 PASS：软删除后列表/搜索/详情/事件口径、恢复后排回原位、`not_in_trash` 409、彻底删除后 404、忘记密码页的一次性丢弃、加密行在列表/详情/回收站三处的删除门禁，以及 1440 / 768 / 390 / 360 四个视口无横向溢出、窄屏动作按钮换行铺满。

- 首页「使用频率」的事件口径从「只统计查询」扩展到「查询 + 笔记与分类的读写」（实现过程与关键逻辑）：
  - **迁移** `sql/0005_activity_events.sql`：SQLite 不能就地改 CHECK，按「新建 `activities_v5` → `INSERT … SELECT` 拷数据 → `DROP TABLE activities` → `RENAME TO activities`」重建，再把 0002 的三条索引补回来（DROP 会把旧索引一起带走）。新 CHECK 枚举 `search` / `note_view` / `note_create` / `note_update` / `note_delete` / `note_restore` / `note_purge` / `category_create` / `category_rename` / `category_delete`——后两个 `note_*` 只是给第 2 条（软删除与回收站）预留，当前无写入点，避免以后为同一件事再重建一次表。`query` 列改为 `NOT NULL DEFAULT ''`：非搜索事件写空串，不引入 NULL 三值语义；历史 `search` 行原样保留，`user_version` 升到 5。
  - **写入点**：`internal/store/activity.go` 新增 `Activity*` 常量、`allowedActivityKinds` 校验表与 `insertActivity` / `recordActivityTx`（同时接受 `*sql.DB` 与 `*sql.Tx`）。笔记的建/改/删与分类的增/改/删都在**业务事务内**顺手插一行事件：`CreateNote` 为此从裸 `ExecContext` 改成 `inTx`、`DeleteNote` 把 `DELETE` 收进事务（`PRAGMA wal_checkpoint(TRUNCATE)` 仍留在事务外），失败路径（不存在、默认分类、非空分类、重名）因为回滚而不留事件。详情页「查看」只能写在 `handleGetNote`：`CreateNote`/`UpdateNote` 内部也会调 `GetNote`，埋在 store 层会把写操作重复计成查看；埋点失败只 `slog.Warn`，绝不影响详情返回。
  - **统计口径**：`Stats` 增加 `total_events`（全部事件）与 `events_by_kind`，`active_days` 与 `days`（热力图）改为按全部事件计数，`total_searches` 与 `top_queries` 保留且仍限 `kind='search'`；`SUM(...)` 用 `COALESCE` 兜住空库的 NULL。`events_by_kind` 只返回出现过的 kind、按次数倒序，中文文案与「分类维护」合并规则留在前端。
  - **前端**：`lib/stats.ts` 新增 `KindCount` / `EventGroup` / `groupEvents()`——固定六组顺序（查看笔记 / 新建笔记 / 编辑笔记 / 删除笔记 / 查询 / 分类维护），计数为 0 的组照常显示（「删除 0 次」本身也是信息），三个 `category_*` 合并成一行，认不出的 kind 汇总成「其他」以保证总量对得上，`share` 按展示总量算。`HomeView.svelte` 把「总查询次数」换成「累计事件」，新增「事件构成」列表（标签 / 占比条 / 次数三列，`app.css` 的 `.event-groups`）；热力图标题下补一句口径说明，`ActivityHeatmap.svelte` 的 tooltip 与 aria-label 改说「N 次操作」。
  - **隐私**：事件只落 `kind`、`created_at` 与（仅搜索时的）`query`，不记录标题、正文或分类名；日志仍不含正文与查询串。
  - TDD：`internal/store/activity_test.go`（8 类事件各一条、失败写操作不留事件、`RecordActivity` 拒绝未知 kind、聚合口径）、`store_test.go` 的 `TestMigration0005ActivityKinds`（CHECK 接受新旧 kind / 拒绝未知 / `query` 默认值 / 三条索引重建）、`internal/web/notes_api_test.go` 的 `TestStatsCountsCRUDAndViewEvents`（增删改查与查看各 1 条、删除后读 404 不计数）、`web/src/lib/stats.test.ts` 的 `groupEvents` 5 例。
  - 验证：`just test`（Go 8 包 145 项 + 前端 14 文件 123 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；真实二进制 + Edge headless 用 `.run/activity-verify.mjs` 跑 24 项全 PASS（`/api/stats` 的 8 类计数、首页六组文案与计数、构成条宽度 13/13/13/13/13/38、tooltip 改说「次操作」），`.run/activity-narrow.mjs` 在 360/390/768/1440 四个视口无横向溢出。

- 顶栏品牌位「笔记」前面加笔记本图标，并去掉下划线（`components/Layout.svelte` + `app.css`）：内联单色 `currentColor` 描边 SVG（书脊 + 两条文本线，`aria-hidden` + `focusable="false"`），`.brand` 改 `inline-flex` + `align-self: center`（顶栏整体是 `align-items: baseline`，不加 `align-self` 图标会把文字基线顶高）。下划线来源是全局 `a:hover { text-decoration: underline }`，因此用 `.brand:hover` / `.brand:focus-visible` 显式 `text-decoration: none`，改以文字与图标同时变 `--accent` 作为悬停反馈；键盘焦点框仍走全局 `a:focus-visible`。浏览器断言：常态与 hover 的 `text-decoration-line` 都是 `none`、图标 19×19、hover 颜色由 `--text` 变为 `--accent`。

- 分类树并入「全部笔记」页右侧，删掉独立的分类管理与「分类」tab（前端实现过程与关键逻辑）：
  - 新增 `web/src/components/CategoryTree.svelte`：只为展示与交互，分类数据与全部 API 调用仍留在 `views/NoteListView.svelte`。组件的三个写操作回调签名统一为 `(…) => Promise<string>`（空串=成功、非空=错误文案），于是 `category_exists` / `category_not_empty` 的 409 文案能就地摆到对应行下面，而组件不碰网络。组件内部状态只有展开、正在改名的 id 与输入值、各节点错误文案。
  - 树结构：根节点「全部笔记」（`$derived` 求和的总数、可展开收起）+「未分类」+ 各分类，子项显示名称与笔记数；默认分类 `id=1` 不渲染改名/删除按钮。行尾操作按钮常驻 DOM（`opacity: 0` + `:hover`/`:focus-within` 显示），因此键盘可达。改名走行内输入框（Enter 保存、Esc 取消、**失焦不保存**以避免误提交），新增走树底部常驻输入框。
  - `NoteListView.svelte` 改为 `.list-layout` 两栏（`minmax(0,1fr) 240px`，≤720px 单列并用 `order: -1` 让树排在列表上方）；新增 `createNamed` / `renameTo` / `removeCategory` 三个包装函数，失败时优先取 `ApiRequestError.fields.name`；删除的正是当前筛选分类时把筛选回落到「全部」并回第 1 页，否则列表会停在空结果。`q`/`category`/`page` 的 URL 约定与 `syncUrl()` 不变。
  - 删除 `views/CategoryManagerView.svelte`、`RouteName` 里的 `categories` 与导航项；`/categories` 在 `router.ts` 里改为映射到 `list`（不是 404）：旧书签仍能打开，`NoteListView` 挂载时 `syncUrl()` 会 `replaceState` 把地址纠正成 `/notes`。同时清掉 `app.css` 里只被 chips 与旧分类页使用的 `.chips`/`.chip*`/`.inline-form*`/`.manager*`。
  - 测试基建：`web/vitest.config.ts` 原本没有 Svelte 插件、解析条件指向 node，`.svelte` 文件在 Vitest 里根本导入不了。补 `plugins: [svelte()]` + `resolve.conditions: ["browser"]`（Svelte 官方 vitest 配法），不新增依赖，用 Svelte 自带的 `mount`/`flushSync` 写组件测试。
  - TDD：`CategoryTree.test.ts` 12 例（根节点计数与收起、行点击回传 id 与 `data-active`、未分类无按钮、新建 trim/空白不提交/错误文案落到输入框下、改名 Enter 与 Esc、删除确认与 409 文案、`busy` 禁用）与 `NoteListView.test.ts` 5 例（mock 整个 `lib/notes`：树与列表同渲染且无 chips、点分类以 `{q:"",category:2,page:1}` 重拉、删掉当前筛选分类后以 `category:0` 重拉、分类接口失败时列表仍可读、`?category=99` 无高亮且不崩）。先跑出 12/12 与 4/5 的真实 RED，再实现到全绿。
  - 验证：`just test`（Go 8 包 + 前端 14 文件 118 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；`.run/cat-verify.mjs` 用 Playwright-core + Edge headless 打真实二进制（独立库与端口）跑 21 项验收全 PASS：导航无「分类」、根节点计数、未分类无按钮、收起展开、筛选写入 `category` 且列表只剩该分类、新建与清空输入、重名文案就地显示、改名 Enter 生效、Esc 取消、非空分类删除 409 文案就地显示、空分类删除后消失、删掉当前筛选分类回到「全部」且 URL 归位、`/categories` 纠正为 `/notes`、≤720px 单列且树在列表上方。

- `note web` 支持后台运行：`note web start` / `note web stop` / `note web status`（v1.6，实现过程与关键逻辑）：
  - 新包 `internal/daemon`（纯库，无业务逻辑）：`state.go` 定义 `State`（pid/addr/urls/started_at/db/log/token/version）与路径纯函数 `StatePath(dbPath)=dir(DBPath)/note.json`、`LogPath(dbPath)=dir(DBPath)/note.log`，`Save` 走临时文件 + `rename` 原子写、权限 `0600`（Windows 权限语义不适用，断言按平台跳过），`Load` 区分「文件不存在」与「JSON 损坏」，`RemoveIfPid` 只在文件里的 pid 仍等于自己时删除（避免删掉新实例的状态文件），`NewToken` 用 `crypto/rand` 生成 32 字节 base64url 令牌。
  - 后台化只能自我 re-exec（Go 无 fork）：`RunDetached` 重新执行自身并立即返回，Unix 用 `SysProcAttr{Setsid: true}` 脱离会话，Windows 用 `DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP`（`0x8|0x200`）+ `HideWindow`；子进程靠环境变量 `NOTE_DAEMON_STATE` 认领后台身份。日志必须在**文件描述符层**接管（`cmd.Stdout`/`cmd.Stderr` 指向 `note.log`）——slog 默认 handler 在启动时就把 `os.Stderr` 绑定死了，运行期换写不出文件。
  - 关停通道 A1：`internal/web` 新增 `Control{Token, Shutdown}` 与 `NewWithControl`，`POST /api/shutdown` 仅在 Token 非空时注册（空 → 403 `shutdown_disabled`，令牌不符 → 403 `shutdown_denied`，常量时间比较，成功 202 后异步调用 `Shutdown`）。令牌由**子进程**生成并写入状态文件，父进程不经手，因此不进命令行与环境变量。middleware 的 CSRF 豁免只针对这一条路径（凭证即令牌），不得扩大。
  - `internal/cli`：`WebAction`（`WebForeground` 零值 / `WebStart` / `WebStop` / `WebStatus`）+ 未知子命令报错；`status.go` 的 `StatusData`/`FormatStatus` 三态渲染（运行中 / 未运行 / 进程存在但不响应），pid 行仅在有 pid 时出现，`formatUptime` 与 `fileSummary` 复用 `info.go` 的 CJK 对齐；`info.go` 数据段新增「日志」行（存在显示大小，不存在显示「未创建」）。
  - `cmd/note/web_service.go`：`runWebStart` 幂等（已在运行且探测通过 → 打印地址退出 0）、就绪轮询 100 ms / 总超时 10 s、失败时回显 `note.log` 最后 20 行；`runWebStop` 仅在「状态文件里有 pid 且该 pid 活着」时才发关停请求，10 s 未退出则 SIGKILL / `TerminateProcess` 并说明「WAL 未完成 checkpoint，数据安全但下次启动会重放日志」，只有探测到响应而无状态文件时判为前台实例、提示回终端 Ctrl+C 并退出 1（不按 pid 猜着杀进程）；`runWebStatus` 把状态文件与 `GET /api/info` 探测合并判定，退出码 0 / 1 / 3，残留状态文件就地清理并标注。`main.go` 新增 `exitError`（错误已打印时不再补一条 `startup failed`）与 `opts.WebAction` 分派；菜单项改为后台启动（前台启动会被菜单退出连带杀掉）。
  - 状态文件按目录唯一，因此同一目录换一个 `--db` 会共用它：新增 `sameDB` 归属校验，`start` 拒绝启动、`stop` 拒绝关停、`status` 只做「属于另一个数据库」提示而不冒领别人的 pid。
  - TDD：`state_test` / `control_test` / `pid_test` / `probe_test` / `spawn_test` / `shutdown_api_test` / `parse_test`（web 动作）/ `status_test` 全部先 RED（undefined 或断言失败）后 GREEN。
  - 验证：`just test`（Go 8 包 139 项 + 前端 12 文件 100 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；真实二进制冒烟 12 项：`start` 生成状态文件与日志、重复 `start` 幂等、`status` 三态与退出码 0/1/3、`info` 有日志行、`stop` 在 0.26 s 内优雅退出且日志出现 `shutting down reason="control endpoint"`（证明不是强杀）、`status` 随后退出 3、端口占用时回显日志尾部、前台实例拒绝 `stop`（仍在响应）、残留状态文件被清理、损坏状态文件报 parse 错误退出 1、同目录另一个库被拒绝。
  - 实现中发现并修复的真实缺陷：Windows 上 `os.FindProcess` 对**已退出但进程对象尚未回收**的 pid 仍返回成功，导致 `stop` 白等 10 s 后误报「已强制结束」、`start` 误判「进程在但不响应」；`pid_windows.go` 改为 `OpenProcess(SYNCHRONIZE)` + `WaitForSingleObject(0)`（`ERROR_ACCESS_DENIED` 视为活着），并补 windows-only 回归用例。

- 后端改为 CLI 子命令启动，引入 bubbletea 交互菜单，默认配置/数据位置迁移到用户目录（v1.5，实现过程与关键逻辑）：
  - 新包 `internal/cli`：`Parse` 手写全局层（裸 `note` → 菜单、`web` / `info` 子命令、全局 `-c`、`help` / `-h` 变体），子命令与参数本身交给标准库 flag；预扫描 `-h` 时跳过带值参数的值（`note web --config -h` 不是帮助请求而是非法配置路径）；参数错误打印错误加帮助并退出码 2。`menu.go` 用 bubbletea 实现菜单（↑↓/j/k 移动、Enter 选择、q/Esc 退出），模型不持有任何服务资源，退出后由 `cmd/note` 接手启动；`isatty` 检测 stdin 非终端（脚本/管道）时直接返回回退动作，主进程打印帮助退出而不挂起——真实控制台拉起菜单存活 3 秒验证 TUI 初始化不崩溃。`info.go` 的 `FormatInfo` 用 uniseg 计算 CJK 显示宽度对齐键值列，数据库不存在时开占位符不建文件。`usage.go` 动态带出本机默认配置/数据库路径。
  - `internal/config/paths.go` 新增 `DefaultConfigFile` / `DefaultDataDir` / `DefaultDBFile`：Windows `%AppData%\note\`、macOS `~/Library/Application Support/note/`、Linux 按 XDG 拆分（配置 `~/.config/note/note.yaml`、数据 `~/.local/share/note/notes.db`，`XDG_DATA_HOME` 缺省回退 home），与其他常见开源项目一致；仓库内 `config/note.yaml` 重置为原始默认值作示例，不再被自动加载。
  - `config.Load`：默认路径改由上述函数解析；新增 `-c` 短参数（与 `--config` 同义，显式指定缺失仍报错）、`-p` / `--port`（在地址解析完成后只替换端口，host 沿用内置默认/配置文件/`--addr`，IPv6 经 `net.SplitHostPort` + `JoinHostPort` 保持方括号）、`--remote`（`--allow-remote` 别名）；`Config` 增加 `ConfigFound` 供 info 展示。
  - `cmd/note`：`run` 只做分发；`loadConfig` 把全局 `-c` prepend 到子命令参数前（flag 后值覆盖前值，子命令 `-c` 优先）；`runWeb` 原服务生命周期不变（启动 URL 打印、优雅退出）；`runInfo` 先 `os.Stat` 确认库存在才 `store.Open`（schema 版本、笔记/加密/分类计数），全新环境不产生文件。
  - `note info` 与 `GET /api/info` 同源同口径（构建信息来自 `internal/build`、运行时来自本进程、配置来自同一套 `Load`），同样不含密文、盐、口令或令牌。
  - 配套：`scripts/dev.ps1`、`scripts/dev.sh` 改为 `go run ./cmd/note web`，`.run/info-e2e.mjs` 的 spawn 参数同步加 `web`；依赖新增 `charmbracelet/bubbletea`（纯 Go 无 CGO），直接引用其随包依赖 `mattn/go-isatty` 与 `rivo/uniseg`。
  - TDD：`paths_test` / `parse_test` / `menu_test` / `info_test` / `usage_test` 先 RED（undefined / 断言失败）后 GREEN；`config_test` 重写默认路径断言（chdir 方案改为环境变量隔离）并新增短参数、端口覆盖（含 IPv6）、非法端口、`--remote` 别名、`ConfigFound`、`-c` 缺失报错六组用例。
  - 验证：`just test`（Go 7 包 108 项 + 前端 12 文件 100 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；真实二进制冒烟：`-h`/`help web` 帮助正常，`note foo` 退出码 2，`info -c` 输出对齐且库未建时零副作用，`web -c <临时配置> --port 8123` 覆盖配置里的 9090 端口、`/` 200、`/api/csrf` 200、`/api/info` 反映最终地址，管道环境裸 `note` 退化为帮助，真控制台菜单正常运行。
  - 文档同步：PRD v1.5（修订说明、8.9 配置与运行重写、NFR-S5/8.8 Host 校验/R8 措辞、AC-45 ~ AC-47、M6 里程碑、8.2 目录、11.1/11.2）；TECH_DESIGN v1.5（架构图、2.3 目录、3 技术选型加 bubbletea、4.1 命令表、4.2 配置重写、8.6 note info 同源说明、12.1 测试表、14.1 运行与迁移指南、15.6 M6、ADR-20）；AGENTS.md 部署形态。

### Fixed

- 冒烟时发现并修掉两个只有真实运行才会暴露的缺陷：(1) Windows 上 `os.FindProcess` 对已退出但进程对象未回收的 pid 仍返回成功，导致 `stop` 白等 10 秒后误报「已强制结束」、`start` 误判「进程在但不响应」，改为等待句柄判定并补回归用例；(2) `stop` 在关停请求返回 403（令牌不符）时仍按 pid 强制结束，可能杀掉占用同端口的无辜进程，改为报错退出 1 并保留状态文件，只有「连不上/超时」才走强制结束。
- `note web status` 的多地址续行改为按标签列宽计算缩进（原来写死空格数，CJK 标签下第二行会偏移）。

### Security

- 软删除把「删除即清页」的安全不变量收窄成「彻底删除才清页」：回收站期间明文与密文仍在库里，因此 PRD FR-3.12 / NFR-S4 与附录 B 的口径同步改为「软删除只是标记，`secure_delete` + WAL checkpoint 只在 `PurgeNote` 后执行」。加密笔记在回收站里仍要求解锁才能彻底删除，忘记密码页的删除是「软删除后立即彻底删除」，不在库里留下既解不开也清不掉的密文残留。

- 新增只对本地可见的关停凭证：`/api/shutdown` 需要 32 字节随机令牌（`X-Shutdown-Token`，常量时间比较），令牌只存在于 `0600` 的 `note.json`，不进日志、响应、命令行或环境变量；该端点只在后台模式注册，仍受 `hostGuard` 约束，CSRF 豁免只覆盖这一条路径。`note web status` 的探测只读 `GET /api/info`（本就 `no-store` 且不含正文/密文/盐/令牌）。

### Changed

- 移动端与浏览器兼容适配：断点收敛为 1024px / 640px 两档，360–430px 触控视口可独立走完主流程（实现过程与关键逻辑）：
  - **断点**：`app.css` 原本零散着 480 / 560 / 720 三处媒体查询，改成两档并统一挪到文件末尾（同优先级下后写的规则才生效；把媒体查询留在基础规则之前，`min-height`、`opacity` 这些会被后面更靠后的基础声明覆盖掉）。`≤1024px` 时编辑页与列表页收成单列、分类树用 `order: -1` 提到列表上方、预览区 `max-height: 50vh` + `overflow-y: auto` 自带滚动；`≤640px` 时进入触控布局。
  - **顶栏**：用 `order` 把品牌/用户名/主题留在第一行、导航整行下移到第二行并横向滚动（隐藏滚动条），四个链接各自 `min-height: 44px`。只改视觉顺序，不动 DOM 顺序，读屏与 Tab 顺序保持「品牌 → 概览 → 全部笔记 → 新建 → 回收站 → 用户」。
  - **触控尺寸**：`.button`、`.cat-act`、`.cat-row` 在 ≤640px 时不低于 44px；输入框字号统一 `1rem`（iOS 对 <16px 的输入框会在聚焦时自动放大页面，等于把布局顶破）。
  - **卡片式列表**：`.note-row` 在 ≤640px 换行，正文按钮与删除按钮各占一行，回收站的恢复/彻底删除并排铺满；首页搜索框与按钮各占一行；解锁表单按钮独占一行；信息页 `.info-rows` 改成「标签一行、值一行」，长路径不再被右对齐挤成锯齿。
  - **热力图**：53 周语义不变，只把格子从 11px 缩到 10px，其余交给容器横向滚动（`overscroll-behavior-x: contain` + `-webkit-overflow-scrolling: touch`），窄屏显示「左右滑动可查看全年」提示；容器补 `tabindex="0"`（可滚动区域需要键盘焦点，WAI 对 scrollable region 的建议，已显式忽略 `a11y_no_noninteractive_tabindex`）与更完整的 `aria-label`。
  - **视口与安全区**：`body`/`.shell`/`.boot`/`.onboarding` 用 `min-height: 100dvh`（前一行保留 `100vh` 回落）；`main` 底部与 `.toasts` 用 `env(safe-area-inset-*)`，`index.html` 的 viewport 补 `viewport-fit=cover` 才能生效。
  - **长文本**：`body` 与 `.note-title`/`.detail-head h1` 补 `overflow-wrap: anywhere`，长中文标题、长查询词、长路径与报错文案就地换行。
  - 断点外的行为不变：`(hover: none)` 或 ≤1024px 时分类操作按钮常显（不可依赖 hover 才能点到），焦点样式统一走 `:focus-visible`。
  - 验证：`just test`（Go 8 包 152 项 + 前端 15 文件 132 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；`.run/responsive-verify.mjs` 用真实二进制 + Edge headless 跑 116 项全 PASS——四个视口 × 七个页面无横向溢出与出屏元素、窄屏按钮 ≥44px 与输入框 ≥16px、热力图可滚动且不压缩、编辑页单列 + 预览自带滚动、列表页分类树在列表上方、三套主题在 390 视口不破版、720×450（≈200% 缩放）无溢出、390×844 触控全流程（搜索 → 详情 → 编辑预览 → 保存 → 删除 → 恢复 → 分类增删改 → 加密笔记解锁与删除）。`.run/trash-verify.mjs` 51 项回归照旧全 PASS。

- 删除语义从「立即永久删除」改为「先移入回收站」：`DELETE /api/notes/{id}` 只打 `deleted_at`，彻底清除走 `?purge=1`；数据库 schema 升到 `user_version = 6`（旧库启动自动加列并建两个部分索引，存量笔记全部视为未删除）。加密笔记的删除改为**必须先解锁**——这与旧 PRD FR-1.4「加密笔记删除不要求密码」相反，门禁落在前端交互层（服务端仍零密码学）。页面数量增至 10 个主视图（新增回收站 `/trash`）。

- 首页「使用统计」不再显示单一的「总查询次数」，改为「累计事件 + 活跃天数 + 事件构成（六行）」，热力图与活跃天数改为按全部事件（查询 / 查看 / 新建 / 编辑 / 删除 / 分类维护）统计；`/api/stats` 新增 `total_events` 与 `events_by_kind`，`total_searches` 与 `top_queries` 保留但仅指查询。数据库 schema 升到 `user_version = 5`（旧库首次启动自动重建成 `activities` 表，历史查询记录不丢）。
- 前端页面数量回到 9 个主视图：「分类管理」页并入「全部笔记」页右侧的分类树，分类的新增、改名、删除与筛选都在树内完成；`/categories` 退化为指向列表页的旧地址兼容。
- `note web`（不带子命令）语义不变：仍在前台运行、日志进终端、不创建日志文件；后台形态是新增的 `note web start`。交互菜单的「启动 Web 服务」改为后台启动，日志写入数据库同目录的 `note.log`。`note info` 增加「日志」一行。
- 破坏性：裸 `note` 不再直接启动 Web 服务（改为交互菜单，`note web` 才启动）；默认配置文件从 `./config/note.yaml` 迁到用户配置目录、默认数据库从 `./data/notes.db` 迁到用户数据目录。旧数据继续使用：`note web -c ./config/note.yaml --db ./data/notes.db`，或把文件拷到新默认位置；不做隐式回退，避免双源配置难以排查。

### Added

- 启动时输出可直接点击的访问地址（实现过程与关键逻辑）：
  - `internal/config/urls.go` 新增纯函数 `AccessURLs(addr)`：回环、具体 IP 或主机名直接拼装成 `http://host:port`（IPv6 经 `net.JoinHostPort` 一律加方括号）；通配 host（`0.0.0.0` 枚举非回环 IPv4，`::` / 空 host 枚举 IPv6）逐接口枚举地址，跳过回环、链路本地（169.254 与 fe80）与组播，去重后按字典序每地址一条；枚举为空回退回环地址，保证至少打印一条可用 URL。
  - Windows 实测发现 `net.Listen("tcp", "0.0.0.0:8100")` 建立的是双栈 socket，`ln.Addr()` 汇报 `[::]:8100`——直接用 listener 地址推导会丢掉「请求的是 IPv4 家族」这层信息，把本机全部非回环 IPv4 漏掉、只打出 `[::1]` 回退。因此再抽 `StartupURLs(请求地址, listener 实际地址)`：家族取请求 host，端口取 listener 实际端口（机制上兼容 `:0`），`main.go` 只调它。
  - `cmd/note/main.go` 从 `ListenAndServe` 改为先 `net.Listen` 再 `Serve(ln)`：监听失败同步返回且错误回带尝试地址（实测 `listen on 127.0.0.1:8098: listen tcp ...`，非零退出）；URL 经 `fmt.Println` 单独打到 stdout（slog 结构化日志与其互不混写，`addr`/`db`/`allow_remote` 字段原样保留），服务在打印前已经开始 Serve，地址枚举不阻塞任何请求处理。
  - 单测 19 项（TDD：先 RED `undefined: AccessURLs`，后 GREEN），通配分支注入假网卡、不依赖真实接口；真实二进制实测四种形态：默认回环打印 `http://127.0.0.1:8099` 且 curl 200；`--addr 0.0.0.0:8100 --allow-remote` 逐条打印本机 5 个非回环 IPv4（无 `0.0.0.0`、无链路本地），列表中地址 curl 可达（返回 400 `host_invalid` 属既有 Host 白名单行为，远程访问门禁归 TODO「远程访问开关与 API Key 访问门禁」项处理）；`--addr [::1]:8101` 打印 `http://[::1]:8101`；端口被占时非零退出且错误回带地址。
  - 验证：`just test`（Go 5 个包 77 项 + 前端 12 个文件 100 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；文档同步 TECH_DESIGN 4.2 / 14.1 与 PRD 8.10。

### Added

- 全分支评审后的修复（改密收尾与续跑的可判定语义，实现过程与关键逻辑）：
  - 改密收尾不再掩盖成功：`InfoView.submitPassword` 原先在 `runMigration` 之后仍串「顺带改助记问题」与「重读清单复核」两步，任一步失败都落进同一个 `catch`，迁移状态卡因此根本不出现，界面只说「操作失败」——而密文其实已经全部换完。用户拿真正的旧口令重试时 `verifyOldPassword` 必然失败，界面就说出了「主密码不正确或加密数据已损坏」这句假话。现在 `sessionKey.set(新密钥)` 之后立刻 `report = result`、`remaining = result.remaining`，附带步骤各自 `try/catch`：改问题失败只追加「主密码已更新，但助记问题没改成功」，复核失败退回报告自带的计数。新增纯函数 `reencrypt.countRemaining(items, newKey, deps, fallback)` 承担这个退回（复核里非 `DecryptError` 的失败不再往上抛），用例先 RED（`countRemaining is not a function`）后 GREEN。
  - 「继续迁移」不再预填一个已被证明解不开的口令：`resume()` 原先无条件把新密码抄进旧密码栏。响应丢失型中断（迁移其实跑完了）这样是对的，但中途退出时剩下那些仍受**上一代**口令保护，预填新密码等于让用户白烧两轮 PBKDF2 再撞一次同样的失败。新增 `reencrypt.needsPreviousPassword(report)`（报告含 `failed_old_key` 即为真），InfoView 据此决定预填与文案：需要上一个口令时旧密码栏留空、按钮写成「继续迁移（请输入上一个主密码）」。两条用例同样先 RED 后 GREEN。
  - 文档收回一处做不到的承诺：规格与技术方案都写过「KDF 强度可以借改密提升」，但改密的旧密钥统一按当前 `KDF_PARAMS` 派生，从不读笔记各自的 `kdf_params`——上调 `iter` 会让改造前的笔记在改密时全部判为 `failed_old_key`（单条解锁不受影响，那条路径按笔记自身参数派生）。措辞改为「归一化，不是提升」并写明收紧需要什么（迁移按笔记参数分别派生旧密钥）；`EncryptedItem.kdf_params` 上加同源注释。
  - 补两处此前只写在评审里的边界：`PUT /cipher` 不带 `rev` 校验，迁移途中另一页签对同一条笔记的编辑会被写回覆盖（规格 §10 不做并发保护，接受并写明收紧办法）；顺带修正端到端断言计数（实际 58 项，此前文档写 57）。
  - 验证：`just test`（Go 4 个包 + 前端 12 个文件 100 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；`just build` 后重跑 `.run/info-e2e.mjs`——60 项断言全绿。「第二条 PUT 失败被记进迁移状态卡」「刷新后迁移状态卡不残留」「新密码填旧密码栏 → 已迁移那条 `already_done`、没迁那条 `failed_old_key` 且密文未损坏」「真实旧口令续跑 `remaining` 归零」这几条走的正是改写后的收尾分支；本次新增的两条断言直接在浏览器里点「继续迁移」，验按钮文案写明「请输入上一个主密码」且点完旧密码栏是空的（不预填刚设的新密码）。「改问题失败只追加提示、不掩盖改密成功」这条分支脚本没有制造该失败，由 `countRemaining` 用例与代码结构覆盖。

### Added

- 用户信息页端到端实测（`.run/info-e2e.mjs`，Playwright 驱动 Edge headless，60 项断言全绿）：
  - 环境：`just build`（必须走 just，否则 `ldflags` 不注入、看不到 commit 短 hash）产出 `note.exe`，脚本自己起停**三次**隔离实例——`--config .run/e2e-note.yaml --addr 127.0.0.1:8099 --db .run/e2e-info.db`。`--config` 单独隔离是必须的：改用户名会原子重写配置文件，不隔离就会把开发者真实 `config/note.yaml` 里的名字改掉。断言用 `node:sqlite` 只读打开同一个库，密文比对取 `cipher` BLOB 原始字节。
  - 引导：全新库走「填名字 → 刷新 → 仍在引导页第 2 步 → 设密码 → 进主界面」，`master_password` 只有一行、盐 16 字节且与 `GET /api/master-password` 的 `salt_b64` 一致，两次不一致被前端拦下时 `POST /api/master-password` 计数为 0，全程只 POST 一次；密码/密钥不进 `localStorage`/`sessionStorage`。
  - 已配置形态：脚本用两个标签页确定性地复现——页面 A 停在第 1 步（它的 settings 还是「主密码未配」），页面 B 先走完引导，回到 A 填名字后第 2 步就是解锁形态，`#master-password` 不再渲染、`POST` 计数为 0（重复 POST 会 409），库里仍是同一行同一盐。
  - 信息页：`/info` 直达；「Git 构建」行含构建时的短 hash，「服务版本」= `1.4.0 (<hash>, <UTC 构建日期>)`，运行平台/Go 版本/主机名/内存/数据文件/结构版本 4/监听地址/允许远程访问逐项核对；改用户名后界面、`GET /api/settings`、`name_set: true` 配置文件三处一致；改助记问题后 `salt_b64` 与首次设置相同。
  - 改密主链路：错误旧密码被拦下且两条 `cipher` 字节 + `updated_at` + `rev` 一字不变；正确旧密码跑完后两条密文都换、`updated_at`/`rev` 仍不变。中途退出用 `page.route` 只放行第一次 `PUT /api/notes/{id}/cipher`、AbortError 掉第二次 → 迁移卡如实显示「成功 1 · 跳过 0 · 失败 1（还有 1 条未迁移）」与「提交失败（Failed to fetch）」；reload 后迁移卡不残留（迁移状态本来就不落盘）。
  - **实测纠正了设计文档的一处乐观表述**：规格说中途关页面后「把新密码填进旧密码栏就能自然续上」，只对「PUT 已发出但响应丢了」的情形成立；真的只迁完一条时，剩下那条仍受**上一个密码**保护，填新密码会得到 `failed_old_key`（remaining 不归零），但已迁移那条判为 `already_done` 未被二次加密、未迁移那条密文字节与中止前完全相同（无损坏），卡片文案「这条笔记不是用刚才那个旧密码加密的」正是用户需要的提示。接着把真正的上一个密码填进旧密码栏续跑：只有没迁的那条被改写，remaining 归零，两条随后都能在详情页用新口令当场解开。
  - Security（明文残留）：改密全程用了 4 个口令、2 条加密正文与 1 条明文对照正文。扫描 `.run/e2e-info.db` 与 `-wal`：4 个主密码明文与 2 段加密正文都不在任何数据文件里，而明文对照组的正文确实出现在文件里——证明扫描方法本身有效、不是漏检。
  - 验证：`just test`（Go 4 个包 + 前端 12 个文件 96 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）在实测前后都通过。
- 用户信息页与浏览器端改密（实现过程与关键逻辑）：
  - 入口：顶栏 `.who` 由 `<span>` 改成 `<a href="/info">`（站内跳转走 `link()`，保留中键/新标签打开），新增路由 `/info`（`RouteName` 加 `'info'`、pattern `^\/info$`，带查询串仍按路径匹配）。
  - 运行信息卡：`web/src/lib/info.ts` 的 `buildRows(ServerInfo)` 把 `GET /api/info` 摊成 14 行中文键值（服务版本 / Git 构建 / 构建日期 / 运行平台 / Go 版本 / 主机名 / CPU 核心数 / 内存占用 / 本次运行时长 / 启动时间 / 数据文件 / 数据库结构版本 / 监听地址 / 允许远程访问）；缺失值统一显示「未知」而不是 `undefined`，`uptime_seconds` 摊成「X 小时 Y 分」，字节数按 B/KB/MB/GB 取一位小数，`allow_remote` 摊成「是/否」——都是纯函数，Vitest 直接断言。
  - 改密引擎：`web/src/lib/reencrypt.ts` 是纯函数 + 依赖注入（`decrypt/encrypt/putCipher` 全部可替换），`verifyOldPassword` 逐条试解、第一条成功即通过（不能只试第一条，库里可能混着更早一次改密留下的笔记），`planMigration` 用新密钥试解把已迁移的排除、使整条流程天然幂等，`runMigration` 把每条落定归入设计文档 §6 的五类（`done`/`already_done`/`skipped_gone`/`failed_old_key`/`failed_network`）并继续跑完其余；`remaining` = 后两类条数。明文只作局部变量，绝不进 `MigrationRow.message`（有用例断言整份报告序列化后不含正文）。
  - 改密流程（`InfoView` 只做编排）：拿盐 → 派生旧密钥 → 取密文清单 → 验证旧密码（清单为空直接通过）→ 派生新密钥 → 逐条 `decrypt(旧) → encrypt(新) → PUT /api/notes/{id}/cipher` → `sessionKey.set(新密钥)` → 重读清单再跑一次 `planMigration` 复核；旧密码验证不过时不动会话密钥、不写任何密文。第 4 张卡在 `remaining > 0` 时列出未迁移条目与原因并给「继续迁移」——中途关页面后把新密码填进旧密码栏续跑，已迁移的判为 `already_done` 原样跳过。
  - 助记问题与信息页改用户名：内联表单调 `PUT /api/master-password/question`，服务端拒绝（空/超 100 码点）时页面显示的仍是旧问题、只有输入框标红；改密表单可留空顺带更新问题。
  - 引导页改两步式：判据抽成纯函数 `decideStep(nameSet, masterConfigured): 0|1|2`（`web/src/lib/onboarding.ts`），`App.svelte` 的门槛与 `document.title` 共用同一判定。步骤 1 存名字成功后只本地推进到步骤 2、不重读设置；步骤 2 挂载时读 `GET /api/master-password`，未配置 → 完整设置表单（密码 + 确认 + 助记问题 + 不可恢复勾选，主密码必填、无「跳过」），已配置（上次 POST 成功但解锁前刷新）→ 只渲染解锁输入框、只 `unlockKey` 绝不重复 POST。`settings.loadSettings()` 改为 `Promise.all` 并行读 `/api/settings` 与 `/api/master-password`，任一个失败整体进 `error` 态——引导判据不能建立在半套数据上。
  - `MasterPasswordView` 的「已经配置过了」只读卡改为指向 `/info`，文案随之从「无法再次修改」改成「可到信息页修改」。
  - 验证：`just test`（Go 66 项 + 前端 96 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；新增 `reencrypt.test.ts` 15 项、`info.test.ts` 9 项、`settings.test.ts` 4 项（含「主密码接口失败必须整体进错误态」这条先用旧实现跑出 RED）、`notes.test.ts` 4 项、`router.test.ts` 1 项、`onboarding.test.ts` 3 项。
- 信息页后端：构建信息端点、加密笔记密文清单/替换、助记问题单独修改（实现过程与关键逻辑）：
  - 构建信息：新增 `internal/build`，`Version`/`GitHash`/`GitDirty`/`BuildDate` 四个包级变量由 `justfile` 的 `ldflags` 变量用 `-X` 注入（按 `os()` 分支，Windows 走 PowerShell、其余走 bash 命令替换）；未注入时（`go run`、`go test`）`Describe()` 回落到 `runtime/debug.ReadBuildInfo` 的 `vcs.revision`/`vcs.time`/`vcs.modified`，`Label()` 此时输出「1.4.0（开发构建）」而不是空括号。just 在 Windows 上把命令包成 `cmd /C "powershell -Command …"` 且只保留最后一行输出，所以 PowerShell 分支必须以裸 `$a` 结尾。
  - 接口层：`GET /api/info` 返回 version / git_hash / git_dirty(bool) / build_date / build_label / go_version / goos / goarch / hostname / cpu_count / mem_alloc_bytes / mem_sys_bytes / started_at / uptime_seconds / db_path / schema_version / addr / allow_remote —— 全是标准库运行时与配置值，不含正文、密码、密钥或令牌；`application` 新增 `startedAt`（进程启动 UTC 时刻），`Store.SchemaVersion` 读 `PRAGMA user_version` 供页面展示已应用迁移编号。
  - 密文迁移端点：`GET /api/notes/encrypted` 只输出 `{id, cipher_b64, kdf_params}`（空清单序列化成 `[]` 而非 `null`，前端不必判空），`PUT /api/notes/{id}/cipher` 只替换 `cipher` 与 `kdf_params`，元数据（title/content/created_at/updated_at/rev/category_id）逐字不变——这是浏览器端批量重加密「改一次口令却不动用户看到的修改时间」的接口基础。路由把静态段 `encrypted` 注册在通配 `GET /api/notes/{id}` 之前，`go test` 断言清单请求没被通配吃掉。
  - 存储层：`ListEncryptedCiphers`（按 `created_at, id` 稳定排序，改密过程中新增/删除笔记不会让分页错位）与 `UpdateNoteCipher`；后者先在事务外做只读探针，把「笔记不存在」和「笔记未加密」这两类错误明确区分成 `ErrNoteNotFound` / `ErrNotEncrypted`（`db.SetMaxOpenConns(1)` 下事务内再开第二条查询会死锁，全局盐也必须事务外取），错误码映射为 404 `not_found` / 409 `not_encrypted` / 400 `validation_failed`。`decodeCipher` 从 `normalizeBody` 抽出复用，base64 与 29–65536 字节的密文长度校验两处语义一致。
  - 助记问题：`Store.UpdateMasterQuestion` 只 UPDATE `question` 一列，盐与 `created_at` 不动（改问题不能顺带换盐，否则已有密文全部解不开），空问题与超 100 码点分别返回字段错误；`PUT /api/master-password/question` 走 CSRF 保护。`master_password_exists` 的旧文案「无法再次修改」改成「主密码已设置，如需更换请到信息页的主密码卡片」——引导页设的主密码如今确实可以在信息页换掉。
  - 验证：`just test`（Go 66 项 + 前端 60 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；`just build` 产出的真实二进制里 `GET /api/info` 返回 commit hash 与 UTC 构建日期，`go run` 返回「开发构建」；`note_cipher_api_test.go` 覆盖清单不含 title/content、PUT 缺 `kdf_params` 与非法 base64 各返回对应字段错误、对明文笔记返回 409、不存在的 id 返回 404、不带 `X-CSRF-Token` 的 PUT 返回 403 且库里密文未变。
- 笔记 CRUD + 分类管理 + 全局主密码浏览器端加密 + Markdown 编写预览（实现过程与关键逻辑）：
  - 数据层：迁移 `sql/0004_note_app.sql` 新增 `categories`（保留 `id=1`「未分类」、`name` NOCASE 唯一）、`master_password`（单行，只存 16 字节公盐 `salt` 与助记问题 `question`，密码与派生密钥都不落库）以及 `notes.category_id` 外键、`notes.cipher` BLOB、`notes.kdf_params` JSON。「不选分类」在库里存成 `category_id IS NULL`，与 `id=1` 行展示等价。
  - 存储层：`internal/store/category.go` 分类 CRUD——删除时若仍有笔记返回 `category_not_empty` 并带 `fields.note_count`，`id=1` 禁止改名与删除（`category_reserved`）；`internal/store/note.go` 笔记 CRUD 与分页搜索（`q` LIKE 通配符转义、按分类过滤、`updated_at` 倒序）；`internal/store/master.go` 主密码一次性设置（重复设置 `master_password_exists`，未设置时加密返回 `master_password_missing`）。
  - 接口层：`GET/POST/PUT/DELETE /api/notes[/{id}]`、`GET/POST /api/categories`、`PUT/DELETE /api/categories/{id}`、`GET/POST /api/master-password`。正文与密文必须二选一（`content` ≤ 2000 码点；`cipher_b64` StdEncoding、29–65536 字节；`kdf_params` 校验 algo/iter/keylen）；已加密笔记不允许改回明文（409 `encrypted_immutable`）。服务端全程零密码学代码，只校验与透传，因此 CLI 直读 SQLite 解密是可行的扩展路径。
  - 浏览器端加密：`web/src/lib/crypto.ts` 用 WebCrypto `PBKDF2-HMAC-SHA256`（NFC 口令 + 库中公盐，iter 150000、keylen 32）派生 `AES-256-GCM` 密钥（`extractable:false`），密文格式 `nonce(12) ‖ ciphertext ‖ tag(16)`，AAD 绑 `note:v1:<noteId>` 防跨笔记挪用密文；派生密钥只存在 `web/src/lib/session-key.ts` 的页面内存，无服务端会话、无自动过期，锁定即 `set(null)` 并清空草稿。派生失败、tag 校验失败、编码错误统一收敛为「密码错误或数据损坏」，不区分原因以免泄露可判定信息。
  - Markdown：`web/src/lib/markdown.ts` + `web/src/components/Markdown.svelte` 用 `marked` 解析、`DOMPurify.sanitize` 清洗后才 `{@html}`，外链补 `rel="noopener noreferrer"`；后端只保存原始 Markdown 文本，编辑器左侧写原文、右侧实时预览，`web/src/lib/draft.ts` 单槽内存草稿保证解锁表单回填不会吞掉已输入正文。
  - 界面：新增/编辑 `NoteEditorView`、列表 `NoteListView`（分类筛选 + 分页 + 删除确认）、详情 `NoteDetailView`（已加密笔记先解锁）、设置主密码 `MasterPasswordView`、忘记密码 `ForgotPasswordView`、分类管理 `CategoryManagerView`；路由 `/notes`、`/notes/new`、`/notes/:id`、`/notes/:id/edit`、`/notes/:id/forgot`、`/master-password`、`/categories`。
  - 验证：`just test`（Go 5 个包 + 前端 54 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；Playwright 驱动 Edge 在隔离实例（`127.0.0.1:8099` + `.run/e2e-notes.db`）跑通 29 项端到端断言；`node:sqlite` + `node:crypto` 写的独立 CLI 直读同一个库解密，正文与浏览器原文逐字一致、错误密码被 GCM 拒绝；二进制扫描确认加密正文未落盘（同库明文笔记的对照串确实出现在 WAL 中，说明扫描方法有效）。修补后又跑了一遍浏览器实测（`.run/fix-verify.mjs`，26 项断言全绿）：两次密码不一致/漏填都被拦下且没写出半成品笔记、交棒设置主密码后草稿原样回到编辑页、另一篇笔记的编辑器拿不到这份草稿、库里 `is_encrypted=1` 且 `kdf_params` 就是应用参数、手改该行 `iter` 后连正确密码也报「密码错误或数据损坏」（改回即恢复）、错误密码与损坏仍是同一句提示、未分类 chip 计数与筛选都含两篇、分类新增与改名撞名都显示「该分类名已存在」、`q` 恰好 200 字通过 / 201 字 400 + `fields.q`。
- 前端三主题切换（亮色 / 暗色 / 护眼，实现逻辑与关键决策）：
  - 机制：`web/src/lib/theme.ts` 提供纯函数 `resolveTheme(存储值, 系统是否暗色)`——合法存储值优先，否则跟随 `prefers-color-scheme`；writable store 订阅时把结果写入 `<html data-theme>`，`setTheme` 同步存 `localStorage('note.theme')`。纯前端偏好，不落 `note.yaml`、不加后端 API。
  - 样式：`app.css` 移除 `prefers-color-scheme` 暗色块，改为 `:root[data-theme=...]` 三块变量——亮/暗直接采用 GitHub Primer 官方色板（亮 `#ffffff/#f6f8fa/#d0d7de/#1f2328/#0969da`，暗 `#0d1117/#161b22/#30363d/#e6edf3/#1f6feb`），热力图绿阶同步 GitHub 贡献图五级色；护眼为自定低饱和米绿纸色（`#e9efdf` 底、深橄榄字、苔绿 accent）。
  - 入口：`ThemeSwitch.svelte` 分段控件（亮色/暗色/护眼，`aria-pressed` 标记当前项、键盘可达）挂在 `Layout` 顶栏右上角；引导页不放置，主题在模块加载时已生效。
  - 验证：新增 `theme.test.ts` 3 项（前端共 14 项）与 `just check` 通过；Playwright 驱动 Edge 实测——初始跟随系统、三主题点击切换后 `data-theme`/前景色/`aria-pressed` 全部正确、刷新后保持护眼、键盘 Enter 可切换、全新上下文系统暗色回退 dark；截图见 `.run/shot-{light,dark,eye-care}.png`。
- `just pull` / `just push` 多远程同步任务：
  - `pull` 先 `git fetch --all --prune` 再 `git pull --rebase`；`push` 复用同样的 fetch + rebase 前置，rebase 有冲突时该行非零退出、just 立即中止，不会带着冲突推送。
  - 推送遍历 `git remote` 的全部远程（本仓库为 gitcode/gitee/origin 三个），Windows 用 PowerShell `ForEach-Object`、Unix 用 `xargs -n1`，按 `os()` 分发。
  - 验证：`just --dry-run pull|push` 展开命令正确；`just test`（Go 22 项 + 前端 11 项）与 `just check` 通过，未实际执行网络推送。
- `just start` / `just stop` / `just restart` 后台开发任务（跨平台脚本，实现逻辑与关键决策）：
  - `restart` 用 just 依赖顺序 `restart: stop start` 实现，先杀旧进程树再拉起；实测重启后前后端均 200。
  - 接口层：`justfile` 按 `os()` 分发——Windows 调 `scripts/dev.ps1`，Unix 调 `scripts/dev.sh`；start 后台拉起 `go run ./cmd/note`（:8080）与 `npm --prefix web run dev`（:5173，/api 已代理到后端），stop 按 `.run/*.pid` 停止。
  - 守护逻辑：首次 start 自动补 `npm ci` 与前端 build；PID 文件存在且进程存活则跳过重复启动；stop 用 `taskkill /T /F`（Windows）或递归 `pkill -P`+`kill`（Unix）杀整棵进程树，因 `go run` 与 npm 都会派生子进程。
  - 关键决策：Windows 端进程经 WMI `Win32_Process.Create` 创建，父进程是 WmiPrvSE，不继承 just 传入的 stdout 管道句柄——此前 `Start-Process` 方案让存活子进程持有该句柄，调用方永远等不到管道 EOF 导致 `just start` 挂起；日志重定向改由子 cmd 内部完成，统一写 `.run/<name>.log`。
  - 约定：`.run/` 已加入 `.gitignore`；`dev.ps1` 含中文输出，必须保持 UTF-8 BOM 以兼容 PowerShell 5.1。
  - 验证：`just start` 约 9 秒返回，后端与前端均探测 200；`just stop` 约 6 秒返回且 5173/8080 端口释放；Go 22 项、前端 11 项测试与 `svelte-check`、`go vet` 全部通过。
- 笔记表扩展字段（迁移 `sql/0003_note_fields.sql`，实现逻辑与关键决策）：
  - 新增 `kind`（笔记类型，`TEXT NOT NULL DEFAULT 'note'`，取值由应用层维护、新增类型无需迁移）、`remark`（纯文本备注，默认空串）、`extra`（JSON 扩展备注，列级 `json_valid` CHECK，NULL 表示无）三个用户要求的字段。
  - 补充 `rev`（修订号，应用侧编辑时 +1，用于乐观并发与变更检测）与 `last_visited_at`（最近查看时间，UTC 秒级文本，配合 activities 支撑"最近打开"排序）。
  - 置顶、回收站、标签等候选字段因与 PRD 2.2 非目标冲突未引入；提醒时间、颜色、来源等未定型信息统一先进 `extra` JSON，稳定后再提升为独立列。
  - 迁移遵循 SQLite `ALTER TABLE ADD COLUMN` 逐列追加约束；`store_test` 断言版本 3 并验证默认值回填、非法 JSON / 空 kind / 非正 rev 被 CHECK 拒绝。
  - 验证：`go vet` + Go 22 项 + 前端 11 项测试全部通过。
- 用户命名与使用统计首页（实现过程与关键逻辑）：
  - 配置层：`config/note.yaml` 新增 `user.name` 与 `user.name_set` 两个配置；`internal/config` 解析该段并新增 `SaveUserName`，以同目录临时文件 + rename 原子写回，保证重启后读回姓名与门禁状态不丢。
  - 数据层：新增迁移 `sql/0002_activity.sql` 建 `activities` 表（`kind='search'`、`query`、UTC 秒级 `created_at`，含按天与按词两个索引）；`internal/store/activity.go` 提供 `SearchNotes`（LIKE 通配符转义防注入扩大匹配，查询即记录活动）、`RecordSearch` 与 `GetStats`（总次数、活跃天数、Top 查询词、近 N 天按天计数）。
  - 接口层：`internal/web` 重构为 `application` 结构承载配置与存储，注册 `GET/PUT /api/settings(/name)`、`GET /api/notes?q=`、`GET /api/stats`；新增 `csrfProtect` 中间件对所有状态变更类 API 做 Cookie 与 `X-CSRF-Token` 恒定时间比对，缺失或不符返回 403。
  - 前端：`lib/api.ts` 统一 CSRF 令牌获取与 403 自动刷新重试；`lib/settings.ts` 启动时拉取设置，`App.svelte` 在 `name_set=false` 时整屏渲染 `OnboardingView` 引导输入姓名并写回配置；路由 `/` 改为统计首页 `HomeView`（问候、搜索框、使用账簿），笔记列表移至 `/notes`。
  - 可视化：`lib/stats.ts` 的 `buildHeatmap` 生成 53 周 × 7 行（周一开头）GitHub 提交图风格网格，`levelFor` 按 0/1-2/3-5/6-9/10+ 分五级；`ActivityHeatmap.svelte` 纯 CSS Grid 渲染，月份/星期标签、少→多图例、`title` 悬停提示，色阶为墨绿渐变并支持暗色模式。
  - 设计决策：按 frontend-design 技能采用「纸上墨迹」方向——冷纸底色 `#f3f4f1`、深青墨主色、衬线问候语（系统中文字体栈，零外部字体），热力图为全页唯一大视觉，统计用点线账簿行而非卡片堆砌。
  - 验证：Go 21 项、前端 11 项测试与 `svelte-check` 全部通过；单二进制实测完整链路（设名 → 重启持久化 → 搜索 3 次 → 统计与 UTF-8 落盘字节正确）。
- 产品需求文档与技术方案，明确单向加密笔记的安全边界与数据流。
- 可运行项目骨架：Go 入口、分层配置、SQLite 自动建库与迁移、内嵌前端、CSRF 与安全中间件、优雅退出。
- `config/note.yaml` 默认配置，支持内置默认值、YAML、命令行参数三级覆盖，YAML 严格解析。
- `sql/0001_init.sql` 初始表结构与 `sql/embed.go` 迁移嵌入，启动时按 `user_version` 增量执行。
- Svelte 5 + Vite + TypeScript 前端骨架，包含 History 路由、页面占位、类型检查与 Vitest 测试。
- `docs/BUILD.md` 构建指南，记录从命令行生成脚手架到单二进制验证的完整流程。
- 跨平台 `justfile`：`install`、`frontend`、`build`、`test`、`check`、`format` 任务，Unix 使用 bash、Windows 使用 PowerShell。

### Changed

- 新增根目录 `.gitattributes` 统一换行符（`* text=auto eol=lf`；`*.ps1 text eol=crlf` 保留 Windows PowerShell 惯例）：仓库与检出工作区一律 LF，`eol` 属性优先级高于系统级 `core.autocrlf=true`，不改任何 git 配置、也不影响其他仓库。一次性把 19 个工作区 CRLF 文件中的 18 个转为 LF（`scripts/dev.ps1` 除外），并顺带清掉 40 个「字节与索引一致、仅 stat 脏」的幽灵 M 标记（对字节一致的文件 `git add` 只刷新 stat 条目，暂存区零内容，索引 blob 不变）。验证：`git ls-files --eol` 复核全库 107 个 `i/lf w/lf` 加 `dev.ps1` `i/lf w/crlf`；`git check-attr` 确认属性生效；`just test`（内含 `go fmt` 与 prettier 全仓库格式化）跑完后 status 仍只有既有的 42 个未提交工作文件——格式化不再产生换行符虚假 diff。`just test` 与 `just check` 通过。
- 文档同步到 v1.4（`docs/PRD.md`、`docs/TECH_DESIGN.md`、`AGENTS.md`、设计文档与实现计划），把已验证的信息页与改密实现写回需求与架构文档：
  - PRD：版本升到 v1.4，新增 §2.5「v1.4 实现偏离」五行对照表（改密 / 助记问题 / 用户名 / 首次配置 / 运行信息）与收敛指针段落；§2.2 非目标明确「改密要求旧密码，与找回是两件事」；§2.3 补「修改密码不算恢复」；§5.3.1 第 4 条改写为可改密不可找回；§5.3.2 由「POST 仅首次配置」推出 FR-3.16（逐条试解、第一条成功即通过、空清单跳过验证）、FR-3.17（只替换 `cipher`/`kdf_params`，元数据与全局盐逐字不变）、FR-3.18（可中断可续跑并如实列出混合状态）、FR-3.19（改问题不换盐）；新增 §5.3.6 用户视角七步改密流程（原文案节顺延为 §5.3.7）；§5.4 路由与 API 表补 `/info` 与四个新端点；新增 §5.6 首次引导 FR-5.1~5.6 与 §5.7 用户信息页 FR-6.1~6.6；§6 页面数 9 → 10；§3.2 加 US-10（换密码后仍可读）与 US-11（复制二进制到另一台机器）；§4 术语表改主密码/助记问题两行、加「重加密迁移」；§9 改写 AC-08/AC-33、新增 AC-35 ~ AC-44；§10 加 M5 里程碑；§11.1 目录结构加 `internal/build/` 与各新文件；§11.2 加两条手工检查；附录 B 三条改写；§13 Q5 结论翻转、§14 移除「修改密码」候选项。
  - TECH_DESIGN：版本升到 v1.4；§1.2 新增第 9 条不变量（旧口令是否正确只由浏览器试解 GCM 标签判定，服务端始终没有验证口令的能力，密文请求体不入日志）；§1.3 非目标与 §15.4 移除「修改主密码」；§2.3 目录树补 `internal/build/info.go`、`handlers_note_cipher.go`、`handlers_info.go`、`reencrypt.ts`/`info.ts`/`onboarding.ts`；§4.3 路由清单此前停留在 v1.3 已删除的 `/encrypt`、`/unlock`、`/lock`、`POST /api/lock` 上，与 §8.3「不存在解密端点」自相矛盾，按 `internal/web/server.go` 实际注册的 20 条重写；§4.4 处理器表、§8.3 表、§8.5 错误码表（加 `not_encrypted`、`master_password_missing`）同步；§5.4 补 `ListEncryptedCiphers`/`UpdateNoteCipher` 的事务外只读探针（`db.SetMaxOpenConns(1)` 下事务内第二条查询会死锁）；§5.5 补「改密只在 WAL 留旧密文，不产生明文残留」；新增 §6.9 六步迁移流程与设计取舍（为何独立端点、为何不动 `updated_at`/`rev`、为何不换全局盐、为何不加 `key_version`，含实测边界）；新增 §8.6 运行信息响应（18 字段样例 + `-X` 注入与 `vcs` 回落）；§9.2/§9.3/§9.5/§9.6/§9.7 前端章节按 `decideStep` 判定与 `Promise.all` 半套数据即错误态重写；§10.3/§10.4 构建章节同步 `justfile` 新 ldflags（含 Windows 只保留最后一行输出的坑）；§11.1 威胁模型加「迁移途中关页面」「信息页暴露运行信息」两行；§12 测试表补四份新 Go 测试与三份前端用例、并描述 `.run/info-e2e.mjs` 三次启动 60 项断言；§15.5 新增 M5；§16 改写 ADR-02/ADR-11，新增 ADR-16（试解即校验器，不加 `key_version`）、ADR-17（独立端点 + 不动元数据 + 不换盐）、ADR-18（ldflags + `vcs` 回落）、ADR-19（两步引导 + 半态解锁分支）。
  - `AGENTS.md`：目录结构加 `internal/build/`；硬性约定加三条——主密码可改不可找回且 `PUT /api/notes/{id}/cipher` 有意不动 `title/content/category_id/rev/updated_at`（不要「顺手」对齐成 `PUT /api/notes/{id}`）、`GET /api/info` 必须 `no-store` 且 git 信息不可用时退化为空值而不是让构建失败、引导判据 `decideStep(nameSet, masterConfigured)` 必须与 `document.title` 共用且第二步已配置时只解锁不重复 POST。
  - 设计文档与实现计划：状态行改为「已实现」；§2.2 半态表补上实测复现路径（同一浏览器另开页签已完成引导、本页签 settings 仍是旧值）并注明该分支不是死代码；§5 续跑语义按实测改写（响应丢失型中断可用新密码续跑；真的只迁完一条时剩下那条仍受上一代口令保护，填新密码得到 `failed_old_key`，需真实旧口令续跑）；§8 端到端小节改为实际脚本行为；文档内引用从行号改为章节号（`§1102` → `§11.1`，AC-33 的交叉引用从 AC-38 改为 AC-39/AC-40）。
  - 验证：引用逐条与代码核对（`masterSaltBytes`/`maxNoteBody`/`kdfParamsError`/`minCipherBytes`/`maxCipherBytes`、`ListEncryptedCiphers`/`UpdateNoteCipher`/`UpdateMasterQuestion`/`SchemaVersion`/`decideStep`/`planMigration`/`runMigration`/`verifyOldPassword`、`internal/build/info.go` 与三个新前端模块均存在且命名一致；`handleUpdateMasterQuestion` 确实落在 `handlers_categories.go`，与目录树注释「分类与主密码端点」相符）。`just test` 与 `just check` 通过。
- `docs/PRD.md` 与 `docs/TECH_DESIGN.md` 升到 v1.3，按已验证实现逐项重写偏离部分：Argon2id → WebCrypto PBKDF2-HMAC-SHA256（纯浏览器无 WASM 成本）、服务端加解密与会话 Cookie（`note_sess`、TTL、401 家族）→ 派生密钥只留页面内存且无服务端会话、`{@html}` 全面禁止 → `Markdown.svelte` 单点例外且输入必过 `DOMPurify`、错误码矩阵修正（分类名重复是 400 而非 409、字段是 `fields.note_count` 而非 `fields.count`、无 415）、正文上限 1 MiB → 2000 码点；性能表区分实测数值（前端 JS 163.61 kB / gzip 58.34 kB）与未测项，新增 ADR-01…ADR-15 与 Q1–Q7 结论，明确「未执行」清单（Firefox/Chrome 实测、10k 笔记性能、`go test -race`、fuzz）。
- `AGENTS.md` 安全不变量改写：`{@html}` 例外表述、密码与派生密钥不进任何浏览器存储、服务端零密码学代码与 CLI 直读解密定位。
- 技术方案补充配置目录、数据库自动检测与迁移流程、`sql/` 迁移目录约定。
- 前端构建输出重定向到 `internal/web/dist`，由 `go:embed` 嵌入，部署不再需要 Node/npm。
- Go 模块版本提升到 1.26，SQLite 使用纯 Go 驱动，构建全程关闭 CGO。

### Fixed

- 首页「近一年使用频率」热力图的月份标签显示不全：`.heat-field` 的周列只有 `calc(var(--cell) + 3px)`（14px）宽，`.heat-month` 作为网格项默认 `justify-self: stretch` 被压成同样宽度，而 CSS 自动换行允许在数字与汉字之间断行，于是「10月」折成两行；行高只有 18px 且 `align-self: end`，上面那行数字被 `.heatmap` 的 `overflow-x: auto`（隐含 `overflow-y: auto`）裁掉，页面上只剩一个「月」字。给 `.heat-month` 加 `white-space: nowrap; width: max-content; justify-self: start`，标签按内容宽度单行渲染并向右溢出到第 1 行的空白区（相邻月份至少隔 4 列约 56px，不会重叠）。
  - 验证：`just test`（Go 50 项 + 前端 60 项）与 `just check`（`go vet` + `svelte-check` 0 error 0 warning）通过；用无头 Edge 对同一页面截图对比——修复前月份只剩「月」且数字行被裁，修复后 `10月 12月 1月 … 10月` 完整单行显示，且截图取自 `just build` 产出的内嵌前端二进制。
- 勾选「用主密码加密」的新笔记在保存后跳到「页面不存在」：编辑器交棒用的是站内跳转 `navigate('/master-password?next=…')`，而 `router.matchRoute()` 拿整串（含查询串）去匹配 `^/master-password$`，匹配失败落到 `notFound`。第一次加密这条主路径因此在真实浏览器里完全走不通（直接地址栏输入或用新标签打开反而正常，所以单元测试与此前的 e2e 都没发现）。`matchRoute()` 改为先剥掉 `?`/`#` 之后的部分再匹配路径；`router.test.ts` 先以 `/master-password?next=…`、`/notes/abc?from=list`、`/notes/abc#top` 三例复现（RED：`notFound`）再修复（GREEN）。

- 首次加密可能把笔记永久锁死：主密码已配置但本会话还没有密钥时，编辑器只要求「输入非空」就用它派生密钥并加密——打错一个字母照样保存成功，刷新后再也解不开，而且当场没有任何异常。现在这条路径必须两次输入主密码且比对一致（`session-key.passwordMismatch`）才继续；已有密文可当场试解的路径不变（由 GCM 标签验证）。
- 浏览器忽略笔记自己声明的 KDF 参数：`kdf_params` 列被写入却从未被读取，手改库里的 `iter` 不影响解锁，PRD AC-23 与技术方案 §6.2/§6.3 的承诺都落空。新增 `crypto.paramsFromNote()`：按该笔记声明的参数派生，越界（`iter` 不在 [100000,1000000]、`keylen≠32`、`algo≠pbkdf2`、JSON 畸形）一律按数据损坏拒绝解密；详情页与编辑器的解锁都改用它，重加密时把同一份参数写回去。顺带修正「`kdf_salt` 供客户端比对」的说法——接口从不返回该列，它只是满足 0001 `CHECK` 的冗余副本。
- 内存草稿会被别的笔记消费：`editorDraft.take()` 在挂载时无条件取走草稿，随后 `loadNote()` 用库里的正文覆盖，结果是「跳去设置主密码时存下的草稿」在下次打开任意已有笔记时被吃掉并注入那条形编辑器的第一条笔记（保存即覆盖）。草稿现在带 `noteId`，`take(noteId)` 只交给同一条笔记的编辑器；离开编辑器（组件卸载）清空草稿，只有跳 `/master-password?next=…` 的交棒除外。
- 分类重名的状态码从 400 改为 409 `category_exists`（仍带 `fields.name`）：名字被占用是状态冲突，与 `category_not_empty`、`id_conflict`、`category_reserved` 同族；此前只有字段级 400，客户端只能靠字符串猜是「太长」还是「重名」。同时补齐此前缺失的用例：改名撞进只差大小写的同名（store 与 API 两层）、`%`/`_`/`\` 作为搜索词必须按字面匹配、前端正文长度改由 `notes.countCodePoints()` 计算并断言与后端 2000 码点上限同源。

- 「未分类」既看不见也统计不到：库里未选分类的笔记存的是 `notes.category_id IS NULL`，而 `ListCategories` 只按 `n.category_id = c.id` 计数、`ListNotes` 只按 `n.category_id = ?` 过滤，导致分类 chip 永远是 0、「全部」少算、点「未分类」列表为空。改为默认分类（`id=1`）计数与筛选都并入 `IS NULL`（`LEFT JOIN ... OR (c.id = ? AND n.category_id IS NULL)`、`(n.category_id = ? OR n.category_id IS NULL)`）。先以 `TestDefaultCategoryCoversUncategorizedNotes` 复现（RED：`NoteCount:0`，期望 2）再修复（GREEN）。
- 编辑已加密笔记时正文一片空白：`NoteEditorView` 拉到笔记后只置 `cipher`、没有尝试用会话里的派生密钥解密，已解锁的会话也要重新走一遍解锁表单。改为有密钥就直接 `decryptNoteContent` 回填，解不开（密钥属于别的密码）才回落到解锁表单并给出提示，切换笔记与重新解锁时清理 `revealFailed`。
- 搜索词长度上限只写在文档里没有实现：`GET /api/notes` 现在校验 `q` ≤ 200 字（`maxQueryRunes`），超限返回 400 + `fields.q`，前端两处搜索框（首页与列表页）同步 `maxlength="200"`，`writeValidationError` 的字段优先级纳入 `q`。
- `just stop` 在记录的 PID 已不存在时报 `NativeCommandError`：PowerShell 5.1 下 `$ErrorActionPreference='Stop'` 会把 taskkill 写到 stderr 的 "process not found" 升级为终止性错误（`2>$null` 也不能豁免），异常还导致 pid 文件残留、下次 stop 再次失败。`Stop-DevProcess` 改为先 `Get-Process` 判存活：已退出则只提示并清理 pid 文件；taskkill 调用局部放宽为 `Continue`，覆盖检查与击杀之间进程恰好退出的竞态。实测陈旧 pid 文件 stop 干净退出（exit 0）、真实 pid 杀树正常。
- Windows 构建输出改为 `note.exe`，避免无扩展名的二进制无法在 Nushell、PowerShell 中直接执行。

### Security

- 加密笔记的密钥派生与加解密全部在浏览器 WebCrypto 内完成：派生密钥 `extractable:false` 且只存在页面内存，密码、明文正文、派生密钥均不写 localStorage / sessionStorage / IndexedDB / Cookie，服务端日志与错误响应不含正文；锁定会话即丢弃密钥并清空内存草稿，无自动过期窗口。密文 AAD 绑定笔记 ID，跨笔记搬运密文会直接解密失败。
- Markdown 渲染只有一处 `{@html}`（`web/src/components/Markdown.svelte`），输入必须先过 `DOMPurify.sanitize`，外链自动补 `rel="noopener noreferrer"`；含正文与解密结果响应保持 `Cache-Control: no-store`。
- 主密码设置界面显式提示弱口令风险（建议 12 字符以上长口令）：密钥由口令派生，库中公盐与 `kdf_params` 公开，弱口令可被离线爆破。
- 默认只监听回环地址，绑定非回环地址必须显式 `--allow-remote` 并输出警告。
- 数据库文件权限收紧为 `0600`，启用 WAL、`secure_delete` 与 `quick_check`。
- 统一安全响应头、Host 白名单、CSRF Cookie，API 错误响应禁止缓存。
