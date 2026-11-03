# AGENTS.md

本文件为在本仓库工作的 AI 编码代理提供项目说明与开发规则。

## 项目概览

个人本地笔记应用（单用户、单机、无登录体系）：

- **后端**：Go 单二进制（`net/http` + `ServeMux`，无框架），SQLite 持久化（纯 Go 驱动 `modernc.org/sqlite`，全程 CGO 关闭）。
- **前端**：Vite + Svelte 5 + TypeScript 的 History 路由 SPA，构建产物输出到 `internal/web/dist`，由 `go:embed` 内嵌；不使用 SvelteKit、UI 组件库、CSS 框架或状态管理库。
- **部署形态**：运行时只有一个可执行文件 + 一个 SQLite 数据文件 + 一个 YAML 配置（默认在用户配置/数据目录，`note -c` 可指定）；入口是 CLI 子命令——裸 `note` 等价于 `note --help`（打印帮助），`note add` 走 bubbletea 向导（选分类 → 标题 → 正文）新建明文笔记，`note list` 筛选并预览（标题/分类 fuzzy + 正文子串），`note show` 渲染一条笔记并把原文复制到剪贴板（不给 id 时先进选择器），`note web` 才启动服务（默认只监听 `127.0.0.1:8080`），`note info` 输出运行信息，`note status` 输出使用统计（事件构成 / 累计事件 / 活跃天数，与网页概览页同源），`note config list|set` 读写 `note.yaml` 的 AI 三项（`ai.base_url` / `ai.api_key` / `ai.prompt`，密钥只显示掩码），`note set / get / del` 按标题寻址（`标题@分类` / `@分类` / `--list`）：`set` 同分类同名即覆盖并默认落内置「速记」，`get` 读正文或列分类，`del` 是软删除。v1.6 起 `note web` 还有 `start`/`stop`/`status` 三个子命令做后台运行；后台实例的日志 `note.log` 与状态文件 `note.json` 与数据库同目录，不带子命令的 `note web` 仍是前台（日志进终端，不建日志文件）。

需求与架构细节以 `docs/PRD.md` 与 `docs/TECH_DESIGN.md` 为准，改动行为前先读这两份文档。

## 目录结构

```text
cmd/note/          入口与优雅退出
internal/build/    版本与构建信息（ldflags 注入，未注入时回落 debug.ReadBuildInfo）
internal/config/   三级配置加载（内置默认值 → YAML → 命令行），严格解析
internal/store/    SQLite 打开、迁移与应用数据访问
internal/daemon/   后台运行支持（状态文件、探测/关停请求、分离进程启动、pid 存活判定）
internal/clipboard/剪贴板写入（原生实现 + OSC52 兜底；失败只返回错误，绝不影响主流程）
internal/web/      HTTP 处理器（含后台模式的 /api/shutdown）、中间件、内嵌前端
sql/               编号迁移脚本（NNNN_name.sql，按 user_version 增量执行）
web/src/           Svelte 5 SPA（router、views、components、lib）
docs/              PRD、技术方案、构建指南
```

## 常用命令（just）

- `just install` / `just frontend` / `just build`：前端依赖、前端产物、单二进制（Windows 输出 `note.exe`）
- `just install-win`（Windows）：先 `just build`，再关停后台 `note web`、替换 `%LOCALAPPDATA%\Programs\note\note.exe`（日志与状态文件仍在数据目录，不动），并幂等地写入用户 PATH
- `just test`：`go test ./...` + 前端 Vitest
- `just check`：`go vet` + `svelte-check`/`tsc`
- 改动合入前必须 `just test` 与 `just check` 全部通过。

## 硬性约定

