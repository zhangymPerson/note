# 速记：首页快速记录 + CLI 直接记录（设计）

日期：2026-11-16
状态：待批准（未开始实现）

## 1. 意图与为什么

「速记」分类（v1.15 内置固定分类 `id=2`）目前只有 `note set/get/del` 可以按标题寻址写入，网页端没有入口，CLI 也没有「随手记一条」的低摩擦路径：`note add` 是三步向导（选分类 → 标题 → 正文），对「一句话灵感」太重；`note set` 要显式给标题，还要想分类。

本需求给「记录碎片」开两条直达通道，标题都由系统自动生成，用户只需给正文：

- **Web 首页**：查询框下方一个速记卡片——一个正文文本框 + 两个按钮「速记」「AI 速记」。
- **CLI**：`note add <内容>` 直接记录一条速记（标题自动生成）；`note add <内容> --ai` 先让 AI 整理内容、生成概括标题，再记录。

## 2. 目标 / 非目标 / 反目标

**目标**

- G1 首页 3 次交互内完成一次速记：聚焦文本框 → 输入 → 点「速记」。
- G2 `note add <内容>` 一次调用即落库：标题自动 `速记-<UTC 秒级时间戳>`，落「速记」分类，永不覆盖已有笔记。
- G3 `note add <内容> --ai` 与网页「AI 速记」共用同一份 `internal/ai` 实现：AI 输出「整理后的正文 + 一句概括标题」，**先预览、用户确认后才落库**（用户已确认「预览保存」）。
- G4 未配 AI 时入口可发现、可提示补齐项，不静默失败。
- G5 正文与 AI 结果遵守现有长度口径（正文 2000 码点、标题 200 码点）。

**非目标**

- 速记不加密、不选分类（固定落「速记」）、不自定义提示词、不选模型（用 `ai.models` 默认第一个）。
- `note add` 不带参数的既有三步向导**保持不变**（向后兼容，不破坏 AC-54 已有行为）。
- 不新增 AI 配置项（速记提示词是 `internal/ai` 内建常量，不是 `ai.prompt` 模板的变体）。

**反目标**（技术上可用但等于失败）

- AI 结果不预览就落库（用户已确认「预览保存」，网页与 CLI 都要有确认步）。
- 标题自动生成与 `created_at` 时间口径不一致（两者都用 UTC 秒级）。
- 同一秒内两条速记互相覆盖（标题唯一索引撞名必须处理，不能静默覆盖）。
- 正文 / AI 标题 / 渲染后提示词 / `api_key` 进日志、`/api/info` 或响应体。

## 3. 约束（沿用现有硬性约定）

- `internal/ai` 是全项目唯一发起对外 HTTP 请求的地方；调用只在用户显式点击/按键时发生。
- 状态变更类 API（POST）必须过 CSRF 双提交；含正文的响应 `Cache-Control: no-store`。
- `api_key` 明文只允许在 `note.yaml`；AI 调用日志只记 host / model / 状态 / 截断后的错误摘要。
- 标题与正文长度一律 `utf8.RuneCountInString` 口径（标题 200、正文 2000）。
- `{@html}` 只允许在 `web/src/components/Markdown.svelte`，输入先过 DOMPurify。
- 数据库结构不动（速记分类、标题唯一索引已由 `sql/0007` 提供）。
- CLI 非交互命令的提示写 stderr、stdout 保持干净（`set/get/del` 同口径）；退出码 0/1/2（用法错误 2、运行错误 1）。

## 4. 方案与取舍

**A. 新增独立端点（采用）**：`POST /api/quick`（创建速记）与 `POST /api/ai/quick`（AI 出标题+正文，不落库）。
不扩展 `POST /api/notes`：它的校验（标题必填等）与语义会被「自动标题」污染；速记是新入口，独立端点语义清晰、校验独立。

**B. CLI 形态（按用户最新指示）**：`note add <内容>` 直接记录，而不是我先前提的 `note add 速记` 子命令。理由：用户明确要「内容直接记录」，命令更短、可脚本化（`echo … | xargs note add` 等价于一条），也不必进 bubbletea 向导。