- 时间戳统一为 UTC 秒级文本 `2006-01-02T15:04:05Z`，可直接按字符串排序。唯一例外是构建时间（`internal/build.BuildDate`）：它只在信息页与 `note info` 里展示、不参与任何排序，所以取构建机本地时区（RFC3339 带偏移，如 `2026-11-03T17:29:33+08:00`），别「顺手」改回 UTC；回落路径的 `vcs.time` 也经 `localBuildTime` 换成本地时区，保持两条路径同口径。
- 数据库结构变更只能新增 `sql/NNNN_*.sql` 迁移，禁止手改既有迁移。
- 状态变更类 API（PUT/POST/DELETE）必须通过 CSRF 双提交校验；前端经 `web/src/lib/api.ts` 统一携带 `X-CSRF-Token`。唯一例外是 `/api/shutdown`（凭证即关停令牌），该豁免不得扩大。
- `/api/shutdown` 只在后台模式存在（`web.Control.Token` 为空时注册的 handler 一律 403 `shutdown_disabled`），凭状态文件里的 32 字节令牌；令牌不得进日志、响应、命令行或环境变量，仍受 `hostGuard` 约束。
- 后台实例的日志与状态文件固定在 `filepath.Dir(DBPath)`（`note.log` / `note.json`），路径只能由 `internal/daemon` 的纯函数推导；`note info` 必须输出日志路径。状态文件 `0600` 且原子写（临时文件 + rename），日志追加写不轮转。
- 存活判定不得用 `os.FindProcess` 判 Windows（对已退出进程仍返回成功），必须用 `internal/daemon` 的等待句柄实现。
- 安全不变量（见 TECH_DESIGN 1.2）：密码与派生密钥不落盘、不进任何浏览器存储；日志不含正文/密码/Cipher；`{@html}` 只允许出现在 `web/src/components/Markdown.svelte`，且输入必须先过 `DOMPurify.sanitize`；解密与含正文响应 `Cache-Control: no-store`。
- 加解密全在浏览器端 WebCrypto（PBKDF2 + AES-256-GCM），服务端零密码学代码；数据库只存密文 BLOB 与公盐，因此 CLI 直读库解密是允许的扩展路径。
- 主密码**可修改但不可找回**：改密时旧口令是否正确只由浏览器试解已有密文的 GCM 认证标签判定，服务端不得新增任何口令校验/解密端点。迁移只走 `PUT /api/notes/{id}/cipher`，它有意不动 `title/content/category_id/rev/updated_at`（改一次口令不该让全部笔记在按更新时间倒序的列表里排到最前），也不换全局盐——不要「顺手」把它对齐成 `PUT /api/notes/{id}`。
- `GET /api/info` 与其它 API 一样 `Cache-Control: no-store`，响应只能是标准库运行时与配置值，不得含正文、密文、盐、密码或令牌；`just build` 用 `-ldflags -X` 注入 `internal/build` 的四个变量，git 不可用时必须退化为空值而不是让构建失败。
- 引导完成判据是纯函数 `decideStep(nameSet, masterConfigured)`（`web/src/lib/onboarding.ts`），`App.svelte` 的门槛与 `document.title` 必须共用它；引导页第二步在库中已有主密码时只解锁、绝不重复 `POST /api/master-password`（重复提交会 409，会把用户永久卡在第二步）。
- 配置文件写入必须原子（临时文件 + rename），且保持 YAML 严格解析（未知键报错）。
- `note.yaml` 的 `ai` 四项（`base_url` / `api_key` / `models` / `prompt`）只有一个写入口：`internal/config.SetValues`（`yaml.Node` 级改写以保留其它键与注释 → 编码 → 再用严格 `decode` 复核 → 临时文件 + rename，文件 `0600` / 目录 `0700`）。不要在别处另写一套 YAML 序列化，也不要直接 `os.WriteFile` 覆盖配置文件。
- `api_key` 明文只允许存在于 `note.yaml`：`GET/PUT /api/ai-config` 只回 `api_key_masked` + `api_key_set`，`/api/info`、网页与日志一律不得出现明文。`PUT /api/ai-config` 的 `api_key` 语义是「省略 = 不改动、空串 = 清除」，不是「空 = 清除」。
- `internal/ai` 是全项目**唯一**发起对外 HTTP 请求的地方：网页走 `POST /api/ai/optimize`（服务端代发），`note add` 的 `Ctrl+O` 直接调同一份实现；不要在 handler、`cmd/note` 或前端另写一套请求。调用只在用户显式点击/按键时发生，不后台轮询、不自动重试、不自动换模型。
- AI 调用的日志只能记 `host` / `model` / 状态与截断后的错误摘要：不得出现笔记正文、渲染后的提示词（`{{content}}` 代入后的结果）、自定义提示词与 `api_key`；`POST /api/ai/optimize` 含正文，和其它 API 一样 `Cache-Control: no-store`。优化结果只是建议——网页与 CLI 都必须先预览、用户确认后才替换，超 2000 码点禁止替换而不截断。
- 按次的自定义提示词（`POST /api/ai/optimize` 的 `custom_prompt`，`ai.MaxCustomPromptRunes` = 500 码点，校验在 `ai.ValidateCustomPrompt`）只对那一次调用生效：追加在模板之后并重申原有约束，不写回 `note.yaml` 的 `ai.prompt`、不进编辑器草稿、不落库；它与 `ai.prompt`（模板，≤2000 码点且必须含 `{{content}}`）是两种东西，不要合并成一套校验。
- `ai.prompt` 空串 = 内置默认提示词（`config.DefaultAIPrompt`），非空必须含 `config.PromptVariable`（`{{content}}`）：启动加载、`note config set`、`PUT /api/ai-config` 三处同一口径硬失败，不要为了「好写」放宽成可选；`ai.models` 的拆分/校验/展示同样共用 `config.SplitModels` / `ValidateModels` / `ModelsDisplay`。
- `note config` 与 `PUT /api/ai-config` 必须共用 `config.ValidateBaseURL` / `ValidateAPIKey` / `ValidatePrompt`（以及 `config.Normalize`），禁止在 web 层另写一套规则；`config.ValueError` 表示用法错误（命令行退出 2 / 网页 400），`config.ErrUnknownKey` 表示配置文件里已有未知键（网页 409，让用户先手改）。
- CLI「打开笔记」只有一个含义：`note show <id>`、`note show`（无 id 进选择器）与 `note list` 的 `Enter` 预览共走 `cmd/note` 的 `openNote`——渲染到 stdout + 把 `ShowResult.Source`（库里的原始 Markdown，不是渲染结果）复制到剪贴板。复制只在 stdout 是终端时发生（管道/脚本不该被偷改剪贴板），提示写 stderr，失败不改变退出码；加密笔记没有可复制的原文，仍是退出码 4。网页详情页的「复制原文」按钮是同一语义（加密未解锁时不渲染）。
- `note list` 的筛选：标题 + 分类用 fuzzy（bubbles `DefaultFilter`），正文用子串（`internal/cli/list.go#noteFilter`）——fuzzy 套在长正文上等于不筛。正文来自 `store.ListNotesAll` 的 `NoteListItem.FullContent`（`json:"-"`），网页列表走的 `ListNotes` 不得带回全文。
- 「速记」是第二个内置固定分类（`id=2`，与「未分类」同受 `store.IsReservedCategory` 守卫，迁移 `sql/0007`）；同一分类内标题唯一（两条 partial unique index，`title COLLATE NOCASE`，**含回收站行**，因此恢复不会撞名死锁），网页撞名返回 409 `title_conflict` / `title_in_trash` 并带 `fields.title`。前端判断「能不能改名/删除」只认后端 `Category.reserved`，不得再硬编码分类 id。CLI 的 `note set/get/del` 只按 `(分类, 标题)` 寻址，`set`/`del` 提示写 stderr、`get` 正文写 stdout（`get @分类` 默认只列标题，`--detail` 才出「标题/更新时间/正文」表格），退出码 0/1/2/4；`del @分类` 必须显式 `--yes`，`set` 撞上回收站同名拒绝覆盖并报出 id。

## 开发流程规则

1. 每完成一部分开发，无论是否提交 git，都必须把实现过程与主要步骤逻辑（做了什么、改了哪些模块、关键设计决策与数据流）总结为条目，记录到 `CHANGELOG.md` 的 `[Unreleased]` 小节（按 Added / Changed / Fixed / Security 分类，Keep a Changelog 格式）。
2. 记录只写已验证的事实：先跑 `just test` 与 `just check`，通过后再写条目；测试未通过的状态不得写成"已完成"。
3. 不提交 git 的阶段性改动，同样适用规则 1，以便下次继续（人或 AI）能从 CHANGELOG 恢复上下文。