**C. AI 速记的双输出**：`internal/ai.QuickNote` 用内建专用提示词要求模型输出 JSON `{"title": "...", "content": "..."}`，一次调用同时拿到标题与整理后正文。不沿用 `ai.prompt`（那是优化用途的 Markdown 美化模板，语义不同）；JSON 形态比「第一行当标题」的文本约定可解析、可校验。

## 5. 设计决定

### 5.1 `internal/store.CreateQuickNote(ctx, content, title string) (Note, error)`

- `title` 为空 → 自动生成 `速记-<now UTC 秒级 RFC3339>`（与 `nowUTC()` 同格式 `2006-01-02T15:04:05Z`），落「速记」分类（`QuickCategoryID`）。
- 自动标题撞名（同一秒重复创建）→ 顺延下一秒重试，最多试 5 次（`ErrNoteTitleExists` / `ErrNoteTitleInTrash` 都算撞名）。
- `title` 非空（AI 模式）→ 单次创建，撞名原样返回 `ErrNoteTitleExists`（不猜、不自动改 AI 给的标题）。
- 校验：正文非空且 ≤2000 码点、标题 ≤200 码点；正文空返回 `ErrInvalidNote`。
- CLI 与 web 共用这一个入口（CLI 本地时钟与 store 同一进程，自动标题与 `created_at` 同源）。

### 5.2 `internal/ai.QuickNote(ctx, req Request) (QuickNoteResult, error)`

```go
type QuickNoteResult struct{ Content, Title, Model string }
```

- 复用 `Client.Optimize` 的请求管线（同一 chat/completions、超时、错误包装、`StripFence`）。
- 内建提示词（常量 `defaultQuickPrompt`，不在配置里）：要求模型只输出一个 JSON 对象 `{"title": "…", "content": "…"}`，标题是一句 ≤30 字的概括、正文是把速记整理成条理清晰的 Markdown（保留要点、不编造）。
- 解析：`StripFence` 后 `json.Unmarshal`；`title` 非空且 ≤60 码点、`content` 非空且 ≤2000 码点，任一不符 → 明确错误（不截断，见 §5.9 验收 6）。
- 错误语义与 `Optimize` 一致（`ErrNotConfigured` / `ModelError` / `TimeoutError` / `UpstreamError` / `ErrBadResponse`）。
- 日志：调用方只记 host / model / 状态与截断后的错误摘要；本包不打印、错误里不含正文与标题。

### 5.3 Web 端点

**`POST /api/quick`**：请求 `{ "content": "...", "title": "可选" }`，响应 = 创建的笔记（`NoteMeta`，`writeJSON` 自带 `no-store`）。

| 情形                       | 状态码 | code                                                |
| -------------------------- | ------ | --------------------------------------------------- |
| 成功（title 缺省自动生成） | 201    | —                                                   |
| 正文空 / 超 2000 码点      | 400    | `validation_failed`（字段 `content`）               |
| 显式 title 超 200 码点     | 400    | `validation_failed`（字段 `title`）                 |
| 显式 title 撞名（AI 模式） | 409    | `title_conflict` / `title_in_trash`（复用现有映射） |
| 未带 CSRF                  | 403    | 现有中间件                                          |

**`POST /api/ai/quick`**：请求 `{ "content": "..." }`，响应 `{ "title": "...", "content": "...", "model": "..." }`，**不落库**。校验同优化端点（正文非空 ≤2000）；AI 未配置 → 409 `ai_not_configured`（消息列出缺哪几项 + 怎么补）；上游失败 → 502/504（复用 `writeOptimizeError`）。每次请求现读 `note.yaml` 的 AI 四项。

### 5.4 前端（HomeView）

- `web/src/lib/notes.ts` 加 `createQuickNote(content, title?)` → `POST /api/quick`。
- `web/src/lib/ai.ts` 加 `aiQuickNote(content)` → `POST /api/ai/quick`（返回 `{title, content, model}`）。
- 首页查询表单下方加速记卡片：
  - 正文文本框（≤2000 码点，带计数）+ 两个按钮「速记」「AI 速记」；正文空时两按钮禁用。
  - **速记** → `createQuickNote(content)` → 成功：清空文本框、显示「已记入速记「标题」」、刷新热力图/统计（新建计入账簿）。
  - **AI 速记** → `aiQuickNote(content)` → 预览面板：AI 标题（可编辑输入框，预设 AI 给的标题）+ 整理后正文（`Markdown.svelte` 渲染）+ 模型名 + 按钮「保存」「返回修改」。
    - 「保存」→ `createQuickNote(aiContent, aiTitle)`（用预览里的标题，用户可改）→ 清空、提示、刷新统计。
    - 「返回修改」→ 收起面板，原文留在文本框。
  - 错误内联展示（含 `ai_not_configured` 的补齐提示）；AI 失败时原文不动、面板可重试。

### 5.5 CLI 解析（`internal/cli/parse.go`）

- `note add` 分支扩展：
  - 无位置参数 → 既有三步向导（不变）。
  - 有位置参数 → 直接记录模式：第一个位置参数是正文（多词用引号）；`--ai` 标志开启 AI 速记；`-c/--config/--db` 照常透传。
  - `note add --ai` 但无正文 → 用法错误退出 2。
  - 其余未知参数 → 用法错误退出 2（沿用现有报错风格）。
- 注意：AC-54 里「`note add foo` 退出 2」的行为随本版**改变**（`note add foo` 现在会记录一条速记），PRD 该条 AC 要改写。

### 5.6 CLI 实现（`cmd/note/add.go` + `internal/cli`）

- **`note add <内容>`**（非交互，无需 bubbletea）：`store.CreateQuickNote(ctx, content, "")` → stderr 提示「已记入速记「标题」」+ ID/链接（复用 `printAddedNote` 风格），stdout 干净，退出 0；正文超长/空 → 用法错误 2。
- **`note add <内容> --ai`**：进 bubbletea 单屏预览确认（复用现有预览屏组件风格）：
  - 先调 `ai.QuickNote`（`addDepsWithAI` 已有的配置判定：未配置 → 状态行提示补齐项，退出 130 或留在输入态——**定为留在输入态提示，Esc 才放弃**，与 `note add` 的 `Ctrl+O` 口径一致）。
  - 成功 → 预览屏显示 AI 标题 + 整理后正文（glamour 渲染）+ 模型名；`Enter` 保存（`store.CreateQuickNote(ctx, content, title)`）、`Esc` 放弃（退出 130，不落库）。
  - AI 失败 → 状态行显示截断原因，不落库、可重试。
- 复用 `addDeps` 的注入风格：`internal/cli` 不 import `internal/ai` / `internal/config`，新 deps 方法 `CreateQuick(ctx, content, title string) (NoteRef, error)` 与 `QuickNote(ctx, content) (QuickNoteResult, error)` 由 `cmd/note` 注入。
- 帮助文本（`usage.go` 总帮助与 `addUsage`）同步：`note add <内容>` / `note add <内容> --ai`。

### 5.7 安全与隐私（写进 TECH_DESIGN 与 AGENTS.md）

1. `POST /api/quick` / `POST /api/ai/quick` 都是状态变更类 POST，过 CSRF 双提交，不新增豁免。
2. 两个端点响应含正文 → `Cache-Control: no-store`。
3. AI 速记调用只在用户点按钮 / 敲命令时发生；不轮询、不自动重试。
4. 日志只有 host / model / 状态 / 截断错误摘要；`/api/info` 不含任何速记数据。
5. 速记是明文笔记（加密不在范围内）；不引入任何新密钥材料。

### 5.8 文档

- `docs/PRD.md`：版本 v1.18——修订记录、FR（首页速记卡片、CLI 直接记录、AI 速记预览确认）、路由表补两端点、AC（首页两按钮 + 预览确认、`note add <内容>` / `--ai`、撞名与同秒重试、AC-54 改写）、里程碑 M15。
- `docs/TECH_DESIGN.md`：端点表、`internal/ai` 段（QuickNote）、安全不变量补两条。
- `CHANGELOG.md` `[Unreleased]`：实现过程、关键决策、验证结果。
- 本设计文档随版本库提交。

### 5.9 测试

**Go**

- `internal/store`：`CreateQuickNote` 自动标题格式（UTC 秒级 RFC3339）、落速记分类、同秒撞名顺延重试、显式标题撞名返回 `ErrNoteTitleExists`、正文空/超长拒绝。
- `internal/ai`：`QuickNote` 请求体包含 JSON 提示词、成功解析 `{title, content}`、缺 title / title 超 60 / content 超 2000 / 坏 JSON / 围栏剥离、错误类型与 `Optimize` 一致。
- `internal/web`：两端点表驱动——校验、CSRF 403、未配置 409、`no-store`、显式标题撞名 409、AI 端点不落库。
- `internal/cli`（解析）：`note add 内容`、`note add 内容 --ai`、`note add --ai`（无正文退出 2）、未知参数退出 2、向导无参数路径不变。
- `internal/cli`（向导）：`--ai` 未配置提示、成功预览、Enter 保存 / Esc 放弃（130）、AI 失败不落库。

**前端（Vitest）**

- `lib/notes.ts` / `lib/ai.ts`：`createQuickNote` / `aiQuickNote` 请求形态。
- `HomeView.test.ts`（新增）：速记按钮清空文本框并刷新统计；AI 速记出预览、改标题后保存用改过的标题、返回修改保留原文、未配置显示补齐提示。

### 5.10 验收（人工冒烟）

1. 首页：输入一句话 → 「速记」→ 文本框清空、提示标题；「速记」分类里出现 `速记-<时间>` 的笔记；热力图/统计刷新。
2. 同一秒连点两次「速记」→ 两条都成功（第二条标题顺延一秒，不覆盖）。
3. 配好 AI：首页「AI 速记」→ 预览面板出现（标题可改 + 整理后正文 + 模型名）→ 保存后库里有 AI 标题的笔记；正文保持整理后版本。
4. 未配 AI：首页「AI 速记」提示补齐项；`note add 内容 --ai` 提示补齐项且不落库。
5. CLI：`note add "一行速记"` → stderr 提示、stdout 干净、退出 0、库中可见；`note add "x" --ai` → 预览屏 → Enter 落库；Esc 退出 130 不落库；`note add --ai` 退出 2。
6. AI 返回坏 JSON / 超长标题 / 超长正文 → 明确报错、不落库、不截断。
7. 检索 `.db`、`note.log`、浏览器 console：搜不到正文 / AI 标题 / `api_key`；两端点响应头有 `no-store`。

## 6. 留给实现者的

- 速记卡片在首页的视觉样式沿用现有 `.search-form` / `.button` 风格，不引入新组件库。
- 预览面板的「返回修改」与「保存」之间的竞态（用户返回后正文变了）由前端状态保证：保存用预览面板里锁定的那份内容，不读文本框当前值。
- `note add <内容>` 的多行正文：shell 引号内可直接换行，不额外做交互编辑（gum write 式编辑不在本版范围，`note add` 无参向导仍提供完整编辑体验）。

## 7. 我替用户做的决定（请在批准时一并核对）

1. **CLI 形态**：`note add <内容>` 直接记录、`note add <内容> --ai` AI 处理后记录（用户明确指示，非我擅自变更）；无参 `note add` 向导保持不变。
2. **AI 速记一律预览后保存**：网页预览面板 + CLI 预览屏，都是确认步（沿用用户此前确认的「预览保存」）。
3. **AI 标题可编辑**：网页预览面板的标题是输入框，用户可改；CLI 预览屏标题只读（命令行改起来不便）。
4. **AI 标题/正文超限不截断**：明确报错、不落库（与优化端点「超 2000 禁止替换而不截断」同口径）。
5. **同秒撞名策略**：自动标题顺延下一秒重试（≤5 次）；AI 显式标题撞名直接报 409 / 错误，不自动改名。
6. **端点命名**：`POST /api/quick` 与 `POST /api/ai/quick`（语义独立，不污染 `/api/notes` 与 `/api/ai/optimize`）。
