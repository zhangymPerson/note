# 个人笔记应用 技术方案

| 项目     | 内容                                                                                                                                                                                                                                                                          |
| -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 文档版本 | v1.8                                                                                                                                                                                                                                                                          |
| 日期     | 2026-10-28                                                                                                                                                                                                                                                                    |
| 状态     | 已实现（与本文件第 6、7.1、9 节一致；偏离来源见 PRD 2.4 与 2.5）                                                                                                                                                                                                              |
| 关联文档 | [产品需求文档 v1.8](./PRD.md)、[后台运行设计](./superpowers/specs/2026-10-28-background-web-design.md)、[信息页与改密设计](./superpowers/specs/2026-10-27-user-info-page-design.md)、[已实现设计说明](./superpowers/specs/2026-10-27-note-crud-category-encryption-design.md) |
| 目标     | 将 PRD 落地为可实现的 Go + SQLite + Svelte 5 技术方案                                                                                                                                                                                                                         |

---

## 1. 目标与约束

### 1.1 实现目标

- 实现 PRD v1.4 的 MVP 范围：Markdown 笔记 CRUD、列表分页与分类筛选、模糊查询、分类管理、全局主密码 + 浏览器端不可逆加密、助记问题、忘记密码、用户信息页（运行信息 + 改名 + 改助记问题 + 改主密码与浏览器端批量重加密）。
- 运行时为单个 Go 可执行文件；前端在构建阶段编译并嵌入，不单独部署。
- 数据只保存在本地 SQLite 文件中，默认仅监听 `127.0.0.1`。
- 加密笔记在数据库中只保存密文字节；不保存密码、密码哈希或可用于重置的密钥。**加解密在浏览器完成，服务端零密码学代码。**
- 页面由独立轻量 Svelte 5 SPA 提供，不使用 SvelteKit、UI 组件库、CSS 框架、路由库或状态管理库；Markdown 只用 `marked` + `dompurify`。
- 后端只提供 JSON API、静态资源和 SPA 回退，不进行服务端页面渲染。

### 1.2 不可妥协的安全不变量

1. 加密笔记在数据库中必须 `content IS NULL` 且 `cipher IS NOT NULL`（由 CHECK 约束保证）。
2. 主密码只用于在浏览器派生密钥，不写入数据库、日志、Cookie，也不写入 `localStorage`/`sessionStorage`/IndexedDB；发往服务端的只有派生用的盐与助记问题。
3. 派生密钥与解密正文只存在于当前页面内存；锁定同时清除内存草稿。
4. 密码错误、密文损坏、跨笔记移植密文、`kdf_params` 越界在界面上归并为同一句「密码错误或数据损坏」。
5. 所有 `/api/*` 响应返回 `Cache-Control: no-store`（含返回密文的笔记详情）。
6. `{@html}` 只允许出现在 `components/Markdown.svelte`，其输入必须先经 `DOMPurify.sanitize`；标题、摘要、分类名、助记问题只通过文本插值渲染。
7. 日志不包含正文、密文、密码、助记问题、完整查询串、Cookie 或请求体。
8. 服务端不参与加解密：`cipher` 对服务端是不透明字节，因此不存在解密端点，也没有服务端解锁会话。
9. AI 配置里的 `api_key` 是唯一以明文存放在磁盘上的凭据：它只存在 `note.yaml`（文件 `0600`、目录 `0700`），任何 API 响应（含 `GET /api/ai-config` 与 `/api/info`）、网页与日志都只能看到掩码；`GET /api/ai-config` 与其它 API 一样 `Cache-Control: no-store`。
10. 主密码可修改，但仍不可找回。「旧密码是否正确」只由浏览器试解已有密文的 GCM 认证标签判定，服务端永远不具备验证口令的能力；改密全程密码与派生密钥都不落盘、不进任何浏览器存储，`PUT /api/notes/{id}/cipher` 的密文请求体也不入日志（`accessLog` 从不记 body）。
11. **唯一的对外网络出口是 `internal/ai`**：只有用户显式点「用 AI 优化格式」或按 `Ctrl+O` 时才由它向 `ai.base_url` 发起请求，正文只在这一刻外发；服务端不后台轮询、不自动重试、不换模型。日志只记 host / model / 状态与错误摘要，不记正文、渲染后的提示词与密钥；含正文的优化响应（`POST /api/ai/optimize`）与其它 API 一样 `Cache-Control: no-store`。

### 1.3 非目标

多用户、云同步、分享、移动端原生应用、附件、标签、回收站、密码找回/重置、取消加密、加密正文搜索、公网认证与 TLS 终止。Markdown/富文本渲染已在 v1.3 移出非目标（仅支持 Markdown 源码存储 + 浏览器端渲染，不做所见即所得编辑器）。修改主密码已在 v1.4 移出非目标：需旧密码，由浏览器端批量重加密实现（见 6.9），与「找回」仍是两件事。详见 PRD 第 2.2 与 2.5 节。

---

## 2. 总体架构

```text
┌──────────────────────────────────────────────────┐
│ 浏览器（Svelte 5 SPA + TypeScript）               │
│ - 自写 History 路由                              │
│ - WebCrypto：PBKDF2 派生密钥 + AES-256-GCM       │
│   加解密（密码与密钥都不离开本页内存）            │
│ - marked + DOMPurify 渲染 Markdown               │
│ - fetch /api，携带 X-CSRF-Token                  │
│ - 不持久化密码、正文、派生密钥                   │
└──────────────────────┬───────────────────────────┘
                       │ HTTP 127.0.0.1:8080
┌──────────────────────▼───────────────────────────┐
│ Go 进程（无任何密码学代码）                        │
│ net/http + ServeMux                              │
│ CLI: note 菜单 / web / info（bubbletea）         │
│ 配置/数据: 用户目录 note.yaml、notes.db          │
│ 后台模式: note web start/stop/status            │
│   日志 note.log、状态 note.json（同 db 目录）    │
│                                                  │
│ middleware: 恢复 / Host / 安全头 / 日志 / CSRF     │
│                                                  │
│ API handlers（cipher 只是黑盒字节）                │
│   └── store  SQLite (notes.db, WAL)              │
│                                                  │
│ 静态资源: //go:embed internal/web/dist           │
└──────────────────────┬───────────────────────────┘
                       │
              notes.db (+ -wal/-shm)
```

### 2.1 请求路径

| 请求                      | 处理                                                      |
| ------------------------- | --------------------------------------------------------- |
| `GET /assets/*`           | 返回内嵌的 Vite 构建产物，带内容哈希的资源长期缓存        |
| `GET /api/*`              | JSON API，动态响应 `no-store`；未知 API 返回 JSON 404     |
| `GET /`、`GET /notes/...` | 非 API 的 GET 回退到 `index.html`，由 Svelte 前端路由接管 |
| `POST/PUT/DELETE /api/*`  | JSON 写请求，经过 Host、Origin、CSRF 校验                 |

### 2.2 进程模型

- 单进程、单用户、无后台任务队列。
- SQLite 使用单连接（`SetMaxOpenConns(1)`），避免并发写与 PRAGMA 状态不一致；个人场景吞吐足够。
- PBKDF2 派生与 AES-GCM 加解密都发生在浏览器进程；Go 侧没有密码学计算，因此没有 KDF 串行化、内存峰值或超时特殊处理。
- 数据库事务只覆盖短小的读写操作；加密状态转换在一次事务内完成（写密文、置 `content = NULL`、置 `is_encrypted`）。
- 没有服务端会话：解锁态由浏览器页面内存中的密钥表示，服务端每个请求都是无状态的。
- 后台模式（`note web start`）只是同一份服务代码换了个入口：父进程 re-exec 自身并立刻退出，子进程脱离终端继续跑，日志经 fd 重定向写入 `note.log`。`stop` 通过 `POST /api/shutdown` 把关停请求交回该进程自己的优雅关闭路径（见 4.1.1）。

### 2.3 目录结构

```text
note/
├── go.mod
├── cmd/note/main.go             # 子命令分发（note 菜单 / web / info / status）、runWeb、runInfo
├── cmd/note/web_service.go      # web start / stop / status 三命令实现
├── cmd/note/status.go           # note status：只读采集 activities 统计并交给 internal/cli 渲染
├── cmd/note/config.go           # note config：list 读三项 / set 原子写入 note.yaml
├── cmd/note/quick.go            # note set/get/del：加载配置 → 开库 → 调 internal/cli → 退出码与 stdout/stderr 分流
├── config/
│   └── note.yaml                 # 示例配置（原始默认值；运行时读用户配置目录，不自动加载）
├── sql/
│   ├── embed.go                  # //go:embed *.sql
│   ├── 0001_init.sql             # v1 notes 表与 CHECK 约束
│   ├── 0002_activity.sql         # 活动事件表（0005 重建了 kind 枚举）
│   ├── 0003_note_fields.sql      # kind/remark/rev
│   ├── 0004_note_app.sql         # categories、master_password、notes.category_id
│   ├── 0005_activity_events.sql  # activities 重建：kind 枚举扩到查询 + 笔记/分类读写
│   ├── 0006_note_soft_delete.sql # notes.deleted_at + 活动/回收站两条部分索引
│   └── 0007_quick_category_and_unique_title.sql # 「速记」分类 + 存量重名清理 + 两条标题唯一索引
├── internal/
│   ├── build/info.go             # 版本/git hash/构建日期变量（ldflags 注入）+ vcs 回落
│   ├── ai/                       # 唯一对外网络出口：OpenAI 兼容的 optimize 客户端（模板渲染、剥围栏、超时与错误映射）
│   │   └── client.go             # Client/NewClient/Configured/Missing/DefaultModel/Host/Optimize、BuildPrompt、StripFence
│   ├── cli/                      # 命令行界面：解析、note add 向导（含 Ctrl+O AI 优化预览）、note list/show 渲染、共用选择器、复制原文、信息/状态渲染、帮助
│   │   ├── parse.go              # 子命令与全局 -c 解析、web 动作（start/stop/status）、list/show 开关、-h 预扫描
│   │   ├── add.go                # note add 三步向导 + AI 优化预览屏（stepOptimized；Ctrl+O 调用、Enter 采用、Esc 保留原文）
│   │   ├── pick.go               # 共用选择器：bubbles/list + gum filter 键位语义（note list 与分类步都用）
│   │   ├── list.go               # note list：交互列表（标题/分类 fuzzy + 正文子串）、非交互纯文本、行渲染与命中片段
│   │   ├── show.go               # note show：渲染参数解析、头部拼接、Source 原文（供复制）、加密笔记的拒绝与退出码 4
│   │   ├── info.go               # note info 渲染（uniseg CJK 对齐、缺库占位、日志行）
│   │   ├── status.go             # note web status 三态渲染与 uptime/文件大小文案
│   │   ├── stats.go              # note status 使用统计渲染：事件分组合并、uniseg 对齐、缺库占位
│   │   ├── config.go             # note config list/set 渲染（密钥掩码、单行折叠、CJK 列对齐）
│   │   ├── ref.go                # 标题寻址：Ref/ParseRef（--type 优先、@分类 引用、最后一个 @ 切分、默认分类）
│   │   ├── quick.go              # note set/get/del 的纯逻辑：QuickDeps + RunSet/RunGet/RunGetCategory/RunGetCategories/RunDel/RunDelCategory
│   │   └── usage.go              # 总帮助与子命令帮助（动态带出本机默认路径）
│   ├── clipboard/                # 把文本写进剪贴板：原生实现（clip.exe/pbcopy/xclip）+ OSC52 兜底
│   │   └── clipboard.go          # Copy / writeOSC52 / ExplainFailure（失败只返回错误，从不 panic）
│   ├── markdown/                 # 全项目唯一 import glamour 的地方（把 Markdown 渲染成终端文本）
│   │   └── render.go             # Options{Width,Style} 与 Render（notty 风格即纯文本）
│   ├── daemon/                   # 后台运行支持（纯库，不含业务逻辑）
│   │   ├── state.go              # note.json 结构、路径推导、原子写、令牌生成、按 pid 删除
│   │   ├── control.go            # ControlAddr：通配地址探测时映射回环
│   │   ├── probe.go              # GET /api/info 探测与 POST /api/shutdown 请求
│   │   ├── spawn.go              # 分离进程启动 + 日志 fd 接管
│   │   ├── spawn_unix.go         # Setsid（新会话）
│   │   ├── spawn_windows.go      # DETACHED_PROCESS + 隐藏窗口
│   │   └── pid_unix.go/pid_windows.go # 存活判定（Windows 用等待句柄）
│   ├── config/
│   │   ├── config.go             # 加载、合并、校验 note.yaml（-c/-p/--remote 等参数）
│   │   ├── ai.go                 # AI 四项：ReadAI、SetValue/SetValues（Node 级改写 + 原子写 + 复核）、模型列表与提示词模板校验、默认提示词、掩码
│   │   ├── paths.go              # 默认配置/数据目录解析（用户目录，XDG 惯例）
│   │   └── urls.go               # 启动访问地址推导
│   ├── store/
│   │   ├── store.go              # 打开、PRAGMA、迁移、关闭 checkpoint、SchemaVersion
│   │   ├── note.go               # 笔记 CRUD、搜索、加密清单、密文替换、删除后 checkpoint
│   │   ├── category.go           # 分类 CRUD、默认分类与占用守卫
│   │   ├── master.go             # 主密码单行（盐 + 助记问题）与问题单独修改
│   │   └── activity.go           # 搜索记录与统计
│   ├── template/
│   │   ├── template.go           # note-example.md 路径推导、默认值内嵌、原子读/写
│   │   └── default.md            # 内置默认 markdown 骨架
│   └── web/
│       ├── server.go             # 路由、静态资源、SPA 回退、Control 注入
│       ├── control.go            # 后台模式的 /api/shutdown（令牌校验 + 异步关停）
│       ├── middleware.go         # 恢复、Host、安全头、日志、CSRF（豁免 /api/shutdown）
│       ├── handlers_notes.go
│       ├── handlers_note_cipher.go # 密文清单与单条替换（改密迁移用）
│       ├── handlers_categories.go # 分类与主密码端点
│       ├── handlers_info.go      # GET /api/info
│       ├── handlers_settings.go
│       ├── handlers_template.go  # GET/PUT /api/template（新建笔记骨架）
│       ├── handlers_ai.go        # GET/PUT /api/ai-config（密钥只回掩码，写入原子；models 逗号串 ↔ YAML 序列）
│       ├── handlers_optimize.go  # POST /api/ai/optimize（服务端代发，结果只回预览；错误映射 400/409/502/504）
│       ├── handlers_stats.go
│       ├── handlers_csrf.go
│       ├── errors.go
│       ├── assets.go             # //go:embed all:dist
│       └── dist/                 # 前端构建产物（生成物，不提交）
├── web/
│   ├── src/
│   │   ├── lib/
│   │   │   ├── api.ts            # fetch 封装、CSRF 刷新重试、错误映射
│   │   │   ├── notes.ts          # 笔记/分类/主密码领域客户端（含密文清单与单条替换）
│   │   │   ├── crypto.ts         # PBKDF2 + AES-GCM、密文编解码
│   │   │   ├── reencrypt.ts      # 改密迁移引擎（纯函数 + 依赖注入，五类落定）
│   │   │   ├── info.ts           # /api/info 摊成界面可读行、字节与时长格式化
│   │   │   ├── onboarding.ts     # 两步引导判据 decideStep(nameSet, masterConfigured)
│   │   │   ├── session-key.ts    # 页面内存密钥、unlockKey/lockSession
│   │   │   ├── draft.ts          # 编辑器内存草稿
│   │   │   ├── template.ts       # 新建笔记骨架客户端（/api/template）
│   │   │   ├── ai-config.ts      # AI 四项客户端：读取掩码与默认模板、保存（省略/清空密钥）、字数与提示文案
│   │   │   ├── ai.ts             # 优化客户端：optimizeNote（省略模型即用第一个）、defaultModel、hostOf
│   │   │   ├── markdown.ts       # marked + DOMPurify 净化
│   │   │   ├── stats.ts / theme.ts / toast.ts / settings.ts
│   │   ├── components/           # Layout、Markdown、CategoryTree、ActivityHeatmap、ThemeSwitch
│   │   ├── views/                # 列表、详情、编辑器、模板、回收站、主密码、信息、忘记密码、概览、引导、404
│   │   ├── router/               # 约 60 行 History 路由
│   │   ├── app.css               # 全局样式与三主题变量
│   │   ├── App.svelte
│   │   └── main.ts
│   ├── index.html
│   ├── package.json
│   ├── package-lock.json
│   ├── svelte.config.js
│   ├── tsconfig.json
│   ├── tsconfig.app.json
│   ├── tsconfig.node.json
│   ├── vite.config.ts
│   └── vitest.config.ts
├── data/                         # 运行时生成，不提交
├── docs/
│   ├── PRD.md
│   ├── TECH_DESIGN.md
│   └── superpowers/specs/        # 已实现设计说明
├── justfile
├── README.md
└── LICENSE
```

---

## 3. 技术选型

| 层       | 选型                                                    | 说明                                                                                          |
| -------- | ------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| 语言     | Go 1.26+                                                | 单二进制、标准库 HTTP 路由、部署简单                                                          |
| HTTP     | `net/http` + `ServeMux`                                 | 使用标准库方法路由，不引入 Web 框架                                                           |
| 配置     | `gopkg.in/yaml.v3`                                      | 解析 `note.yaml`（用户配置目录），严格字段校验                                                |
| CLI 向导 | `github.com/charmbracelet/bubbletea` + `bubbles`        | `note add` 三步向导与 `note list` 的 TUI 与输入控件（textarea/textinput/list；纯 Go，无 CGO） |
| 终端渲染 | `github.com/charmbracelet/glamour`                      | `note show` 的 Markdown 渲染内核（glow 用的同一套；纯 Go，无 CGO）                            |
| 终端尺寸 | `github.com/charmbracelet/x/term`                       | `note show` 探测终端是否为 TTY 与宽度（bubbletea 已间接依赖）                                 |
| 数据库   | SQLite + `modernc.org/sqlite`                           | 纯 Go，无 CGO；本地单文件                                                                     |
| 密钥派生 | 浏览器 WebCrypto `PBKDF2-HMAC-SHA256`                   | Go 侧零密码学依赖；Node 22+ 与主流浏览器原生可用                                              |
| 对称加密 | 浏览器 WebCrypto `AES-256-GCM`                          | 认证加密；GCM tag 即密码校验                                                                  |
| Unicode  | 浏览器 `String.prototype.normalize('NFC')`              | 跨客户端派生同一密钥                                                                          |
| ID       | `github.com/google/uuid`（Go 标准做法，当前为直接实现） | UUID v4，避免自增 ID 泄露数量；加密笔记由浏览器先生成 ID 以绑定 AAD                           |
| 前端     | Vite + Svelte 5 + TypeScript                            | 独立轻量 SPA，不使用 SvelteKit                                                                |
| Markdown | `marked` + `dompurify`                                  | 构建期内嵌，无外部请求；净化后才能进入 `{@html}`                                              |
| 路由     | 自写 History 路由                                       | 视图只有十来个，不引入路由库                                                                  |
| 样式     | Svelte scoped CSS + 全局 reset 与主题变量               | 不引入 CSS 框架；亮/暗/护眼三主题                                                             |
| 前端测试 | Vitest（DOM 用例按文件启用 jsdom 环境）                 | 加密往返、Markdown 净化、API 客户端、路由匹配                                                 |
| 类型检查 | `svelte-check`                                          | 在构建和 CI 中执行                                                                            |
| 运行时   | Go 单二进制                                             | 前端产物通过 `//go:embed` 内嵌                                                                |

前端运行时依赖只有 `marked` 与 `dompurify`；Svelte 由 Vite 在构建阶段编译。Node/npm 只存在于开发和构建环境。

---

## 4. 后端设计

### 4.1 启动与关闭

v1.5 起入口是 CLI 子命令（`internal/cli.Parse` 分发）：

| 调用                                            | 行为                                                                                                                                                                                                                                                                                |
| ----------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `note`                                          | 打印帮助（等价 `note help`）：把可用命令直接摆出来，不再进交互菜单                                                                                                                                                                                                                  |
| `note add [参数]`                               | bubbletea 三步向导：选分类（输入即模糊筛选，可就地新建）→ 标题 → 正文（支持换行，`Ctrl+D` 完成）；只创建明文笔记；stdin 非终端时报错退出 1；正文步 `Ctrl+O` 用 AI 优化（预览后 `Enter` 采用、`Esc` 保留原文，未配置时提示该补哪几项）                                               |
| `note list [参数]`                              | 可筛选的笔记列表（输入即筛选标题/分类（fuzzy）与正文（子串）、`Enter` 预览并复制原文、`Esc` 退出）；非终端时输出纯文本一行一条，空结果零字节；数据一次性读进内存（`store.ListNotesAll`，避免逐键查库污染 `RecordSearch`）                                                           |
| `note set <标题[@分类]> <正文>`                 | 新建或覆盖一条明文笔记：默认落内置「速记」，同分类同名即覆盖（`rev+1`），分类不存在时 `EnsureCategory` 顺带创建；提示写 stderr，stdout 为空                                                                                                                                         |
| `note get <标题[@分类]>` / `@<分类>` / `--list` | 读一条正文到 stdout（缺结尾换行时补一个）/ 列分类下笔记（默认每行一个标题，`--detail` 用 `renderTable` 输出「标题 / 更新时间 / 正文摘要」表格）/ 列全部分类（ID、名字、笔记数、固定分类标记）                                                                                       |
| `note del <标题[@分类]>` / `@<分类> --yes`      | 软删除一条；`@分类` 删除该分类下所有未删除笔记，缺 `--yes` 是用法错误（退出 2）。不做 CLI 恢复，恢复与彻底删除仍在网页                                                                                                                                                              |
| `note show [<id>] [参数]`                       | 渲染一条笔记（`internal/markdown` → glamour）并把库里的原始 Markdown 复制到剪贴板（仅 stdout 是终端时复制，失败只提示）；省略 id 时进与 `note list` 同一个选择器（要求 stdin/stdout 都是终端）；真终端打印三行头部，非终端无头部且默认 `notty`（无 ANSI）；加密笔记打印提示并退出 4 |
| `note web [参数]`                               | 前台启动 Web 服务，唯一会启动前端页面的基线命令（日志进终端，不建日志文件）                                                                                                                                                                                                         |
| `note web start [参数]`                         | 后台启动；已在运行则打印地址后退出 0                                                                                                                                                                                                                                                |
| `note web stop [参数]`                          | 请求后台实例优雅停止；未运行则提示并退出 0                                                                                                                                                                                                                                          |
| `note web status [参数]`                        | 输出运行情况；运行中 0 / 进程在但不响应 1 / 未运行 3                                                                                                                                                                                                                                |
| `note info [参数]`                              | 输出软件与运行信息（见 8.6 同源口径）后退出；数据库文件已存在时才打开，不产生新文件                                                                                                                                                                                                 |
| `note status [参数]`                            | 输出使用统计（累计事件、活跃天数、事件构成、Top 查询词）；读 `store.GetStats`，与 `/api/stats` 同源；文件不存在时不建库                                                                                                                                                             |
| `note help [命令]` / `-h`                       | 帮助；`note web -h` / `note help web` 显示子命令参数                                                                                                                                                                                                                                |
| 参数错误                                        | 错误信息 + 帮助，退出码 2（与 flag 包惯例一致）                                                                                                                                                                                                                                     |

启动顺序（`note web`）：

1. 解析全局 `-c` 与子命令参数，加载配置文件（默认在用户配置目录，见 4.2），合并为最终配置并校验。
2. 创建数据目录（POSIX `0700`）。
3. 打开 SQLite 连接，设置连接数与 PRAGMA。
4. 检测数据库状态：新建空库执行全量迁移；已有库做完整性检查并按需迁移（见 5.2）。
5. 校验内嵌前端是否存在 `index.html`。
6. 初始化解锁会话管理器与清理协程。
7. 装配路由与中间件，启动 HTTP 服务。
8. 监听 `SIGINT`/`SIGTERM`。

关闭顺序：

1. 收到信号后停止接收新请求。
2. 调用 `http.Server.Shutdown`（最多 10 秒）。
3. 停止会话清理协程，清空会话密钥。
4. 执行 `PRAGMA wal_checkpoint(TRUNCATE)`。
5. 关闭数据库连接。

启动失败时输出明确错误并以非零码退出；不得在数据库损坏时静默重建文件。

#### 打开笔记即复制原文（v1.12）

`note show <id>`、`note show`（不给 id 时先选）与 `note list` 的 `Enter` 预览是同一件事的三个入口，行为必须一致：渲染到 stdout，**同时**把库里的原始 Markdown 复制到剪贴板。

- **复制的是原文**：`cli.BuildShow` 的 `ShowResult` 除了 `Header`/`Body` 还带 `Source`（`NoteDetail.Content` 原样，`--raw` 与否都一样）。复制 `Source` 而不是 `Body`：渲染结果带着色码与按宽度折行，粘到编辑器里就废了。
- **只在终端下复制**：`cmd/note` 的 `openNote` 判断 stdout 是终端才调 `internal/clipboard`；`note show x --raw > a.md` 这类管道不改用户的剪贴板——脚本里不该有看不见的副作用。成功/失败提示都写 stderr，stdout 只留正文或渲染结果。
- **失败不致命**：`internal/clipboard.Copy` 先试原生实现（`github.com/atotto/clipboard`：Windows `clip.exe`、macOS `pbcopy`、Linux `xclip`/`xsel`），失败再试 OSC52（`ESC ] 52 ; c ; <base64> ST`，SSH 下只要终端支持就能用，超过 100 KiB 不发）。两条都失败只打印「复制到剪贴板失败：…」，笔记照常显示、退出码不变。
- **加密笔记没有可复制的原文**：CLI 没有主密码，`note show` 打提示并以退出码 4 结束（不碰剪贴板）；选择器里选中加密笔记同理。

`note list` 的筛选因此同时看两样东西（`internal/cli/list.go`）：标题 + 分类仍走 bubbles 的 `DefaultFilter`（fuzzy，命中下标用来染色、命中项排前面），正文用**子串**匹配（fuzzy 套在长正文上几乎任何子序列都算命中，列表就失去筛选意义）。只命中正文的项追加在标题命中项之后，第二行改显示正文里命中处附近的一小段（前推 12 个字符、按显示宽度截断、命中词染色），否则用户看不出这条为什么被筛出来。正文来自 `store.ListNotesAll` 新增的 `NoteListItem.FullContent`（`json:"-"`，只给 CLI 用；网页列表走的 `ListNotes` 仍只带 140 码点摘要，响应不会变大）。

### 4.1.1 后台运行（v1.6）

```text
启动：`note web start`（父）── re-exec 自身（子，带 NOTE_DAEMON_STATE=<状态文件>）
      └─ 子：Listen → 生成令牌 → 写 note.json → Serve
停止：`note web stop`（父）── 读 note.json → POST /api/shutdown（X-Shutdown-Token）
      └─ 子：202 → 关闭 stopCh → Shutdown(10s) → store.Close() → 删 note.json → 退出
      └─ 父：若 10s 内未退出 → SIGKILL / TerminateProcess + WAL 提示 → 清 note.json
探测：`note web status`（父）── 读 note.json（pid/日志/启动时间） + GET /api/info（确实在响应）
```

- 后台化只能靠自我 re-exec：Go 没有 `fork`。Unix 子进程 `SysProcAttr{Setsid: true}`，Windows 用 `DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP` + `HideWindow`；子进程靠环境变量 `NOTE_DAEMON_STATE` 认领后台身份。
- 日志在**文件描述符层**接管：`cmd.Stdout` / `cmd.Stderr` 指向 `note.log`。不能“运行期切日志”——slog 默认 handler 在启动时就绑定了当时的 `os.Stderr`。
- 令牌由**子进程**用 `crypto/rand` 生成（32 字节，base64url）并写入状态文件，父进程不经手，因此不进命令行与环境变量。
- 状态文件 `note.json` 与日志 `note.log` 固定在 `filepath.Dir(DBPath)`；状态文件 `0600` + 临时文件 rename 原子写；日志追加写、不轮转。
- 状态文件按目录唯一，所以同一目录下换一个 `--db` 会被判为「已被另一个实例登记」，`start`/`stop` 都以退出码 1 拒绝（比对 `db` 字段，允许相对/绝对两种写法）。
- `stop` 只有在取得「状态文件里有 pid」且「该 pid 确实活着」时才发关停请求；只有探测到响应而无状态文件时，判定为前台实例，提示回终端 Ctrl+C 并退出 1，不猜 pid 杀进程。
- `status` 把状态文件与 `GET /api/info` 探测合并判定（双保险）：状态文件 pid 已死且探测失败 → 按残留清理并以 3 退出；pid 活着但探测失败 → 1；探测成功 → 0。状态文件里的通配地址（`0.0.0.0` / `::`）在探测时映射为回环地址（`daemon.ControlAddr`）。

### 4.2 配置

配置来源优先级：内置默认值 < 配置文件 < 命令行参数。不读取环境变量，避免配置隐式来源。

配置文件默认位置与其他常见开源项目一致，跟随操作系统用户目录（`internal/config/paths.go`）：

| 平台    | 配置文件（`DefaultConfigFile`）                       | 数据库（`DefaultDBFile`）                               |
| ------- | ----------------------------------------------------- | ------------------------------------------------------- |
| Windows | `%AppData%\note\note.yaml`                            | `%AppData%\note\notes.db`                               |
| macOS   | `~/Library/Application Support/note/note.yaml`        | `~/Library/Application Support/note/notes.db`           |
| Linux   | `$XDG_CONFIG_HOME/note/note.yaml`（默认 `~/.config`） | `$XDG_DATA_HOME/note/notes.db`（默认 `~/.local/share`） |

v1.5 之前的默认位置是 `./config/note.yaml` 与 `./data/notes.db`（相对进程工作目录）；既有数据用 `-c` / `--db` 指向原文件即可继续使用，不做隐式回退。

仓库内 `config/note.yaml` 保留为示例（原始默认值，含 `server` / `database` 两段），不再被自动加载：

```yaml
server:
  addr: 127.0.0.1:8080
  allow_remote: false

database:
  path: ./data/notes.db

ai:
  base_url: https://api.openai.com/v1
  api_key: sk-...
  prompt: |
    把下面的内容整理成 Markdown 笔记，保留标题层级。
```

| 参数                       | YAML 键               | 默认值                          | 说明                                             |
| -------------------------- | --------------------- | ------------------------------- | ------------------------------------------------ |
| `-c, --config <路径>`      | —                     | `<用户配置目录>/note/note.yaml` | 配置文件路径（全局参数，也可写在子命令后）       |
| `--addr <host:port>`       | `server.addr`         | `127.0.0.1:8080`                | 监听地址                                         |
| `-p, --port <端口>`        | —                     | 沿用 `--addr` / 配置文件的端口  | 只覆盖端口，host 沿用已生效地址（IPv6 加方括号） |
| `--db <路径>`              | `database.path`       | `<用户数据目录>/note/notes.db`  | SQLite 文件路径                                  |
| `--remote, --allow-remote` | `server.allow_remote` | `false`                         | 允许绑定非回环地址；启动时打印安全警告           |

规则：

- YAML 使用严格解码：未知键、类型错误、非法取值都拒绝启动，并输出具体字段。
- `-c` / `--config` 显式指定（全局或子命令均可，子命令后者生效）但文件不存在或无法读取 → 拒绝启动；使用默认路径且文件缺失 → 使用内置默认值并打印警告。
- 命令行参数只覆盖显式提供的项，未提供的项沿用 YAML 或内置默认值；`-p/--port` 在地址解析完成后只替换端口部分。
- 相对路径基于进程工作目录解析。
- 未启用 `allow_remote` 时，`addr` 必须是回环地址；否则拒绝启动。
- 启动日志记录最终生效的监听地址、数据库路径与 `allow_remote`，不打印其他配置内容。
- 监听成功后另向 stdout 打印可直接点击的访问地址（`http://host:port`，IPv6 一律加方括号），每地址一行；结构化 slog 字段与格式不变。地址由 `internal/config` 的 `StartupURLs(请求地址, listener 实际地址)` 推导：家族取请求 host（`0.0.0.0` 枚举本机非回环 IPv4，`::` 枚举 IPv6），端口取 listener 实际端口（兼容 `:0`）；枚举跳过回环与链路本地地址并去重排序，结果为空时回退回环地址。Windows 对 `0.0.0.0` 监听会建立双栈 socket 并把 `Addr()` 汇报成 `[::]`，家族因此不能取自 listener 地址。监听失败时错误信息回带尝试的地址。
- 日志与状态文件位置（v1.6）：纯函数 `daemon.StatePath(dbPath) = dir(dbPath)/note.json`、`daemon.LogPath(dbPath) = dir(dbPath)/note.log`；不读环境变量、不引入新配置项。`note info` 与 `note web status` 都从同一个纯函数推导，不会不一致。
- 新建笔记骨架位置（v1.7）：纯函数 `template.Path(configPath) = dir(configPath)/note-example.md`，与 `note.yaml` 同目录，不读环境变量、不引入新配置项；`note web` 启动时（前台与后台子进程共用 `serve`）只在文件缺失时写入 `go:embed` 的内置默认骨架，**已存在的文件绝不覆盖**，配置目录不可写只记 `slog.Warn`、不影响启动；写入走临时文件 + rename，权限 `0644`（内容不含正文与密码，配置目录本身是 `0700`）。`GET/PUT /api/template` 返回的 `path` 与 `/api/info` 的 `db_path` 属同一尺度的路径信息公开。
- 数据目录不存在时自动创建，权限 `0700`。
- 数据库文件创建后，在 POSIX 上尽力设置为 `0600`。

#### AI 四项（v1.13 起，v1.14 扩展）

`ai.base_url` / `ai.api_key` / `ai.models` / `ai.prompt` 存在同一个 `note.yaml` 里，不引入第二份配置文件、不读环境变量。v1.13 只做存储与读写；v1.14 起由 `internal/ai` 在用户触发时调用 `/chat/completions`（见 4.4.1）。

- **单一写入口**：`internal/config/ai.go` 的 `SetValues(path, []KeyValue)` 是唯一写路径（`SetValue` 是它的单元素包装）。流程是：值归一化（地址与密钥 trim 首尾空白，提示词原样）→ 逐项校验（`ValidateBaseURL` / `ValidateAPIKey` / `ValidatePrompt`）→ 用 `yaml.Node` 读现有文档、只改目标节点（**保留其它键与注释**）→ 编码 → **再 `decode` 一遍**确认服务仍能严格解析 → 同目录临时文件 + rename 原子替换（文件 `0600`、目录 `0700`）。全部校验通过才落盘，中途失败文件保持原样。
- **两类拒绝**：`ValueError`（键未知或值非法）→ 命令行退出 2、网页 400 字段级提示；`ErrUnknownKey`（配置文件里已有严格解析不认识的键，写回会把它们带出去）→ 网页 409 `config_invalid`、命令行直接报错，提示用户先手改 YAML。
- **读**：`ReadAI(path)` 只读文件本身（`found=false` 表示文件还没创建），供 `note config list` 与 `GET /api/ai-config` 使用；两者都不建文件。启动时 `Load` 会把三项读进 `Config.AI` 并用 `ValidateAI` 校验，坏地址在启动阶段就报错。
- **密钥只出掩码**：`MaskSecret` 输出「前 3 + `…` + 后 4」（不足 12 字符给 8 个 `*`）。`GET/PUT /api/ai-config` 的响应体是 `{base_url, models, prompt, default_prompt, prompt_variable, api_key_set, api_key_masked, path}`（`models` 空时给 `[]`），明文密钥不进响应、不进网页、不进日志（`slog` 只记键名与失败原因）。
- **网页的「留空≠清空」**：`PUT` 的 `api_key` 是 `*string`——省略表示沿用已有密钥，空串表示清除，非空表示替换。这样页面每次保存都不需要把密钥回填到输入框里。
- **模型列表（v1.14）**：`ai.models` 在 YAML 里是 `!!seq`（`SetValues` 用 `setModelsNode` 写序列，清空写 `[]`），命令行与网页都用逗号串读写，拆分为 `SplitModels`（逗号/换行分隔 → trim → 去空 → 按首现顺序去重）；校验 `ValidateModels`：最多 10 项、每项 ≤200 字节且不含空白/控制字符。第一个即默认模型；空列表表示未配置。手写成标量（`models: gpt-4o`）会在启动加载时直接报错，不猜成单元素列表。
- **提示词模板（v1.14）**：`ai.prompt` 空串 = 使用内置默认提示词 `config.DefaultAIPrompt`（唯一来源，`note.yaml` 不存副本）；非空**必须**含 `{{content}}`，否则启动加载、`note config set` 与 `PUT /api/ai-config` 都拒绝（`PromptVariable` 是唯一常量名）。`PromptOrDefault` / `PromptDisplay` / `ModelsDisplay` 与 `SplitModels` / `JoinModels` 由命令行与网页共用，避免两个入口对同一个值给出不同理解。
- 校验口径（与启动加载、命令行共用）：`base_url` 必须 http(s) 绝对地址、带 host、不带查询串或锚点、≤500 字节；`api_key` ≤512 字节且不含空白/控制字符；`models` 见上；`prompt` ≤2000 码点（与正文上限一致，因为它最终会拼进正文一起用）且含变量。

### 4.3 路由与中间件

```text
GET    /api/csrf
GET    /api/info
POST   /api/shutdown              # 仅后台模式（Control.Token 非空）存在；无 CSRF，凭 X-Shutdown-Token
GET    /api/settings
PUT    /api/settings/name
GET    /api/template              # 新建笔记的 markdown 骨架（文件缺失回退内置默认）
PUT    /api/template              # 覆盖骨架文件（需 CSRF，≤2000 码点）
GET    /api/notes
POST   /api/notes
GET    /api/notes/encrypted       # 密文清单（改密迁移用），静态段优先于 {id}
GET    /api/notes/{id}
PUT    /api/notes/{id}
PUT    /api/notes/{id}/cipher     # 只替换密文（需 CSRF）
DELETE /api/notes/{id}           # 默认软删除；?purge=1 彻底删除（仅回收站内）
POST   /api/notes/{id}/restore    # 从回收站恢复
GET    /api/categories
POST   /api/categories
PUT    /api/categories/{id}
DELETE /api/categories/{id}
GET    /api/master-password
POST   /api/master-password       # 仅首次配置
PUT    /api/master-password/question
GET    /api/stats
GET    /assets/*
GET    /*                     # 非 API GET → index.html
```

没有 `POST /api/notes/{id}/encrypt`、`/unlock`、`/lock`：加解密与锁定都是浏览器本地动作（见 8.3）。

中间件顺序：

1. panic 恢复：记录不含敏感数据的堆栈摘要，返回 500。
2. Host 校验：只允许配置主机及回环别名，失败返回 400。
3. 安全响应头：CSP、`nosniff`、`DENY`、`Referrer-Policy`、缓存策略。
4. 访问日志：方法、路由模式/路径、状态码、耗时、响应字节数；不记录查询串与请求体。
5. CSRF：仅对 `POST`/`PUT`/`DELETE` 校验 `X-CSRF-Token`、Cookie、Origin/Sec-Fetch-Site；唯一例外是 `/api/shutdown`（凭证即令牌，豁免不得扩大）。
6. 业务处理器。

静态与回退规则：

- `/assets/*` 由内嵌文件系统提供；缺失资源返回 404，不回退到 `index.html`。
- `/api/*` 未知路径返回 JSON 404，不回退到 `index.html`。
- 其余 GET 请求满足以下任一条件时返回 `index.html`：
  - `Accept` 包含 `text/html`；
  - 路径最后一段不含 `.`。
- `index.html` 返回 `Cache-Control: no-store`；带哈希的 `/assets/*` 返回 `public, max-age=31536000, immutable`。

### 4.4 处理器职责与校验

| 处理器                                         | 职责                                                                                                              | 关键校验                                                                                                                                                   |
| ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /api/csrf`                                | 签发/复用 CSRF Token                                                                                              | 无                                                                                                                                                         |
| `GET /api/notes`                               | 列表、搜索与分类筛选；`deleted=1` 改为回收站视图                                                                  | `q` ≤ 200 字符；`page` ≥ 1；`category` 可选；`deleted` 仅接受 `1`；页大小固定 20                                                                           |
| `POST /api/notes`                              | 创建明文笔记或写入浏览器密文                                                                                      | 标题、正文或密文二选一、`kdf_params`、`category_id`、可选客户端 `id`                                                                                       |
| `GET /api/notes/{id}`                          | 详情；加密笔记返回元数据 + base64 密文，永不含 `content`                                                          | UUID 合法；否则 404                                                                                                                                        |
| `PUT /api/notes/{id}`                          | 更新标题/正文或提交新密文                                                                                         | 同 POST；加密笔记不允许改回明文                                                                                                                            |
| `PUT /api/notes/{id}/cipher`                   | 改密迁移：只替换单条笔记的密文与派生参数                                                                          | 只接受 `cipher_b64` + `kdf_params`，其余字段不接受；长度与 base64 语义同 POST（共用 `decodeCipher`）；明文笔记 409 `not_encrypted`，不存在 404             |
| `GET /api/notes/encrypted`                     | 全部加密笔记的 `{id, cipher_b64, kdf_params}` 清单，按 `created_at, id` 稳定排序                                  | 无；不含标题与正文，空清单序列化为 `[]`                                                                                                                    |
| `DELETE /api/notes/{id}`                       | 默认软删除（记 `note_delete`）；`?purge=1` 彻底删除（记 `note_purge`）                                            | UUID 合法；`purge` 仅接受 `1`；笔记不在回收站时 purge 返回 409 `not_in_trash`                                                                              |
| `POST /api/notes/{id}/restore`                 | 从回收站恢复，返回完整笔记（含分类）                                                                              | UUID 合法；不在回收站返回 409 `not_in_trash`，不存在 404                                                                                                   |
| `GET /api/master-password`                     | 返回 `{configured, salt_b64, question}`                                                                           | 无；盐是公开 KDF 参数                                                                                                                                      |
| `POST /api/master-password`                    | 首次设置盐与助记问题（引导页步骤 2）                                                                              | `salt_b64` 解码后恰好 16 字节；`question` 1–100 字符；已存在返回 409                                                                                       |
| `PUT /api/master-password/question`            | 只改助记问题，不换盐                                                                                              | `question` 去空白后 1–100 码点；未配置主密码返回 409 `master_password_missing`                                                                             |
| `GET /api/info`                                | 运行信息与构建版本（见 8.6）                                                                                      | 无；只读标准库运行时与配置，失败时仅 `PRAGMA user_version` 读错会 500                                                                                      |
| `GET /api/categories`                          | 分类列表 + 每类笔记数                                                                                             | 无                                                                                                                                                         |
| `POST /api/categories`                         | 新增分类                                                                                                          | 名称 1–30 字符、忽略大小写唯一                                                                                                                             |
| `PUT /api/categories/{id}`                     | 改名                                                                                                              | `id=1` 返回 409；重名 400 字段错误                                                                                                                         |
| `DELETE /api/categories/{id}`                  | 删除空分类                                                                                                        | `id=1` 返回 409；仍有笔记返回 409 + `fields.note_count`                                                                                                    |
| `GET /api/stats`                               | 概览页统计与热力图数据                                                                                            | 无                                                                                                                                                         |
| `GET /api/settings` / `PUT /api/settings/name` | 应用命名                                                                                                          | 名称长度与字符合法                                                                                                                                         |
| `GET /api/template` / `PUT /api/template`      | 新建笔记骨架：读写 `<配置目录>/note-example.md`（见 4.2）                                                         | `content` ≤ 2000 码点；写请求需 CSRF；文件不可读时 GET 回退内置默认并标 `source=default`                                                                   |
| `GET /api/ai-config` / `PUT /api/ai-config`    | AI 四项配置：读 `note.yaml` 并回掩码 / 原子写入四项（`models` 逗号串 ↔ YAML 序列）（见 4.2「AI 三项」）           | `base_url` http(s) 绝对地址、`api_key` 无空白且 ≤512 字节、`prompt` ≤2000 码点；省略 `api_key` 表示不改、空串表示清除；文件已有未知键 409 `config_invalid` |
| `POST /api/ai/optimize`                        | 用 AI 润色正文（见 4.4.1）：服务端代发 `/chat/completions`，回 `{content, model}`，不落库；400/409/502/504 见 4.5 |

通用校验：

- 请求体上限：笔记接口先看 `Content-Length`，超过 128 KiB 直接 413 `body_too_large`；分类、主密码、姓名的 4 KiB 上限由 `io.LimitReader` 实现，超限时 JSON 被截断、解析失败，表现为 400 `body_invalid`。未知字段忽略，因此新增字段不会破坏旧客户端。
- 标题：去首尾空白后 1–200 个字符（`utf8.RuneCountInString`）。
- 正文：原始 Markdown 文本，最大 2000 个字符（按 Unicode 码点计，emoji 计 1）；允许空字符串，但明文与密文不能同时缺失或同时存在。
- 密文：标准 base64（`base64.StdEncoding`，不接受换行或空格混排），解码后长度 29–65536 字节；`kdf_params` 必须是 `algo=pbkdf2`、`iter ∈ [100000,1000000]`、`keylen = 32` 的 JSON。
- 客户端可自带笔记 UUID（加密流程需要先用 ID 绑定 AAD）；非法 UUID 或重复 ID 分别返回 400 与 409。
- 分类名 1–30 字符，`UNIQUE COLLATE NOCASE`；助记问题 1–100 字符。
- 主密码本身不作为任何请求字段出现，因此服务端没有长度/一致性校验；确认密码、勾选确认项都是浏览器侧的表单校验。
- 所有错误信息不得包含 SQL、文件路径、密钥、密码或密文内容。

#### 4.4.1 AI 优化（v1.14）

`POST /api/ai/optimize` 是唯一会主动发起对外网络请求的处理器，实现分三层：

1. **校验**（发请求之前）：`decodeJSON` 上限 64 KiB → 正文 trim 后非空且 ≤2000 码点（与保存笔记同一常量 `maxContentRunes`）→ 现读 `note.yaml`（`config.ReadAI`，手改立即生效）→ `ai.NewClient(...).Configured()`（缺项时 409 `ai_not_configured`，文案点名 `Missing()` 的键）→ 显式 `model` 必须在 `ai.models` 里（不在则 400 字段级 `model`）。空 `model` 表示用 `DefaultModel()`（列表第一个）。
2. **调用**：`context.WithTimeout(r.Context(), 60s)` → `client.Optimize(ctx, content, model)`。提示词由 `ai.BuildPrompt` 渲染（空模板回落到 `config.DefaultAIPrompt`，只替换第一个 `{{content}}`，正文里再出现同名变量不二次展开）；`temperature=0.2`、`stream=false`；`BaseURL` 末尾斜杠先 trim 再拼 `/chat/completions`。
3. **错误映射**：`context.DeadlineExceeded` → 504 `ai_timeout`；`ai.ModelError` → 400 字段级 `model`；`ai.UpstreamError` → 502 `ai_upstream_failed`（消息带状态码与截断到 200 码点的上游摘要）；`ai.ErrBadResponse` / `ai.ErrEmptyResult` / `ai.ErrTooLarge` → 502 `ai_bad_response`；其余 → 502 `ai_upstream_failed`。日志只写 `host` / `model` / `error`，**不写正文与密钥**。

`internal/ai` 的响应处理：`io.LimitReader(body, 1MiB+1)` 读满即 `ErrTooLarge`；非 2xx 尝试取 `{"error":{"message":…}}`，取不到用 `http.StatusText`；成功后取 `choices[0].message.content`，`StripFence` 只在「整段被一对 ``` 或 ~~~ 围住且内部没有同种围栏」时剥壳（避免吃掉正文里的代码块），结果为空则 `ErrEmptyResult`。客户端不重试、不自动换模型。

网页与 CLI 共用同一份 `internal/ai`：网页经端点（浏览器拿不到密钥），`note add` 的 `Ctrl+O` 直接调用（不要求 `note web` 在跑）。两处都遵守「先预览、用户确认后才替换」，并都预先检查结果 ≤2000 码点，超限不截断、直接禁止替换。

### 4.5 错误模型

统一响应：

```json
{
  "error": {
    "code": "validation_failed",
    "message": "标题不能为空",
    "fields": {
      "title": "标题不能为空"
    }
  }
}
```

| code                      | HTTP | 说明                                                              |
| ------------------------- | ---- | ----------------------------------------------------------------- |
| `validation_failed`       | 400  | 字段校验失败，`fields` 为字段级文案                               |
| `body_invalid`            | 400  | JSON 解析失败                                                     |
| `host_invalid`            | 400  | Host 不在白名单                                                   |
| `csrf_invalid`            | 403  | `X-CSRF-Token` 缺失或与 `csrf` Cookie 不一致                      |
| `not_found`               | 404  | 笔记、分类或 API 不存在                                           |
| （`ServeMux` 默认）       | 405  | 已注册路径的其它方法；写请求会先过 CSRF 校验                      |
| `encrypted_immutable`     | 409  | 加密笔记不能改回明文                                              |
| `master_password_missing` | 409  | 未设置主密码就提交密文                                            |
| `master_password_exists`  | 409  | 重复设置主密码                                                    |
| `id_conflict`             | 409  | 客户端提供的笔记 ID 已存在                                        |
| `title_conflict`          | 409  | 同分类下已有同名笔记（`title COLLATE NOCASE`），带 `fields.title` |
| `title_in_trash`          | 409  | 同名笔记在回收站里（仍占着这个名字），带 `fields.title`           |
| `category_reserved`       | 409  | 改名或删除默认分类                                                |
| `category_not_empty`      | 409  | 分类下仍有笔记，`fields.note_count` 为条数                        |
| `ai_not_configured`       | 409  | AI 没配齐（`base_url`/`api_key`/`models`），文案点名缺项          |
| `ai_upstream_failed`      | 502  | 上游非 2xx 或网络失败（消息含状态码与截断摘要）                   |
| `ai_bad_response`         | 502  | 上游返回坏 JSON / 空 choices / 空正文 / 响应过大                  |
| `ai_timeout`              | 504  | 单次调用超 60 秒                                                  |
| `body_too_large`          | 413  | 请求体超过限制                                                    |
| `internal`                | 500  | 内部错误，不返回细节                                              |

- 没有 401 家族：服务端不做解密，也不维护解锁会话。「密码错误或数据损坏」是纯前端文案，由 `web/src/lib/crypto.ts` 的 `DecryptError` 归并产生（密码错误、tag 校验失败、base64 非法、密文过短、`iter`/`keylen` 越界全部映射到它）。
- 密文对服务端是黑盒：服务端只校验字节长度与 `kdf_params` 边界，非法输入返回 400 而非 panic（`recoverPanics` 是最后兜底，不是常规路径）。

### 4.6 日志

- 输出到 stdout（后台模式重定向到 `note.log`），单行结构化文本或 JSON。
- 访问日志字段：时间、级别、方法、路由模式、状态码、耗时、响应字节数。
- 禁止记录：请求体、密码、正文、密文、助记问题、完整查询串、Cookie、Authorization、关停令牌。
- 服务端没有加解密事件可记录；只有 API 状态码与错误类别。AI 调用另记一条 `optimize note`，字段仅 `host` / `model` / `error`（上游摘要已截断）：不记正文、渲染后的提示词与密钥。
- 启动日志可记录数据库路径、监听地址、是否启用远程绑定；不记录密钥或密码。
- 关停日志：`shutdown requested remote=<ip:port>`（仅后台模式，remote 非秘密）→ `shutting down reason="control endpoint"` 或 `reason=signal`。

### 4.7 并发、超时与限制

- HTTP：`ReadHeaderTimeout=5s`、`ReadTimeout=15s`、`WriteTimeout=30s`、`IdleTimeout=60s`。
- 请求体：`io.LimitReader`——笔记 128 KiB（正文 2000 字符 + 密文 64 KiB + 元数据），分类与主密码 4 KiB。
- 数据库：单连接（`SetMaxOpenConns(1)`），短事务；无后台任务。
- AI 优化：每次调用 `context.WithTimeout` 60 秒，`internal/ai` 另有 60 秒的 HTTP 客户端超时与 1 MiB 响应上限；请求体上限 64 KiB。
- 口令防护发生在客户端：PBKDF2 迭代 150000，界面要求 12 字符以上长口令。服务端限速无法防御离线读取数据库的攻击者，应用层不做账户锁定。

---

## 5. 数据层

### 5.1 Schema

结构由 `sql/` 下的编号迁移脚本累积而成，当前 schema 版本 `user_version = 7`。以下是迁移全部执行完之后的最终形态：

```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA secure_delete = ON;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;

-- 0001_init.sql
CREATE TABLE IF NOT EXISTS notes (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL,
    content      TEXT,
    is_encrypted INTEGER NOT NULL DEFAULT 0 CHECK (is_encrypted IN (0, 1)),
    cipher       BLOB,
    kdf_salt     BLOB,
    kdf_params   TEXT,
    hints        TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    CHECK (
        (is_encrypted = 0
            AND content IS NOT NULL
            AND cipher IS NULL
            AND kdf_salt IS NULL
            AND kdf_params IS NULL
            AND hints IS NULL)
        OR
        (is_encrypted = 1
            AND content IS NULL
            AND cipher IS NOT NULL
            AND kdf_salt IS NOT NULL
            AND kdf_params IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_notes_updated_at
    ON notes(updated_at DESC);

-- 0006_note_soft_delete.sql：软删除列 + 两个部分索引（活动列表 / 回收站列表各走各的）
ALTER TABLE notes ADD COLUMN deleted_at TEXT;
CREATE INDEX IF NOT EXISTS idx_notes_active
    ON notes(updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_notes_deleted_at
    ON notes(deleted_at DESC) WHERE deleted_at IS NOT NULL;

-- 0002_activity.sql：首页活动频率与高频查询词的数据源（0005 重建了 kind 枚举）
CREATE TABLE IF NOT EXISTS activities (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind       TEXT NOT NULL CHECK (kind IN ('search')),
    query      TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_activities_created_at ON activities(created_at);
CREATE INDEX IF NOT EXISTS idx_activities_query     ON activities(query);

-- 0003_note_fields.sql：逐列 ALTER，存量行按默认值回填
ALTER TABLE notes ADD COLUMN kind TEXT NOT NULL DEFAULT 'note' CHECK (kind <> '');
ALTER TABLE notes ADD COLUMN remark TEXT NOT NULL DEFAULT '';
ALTER TABLE notes ADD COLUMN extra TEXT CHECK (extra IS NULL OR json_valid(extra));
ALTER TABLE notes ADD COLUMN rev INTEGER NOT NULL DEFAULT 1 CHECK (rev > 0);
ALTER TABLE notes ADD COLUMN last_visited_at TEXT;

-- 0004_note_app.sql：分类表、全局主密码表、笔记分类外键
CREATE TABLE IF NOT EXISTS categories (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    created_at TEXT NOT NULL
);
INSERT INTO categories (id, name, created_at) VALUES (1, '未分类', '2000-01-01T00:00:00Z');

CREATE TABLE IF NOT EXISTS master_password (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    salt       BLOB NOT NULL,
    question   TEXT NOT NULL,
    created_at TEXT NOT NULL
);

ALTER TABLE notes ADD COLUMN category_id INTEGER REFERENCES categories(id);

-- 0005_activity_events.sql：SQLite 不能就地改 CHECK，只能新建 → 拷数据 → 删旧 → 改名。
-- 重建后 kind 覆盖查询、笔记的增删改查与分类维护（note_restore/note_purge 预留给回收站）。
-- query 列对非搜索事件写空串，保持 NOT NULL，不引入 NULL 三值语义。
CREATE TABLE activities_v5 (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind       TEXT NOT NULL CHECK (kind IN (
        'search', 'note_view', 'note_create', 'note_update', 'note_delete',
        'note_restore', 'note_purge',
        'category_create', 'category_rename', 'category_delete'
    )),
    query      TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
INSERT INTO activities_v5 (id, kind, query, created_at)
SELECT id, kind, query, created_at FROM activities;
DROP TABLE activities;
ALTER TABLE activities_v5 RENAME TO activities;
CREATE INDEX IF NOT EXISTS idx_activities_created_at ON activities(created_at);
CREATE INDEX IF NOT EXISTS idx_activities_kind       ON activities(kind);
CREATE INDEX IF NOT EXISTS idx_activities_query      ON activities(query);

-- 0007_quick_category_and_unique_title.sql：第二内置分类「速记」+ 同分类标题唯一
INSERT INTO categories (id, name, created_at) VALUES (2, '速记', '2000-01-01T00:00:00Z');
-- 存量重名先改名（ROW_NUMBER 按「活着的优先 → updated_at 最新 → id 最大」保留一条），再建索引；
-- 唯一索引刻意不加 deleted_at IS NULL：回收站里的那条也占着名字，恢复时不会撞名。
CREATE UNIQUE INDEX IF NOT EXISTS idx_notes_title_category
    ON notes(title COLLATE NOCASE, category_id) WHERE category_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_notes_title_uncategorized
    ON notes(title COLLATE NOCASE) WHERE category_id IS NULL;
```

字段约定：

- `notes.id`：UUID v4 字符串，由客户端生成（加密笔记的 AAD 绑定最终 id，必须在浏览器加密前确定），服务端在缺省时自行生成。
- `notes.title`：明文，用于列表与搜索。
- `notes.content`：明文正文（原始 Markdown）；加密后必须为 `NULL`。
- `notes.cipher`：`nonce(12) || ciphertext || tag(16)` 的 BLOB，对服务端是不透明字节。
- `notes.kdf_salt`：为通过 0001 的 `CHECK` 而写入的 `master_password.salt` 冗余副本；解密只以 `master_password.salt` 为准，两者不一致视为数据损坏。
- `notes.kdf_params`：JSON，见 6.1。
- `notes.hints`：0001 遗留列，v1.3 起不再使用（提示改为全局助记问题），保持 `NULL`。
- `notes.kind` / `notes.remark` / `notes.rev`：0003 引入。创建写 `'note'` 与空备注，每次更新 `rev + 1`；`kind`、`remark` 目前没有编辑入口，原值透传。
- `notes.extra` / `notes.last_visited_at`：0003 预留列，应用当前只读 schema、不写入，保持 `NULL`。
- `notes.category_id`：所属分类；`NULL` 与 `id = 1`「未分类」在展示与筛选上等价（见 5.4）。
- `categories.id = 2`「速记」：0007 引入的第二个内置固定分类，与「未分类」同受 `store.IsReservedCategory` 守卫（不可改名、不可删除）；`Category.Reserved` 是 API 响应里的 `reserved` 字段，也是前端判断「给不给操作按钮」的唯一依据。
- `notes.title` 的唯一性：0007 起同一分类内标题唯一（`COLLATE NOCASE`，回收站里的行也参与），因此按 `(分类, 标题)` 可以定位到唯一一条；查询必须显式写 `title = ? COLLATE NOCASE`（列本身没声明 COLLATE，只有索引是 NOCASE）。
- `notes.deleted_at`：0006 引入的软删除标记；`NULL` = 正常，非空 = 在回收站里（值就是移入时刻，UTC 秒级）。列表、搜索、分类计数、统计都只看 `deleted_at IS NULL`。
- 时间：UTC，固定格式 `2006-01-02T15:04:05Z`（秒级），便于字符串排序；相同时间以 `id DESC` 兜底。
- `master_password` 是单行表（`CHECK (id = 1)`），只存派生参数，永不存口令本身；`question` 为助记问题原文，只用于展示提示。
- `kdf_params` 非法时解密失败，属预期行为；不存在需要解析失败的 JSON 列。

### 5.2 启动检测、连接与 PRAGMA

启动时按固定顺序处理数据库；任一步失败都终止启动并保留原文件，不自动删除或重建：

1. 确认数据目录存在，不存在则创建（POSIX `0700`）。
2. 记录数据库文件是否存在；打开连接并设置连接数与 PRAGMA（如下）。文件不存在时由 SQLite 创建空库。
3. 检测数据库状态：
   - 本次启动新建了空库 → 执行全部迁移，记录 `database initialized`；
   - 已有文件 → 执行 `PRAGMA quick_check`，失败则拒绝启动；
   - `user_version` 高于当前程序支持的版本 → 拒绝启动，避免旧程序写坏新结构。
4. 执行 `user_version` 之后待应用的迁移（见 5.3），成功后记录 schema 版本。
5. 以上步骤完成后才启动 HTTP 服务，页面可用时数据库已就绪。

连接与 PRAGMA：

- DSN 示例：
  `file:data/notes.db?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=secure_delete(ON)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)`
- `db.SetMaxOpenConns(1)`、`db.SetMaxIdleConns(1)`。
- 单连接让 PRAGMA、事务和关闭顺序确定；代价是查询串行化，本项目可接受。
- 数据库路径指向目录、权限不足或文件不是有效 SQLite 库时，输出明确错误并以非零码退出。
- 数据库文件权限在 POSIX 上设为 `0600`，失败时记录告警但不阻止启动。

### 5.3 迁移

- 表结构定义集中在仓库根目录 `sql/`：
  - `0001_init.sql`：v1 的 `notes` 表与索引，内容与 5.1 一致；
  - `0005_activity_events.sql`：`activities` 重建（新建 → 拷数据 → 删旧 → 改名），把 `kind` 的 CHECK 从 `('search')` 扩到查询 + 笔记/分类读写，并补回三条索引；
  - `0006_note_soft_delete.sql`：`ALTER TABLE notes ADD COLUMN deleted_at TEXT`（存量行保持 `NULL`，升级后全是「未删除」），并建两个部分索引 `idx_notes_active`（`deleted_at IS NULL`，按 `updated_at DESC`）与 `idx_notes_deleted_at`（按 `deleted_at DESC`），让默认列表与回收站列表各走一条窄索引；
  - `0007_quick_category_and_unique_title.sql`：插入 `categories(2, '速记')`；用 `ROW_NUMBER()` 把存量同分类重名改成 ` (2)`、` (3)`…（冲突时退化为 ` (n · <id 前 8 位>)`，超 200 码点截断）；再建两条部分唯一索引 `idx_notes_title_category`（`category_id IS NOT NULL`）与 `idx_notes_title_uncategorized`（`category_id IS NULL`），键都是 `title COLLATE NOCASE`，且**不**排除回收站行；
  - `embed.go`：用 `//go:embed *.sql` 暴露嵌入文件系统，供 `internal/store` 读取。
- 文件名前缀为版本号；启动时按版本升序执行，已执行版本由 `PRAGMA user_version` 记录。
- 每个版本的迁移在单个事务内执行；任一语句失败则回滚并拒绝启动，成功后更新 `user_version`。
- 新建数据库执行全部迁移；已有数据库只执行 `user_version` 之后的版本。
- 迁移只向前；不自动删除列。唯一例外是 SQLite 无法就地修改 CHECK 时重建表（0005），重建必须显式拷数据、重建全部索引。
- 升级前建议用户备份数据库；不自动执行 `VACUUM`，避免大库启动耗时。

### 5.4 查询与搜索

`Store.ListNotes(ctx, q string, categoryID int64, page int)` 用同一组 `WHERE` 片段先 `COUNT(*)` 再取页，实际 SQL 形状：

```sql
-- 计数
SELECT COUNT(*) FROM notes n
 WHERE 1 = 1
   AND (n.title LIKE ? ESCAPE '\' OR (n.is_encrypted = 0 AND n.content LIKE ? ESCAPE '\'))
   AND (n.category_id = ? OR n.category_id IS NULL);   -- 仅筛选默认分类时

-- 取页（:rank 为标题命中排序，只在 q 非空时出现）
SELECT n.id, n.title, n.content, n.is_encrypted, n.category_id, c.name, n.updated_at
  FROM notes n LEFT JOIN categories c ON c.id = n.category_id
 WHERE <同上>
 ORDER BY CASE WHEN n.title LIKE ? ESCAPE '\' THEN 0 ELSE 1 END,
          n.updated_at DESC, n.id DESC
 LIMIT 20 OFFSET ?;
```

- 转义顺序：`\` → `\\`，`%` → `\%`，`_` → `\_`，全部配合 `ESCAPE '\'`。
- `q` 去首尾空白；为空时返回最近更新列表。web 层限制 `q` 不超过 200 个字符，超限返回字段级校验错误，前端搜索框同步 `maxlength="200"`。
- `page` 最小为 1；页大小固定 20（`notesPerPage`）；总数用同条件 `COUNT(*)`。
- `deleted=1` 走回收站：只看 `deleted_at IS NOT NULL`，按 `deleted_at DESC` 排序，标题命中排序（`title == q` 优先）只在非回收站视图生效——回收站是按「何时扔进来的」看的列表，不是搜索结果。
- `categoryID` 为 0 表示不按分类过滤；等于 `1`（默认分类）时匹配 `category_id = 1 OR category_id IS NULL`——未分类的笔记在库里存的是 `NULL`，`ListCategories` 的笔记数用同一条规则计入默认分类。
- 摘要：只取明文笔记的 `content`，在 Go 侧按码点截断到 140（`truncateRunes`），保留换行；加密笔记的列表项没有 `content` 字段（`omitempty`）。
- SQLite `LIKE` 对 ASCII 大小写不敏感，对中文为精确子串；不做分词、拼音和纠错。
- 每次非空搜索写一条 `activities(kind='search')`，供首页活动频率与高频词统计；查询词本身只进数据库，不进日志。
- 查询词通过 URL 参数传递，会进入浏览器历史。
- 活动事件（`activities`）：埋点全部下沉在 `internal/store` 的读写方法里，接口层不再自己记事件。写操作在自己那个事务里顺带插一行（`recordActivityTx`），创建/更新/软删除/恢复/彻底删除失败就不会留下事件；`note_restore`/`note_purge` 与 `note_delete` 分开计，前端把 `note_purge` 折进「删除笔记」柱、`note_restore` 单列；「查看」由 `store.ViewNote` 记录——它是用户读取的专用入口（Web 详情页与 CLI 的 `note show` / `note list` 预览都走它），`GetNote` 保持内部读取不埋点（`CreateNote`/`UpdateNote`/`SoftDeleteNote` 成功后都要回读，埋在 `GetNote` 里会把一次写操作重复计成查看），回收站里的笔记不算「使用」（`deleted_at` 非空时不记），埋点失败只 `slog.Warn` 不影响读取返回。事件只落 `kind`、`created_at` 与（仅搜索时的）`query`，不记录标题、正文、分类名。CLI 的 `note add` 也因此自动计入「新建笔记」（它本来就走 `CreateNote`）。
- 数据量增长后评估 FTS5 `trigram`，仅索引明文笔记；当前不引入。
- 改密迁移用的两条查询在 `note.go`：`ListEncryptedCiphers` 只选 `id / cipher / kdf_params`，按 `created_at, id` 稳定排序（迁移过程中新增或删除笔记不会让分页错位）；`UpdateNoteCipher` 只写 `cipher / kdf_params / kdf_salt`，先在事务外做只读探针区分「不存在」与「未加密」（`SetMaxOpenConns(1)` 下事务内再开第二条查询会死锁，全局盐也必须事务外取）。语义与取舍见 6.9。

### 5.5 备份、删除与残留

- 备份对象：`notes.db`、`notes.db-wal`、`notes.db-shm`；推荐停止应用或使用 `sqlite3 notes.db ".backup 'backup.db'"`。
- 删除分两步：默认 `DELETE /api/notes/{id}` 只写 `deleted_at`（回收站），`DELETE /api/notes/{id}?purge=1` 才真正 `DELETE` 行。
- 软删除不是内容修订：不动 `updated_at`、不 `rev + 1`；恢复同理（清空 `deleted_at`），所以笔记不会因为删了又恢复就跳到列表最前。
- 恢复保留原 `category_id`。分类删除沿用既有规则：只允许删空分类——「空」指未删除笔记数为 0，因此只剩回收站笔记的分类可以删掉，那条笔记的 `category_id` 被置 `NULL`（外键不认软删除），恢复后落在「未分类」。
- 已删除的笔记 `GetNote` 仍读得到（详情页要能显示「恢复/彻底删除」），但 `UpdateNote` 一律当作 404，编辑入口不会误写一条回收站笔记。
- 硬删除只在 `PurgeNote` 里发生；`not_in_trash` 与 `not_found` 分开映射（409 / 404），前端才能给出可读提示。
- 全局 `secure_delete=ON`；`PurgeNote` 提交后立即执行 `PRAGMA wal_checkpoint(TRUNCATE)`（必须在事务外，`wal_checkpoint` 在事务里无效），`Store.Close()` 再执行一次。软删除不清页：密文/正文明文仍在库里，这是回收站能恢复的前提。
- 明文可能留在 WAL 的窗口：从一次写入（或明文转加密、编辑重加密）到下一次 checkpoint 之间。应用只在删除与退出时主动压缩，接受这个窗口；退出后 WAL 已截断。
- 改密迁移同样只在 WAL 里留下**旧密文**（对它没有口令，也不打算解），明文一侧不产生任何新残留：新密文由浏览器用旧密钥解密后重新加密，明文与两把派生密钥都只在页面内存。
- `VACUUM` 作为可选维护操作；不自动执行，避免大库启动耗时。
- 边界：旧备份、文件系统快照、SSD 磨损均衡层可能保留旧明文；应用只保证运行时数据库逻辑删除与 WAL 清理。
- 已验证：哨兵字符串扫描 `.db` 与 `.db-wal`，加密笔记正文不落盘，明文笔记正文照常可见。

---

## 6. 加密设计

加解密全部在浏览器里完成（`web/src/lib/crypto.ts`），服务端没有任何密码学代码：它只校验结构、搬运 `cipher` 字节。密文是自描述的，因此未来的 CLI、其它客户端只要拿到 `cipher`、`kdf_params`、`master_password.salt` 和口令本身，就能独立解密。

### 6.1 参数

| 项目           | 值                                                                          |
| -------------- | --------------------------------------------------------------------------- |
| KDF            | PBKDF2-HMAC-SHA256（WebCrypto `deriveKey`）                                 |
| 迭代次数       | 150000                                                                      |
| 派生密钥长度   | 32 字节（AES-256）                                                          |
| 盐             | 16 字节随机值，全局一份，设置主密码时由浏览器 `crypto.getRandomValues` 生成 |
| AEAD           | AES-256-GCM，12 字节随机 nonce，`tagLength=128`                             |
| AAD            | `note:v1:` + 笔记 UUID                                                      |
| 密文容器       | `nonce \|\| ciphertext \|\| tag`，落库为 BLOB，只在传输时 base64            |
| 密码规范化     | `String.prototype.normalize('NFC')`                                         |
| 派生密钥可导出 | 否（`exportable: false`）                                                   |

`kdf_params` JSON：

```json
{ "algo": "pbkdf2", "prf": "HMAC-SHA256", "iter": 150000, "keylen": 32 }
```

浏览器在派生前校验 `iter ∈ [100000, 1000000]` 且 `keylen == 32`，越界直接抛 `DecryptError`；服务端在写入时校验 `algo == "pbkdf2"`、同一 `iter` 区间、`keylen == 32` 与 JSON 可解析，`prf` 记录但不参与判断。目的是拒绝把明显不合法的参数写成永久损坏的数据，而不是在服务端复现解密。

### 6.2 数据格式

```text
master_password.salt = 16 字节随机值（全局唯一一行）
kdf_params           = 上述 JSON
cipher               = nonce[12] || AES-256-GCM(plaintext, aad)
aad                  = UTF-8("note:v1:" + noteID)
```

- `noteID` 为 UUID 字符串；id 参与 AAD，防止密文跨笔记移植。加密笔记的 id 由浏览器在加密前生成并通过请求体的 `id` 字段提交，服务端按 `noteIDPattern` 校验后原样使用。
- 每次加密/重加密生成新的 12 字节随机 nonce；盐在设置主密码时生成一次，之后所有笔记共用（PBKDF2 的盐是公开参数，不反推口令）。
- `notes.kdf_salt` 是 `master_password.salt` 的冗余副本，只为满足 0001 的 `CHECK`；解密使用 `master_password.salt`。

### 6.3 密钥派生

```text
normalized = password.normalize('NFC')
key = PBKDF2-HMAC-SHA256(normalized, master_salt, iter=150000, keylen=32) → AES-256-GCM key
```

- 口令只在派生那一刻存在于浏览器内存，不写入任何浏览器存储，不进网络请求体。
- 不保存口令、口令哈希或独立校验值；口令是否正确由 GCM 认证标签判断——没有"试密码"的接口，只有"解不开"。
- 派生用 WebCrypto 异步 API，不阻塞渲染线程；150000 次 SHA-256 在桌面浏览器上是百毫秒量级，无需并发限流（服务端不参与，也就没有需要保护的 KDF 资源）。

### 6.4 主密码与加密笔记的流程

#### 配置主密码（仅首次）

1. 浏览器生成 16 字节盐，`POST /api/master-password` 提交 `salt_b64` 与助记问题——**不提交口令**。
2. 服务端以单行 `INSERT` 写入；已存在则返回 409 `master_password_exists`。
3. 口令从此只存在于用户记忆：无法找回、无法重置、没有后台通道。忘记它，加密笔记永久打不开。修改它（6.9）要求先给出旧口令，因此「忘记」和「想换」是两件事。

#### 新建加密笔记

1. 编辑器已有会话密钥（页面内存里的 `CryptoKey`）；没有则就地输入口令，或先去 `/master-password?next=/notes/new` 派生。这条笔记还没有密文可以当场验证口令时，必须两次输入一致才继续（口令记错派生的密钥会永久锁死这条笔记，且当时毫无症状）。
2. 浏览器生成笔记 UUID，用该 id 作为 AAD 加密正文，得到 `cipher_b64` 与 `kdf_params`。
3. `POST /api/notes` 提交 `id`、`title`、`category_id`、`cipher_b64`、`kdf_params`（不带 `content`）。
4. 服务端校验后写入 `content=NULL`、`is_encrypted=1`、`kdf_salt=master.salt`，返回 201。

明文正文从未写入数据库，也从未以明文形式离开浏览器。

#### 明文笔记转为加密

同一条 PUT：浏览器把新正文加密后提交 `cipher_b64`，服务端在 `UPDATE` 里把 `content` 置 `NULL`。旧明文行只存在于 WAL，直到下一次 checkpoint（见 5.5）。

### 6.5 解密流程

1. `GET /api/notes/{id}` 取回 `cipher_b64`、`kdf_params`、`category_id` 等元数据。
2. 视图发现 `encrypted` 且当前无会话密钥，渲染口令输入框。
3. 浏览器用 `master_password.salt` 与 `kdf_params` 派生密钥，`gcm.Open` 时 AAD 绑定本条笔记 id。
4. 成功：明文渲染为 Markdown；密钥留在页面内存，同一次会话内再开其它加密笔记不必再输口令。
5. 失败：抛 `DecryptError('密码错误或数据损坏')`，界面只显示这一句。

口令错误、密文被篡改、base64 非法、nonce 截断、AAD 不匹配、参数越界——全部归并为同一个用户可见错误，避免任何侧信道式区分。

### 6.6 编辑重加密

- 编辑器要求当前页面持有会话密钥，否则先跳解锁。
- 保存时用同一把密钥、新的 12 字节 nonce 重新加密，`PUT /api/notes/{id}` 只带 `cipher_b64`；常规编辑不改盐也不改口令（换口令见 6.9，那条路径才会把 `kdf_params` 刷成当前应用参数）。
- 加密笔记不可逆转回明文：PUT 提交 `content` 而库里已是 `is_encrypted=1` 时返回 409 `encrypted_immutable`。
- 明文只作为 JS 字符串存在于编辑器状态与草稿里；离开编辑器即清草稿，无法保证 GC 已擦除字节，文档不夸大这一点。

### 6.7 错误与内存

- `crypto.ts` 里 `DecryptError` 是唯一的解密失败类型；前端不做二次翻译，也不打印明文。
- 服务端只有结构校验错误（`validation_failed`）与 `master_password_missing`，没有解密失败这一类。
- 密钥是 `exportable: false` 的 `CryptoKey` 对象，不能被序列化、不能被 `window` 上的脚本读取；`lockSession()` 置空引用并清内存草稿。
- 不写日志：口令、明文、密文、派生密钥都不进 access log 或错误日志（见 4.6）。

### 6.8 明文残留

- 加密笔记的明文从不落库；`notes.content` 为 `NULL`。
- `secure_delete=ON` 让 SQLite 覆盖删除页；删除与退出时 `wal_checkpoint(TRUNCATE)`。
- 口令、派生密钥不写 `localStorage`/`sessionStorage`/`IndexedDB`；会话密钥与未保存草稿只在页面内存。
- 旧备份、快照、SSD 层不在应用控制范围；文档提示用户自行保护。
- 验收时对主库与 WAL 执行哨兵标记检索：加密正文不可见，明文正文照常可见。

### 6.9 修改主密码与浏览器端批量重加密

换口令的动作全在浏览器：`web/src/lib/reencrypt.ts` 是纯函数 + 依赖注入（`decrypt`、`encrypt`、`putCipher` 都可替换，因此可在 Vitest 里穷尽失败分支），`views/InfoView.svelte` 只做状态编排。服务端仍然零密码学，只提供两个搬运字节的端点。

流程：

1. `GET /api/master-password` 取全局盐 → 用旧口令派生密钥。
2. `GET /api/notes/encrypted` 取全部加密笔记的 `{id, cipher_b64, kdf_params}`（不含标题与正文），避免逐条 `GET /api/notes/{id}` 的 N+1。
3. `verifyOldPassword`：按清单顺序逐条试解，**第一条能解开的即通过**。不能只试第一条——库里可能混着更早一次改密留下的笔记，先撞上那条就会把正确的旧口令误判成错误。全部试完仍无一可解才报「主密码不正确或数据已损坏」，此时不写入会话密钥、不写任何密文。清单为空则跳过验证（没有可验证的东西），退化为「新口令两次一致」。
4. 用新口令派生密钥 → `planMigration` 先用**新**密钥逐条试解，能解开的判为 `already_done`，其余为待办。这一步让整条流程天然幂等。
5. `runMigration` 逐条 `decrypt(旧) → encrypt(新) → PUT /api/notes/{id}/cipher`，带进度回调；单条失败记录后继续，不中断。落定归入五类：`done` / `already_done` / `skipped_gone`（PUT 返回 409 `not_encrypted` 或 404，迁移途中笔记被删）/ `failed_old_key` / `failed_network`。汇总为「成功 N · 跳过 M · 失败 K」，`remaining = failed_old_key + failed_network`。
6. `sessionKey.set(新密钥)` 之后**先把迁移报告落地**（`report = result`、`remaining = result.remaining`），再跑两件附带的事：改助记问题、重读清单用 `countRemaining` 复核还剩几条解不开。这两步各自 `try/catch`，失败只追加一句提示，绝不把已经成功的改密报成失败——否则用户重试时会拿着真旧口令撞 `verifyOldPassword`，被界面告知「主密码不正确或加密数据已损坏」，而那条口令其实已经被新口令取代了。`countRemaining` 在复核请求自身失败（非 `DecryptError`）时退回迁移报告的计数。
7. `remaining === 0` 提示成功并收起表单；否则界面出现迁移状态卡并给「继续迁移」。续跑该填哪个口令由 `needsPreviousPassword(report)` 判定：报告里有 `failed_old_key` 就说明这些条目仍受**上一代**口令保护，按钮写成「继续迁移（请输入上一个主密码）」且旧密码栏留空——预填刚设的新密码等于让用户再撞一次同样的失败；只有 `failed_network` 时新密码此刻就是旧密码，才预填它。

设计取舍：

- **独立端点而非 `PUT /api/notes/{id}` 加标志**：`store.UpdateNote` 无条件重写 `title`（`normalizeTitle` 要求非空）与 `updated_at`，加标志等于在同一校验函数里塞第二套语义；`UpdateNoteCipher` 只更新 `cipher / kdf_params / kdf_salt`。
- **不动 `updated_at` 与 `rev`**：改一次口令不该让全部笔记在「按更新时间倒序」的列表里排到最前，那是对用户可见的假修订。
- **不换全局盐**：`master_password.salt` 保持首次设置的值。换盐会让尚未迁移的笔记当场不可解（`notes.kdf_salt` 只是它的冗余副本），盐不变则该列无需迁移。重加密把每条笔记的 `kdf_params` 统一刷成当前应用参数——这只是**归一化**，不是提升：改密的旧密钥由调用方按当前 `KDF_PARAMS` 派生一次，从不读笔记各自的 `kdf_params`（`reencrypt.ts` 的 `EncryptedItem.kdf_params` 只回写不参与派生）。因此**上调 `KDF_PARAMS.iter` 会让改造前的笔记在改密时全部判为 `failed_old_key`**，即使用户输入的口令完全正确（单条笔记的解锁不受影响，那条路径按笔记自身参数派生，见 6.6）。真要借改密提升 KDF 强度，得先让迁移按笔记参数分别派生旧密钥。
- **没有 `key_version` 列，也没有迁移状态机**：GCM 认证标签本身就是校验器，试解足以判定某条笔记当前由哪把口令保护；中断后重跑靠 `already_done` 分类自然续上，无需任何持久化状态。
- **中断语义的实测边界**：中途关页面后把新口令填进「旧口令」栏能续跑，因为已迁移的笔记会被判成 `already_done` 原样跳过。但若清单里同时存在「更早一次口令保护的笔记」与「本次已迁移的笔记」，一次续跑只能推进其中一类，另一类以 `failed_old_key` 如实列在迁移状态卡里且不被写坏（实测：该分支跑完后两条密文字节逐字不变）。界面文案「这条笔记不是用刚才那个旧密码加密的」就是为此而写。
- 口令与新派生密钥只活在函数局部变量：不进 toast、日志、`localStorage`/`sessionStorage`/IndexedDB；`PUT /cipher` 的请求体不会入日志（`accessLog` 从不记 body）；CSRF 令牌过期由 `api.ts` 的 403 重取重试兜住，逐条 PUT 天然继承。
- **已知边界：迁移途中不防并发编辑**。`PUT /cipher` 不带 `rev` 校验，它把「迁移开始时读到的那份明文」重新加密后写回。若用户在同一时刻的另一个页签编辑并保存了某条笔记，那条编辑会被这次写回覆盖掉。规格 §10 明确不做并发保护（单用户、单机、监听回环），迁移窗口以秒计且界面要求勾选「过程中请勿关闭页面」，本版接受；真要收紧，给 `UpdateNoteCipher` 加 `rev` 条件即可。

---

## 7. 会话密钥与 CSRF

### 7.1 会话密钥（只在页面内存）

```text
web/src/lib/session-key.ts
  sessionKey = $state<CryptoKey | null>(null)
  unlock(password, saltB64, params)  派生并持有密钥
  lockSession()                      置空密钥 + editorDraft.clear()
```

- 密钥是 `exportable: false` 的 `CryptoKey`，只存在于当前页签的内存：不写 Cookie，不写 `localStorage`/`sessionStorage`/`IndexedDB`，不进任何请求体。
- 「一次解锁、整会话通用」是页签级的：同一页签里解密一条笔记后，打开其它加密笔记不再询问口令；新开页签要重新输入。
- 没有服务端会话、没有 TTL、没有空闲计时器，也没有 `note_sess` 之类的 Cookie——服务端从未有能力判断「是否已解锁」，因此也不存在 401。
- 手动锁定：详情页与编辑器的「锁定」调用 `lockSession()`，置空密钥并清掉内存草稿；已经渲染到屏幕的明文随视图切换消失。
- 刷新或关闭页签即失效（内存被回收），这是唯一的自动过期路径。
- 未保存草稿同样只在内存：`lib/draft.ts` 用模块级单槽变量保存 `{noteId, title, content, categoryId, encrypt}`；`take(noteId)` 只把草稿交给**同一条笔记**的编辑器（把甲的正文灌进乙的编辑器，保存就是拿甲覆盖乙），取出即清空。离开编辑器（组件卸载）一律清草稿，只有跳 `/master-password?next=…` 那次例外——那是交棒，不是离开。不跨笔记并存，也不落盘。
- 忘记口令：`/notes/{id}/forgot` 只展示助记问题原文，没有「重置」入口——口令无法找回，忘记即加密笔记永久打不开。

### 7.2 CSRF

- `GET /api/csrf`：
  - 若 Cookie `csrf` 已存在且格式合法（32 字节 `base64.RawURLEncoding`），复用并把同一串放进 JSON 响应；
  - 否则生成新 Token，设置 `HttpOnly`、`SameSite=Strict`、`Path=/` 的会话级 Cookie，并在响应体里返回给页面脚本。
- 双提交校验（`csrfProtect`）：非 GET/HEAD/OPTIONS 且路径以 `/api` 开头时，`X-CSRF-Token` 头必须与 `csrf` Cookie 都非空、格式合法且常量时间相等，否则 403 `csrf_invalid`。
- 为什么够用：Token 在 `HttpOnly` Cookie 里，跨站脚本读不到；跨站请求拿不到响应体，也就取不到 JSON 里的那份；攻击者自己的页面既不能替本域写 Cookie，也不能用简单表单头带上 `X-CSRF-Token`。
- 服务端不校验 `Origin`、`Sec-Fetch-Site` 或请求 `Content-Type`；跨源读写由「不启用 CORS」挡下，Host 由 7.3 白名单挡下。
- 开发模式下的 Vite 代理用 `changeOrigin: true` 把 Host 换成 Go 的监听地址（7.3 的白名单靠它），并顺手把 `origin` 头重写为同一源；生产环境由 Go 同源提供页面与 API。
- 前端在模块变量里缓存 Token；写请求遇到 403 时重新获取并重试一次（`lib/api.ts`）。

### 7.3 Host 校验

- 默认允许：`127.0.0.1`、`localhost`、`[::1]` 及配置端口。
- 显式 `--allow-remote` 时，额外允许配置的监听主机。
- 规范化 Host（含默认端口）后比较，失败返回 400 `host_invalid`。
- Vite 开发代理使用 `changeOrigin: true`，确保转发后的 Host 为 Go 监听地址。

---

## 8. API 设计

### 8.1 约定

- 前缀：`/api`。
- 请求/响应：`application/json; charset=utf-8`。
- 写请求：JSON 请求体，CSRF 头；成功不返回 303，导航由前端完成。
- 所有 API 响应 `Cache-Control: no-store`（204 除外无响应体）。
- 不启用 CORS，不返回 `Access-Control-Allow-*`。
- 错误结构见 4.5。
- 时间字段为 UTC RFC3339 字符串，前端按本地时区展示。
- 列表页大小固定 20。

### 8.2 数据结构

**笔记（列表项，`store.NoteListItem`）**

```json
{
  "id": "5b0e1f2c-3a4b-4c5d-8e9f-0a1b2c3d4e5f",
  "title": "会议记录",
  "encrypted": false,
  "content": "第一行内容…（前 140 码点）",
  "category_id": 2,
  "category_name": "work",
  "updated_at": "2026-10-26T02:30:00Z"
}
```

- 加密笔记没有 `content` 键（`omitempty`），前端显示「密文内容不在列表展示」。
- 未分类笔记 `category_id` 为 `null`、`category_name` 为空。

**笔记（全量，`store.Note`）**

```json
{
  "id": "5b0e…",
  "title": "私密记录",
  "encrypted": true,
  "category_id": null,
  "kind": "note",
  "remark": "",
  "rev": 1,
  "content": null,
  "cipher_b64": "base64(nonce||ct||tag)",
  "kdf_params": "{\"algo\":\"pbkdf2\",\"prf\":\"HMAC-SHA256\",\"iter\":150000,\"keylen\":32}",
  "created_at": "2026-10-26T02:00:00Z",
  "updated_at": "2026-10-26T02:30:00Z"
}
```

- 明文笔记：`encrypted=false`、`content` 为原始 Markdown、无 `cipher_b64`/`kdf_params`。
- 加密笔记：`content` 恒为 `null`，`cipher_b64` 对服务端是不透明字节；服务端没有 `unlocked` 概念，也没有解密端点。
- 正文、标题、分类名一律纯文本，接口不返回 HTML。

**分页（`store.NotePage`）**：`{"items":[…],"total":42,"page":1,"per_page":20}`——总数与页大小都由服务端给出，前端自己算 `ceil(total/per_page)`。

**分类（`store.Category`）**：`{"id":2,"name":"work","note_count":3,"reserved":false}`；`GET /api/categories` 返回裸数组，`note_count` 对默认分类计入 `category_id IS NULL` 的笔记；`reserved` 为 `true` 的是内置固定分类（「未分类」「速记」），前端据此隐藏改名/删除按钮。

**主密码配置**：`{"configured":true,"salt_b64":"…","question":"…"}`；未配置时 `configured=false` 且后两字段为空串。

**统计（`store.Stats`）**：`{"total_events":N,"total_searches":N,"active_days":N,"events_by_kind":[{"kind":"note_create","count":N}],"top_queries":[{"query":"…","count":N}],"days":[{"date":"2026-10-26","count":N}]}`。

- `total_events` / `active_days` / `days` / `events_by_kind` 覆盖全部事件；`total_searches` 与 `top_queries` 口径仍限 `kind='search'`（查询这件事单独可读）。
- `events_by_kind` 只返回出现过的 kind，按次数倒序；中文文案与「分类维护」合并规则在前端 `lib/stats.ts#groupEvents`，后端保持 kind 原样。CLI `note status` 用同一份 `store.GetStats`，分组与文案落在 `internal/cli/stats.go#GroupEvents`，两边口径必须一起改。
- 事件类型：`search`、`note_view`、`note_create`、`note_update`、`note_delete`、`category_create`、`category_rename`、`category_delete`（另有 `note_restore` / `note_purge` 已列入 CHECK，等回收站上线后启用）。

**设置**：`{"name":"张三","name_set":true}`。

### 8.3 端点

| 方法   | 路径                            | 请求                                        | 成功响应                                                                                                                            |
| ------ | ------------------------------- | ------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| GET    | `/api/csrf`                     | 无                                          | 200 `{"token":"…"}` + Set-Cookie                                                                                                    |
| GET    | `/api/settings`                 | 无                                          | 200 设置                                                                                                                            |
| PUT    | `/api/settings/name`            | `{"name":"…"}`                              | 200 设置                                                                                                                            |
| GET    | `/api/template`                 | 无                                          | 200 `{"content","path","source","default_content"}`；`source` 为 `file` 或 `default`（文件缺失/不可读时回退内置默认）               |
| PUT    | `/api/template`                 | `{"content":"…"}`                           | 200 同上（`source` 恒为 `file`）；超过 2000 码点 400 `validation_failed`                                                            |
| GET    | `/api/notes?q=&category=&page=` | 查询参数                                    | 200 分页                                                                                                                            |
| POST   | `/api/notes`                    | 笔记载荷                                    | 201 全量笔记                                                                                                                        |
| GET    | `/api/notes/{id}`               | 无                                          | 200 全量笔记                                                                                                                        |
| PUT    | `/api/notes/{id}`               | 笔记载荷                                    | 200 全量笔记                                                                                                                        |
| DELETE | `/api/notes/{id}`               | 无                                          | 204                                                                                                                                 |
| GET    | `/api/notes/encrypted`          | 无                                          | 200 `{"notes":[{"id","cipher_b64","kdf_params"}]}`，空清单为 `[]`；静态段优先于 `{id}` 通配                                         |
| PUT    | `/api/notes/{id}/cipher`        | `{"cipher_b64":"…","kdf_params":"…"}`       | 200 全量笔记；只换密文，`title/content/category_id/rev/updated_at` 逐字不变                                                         |
| GET    | `/api/categories`               | 无                                          | 200 分类数组                                                                                                                        |
| POST   | `/api/categories`               | `{"name":"…"}`                              | 201 分类                                                                                                                            |
| PUT    | `/api/categories/{id}`          | `{"name":"…"}`                              | 200 分类数组                                                                                                                        |
| DELETE | `/api/categories/{id}`          | 无                                          | 204                                                                                                                                 |
| GET    | `/api/master-password`          | 无                                          | 200 主密码配置                                                                                                                      |
| POST   | `/api/master-password`          | `{"salt_b64":"…","question":"…"}`           | 201 `{"configured":true,"question":"…"}`；仅首次配置                                                                                |
| PUT    | `/api/master-password/question` | `{"question":"…"}`                          | 200 主密码配置（盐不变）                                                                                                            |
| GET    | `/api/info`                     | 无                                          | 200 运行信息与构建版本（见 8.6）                                                                                                    |
| GET    | `/api/ai-config`                | 无                                          | 200 `{"base_url","models","prompt","default_prompt","prompt_variable","api_key_set","api_key_masked","path"}`（`models` 空为 `[]`） |
| PUT    | `/api/ai-config`                | `{"base_url","models","prompt","api_key"?}` | 200 同上；`api_key` 省略=沿用、空串=清除                                                                                            |
| POST   | `/api/ai/optimize`              | `{"content","model"?}`                      | 200 `{"content","model"}`（已剥围栏）                                                                                               |
| GET    | `/api/stats?days=`              | 查询参数（默认 370 天）                     | 200 统计（见 8.2；事件口径覆盖查询与笔记/分类读写）                                                                                 |

没有 `/encrypt`、`/decrypt`、`/unlock`、`/lock` 这类端点：加解密在浏览器里完成，锁定是前端清内存密钥的本地动作。也没有口令校验端点——改密时的旧口令由浏览器试解已有密文来判定（6.9）。

### 8.4 请求示例

**创建明文笔记**

```json
{
  "title": "购物清单",
  "content": "牛奶\n鸡蛋",
  "category_id": null
}
```

**创建加密笔记**（`cipher_b64` 由浏览器用会话密钥产出，`id` 由浏览器在加密前生成以绑定 AAD）

```json
{
  "id": "5b0e1f2c-3a4b-4c5d-8e9f-0a1b2c3d4e5f",
  "title": "私密记录",
  "cipher_b64": "…nonce||ct||tag 的 base64…",
  "kdf_params": "{\"algo\":\"pbkdf2\",\"prf\":\"HMAC-SHA256\",\"iter\":150000,\"keylen\":32}",
  "category_id": 2
}
```

载荷里 `content` 与 `cipher_b64` 必须恰好出现一个；`category_id` 省略或为 `null` 表示未分类。

**更新笔记**：与创建同构的 PUT，`rev` 自增、`updated_at` 刷新；对已加密笔记提交 `content` 会 409 `encrypted_immutable`。

**设置主密码**

```json
{
  "salt_b64": "base64(16 字节随机盐)",
  "question": "我第一次独自旅行去了哪座城市？"
}
```

口令不在请求里，也不在响应里。

**CSRF**

```http
GET /api/csrf
```

```json
{ "token": "base64url-token" }
```

### 8.5 校验与状态映射

笔记：`title` 去空白后 1–200 码点；`content` ≤2000 码点；`content` 与 `cipher_b64` 恰好二选一；`cipher_b64` 必须是 `base64.StdEncoding` 且解码后 29–65536 字节；`kdf_params` 见 6.1；`id` 非空时必须是 UUID 形态；`category_id` 必须指向存在的分类。搜索：`q` ≤200 码点，`category` 与 `page` 非数字按 0/1 处理。分类：`name` 去空白后 1–30 码点，大小写不敏感唯一。主密码：`salt_b64` 解码后恰好 16 字节，`question` 1–100 码点。姓名：1–32 码点且不含控制字符。

| 场景                     | 状态与 code                                                           |
| ------------------------ | --------------------------------------------------------------------- |
| 创建成功                 | 201                                                                   |
| 读取/更新/改名分类成功   | 200                                                                   |
| 删除笔记、删除分类成功   | 204                                                                   |
| 字段级校验失败           | 400 `validation_failed` + `error.fields`                              |
| 请求体不是 JSON          | 400 `body_invalid`                                                    |
| Host 不在白名单          | 400 `host_invalid`                                                    |
| CSRF 校验失败            | 403 `csrf_invalid`                                                    |
| 笔记/分类不存在          | 404 `not_found`                                                       |
| 加密笔记改回明文         | 409 `encrypted_immutable`                                             |
| 未设置主密码就存加密笔记 | 409 `master_password_missing`                                         |
| 重复设置主密码           | 409 `master_password_exists`（改密走信息页，不重复 POST）             |
| 对明文笔记替换密文       | 409 `not_encrypted`                                                   |
| 未设置主密码就改助记问题 | 409 `master_password_missing`                                         |
| 客户端 id 撞库           | 409 `id_conflict`                                                     |
| 默认分类改名/删除        | 409 `category_reserved`                                               |
| 同分类标题已存在         | 409 `title_conflict` + `fields.title`                                 |
| 同名笔记在回收站里       | 409 `title_in_trash` + `fields.title`                                 |
| 删除非空分类             | 409 `category_not_empty` + `fields.note_count`                        |
| 笔记请求体声明长度超限   | 413 `body_too_large`（分类/主密码/姓名超限表现为 400 `body_invalid`） |
| 其它                     | 500 `internal`                                                        |

分类名重复走 409 `category_exists` + `fields.name = 该分类名已存在`（与 `category_not_empty`、`id_conflict` 同族：名字被占是状态冲突，不是输入格式错误），前端仍按字段级反馈标红输入框。没有 401 家族：不存在服务端会话，解密失败只在浏览器里表现为一句 `密码错误或数据损坏`。

`PUT /api/notes/{id}/cipher` 的校验只剩「必须带密文与派生参数，其余不接受」——base64 与 29–65536 字节的密文长度语义与 `normalizeBody` 共用 `decodeCipher`，因此两条路径同值。`store.UpdateNoteCipher` 先在事务外做只读探针，把「笔记不存在」和「笔记未加密」明确区分成 `ErrNoteNotFound` / `ErrNotEncrypted`：`db.SetMaxOpenConns(1)` 下事务内再开第二条查询会死锁，全局盐也必须事务外取。

### 8.6 运行信息响应

`GET /api/info` 输出 18 个字段，全部来自标准库运行时与已加载配置，`Cache-Control: no-store`：

```json
{
  "version": "1.4.0",
  "git_hash": "805b6a1",
  "git_dirty": false,
  "build_date": "2026-10-27T12:45:05Z",
  "build_label": "1.4.0 (805b6a1, 2026-10-27T12:45:05Z)",
  "go_version": "go1.27.0",
  "goos": "windows",
  "goarch": "amd64",
  "hostname": "LAPTOP-XXXX",
  "cpu_count": 8,
  "mem_alloc_bytes": 808392,
  "mem_sys_bytes": 11321400,
  "started_at": "2026-10-27T12:50:00Z",
  "uptime_seconds": 20,
  "db_path": ".run/e2e-info.db",
  "schema_version": 4,
  "addr": "127.0.0.1:8099",
  "allow_remote": false
}
```

- `version / git_hash / git_dirty / build_date` 来自 `internal/build`：`just build` 用 `-ldflags -X` 注入，未注入时 `Describe()` 回落到 `debug.ReadBuildInfo()` 的 `vcs.revision / vcs.time / vcs.modified`，此时 `build_label` 是「1.4.0（开发构建）」。
- `started_at` 在 `newApplication` 里记 `time.Now().UTC()`，以 RFC3339 输出，UTC 时形如 `2006-01-02T15:04:05Z`，遵守时间戳约定。
- `schema_version` 由 `Store.SchemaVersion` 读 `PRAGMA user_version`。
- 响应里没有密文、盐、助记问题、口令或令牌；`hostname`、`db_path`、`addr` 是本机自用信息的展示，不构成对外泄露面（应用只监听回环，除非显式 `--allow-remote`）。
- `note info`（`internal/cli/info.go` + `cmd/note` 采集）与本响应同源同口径：构建信息来自 `internal/build`，运行时来自本进程，配置来自同一套 `config.Load`；额外展示数据库只读快照（大小、schema 版本、笔记/加密/分类计数，`ListNotes(ctx, "", 0, 1).Total`、`ListEncryptedCiphers`、`ListCategories`），数据库文件不存在时不开库不建文件。输出同样不含密文、盐、口令或令牌。

---

## 9. 前端设计

### 9.1 技术栈与脚本

- Vite + Svelte 5 + TypeScript；不使用 SvelteKit。
- 路由自写（History API），状态用 Svelte 5 runes，样式手写 CSS；不引入 UI 组件库、CSS 框架或状态管理库。
- TypeScript 开启 `strict`，`check` 同时跑 `svelte-check`（`tsconfig.app.json`）与 `tsc`（`tsconfig.node.json`）。
- `package.json` 实际脚本：

```json
{
  "scripts": {
    "dev": "vite",
    "build": "npm run check && vite build",
    "preview": "vite preview",
    "test": "vitest run",
    "check": "svelte-check --tsconfig ./tsconfig.app.json && tsc -p tsconfig.node.json"
  }
}
```

- 运行时依赖只有 `marked` 与 `dompurify`；Svelte/Vite/Vitest/`jsdom` 全在 devDependencies。测试环境按文件用 `// @vitest-environment jsdom` 声明，没有全局 jsdom 配置。

### 9.2 目录与模块

```text
web/src/
├── components/
│   ├── Layout.svelte          # 页头、主题切换、toast 容器
│   ├── CategoryTree.svelte    # 列表页右侧分类树：筛选 + 行内新增/改名/删除
│   ├── ActivityHeatmap.svelte # 首页活动频率热力图（查询 + 增删改查）
│   ├── Markdown.svelte        # 全仓唯一允许 {@html} 的组件
│   └── ThemeSwitch.svelte     # 单按钮循环 亮色 → 暗色 → 护眼
├── views/                     # 9 个视图，见 9.3 路由表
├── router/router.ts           # 自写 History 路由
├── lib/
│   ├── api.ts                 # fetch、CSRF、错误映射
│   ├── notes.ts               # 笔记/分类/主密码接口的类型与调用（含密文清单与替换）
│   ├── crypto.ts              # WebCrypto PBKDF2 + AES-256-GCM
│   ├── reencrypt.ts           # 改密迁移引擎（纯函数 + 依赖注入）
│   ├── info.ts                # /api/info → 界面可读行
│   ├── onboarding.ts          # decideStep(nameSet, masterConfigured)
│   ├── session-key.ts         # 页面内存里的会话密钥
│   ├── draft.ts               # 页面内存里的未保存草稿
│   ├── markdown.ts            # marked → DOMPurify
│   ├── clipboard.ts           # copyText：异步剪贴板 API + textarea/execCommand 兜底
│   ├── stats.ts / theme.ts / toast.ts / settings.ts
│   └── *.test.ts              # 同名 Vitest 用例
├── App.svelte                 # 路由分发 + 首屏载入/失败态
├── app.css                    # CSS 变量、三主题、reset、表单、按钮
└── main.ts
```

纯函数与管道（`crypto`、`markdown`、`draft`、`notes`、`stats`、`theme`、`toast`、`router`、`settings`、`reencrypt`、`info`、`onboarding`、`ai-config`）都有同名 Vitest 用例；`session-key.ts` 的用例并在 `crypto.test.ts` 里（它断言的是密钥与口令派生的同一套语义）。改密迁移引擎之所以必须是纯函数 + 依赖注入，是因为它的五类落定（6.9）要在没有浏览器和没有服务端的情况下穷尽；视图组件基本不写单测、靠端到端验证覆盖，例外是 `views/NoteDetailView.test.ts`——「复制原文按钮在有明文时出现、加密未解锁时不出现」属于安全相关的可见性，值得锁在用例里。

### 9.3 路由

| 路径                | 视图                                                                     |
| ------------------- | ------------------------------------------------------------------------ |
| `/`                 | 首页（统计、热力图、搜索入口、命名引导）                                 |
| `/notes`            | 列表/搜索 + 右侧分类树（筛选与分类增删改都在树内完成）                   |
| `/notes/new`        | 新建                                                                     |
| `/notes/:id`        | 详情（含解锁）                                                           |
| `/notes/:id/edit`   | 编辑                                                                     |
| `/notes/:id/forgot` | 忘记主密码                                                               |
| `/master-password`  | 首次设置主密码，支持 `?next=` 回跳；已配置时转为只读卡并指向信息页       |
| `/info`             | 用户信息页：运行信息、用户名、主密码与助记问题、迁移状态                 |
| `/categories`       | 旧地址，映射到 `list`（分类树已并入列表页，`syncUrl()` 纠正为 `/notes`） |
| 其它                | 前端 404 视图                                                            |

- `router/router.ts` 用 `writable<Route>` 暴露当前路由，`matchRoute(pathname)` 按上表顺序匹配正则（`edit`/`forgot` 排在 `detail` 之前，避免被贪婪匹配吃掉），参数经 `safeDecode` 容错解码。
- `navigate(path, replace?)` 调用 `pushState`/`replaceState` 并滚回顶部；`startRouter()`/`stopRouter()` 挂载与卸载 `popstate` 监听，前进/后退照常工作。
- 查询参数（`q`、`category`、`page`、`next`）由视图自己用 `URLSearchParams` 解析，列表用 `replaceState` 同步而不堆历史。
- `document.title` 由 `App.svelte` 按路由合成（如「全部笔记 · 张三的笔记」，未命名时为「笔记 · 欢迎」）；路由切换不移动焦点——键盘用户靠 Tab 顺序与可见焦点样式遍历，切换后焦点留在触发元素上。
- 内部链接写成真正的 `<a href>` 并在点击时 `preventDefault()` 后调用 `navigate()`：SPA 内切换，同时保留「新标签页打开」「右键复制链接」的原生能力。
- 编辑器有未保存内容时挂 `beforeunload` 并 `preventDefault()`，关标签页前由浏览器提示；内存草稿不会因此落盘。
- 未知路径显示前端 404；Go 只负责把非 API 路径回退到 `index.html`。

### 9.4 API 客户端

`lib/api.ts` 导出 `getJSON` / `putJSON` / `postJSON` / `deleteJSON`：

```text
sendJSON(method, path, payload)
  ├── 首次写请求前先 GET /api/csrf 拿 Token，缓存在模块变量
  ├── 带 Content-Type: application/json 与 X-CSRF-Token
  ├── 403 → 重新获取 Token 后重发一次
  └── 非 2xx → 解析 {"error":{code,message,fields}} 抛 ApiRequestError
```

- `ApiRequestError` 携带 `code`、`message`、`fields`，视图按字段名把 `fields` 渲染到对应输入框下方。
- 响应体不是 JSON 时退回「请求失败（状态码）」的通用文案，不抛解析异常。
- `deleteJSON` 单独处理 204 无正文（不能走 `parseResponse`），并取消响应体。
- 请求不传 `cache` 选项也不带 `AbortController`：GET 靠服务端的 `Cache-Control: no-store`，视图切换后过期的响应由「载入中」状态与重新拉取覆盖，不做取消。
- 没有 401 分支：解密失败是纯前端事件，见 6.5。
- 客户端不缓存笔记正文；正文只存在于当前视图状态与（编辑器的）内存草稿里。
- `lib/ai.ts` 用同一个 `postJSON` 调 `/api/ai/optimize`：`{content}` 或 `{content, model}`（空模型时不带该字段，让服务端用列表第一个）；`defaultModel(cfg)` 与 `hostOf(baseUrl)` 是纯函数，供按钮状态与「正文会发送到 …」的披露文案使用。优化结果只进视图状态，不写草稿、不写浏览器存储。

### 9.5 状态与数据流

- 跨视图状态只有三样：路由（`writable<Route>`）、toast 队列（`lib/toast.ts`）、姓名与主题（`lib/settings.ts`、`lib/theme.ts`）。其余全是视图局部 `$state`。
- `lib/settings.ts` 的 `SettingsState` 同时带 `nameSet` 与 `masterConfigured`：`loadSettings()` 用 `Promise.all` 并行读 `/api/settings` 与 `/api/master-password`，任一个失败整体进 `error` 态——引导判据不能建立在半套数据上。`saveName()` 只回写 `name/nameSet`，不重读主密码（引导页步骤 1 靠它本地推进）。
- 两个模块级单例活在页面内存：`session-key.ts` 的会话密钥与 `draft.ts` 的未保存草稿。浏览器持久存储只用于主题偏好（`localStorage`），密码、正文、派生密钥一律不落任何浏览器存储。
- 刷新或新开页签后密钥即失效，加密笔记重新要求口令——没有服务端会话可以续。
- 「锁定」是纯前端动作：`lockSession()` 置空密钥并清草稿，当前视图里已解密的正文同步清空；不发请求、不调 API。
- 列表、详情、统计每次进入都重新请求，不做客户端缓存；写操作成功后由视图主动刷新（如删除笔记后重取分类计数）。

### 9.6 页面行为

| 视图       | 行为                                                                                                                                                                                                                                                                                                                                                                                                                               |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 首页       | 拉取统计渲染热力图与「事件构成」（查询、查看与增删改都计入）；搜索框就地列出命中并计入一次查询                                                                                                                                                                                                                                                                                                                                     |
| 引导页     | 两步：步骤 1 只填称呼（`PUT /api/settings/name`，成功后本地推进、不重读设置）；步骤 2 收**必填**的初始主密码（口令 + 确认 + 助记问题 + 不可逆勾选，生成盐 → `POST` → `unlockKey`），没有「暂时跳过」。挂载时先 `GET /api/master-password`：已配置则变成「输入口令解锁」单输入框，绝不重复 POST                                                                                                                                     |
| 信息页     | 五张卡：运行信息（`GET /api/info` 摊成只读键值行，缺失显示「未知」）、用户名（`PUT /api/settings/name`）、**AI 配置**（`GET/PUT /api/ai-config`：地址 + 密钥 + 提示词，密钥框留空=不改动、点「清除密钥」才清空，保存后清空密钥输入框并只显示掩码，卡片里给出配置文件路径；加载失败只影响这张卡）、主密码（当前助记问题 + 内联改问题 + 改密表单）、迁移状态（`remaining > 0` 时列出未迁移条目与原因并给「继续迁移」）；全部文本插值 |
| 列表       | 解析 `q`/`category`/`page`，右侧分类树带各级笔记数（根节点「全部笔记」+「未分类」+ 各分类）；行内显示明文摘要或「密文内容不在列表展示」；删除有二次确认                                                                                                                                                                                                                                                                            |
| 详情       | 明文直接渲染 Markdown；标题旁有「复制原文」图标按钮（`lib/clipboard.ts#copyText` 写入库里的原始 Markdown，失败用 toast 提示手动选中；加密笔记解锁前不渲染该按钮）；加密且无密钥时显示口令框与助记问题入口；解密成功后可编辑、删除、锁定                                                                                                                                                                                            |     |
| 编辑器     | 标题、分类（含「未分类」）、正文与防抖预览；勾选加密时若无会话密钥先就地解锁或跳 `/master-password?next=…`，跳转前把内容存进内存草稿；离开时有未保存提醒；正文标签旁的「用 AI 优化格式」调 `POST /api/ai/optimize`，结果进独立面板（Markdown 渲染 + 码点计数），点「替换正文」才写回输入框，「放弃」只清面板；多模型时给临时下拉，旁边常驻目标主机，加密笔记提示「以明文发送」                                                     |
| 主密码     | 未配置：口令 + 确认 + 助记问题 + 不可逆勾选，成功后派生密钥并回跳 `next`；已配置：只读展示助记问题并给一个去 `/info` 的按钮，改密与改问题都在信息页，本页不再重复表单                                                                                                                                                                                                                                                              |
| 忘记主密码 | 展示助记问题与「无法找回」说明；唯一出路是确认删除这条加密笔记，不要求口令                                                                                                                                                                                                                                                                                                                                                         |
| 分类树     | 列表页右侧：根节点筛选 + 分类筛选；新增（树底输入框）、改名（行内，Enter 保存 / Esc 取消）、删除（二次确认），非空时把服务端返回的笔记数显示在对应行下                                                                                                                                                                                                                                                                             |
| 404        | 说明路径不存在，给返回列表链接                                                                                                                                                                                                                                                                                                                                                                                                     |

`App.svelte` 在最外层把关：设置载入中显示 `role="status"` 提示，载入失败给出「重新载入」按钮；引导完成的判据是纯函数 `decideStep(nameSet, masterConfigured)`（返回 0 才渲染 `Layout`，否则渲染引导页并按这两个布尔决定从哪一步开始），`document.title` 用同一判定，避免出现「还在引导却显示『XX的笔记』」。只按 `nameSet` 判断会漏掉「名字存好、主密码还没设就刷新」这个半态，用户会绕过必填进主界面。

### 9.7 前端安全约束

- `{@html}` 只出现在 `components/Markdown.svelte`，其唯一输入来自 `lib/markdown.ts` 的 `renderMarkdown()`——`marked` 解析（`gfm`，同步模式）后交 `DOMPurify.sanitize`，并用 `afterSanitizeAttributes` 钩子给所有 `<a>` 补 `noopener noreferrer`。其它地方一律 `{...}` 文本插值。
- 口令输入框：解锁用 `autocomplete="current-password"`，设置主密码用 `autocomplete="new-password"`；不预填 `value`，提交后清空内存状态。派生密钥 `exportable: false`，无法被脚本序列化。
- 不把口令、正文明文、助记问题答案写进 URL、日志、分析埋点或浏览器存储；未保存草稿只在内存。改密流程同样：旧口令、新口令、两个派生密钥只作局部变量，不进 toast 文案、不进 `MigrationRow.message`（用例断言整份迁移报告序列化后不含正文）。
- 旧口令是否正确只由浏览器试解 GCM 认证标签判定（6.9）。服务端没有口令校验端点，信息页的「改密」不构成任何找回通道。
- 不使用外部 CDN、字体、图片或脚本；CSP 为 `default-src 'self'`。
- 服务端响应带 `Cache-Control: no-store`，含正文的响应同样不缓存；前端不额外设置 `cache` 选项。
- AI 优化只在用户点击/按键时发生，浏览器永远拿不到 `api_key`：请求只带正文与可选模型，服务端代发。界面必须显示「正文会发送到 <host>」（加密笔记另注明「以明文发送」），结果先预览、确认后才替换。
- 破坏性操作（删除笔记、删除分类、忘记主密码后删笔记）都要 `window.confirm` 二次确认；主密码的不可逆勾选默认不勾，未勾选时提交按钮禁用。

### 9.8 样式与可访问性

- `app.css` 提供 reset、CSS 变量、三主题（亮色 / 暗色 / 护眼）、布局、表单、按钮与状态样式；组件用 Svelte scoped CSS。
- 移动优先，≥360px 可用；桌面最大宽度约 880px。
- **断点只有两档**（写在 `app.css` 末尾、所有基础规则之后，避免同优先级覆盖）：
  - `≤1024px`：编辑页 `.editor-grid` 与列表页 `.list-layout` 收成单列；预览区 `max-height: 50vh` + `overflow-y: auto` 自己滚动，分类树（DOM 在列表之后）用 `order: -1` 提到列表上方；分类操作按钮改为常显（`(hover: none)` 同样触发）。
  - `≤640px`：顶栏用 `order` 排成两行（品牌/用户名/主题在第一行，导航整行下移并横向滚动，链接 `min-height: 44px`）；`main` 的底部内边距叠加 `env(safe-area-inset-bottom)`；`.button`、`.cat-act`、`.note-row` 里的动作都 ≥44px；输入框字号统一 `1rem`（iOS 对 <16px 输入框会在聚焦时自动放大页面）；`.note-row` 变卡片（正文按钮与删除按钮各占一行）、首页搜索框与按钮各占一行、`.info-rows` 改为「标签一行、值一行」、解锁表单按钮独占一行。
  - 动态视口用 `min-height: 100dvh`，前一行保留 `100vh` 作老浏览器回落；`env(safe-area-inset-*)` 依赖 `index.html` 的 `viewport-fit=cover`。
  - 热力图保持 53 周语义：窄屏只把格子从 11px 缩到 10px（`--cell`），其余交给 `.heatmap` 的横向滚动（`overscroll-behavior-x: contain`），并在窄屏显示「左右滑动可查看全年」提示；容器带 `tabindex="0"` 让键盘也能左右翻，`role="img"` + `aria-label` 说明它是图表。
  - 长中文标题、长查询词、长路径与报错文案用 `overflow-wrap: anywhere` 就地换行，不撑出横向滚动。
- 语义化 HTML，`label` 关联输入，错误用 `role="alert"`，载入/通知用 `role="status"` 与 `aria-live="polite"`。
- 触控设备上不依赖 hover：分类行的 `.cat-actions` 在 `(hover: none)` 或 `≤1024px` 时常显（键盘可达性不变，按钮一直在 DOM 里）。
- 焦点不随路由强制迁移，但保留浏览器原生焦点顺序与可见焦点样式；确认框使用原生 `window.confirm`，焦点行为由浏览器管理。
- 不依赖颜色表达唯一状态（加密、选中、错误另有文字或图标）；主流程可纯键盘完成。

### 9.9 开发与生产

- 开发：Vite `5173`，`/api` 代理到 `127.0.0.1:8080`，`changeOrigin: true` 并重写 `origin` 头。
- 生产：Vite 构建到 `internal/web/dist`，Go 同源提供页面、`/assets/` 指纹资源与 API。
- 首次开发或拉取新代码后需先构建一次 `dist`，否则 Go 的 `//go:embed` 编译失败；之后前端改动由 Vite 开发服务器负责。

---

## 10. 构建与发布

### 10.1 Vite 与 Svelte 配置

`web/vite.config.ts`：

```ts
import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: "../internal/web/dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
        configure(proxy) {
          proxy.on("proxyReq", (proxyReq) => {
            proxyReq.setHeader("origin", "http://127.0.0.1:8080");
          });
        },
      },
    },
  },
});
```

- `changeOrigin` 让转发后的 Host 与 Go 监听地址一致，这是 7.3 Host 白名单通过的前提。
- 代理同时把 `origin` 头重写为 Go 的目标源，保持开发/生产一致；服务端当前不校验 Origin，重写不是放行写请求的必要条件（见 7.2）。
- 若后端端口改变，代理目标与 Origin 重写值需同步修改。

`web/svelte.config.js`：

```js
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";

export default {
  preprocess: vitePreprocess(),
};
```

### 10.2 Go 嵌入与静态服务

```go
//go:embed all:dist
var embeddedDist embed.FS

func distFS() fs.FS {
    sub, err := fs.Sub(embeddedDist, "dist")
    if err != nil {
        panic(err)
    }
    return sub
}
```

- 构建前必须存在 `internal/web/dist/index.html`。
- 使用 `http.FileServer` 或自定义文件处理器提供 `/assets/*`；禁止目录列表。
- 设置 `/assets/*` 长期缓存；`index.html` 不缓存。
- SPA 回退只处理非 API、非资源 GET 请求。

### 10.3 justfile 目标

```make
export CGO_ENABLED := "0"
binary := if os() == "windows" { "note.exe" } else { "note" }

install:
    npm --prefix web ci

frontend: install
    npm --prefix web run build

build: frontend
    go build -trimpath -ldflags "-s -w {{ldflags}}" -o {{binary}} ./cmd/note

[windows]
install-win: build
    powershell.exe -NoLogo -ExecutionPolicy Bypass -File scripts/install-win.ps1

test: frontend
    go test ./...
    npm --prefix web test

check:
    go vet ./...
    npm --prefix web run check
```

- 发布统一使用 `just build`：`npm ci` → 前端构建 → Go 编译；Windows 产出 `note.exe`，其它平台 `note`。
- `ldflags` 是 just 表达式，按 `os()` 分两支（Windows 走 PowerShell、其余走 bash 命令替换），拼出四个 `-X note/internal/build.*` 注入变量：`GitHash`、`BuildDate`（UTC 秒级）、`GitDirty`（`git status --porcelain` 非空才加）、以及源码里已有的 `Version`。两个要点：不在 git 仓库内或 `git` 不存在时命令失败**必须退化成空值**，不能让构建挂掉（`internal/build.Describe()` 会回落到 `debug.ReadBuildInfo()`）；just 在 Windows 上把命令包成 `cmd /C "powershell -Command …"` 且只保留最后一行输出，因此 PowerShell 分支必须以裸 `$a` 结尾。
- `just test` 依赖 `frontend`，因此跑 Go 测试前会重建 `internal/web/dist`——embed 的产物永远与被测代码一致。
- `internal/web/dist` 为生成物，不提交；缺失时 `go build` 因 embed 失败而中止。
- 另有 `just start` / `just stop` / `just restart`（后台开发服务，日志与 pid 写入 `.run/`）与 `just pull` / `just push`（多远程同步）。
- `just install-win`（`[windows]` 限定）把 `build` 的产物装到本机：`scripts/install-win.ps1` 先 `note web status` 探测后台实例并 `note web stop`（轮询到进程真正退出才继续），再删旧版本、拷新版本、就地 `--help` 校验，最后把 `%LOCALAPPDATA%\Programs\note` 幂等地写进用户 PATH（写回保留 `REG_EXPAND_SZ`，不破坏 `%USERPROFILE%` 类条目）。关停失败（比如前台实例）时报错停住，不带着运行中的进程换文件。
- 目标机器只需二进制与数据目录，不需要 Node/npm。

### 10.4 发布检查

1. 干净目录执行 `npm ci` 与前端构建。
2. `go test ./...` 与 `go test -race ./...`。
3. `just build` 产出单二进制，并确认 `GET /api/info` 返回真实 commit hash 与 UTC 构建日期（`go run` 下应显示「开发构建」）。
4. 将二进制复制到未安装 Node 的机器，验证页面、深链接、API。
5. 在空数据目录首次 `note web` 启动，确认自动建库与迁移；修改配置文件与命令行覆盖（含 `-p` 只换端口）的优先级符合 4.2；`note info` 输出与 `/api/info` 一致。
6. 执行 PRD 第 9 节验收项，特别是明文残留检查。
7. 检查 `svelte-check`、前端测试与 `npm audit`（按需）。

### 10.5 构建可复现性

- 提交 `package-lock.json`；`just install` 与 `just frontend` 都走 `npm ci`。
- 开发机为 Node 24（`v24.18.0`）；仓库当前没有 `.nvmrc` 或 `engines` 约束，Vite 8 与 Vitest 5 要求较新的 Node 版本，用明显更旧的版本构建可能直接报错。
- 不提交 `node_modules` 与 `internal/web/dist`。
- Go 侧由 `go.mod` 的 `go 1.26.0` 与 `CGO_ENABLED=0` 保证跨平台产物一致；SQLite 驱动是纯 Go 实现，无需目标机器工具链。

---

## 11. 安全设计

### 11.1 威胁模型摘要

| 威胁                       | 防护 | 说明                                                                                                                                                                  |
| -------------------------- | ---- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 数据库文件被复制           | 部分 | 加密笔记正文是密文；标题、分类名、时间、助记问题始终明文；口令正确性由 GCM tag 验证，没有可离线查询的哈希                                                             |
| 磁盘离线爆破               | 部分 | PBKDF2-HMAC-SHA256 仅 150000 次迭代且盐公开，弱口令可被离线爆破；相对 Argon2id 是已知降级（见 PRD 风险 R4）                                                           |
| 同机他人操作应用           | 否   | 无应用登录，依赖系统账户；页签已解锁时别人能看到明文                                                                                                                  |
| 键盘记录 / 截屏 / 内存抓取 | 否   | 本地明文工具防不住；派生密钥不可导出，但仍存在于页签内存                                                                                                              |
| 跨站写请求                 | 是   | CSRF 双提交（HttpOnly Cookie + 头，常量时间比较）+ 不启用 CORS + Host 白名单                                                                                          |
| DNS Rebinding              | 是   | Host 白名单 + 默认只监听回环；非回环绑定需显式 `--allow-remote`                                                                                                       |
| 存储型 XSS                 | 是   | Markdown 必经 `DOMPurify.sanitize`，`{@html}` 只在一处，CSP `default-src 'self'`，外链补 `noopener noreferrer`                                                        |
| 口令遗忘                   | 无解 | 主密码不可找回、不可重置；唯一出路是确认删除该加密笔记。修改主密码（6.9）不改变这一条：它要求先给出旧口令，服务端始终没有验证口令的能力                               |
| 迁移途中关页面             | 是   | 每条笔记的落定只由试解判定，已迁移的判为 `already_done` 可续跑；没迁完的条目如实列在迁移状态卡里，不会被写坏（实测：失败分支跑完后库里密文字节逐字不变）              |
| 信息页暴露运行信息         | 部分 | `/api/info` 只给标准库运行时与已加载配置（主机名、数据文件路径、监听地址、内存），同源、`no-store`，不含密文、盐、口令或令牌；默认只监听回环                          |
| SSD 物理残留 / 旧备份      | 否   | 应用只做逻辑删除与 WAL checkpoint，其余由用户自行保护                                                                                                                 |
| 本地进程冒充关停请求       | 是   | `/api/shutdown` 需 32 字节随机令牌（只存 `0600` 的 `note.json`，不进日志/响应/命令行/环境变量），常量时间比较；`/api/shutdown` 只在后台模式注册，仍受 Host 白名单约束 |

### 11.2 响应头

```text
Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self';
  img-src 'self' data:; font-src 'self'; connect-src 'self';
  form-action 'self'; frame-ancestors 'none'; base-uri 'none'
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Referrer-Policy: no-referrer
```

缓存：

- API：`Cache-Control: no-store`。
- `index.html`：`no-store`。
- `/assets/*`：`public, max-age=31536000, immutable`。

### 11.3 CSRF 与 Host

- CSRF Token 与 Cookie 双提交；Token 通过 JSON 返回给页面脚本，Cookie 为 HttpOnly、`SameSite=Strict`，服务端用 `subtle.ConstantTimeCompare` 比较。
- 服务端不校验 `Origin`、`Sec-Fetch-Site` 或请求 `Content-Type`；跨源读写由「不启用 CORS」挡下，主机伪装由 Host 白名单（7.3）挡下。
- Host 白名单默认只有 `127.0.0.1`、`localhost`、`::1`；`--allow-remote` 才把监听主机加进去。
- 不启用 CORS，不返回 `Access-Control-Allow-*`，也没有 `OPTIONS` 预检通路。
- 唯一不依赖 CSRF 的写路径是 `/api/shutdown`：它的凭证是 `X-Shutdown-Token`（关停令牌），豁免只覆盖这一条路径，不得扩大。

### 11.4 文件权限

- 数据目录 `0700`，数据库文件 `0600`（POSIX）。
- 后台状态文件 `note.json` `0600`（含关停令牌与 pid）；日志 `note.log` 用默认权限，内容与前台日志同口径（不含正文/密码/密文/令牌）。
- 不把数据库放在同步盘或公开目录；文档提示用户。
- 备份文件同样按敏感数据处理。

### 11.5 日志与错误

- access log 只记 `method`、`path`（不含 query）、`status`、`duration_ms`、`bytes`：查询词、正文、口令、密文、Cookie 都不进日志。
- 外部错误响应统一为语义化 code + 面向用户的中文文案，不回传 SQL、文件路径、密钥或密文内容。
- `slog.Error` 记录服务端内部错误（含驱动返回的 wrap 文本）到 stderr（后台模式即 `note.log`），与响应无关；关停令牌任何情况下都不进日志。
- panic 由 `recoverPanics` 兜住，日志记类别、method、path，响应是 500 `internal` 通用文案。

### 11.6 限制

- 无应用级认证：任何能访问本机端口的人都能操作应用。
- 加密只保护静态数据库副本；已解锁页面可被截屏或从内存抓取。
- 没有自动锁定：解锁状态活到页签关闭或刷新为止，手动「锁定」是唯一的主动过期路径。
- 忘记主密码无法恢复，只能删除该加密笔记。
- 公网暴露需要额外的 TLS 与认证层，本项目不提供；`--allow-remote` 只为局域网调试存在。

---

## 12. 测试方案

### 12.1 Go 单元与集成测试

| 文件                                       | 覆盖                                                                                                                                                                                                                                                                                                                                                                                                                  |
| ------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/config/config_test.go`           | 内置默认值、YAML 解析、命令行覆盖、未知键拒绝、非回环绑定需 `--allow-remote`、缺失显式配置报错、用户段读写往返、`-c` 短参数、`-p/--port` 只覆盖端口（含 IPv6）、`--remote` 别名、默认路径解析与 `ConfigFound`                                                                                                                                                                                                         |
| `internal/config/ai_test.go`               | `SetValue` 从零建文件、保留其它键与注释、未知键/非法值拒绝且不建文件、已有未知键时拒绝写入、空串清空、多行提示词写字面量块且逐字往返、`0600` 与不留临时文件（Windows 跳过）、`SetValues` 三项一次写入且部分失败不留痕、`ReadAI` 缺文件不算错、启动校验坏 `base_url`、`MaskSecret` 四档                                                                                                                                |
| `internal/config/paths_test.go`            | 默认配置/数据目录解析（环境变量隔离，各平台期望路径；Linux 的 `XDG_DATA_HOME` 缺省回退 `~/.local/share`）                                                                                                                                                                                                                                                                                                             |
| `internal/cli/parse_test.go`               | 子命令分发（菜单/web/info）、全局 `-c`、帮助变体（`-h`/`--help`/`help`/`help web`/`web -h`）、带值参数的 `-h` 预扫描（`--config -h` 不是帮助）、未知命令/参数与缺值报错                                                                                                                                                                                                                                               |
| `internal/cli/menu_test.go`                | 菜单模型：光标上下移动与越界保持、回车选中当前项、q/Esc 保持零值退出、视图含全部菜单项；非终端环境 `RunMenu` 返回回退动作                                                                                                                                                                                                                                                                                             |
| `internal/cli/info_test.go`                | `FormatInfo`：分区与字段齐全、缺库占位、CJK 标签按显示宽度对齐（逐行解析值列）、`humanSize` 三档                                                                                                                                                                                                                                                                                                                      |
| `internal/cli/usage_test.go`               | 总帮助覆盖命令/全局参数/默认位置说明；`UsageFor` 各主题参数齐全，未知主题回退总帮助                                                                                                                                                                                                                                                                                                                                   |
| `internal/cli/parse_test.go`（web 动作）   | `web start`/`stop`/`status` 识别（动作后的 flag 原样透传）、`web --addr` 不受影响、未知 web 子命令报错并要求退出码 2                                                                                                                                                                                                                                                                                                  |
| `internal/cli/status_test.go`              | `FormatStatus` 三种形态（运行中/未运行/进程在但不响应）、pid 行仅在有 pid 时出现、状态文件四态（存在/不存在/残留已清理/属于另一个库）、uptime 与文件大小文案、CJK 对齐                                                                                                                                                                                                                                                |
| `internal/cli/filter_test.go`              | `noteFilter`：标题/分类 fuzzy 与正文子串都能命中、同一项不重复、正文命中不带标题下标、大小写不敏感；只命中正文时列表第二行换成命中片段（`snippetAround` 的前推/截断/省略号/折叠换行）；`highlightTerm` 不改动文本                                                                                                                                                                                                     |
| `internal/clipboard/clipboard_test.go`     | 原生成功时不再发 OSC52、失败时回落并逐字节检查 OSC52 序列（base64 + `ESC \\` 结尾）、stdout 非终端不写序列、超 100 KiB 拒绝、`ExplainFailure` 文案                                                                                                                                                                                                                                                                    |
| `internal/daemon/state_test.go`            | `note.json` 往返、`0600`（Windows 跳过）、临时文件 + rename 原子覆盖、损坏 JSON 与非存在文件的错误区分、`StatePath`/`LogPath` 推导、`RemoveIfPid` 只在 pid 匹配时删除、令牌 32 字节且每次不同                                                                                                                                                                                                                         |
| `internal/daemon/control_test.go`          | 通配地址映射（`0.0.0.0`→`127.0.0.1`、`::`→`::1`）、空地址与不可解析地址原样返回                                                                                                                                                                                                                                                                                                                                       |
| `internal/daemon/pid_test.go`（+windows）  | 自身 pid 存活、不存在的 pid 判死、pid ≤ 0 判死；Windows 专有：已退出但对象未回收的 pid 必须判死（回归 `os.FindProcess` 假阳性）                                                                                                                                                                                                                                                                                       |
| `internal/daemon/probe_test.go`            | `Probe` 解析 `addr`/`started_at`/`build_label`、非 200 与超时返回错误；`RequestStop` 的路径/方法/令牌头、202 成功、403 映射为「关停令牌无效」                                                                                                                                                                                                                                                                         |
| `internal/daemon/spawn_test.go`            | 分离进程启动后父进程立即返回、子进程经日志 fd 写入 `note.log`、环境变量 `NOTE_DAEMON_STATE` 传递                                                                                                                                                                                                                                                                                                                      |
| `internal/web/handlers_ai_test.go`         | 首读全空且不建文件、响应 `no-store`、PUT 与 GET 都不含明文密钥（只有掩码与 `api_key_set`）、省略 `api_key` 保持旧值、空串清除、四项字段级 400 且不落盘、文件已有未知键 409、无 CSRF 令牌 403 且不写文件                                                                                                                                                                                                               |
| `internal/ai/client_test.go`               | `Configured`/`Missing`/`DefaultModel`/`Host`、URL 拼接与末尾斜杠、`temperature`/`stream`/`model` 载荷、显式未知模型 → `ModelError`、非 2xx → `UpstreamError`（摘要按 200 码点截断）、坏 JSON/空 choices → `ErrBadResponse`、1 MiB 上限 → `ErrTooLarge`、空正文 → `ErrEmptyResult`、`BuildPrompt` 空模板回落与只替换首个变量、`StripFence` 只剥整段围栏                                                                |
| `internal/web/optimize_api_test.go`        | `POST /api/ai/optimize` 成功与 `no-store`、空/超长正文 400、模型不在列表 400 字段 `model`、未配置 409、上游 5xx 502 与超时 504（`fakeUpstream` 起 `httptest` 假上游）、结果不落库                                                                                                                                                                                                                                     |
| `internal/web/shutdown_api_test.go`        | 正确令牌 → 202 且回调只调用一次；错误/缺失令牌 → 403；未启用（Token 为空）→ 403 `shutdown_disabled`；GET → 405 带 `Allow`；不带 CSRF Cookie 也能通过；非法 Host 仍被 `hostGuard` 拦下                                                                                                                                                                                                                                 |
| `internal/store/store_test.go`             | 空库自动建表并跑完 7 个迁移、重复打开幂等、`user_version` 高于程序时拒绝启动、非法文件拒绝启动、0004 对象存在、0005 的 kind 枚举与索引重建、0006 的 `deleted_at` 为可空 TEXT 且存量行为 `NULL` + 两个部分索引存在、0003 扩展列默认值与 CHECK                                                                                                                                                                          |
| `internal/store/activity_test.go`          | 笔记与分类的增删改各写一条对应事件、`RecordActivity` 只接受已知 kind、失败的写操作（不存在/非空分类/默认分类）不留事件、`GetStats` 的总量/活跃天数/按天计数覆盖全部事件而 Top 查询只算 `search`                                                                                                                                                                                                                       |
| `internal/store/category_test.go`          | 分类 CRUD 与笔记数、大小写不敏感重名、默认分类守卫（改名/删除）、非空分类删除返回条数、**未分类（`category_id IS NULL`）计入默认分类并可被筛出**                                                                                                                                                                                                                                                                      |
| `internal/store/migration_test.go`         | 用只跑到 `user_version=6` 的旧库验证 0007：插入「速记」、同分类重名按「活着的优先 → 更新时间新 → id 大」保留并追加后缀（大小写不敏感）、后缀冲突退化为 id 片段、超 200 码点截断、两条部分唯一索引存在且覆盖未分类                                                                                                                                                                                                     |
| `internal/store/note_title_test.go`        | `FindNoteByTitle`/`UpsertNoteByTitle`/`SoftDeleteNoteByTitle`/`SoftDeleteCategoryNotes`/`ListNotesInCategory` 全分支：新建→覆盖同 id 且 `rev+1`、NOCASE 命中、回收站同名拒绝（不复活不覆盖）、加密笔记拒绝、按分类软删只动未删除行且每条记一次 `note_delete`、`CreateNote`/`UpdateNote` 的撞名映射（活着 → `ErrNoteTitleExists`，回收站 → `ErrNoteTitleInTrash`）                                                     |
| `internal/cli/ref_test.go`                 | `ParseRef` 表驱动：`--type` 优先与标题里的 `@`、`@分类` 引用、`a@b@c` 按最后一个 `@` 切分、空标题/空分类/`@`/超长标题/超长分类名，前后空白裁剪                                                                                                                                                                                                                                                                        |
| `internal/cli/quick_test.go`               | `RunSet/RunGet/RunGetCategory/RunGetCategories/RunDel/RunDelCategory`：新建与覆盖文案、超长正文不写库、按需建分类、结尾换行三态、回收站与加密映射、空分类不是错误、分类表格的「固定」标记与无尾随空格、整分类删除的 0 条报错                                                                                                                                                                                          |
| `cmd/note/quick_test.go`                   | cli 与 store 的「速记」常量一致、未分类映射 `category_id IS NULL`、真实库全链路（写入/覆盖/列表/软删/整分类删除）、缺失笔记与分类的哨兵映射、未分类往返                                                                                                                                                                                                                                                               |
| `internal/store/master_test.go`            | 主密码只写一次、盐长度与空问题拒绝、`UpdateMasterQuestion` 只改问题列（空/超 100 码点拒绝、未配置时报 `master_password_missing`、盐与 `created_at` 不动）                                                                                                                                                                                                                                                             |
| `internal/store/note_test.go`              | id 形态、明文/密文建改删、明文转加密、`rev`/`updated_at`、分页、标题命中排前、分类筛选与空查询、按码点计数的 2000 边界、结构非法拒绝、软删除不碰 `rev`/`updated_at` 且删除后不可更新、彻底删除后 404、`ListEncryptedCiphers` 只含加密行且不带 `content`、`UpdateNoteCipher` 只换密文（`title/content/category_id/rev/updated_at` 逐字不变）                                                                           |
| `internal/store/note_trash_test.go`        | 软删除后列表/搜索/分类计数全部看不到它而 `deleted=1` 能列出、恢复到原分类且 `updated_at`/`rev` 不变、`restore`/`purge` 对未删除笔记返回 `ErrNoteNotInTrash` 而对不存在返回 `ErrNoteNotFound`、重复删除 404、软删除/恢复/彻底删除各落一条事件、只剩回收站笔记的分类可删且恢复后落到「未分类」                                                                                                                          |
| `internal/build/info_test.go`              | `Describe()` 的「已注入」与「回落到 vcs 信息」两分支、`Label()` 的开发构建标注                                                                                                                                                                                                                                                                                                                                        |
| `internal/web/handlers_info_test.go`       | `/api/info` 字段齐全、`no-store`、时间戳形如 `2006-01-02T15:04:05Z`、`schema_version` 为 7                                                                                                                                                                                                                                                                                                                            |
| `internal/web/note_cipher_api_test.go`     | 清单不含 title/content、缺 `kdf_params` 与非法 base64 各返回对应字段错误、对明文笔记 409 `not_encrypted`、不存在的 id 404、不带 `X-CSRF-Token` 的 PUT 403 且库里密文未变、`/api/notes/encrypted` 不被 `{id}` 通配吃掉                                                                                                                                                                                                 |
| `internal/web/master_question_api_test.go` | `PUT /api/master-password/question` 的成功、字段校验与 CSRF 拒绝                                                                                                                                                                                                                                                                                                                                                      |
| `internal/web/server_test.go`              | SPA 回退与安全头、`/assets` 长缓存与缺失 404、未知 API 返回 JSON 404、CSRF 签发与复用、Host 白名单、带扩展名非 HTML 路径 404、设置门禁与改名、统计与搜索端点                                                                                                                                                                                                                                                          |
| `internal/web/notes_api_test.go`           | 创建与字段级校验、密文笔记往返、客户端自带 id、列表分页与筛选、`q` 超长 400、读改删与 404/409 映射、同分类标题唯一（`title_conflict` / `title_in_trash`、NOCASE、换分类同名合法、改标题撞邻居 409）、回收站全流程（软删除后详情带 `deleted_at`、默认列表不出现、`deleted=1` 列出、恢复回原分类、`not_in_trash` 409、`?purge=1` 后 404、restore 的 CSRF 拒绝）、增删改查与详情查看各自落一条事件（回收站详情不算查看） |
| `internal/web/categories_api_test.go`      | 主密码 GET/POST 与重复设置 409、分类增删改与错误码、非空删除 409 带 `note_count`                                                                                                                                                                                                                                                                                                                                      |

- 运行 `just test`（内含 `go test ./...`）与 `just check`（`go vet ./...`）。
- LIKE 转义由 `TestListNotesSearchOrdersTitleHitsFirst` 等搜索用例间接覆盖；没有独立的 fuzz 目标。
- 不依赖真实网络和外部数据库；每个用例用 `t.TempDir()` 建独立库（`internal/daemon` 的探测用例用 `httptest` 起本地服务）。
- 后台运行的真实二进制冒烟不进 `go test`，过程与结果记录在 CHANGELOG（启动/幂等/停止/三态退出码/异常分支）。

### 12.2 前端测试

- Vitest 5，不引入组件测试库；需要 DOM 的文件用 `// @vitest-environment jsdom` 单独声明。
- 覆盖（`web/src/**/*.test.ts`）：
  - `router/router.test.ts`：路径匹配、参数解析、`edit`/`forgot` 优先于 `detail`、`/trash` 与 `/notes/trash` 不混淆、未知路径；
  - `lib/crypto.test.ts`：口令 NFC、加解密往返、错误口令与篡改密文归并为 `DecryptError`、KDF 参数边界、base64 工具；
  - `lib/markdown.test.ts`：GFM 渲染、脚本与 `onerror` 被净化、外链补 `noopener noreferrer`；
  - `lib/draft.test.ts`、`lib/notes.test.ts`、`lib/stats.test.ts`、`lib/theme.test.ts`、`lib/toast.test.ts`：内存草稿单槽语义、API 载荷与错误映射、热力图分桶与事件构成分组（固定顺序、分类合并、占比、未知 kind 归入「其他」）、主题持久化与提示队列；
  - `lib/reencrypt.test.ts`：注入假 `decrypt/encrypt/putCipher` 穷尽 6.9 的五类落定；幂等续跑（第二次跑全是 `already_done`）；无加密笔记时跳过旧口令验证；`remaining` 汇总正确；**旧口令只能解开清单靠后那条时验证仍须通过**（防「只试第一条」的回归）；整份报告序列化后不含正文；`countRemaining` 在复核请求自身失败时退回迁移报告的计数（不把已成功的改密报成失败）；`needsPreviousPassword` 只在报告含 `failed_old_key` 时要求上一个口令；
  - `lib/info.test.ts`：`buildRows` 的 14 行中文键、缺失值统一「未知」、字节数与运行时长格式化、`allow_remote` 的是/否；
  - `lib/onboarding.test.ts`：`decideStep(nameSet, masterConfigured)` 的四个组合；
  - `lib/settings.test.ts`：并行读两个端点、主密码端点失败必须整体进 `error` 态（这条先用旧实现跑出 RED——引导判据不能建立在半套数据上）；
  - `lib/clipboard.test.ts`：异步剪贴板 API 直通、抛错时回落 `execCommand`、两条都失败返回 `false`、临时 textarea 用完必删（含失败路径）；
  - `lib/ai.test.ts`：`optimizeNote` 的载荷（省略模型时不带 `model`）、`defaultModel`、`hostOf`；`lib/ai-config.test.ts` 的四项读写与 `countRunes`；
  - `views/NoteEditorView.test.ts`：「用 AI 优化格式」的启用/禁用、结果面板渲染与码点计数、替换与放弃、加密笔记的明文提示；
  - `views/NoteDetailView.test.ts`：明文笔记渲染「复制原文」图标且复制的是原文 Markdown、复制失败 toast 提示、加密笔记解锁前不渲染该按钮；
  - `lib/notes.test.ts` 另含密文清单与单条替换的载荷与错误映射、回收站的 `deleted=1` 查询串与 `purge=1`/`restore` 的载荷；`lib/stats.test.ts` 另含 `note_purge` 折进「删除笔记」、`note_restore` 单列；`router/router.test.ts` 另含 `/info` 与 `/info?x=1`。
- `just check` 跑 `svelte-check --tsconfig ./tsconfig.app.json` 与 `tsc -p tsconfig.node.json`，构建脚本 `npm run build` 会先跑同一套检查。
- 视图组件大多不在单测里渲染（例外：`NoteDetailView.test.ts` 锁「复制原文」按钮，`NoteEditorView.test.ts` 与 `InfoView.test.ts` 锁 AI 相关交互）：见性与复制内容），其余由 12.4 的端到端脚本覆盖（含回收站的恢复/彻底删除与多视口排版）。

### 12.3 端到端验证

本轮用一次性脚本驱动真实二进制，而不是手工点：

- 起服务：`note.exe` 监听 `127.0.0.1:8099`，数据库用 `.run/e2e-notes.db`，与用户的 `data/notes.db` 完全隔离。
- 浏览器侧：Playwright-core 驱动本机 Edge headless，走 29 项检查——命名引导、建明文/加密笔记、Markdown 预览与净化、分类管理全流程、锁定与再解锁、跨视图刷新、删除确认，全部通过。
- 跨客户端解密：用 `node:sqlite` 直接读库、`node:crypto` 按 `kdf_params` + `master_password.salt` 派生密钥解密，明文与浏览器输入逐字一致；换一个字符的口令被 GCM tag 拒掉。
- 残留：哨兵串扫描 `.db` 与 `.db-wal`，加密正文不出现，对照的明文笔记正文照常出现。
- 日志：抓取 access log，确认无正文、无口令、无密文、无 Cookie。
- AI 优化（v1.14）：在 `%TEMP%` 隔离目录跑真实二进制 + 本地假上游（`127.0.0.1:18081`），走 6 步——`note config list` 的四项显示与默认提示词标记、缺 `{{content}}` 时命令行退出 2 与网页 400、`GET/PUT /api/ai-config` 往返（模型列表保留、`default_prompt` 回填、`api_key_set` 不变）、`POST /api/ai/optimize` 200 且带 `no-store` 与上游收到所选模型、关掉假上游后 502 且正文不变、`note.log` 里 `grep -c sk-test` 为 0 且无正文。

v1.4 的信息页与改密链路由 `.run/info-e2e.mjs` 覆盖（同样 Playwright-core + Edge headless，独立端口与 `.run/e2e-info.db`，60 项断言），三次启动分别验证：

- **空库引导**：步骤 1 只填名字就刷新，必须仍在引导页且直接落在步骤 2；两次口令不一致或未勾选确认时 `POST` 次数为 0；成功后 `master_password` 只有一行、16 字节盐与 `GET /api/master-password.salt_b64` 一致；浏览器存储里没有口令与密钥。
- **已配置形态**：用两个页签确定性地复现「POST 已成功、解锁前刷新」—— parked 在步骤 1 的页签存名字时，另一页签已完成引导，此时步骤 2 必须渲染解锁输入框且 `POST` 次数为 0（重复 POST 会 409，缺这条分支就把用户永久卡在第二步）。老库（已配主密码、`name_set: false`）重启后填完名字直接进主界面，无死路。
- **改密主链路**：`/info` 的 14 行运行信息含真实 commit hash 与 UTC 构建日期 → 改名（UI / `/api/settings` / 配置文件三处一致）→ 改助记问题（盐不变）→ 错误旧口令被拦下且两条密文的 `cipher`/`updated_at`/`rev` 全不变 → 正确旧口令跑完迁移（密文全换、元数据不动）→ 刷新后用新口令解锁两条 → **中断续跑**：用 `page.route` 让第二条 `PUT /cipher` 失败，界面给出「成功 1 · 跳过 0 · 失败 1（还有 1 条未迁移）」；刷新后状态卡不在任何浏览器存储留痕；用最新口令续跑时未迁移那条判为 `failed_old_key` 且字节不变，此时在浏览器里点状态卡的「继续迁移」，断言按钮文案是「继续迁移（请输入上一个主密码）」且点完旧密码栏为空（不预填刚设的新密码）；再用它的真实旧口令续跑后状态卡消失、两条都可用新口令解开。收尾对 `.db` 与 `-wal` 检索本次用过的 4 个口令与 2 段加密正文，全部不出现，对照明文哨兵照常出现。

v1.8 的移动端与兼容性由 `.run/responsive-verify.mjs` 覆盖（Playwright-core + Edge headless，端口 8096 与 `.run/responsive-verify.db`，116 项断言）：

- **布局矩阵**：360×640 / 390×844 / 768×1024 / 1440×900 四个视口 × 概览、列表、详情、编辑、回收站、信息、主密码七页，逐页断言页面级横向溢出为 0，并扫描所有可见元素是否出屏（跳过 `overflow-x: auto/scroll` 祖先节点，只放过热力图与窄屏导航这类滚动容器内部）。
- **窄屏细则**：按钮、`.cat-act`、导航链接实测高度 ≥44px；输入框字号 ≥16px；`.cat-actions` 的 `opacity` 为 1（不需要 hover）；热力图 `scrollWidth > clientWidth` 且能滚到末尾、格子总宽仍 >600px（53 周没被压缩）、出现滑动提示；编辑页单列且 `.preview` 的 `clientHeight < scrollHeight`；列表页单列且分类树在列表上方。
- **宽屏对照**：1440 下编辑页仍是两栏、热力图一屏显示无需滚动；Tab 顺序依次覆盖品牌与四个导航（键盘路径不依赖 hover）。
- **主题**：亮色 / 暗色 / 护眼三套在 390 视口下各跑五个页面，断言无溢出。
- **200% 缩等等价**：720×450 视口跑六个页面，断言无溢出。
- **触控全流程**（390×844、`hasTouch`、`isMobile`，全部用 `tap`）：概览搜索 → 搜索结果进详情 → 编辑并确认预览实时渲染 → 保存 → 移入回收站 → 回收站恢复 → 分类新建/改名/删除 → 加密笔记解锁（未解锁时只有解锁区，解锁后正文可读）→ 移入回收站后彻底删除仍禁用 → 信息页键值行上下两行 → 主密码页只读。

### 12.4 安全与其它检查

- 越权与畸形输入走 API 单测：非法 UUID、非法 base64、超长密文、缺 `kdf_params`、未知分类 id 都是 400 + 字段级文案，不 panic（`recoverPanics` 只作兜底）。
- 未执行、留给后续：Firefox/Chrome 跨浏览器手工验证（本轮只有 Edge）、1 万条笔记的列表与搜索延迟基准、首屏 gzip 体积预算、`go test -race`。
- 手工回归清单（每次改前端后建议过一遍）：深链接刷新、前进/后退、360px / 390px / 768px / 1440px 布局、解锁后刷新需重输口令、手动锁定清草稿、空数据目录首次建库、损坏库拒绝启动、配置三级优先级、响应头与浏览器存储。

### 12.5 完成标准

- PRD 第 9 节全部验收项通过。
- `just test` 与 `just check` 全部通过。
- 干净环境 `just build` 成功，单二进制可在无 Node 环境运行。
- 明文残留检查通过。
- 安全自检清单（PRD 附录 B）逐项确认。

---

## 13. 性能与容量

代码里真正强制的上限：

| 项                       | 值                              | 出处                                                       |
| ------------------------ | ------------------------------- | ---------------------------------------------------------- |
| 标题                     | 1–200 码点                      | `store.maxTitleRunes` / `web.maxTitleRunes`                |
| 明文正文                 | ≤ 2000 码点                     | `store.maxContentRunes`                                    |
| 密文                     | 29–65536 字节，请求体 ≤ 128 KiB | `store.minCipherBytes`/`maxCipherBytes`、`web.maxNoteBody` |
| 搜索词                   | ≤ 200 码点                      | `web.maxQueryRunes`                                        |
| 列表页大小               | 固定 20                         | `store.notesPerPage`                                       |
| 分类名 / 助记问题 / 姓名 | ≤ 30 / 100 / 32 码点            | `web.maxCategoryNameRunes` 等                              |
| 统计回溯                 | 默认 370 天，`days` 上限 3650   | `web.statsLookbackDays`                                    |
| 数据库连接               | 1 开 1 闲，串行                 | `store.Open`                                               |

实测与未测：

| 指标                                      | 状态                                                                                                                         |
| ----------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| 首屏前端体积                              | 实测 JS 163.61 kB（gzip 58.34 kB）+ CSS 14.26 kB（gzip 3.45 kB）+ `index.html` 0.39 kB，合计 gzip 约 62 kB，在 150 kB 预算内 |
| 前端构建                                  | `vite build` 4.25 s，139 个模块，`svelte-check` 0 error 0 warning                                                            |
| 浏览器端 PBKDF2 + AES-GCM                 | 单条笔记在 Edge headless 里即时完成（端到端脚本未掐表，未做峰值内存测量）                                                    |
| 冷启动、10k 笔记列表/搜索延迟、1 MiB 解密 | 未实测；正文上限 2000 码点使「1 MiB 单条」这一旧目标不再是路径                                                               |

策略：

- 列表固定 20 条，避免一次返回全量。
- 搜索使用 `LIKE` 全表扫描；个人数据量下可接受，后续按需引入 FTS5 trigram。
- 单连接数据库串行处理；个人场景足够，避免并发复杂度。
- 不引入缓存层；API 全部 `no-store`，避免明文/密文缓存歧义。
- KDF 在浏览器里跑，服务端没有需要限流的 CPU 密集路径。

---

## 14. 部署与运维

### 14.1 运行

```bash
note                       # 打印帮助（等价 note help）
note add                   # 交互向导新建一条明文笔记（选分类 → 标题 → 正文，Ctrl+D 完成；Ctrl+O 用 AI 优化）
note web                   # 前台启动 Web 服务（读用户配置目录，默认 127.0.0.1:8080）
note web start             # 后台启动；日志与状态文件在数据库同目录（note.log / note.json）
note web status            # 运行情况；运行中 0 / 不响应 1 / 未运行 3
note web stop              # 优雅停止后台实例
note web -p 9090           # 指定端口；host 沿用配置文件或 --addr
note web start -p 9090     # 后台启动并指定端口（--db/--addr/-p 写在 web 之后）
note web --remote --addr 0.0.0.0:8080    # 监听局域网（须配 TLS 与认证）
note info                  # 输出软件与运行信息（含日志路径）
note status                # 使用统计：累计事件 / 活跃天数 / 事件构成 / Top 查询词
note -c /path/note.yaml web             # 全局 -c 指定配置文件
```

- 首次 `note web` 自动创建数据目录、数据库文件并执行初始化迁移（见 5.2）；配置文件不存在时用内置默认值并警告（改名等需要写回配置的操作会自动创建用户配置目录）。
- 默认只允许本机访问；启动成功后在 stdout 打印可直接点击的访问地址（默认 `http://127.0.0.1:8080`）；通配绑定（`--addr 0.0.0.0` / `[::]` 配 `--allow-remote`）时枚举本机非回环地址逐条打印。远程设备按打印地址访问时仍受 Host 白名单校验（见 7.3）。
- 远程绑定必须显式 `note web --allow-remote`（或 `--remote`），且应置于 TLS + 认证反向代理之后；MVP 不提供这些能力。
- 管道/脚本环境（stdin 非终端）运行裸 `note` 不会进入菜单，直接打印帮助并退出；子命令与参数错误退出码 2，服务错误退出码 1。
- 从 v1.5 之前的默认位置迁移：旧配置在 `./config/note.yaml`、旧数据在 `./data/notes.db`，用 `note web -c ./config/note.yaml --db ./data/notes.db` 继续使用原文件，或把两个文件拷到 4.2 表中的用户目录位置。
- 后台模式（v1.6）：`start` 成功后终端可以关。日志追加写 `note.log`，不轮转，需要时自行清理（删除运行中的日志文件会让写入继续落到旧 inode，建议先 `stop`）。`note.json` 只是运行期凭据（pid + 地址 + 令牌），可安全删除；残留文件会在下一次 `start` / `status` 被识别并清理。`start` 失败时打印 `note.log` 最后 20 行，端口占用这类启动期错误能直接看到原因。
- 同一目录只有一个状态文件，因此同一个数据目录里不要用两个不同 `--db` 起两个后台实例；`start` / `stop` 会拒绝并提示。

### 14.2 备份

1. 停止应用（后台实例用 `note web stop`），或使用 SQLite 在线备份命令。
2. 复制 `notes.db`、`notes.db-wal`、`notes.db-shm`（同目录的 `note.log` / `note.json` 不需要备份，`note.json` 含关停令牌，对外分享前建议排除）。
3. 加密笔记仍为密文；明文笔记仍为明文，备份需同等保护。
4. 旧备份可能包含加密前的明文，应用无法追溯清理。

### 14.3 升级与回滚

- 升级：停止应用 → 备份数据库 → 替换二进制 → 启动；启动时执行迁移。
- 迁移只向前；不支持自动回滚 schema。
- 回滚需恢复旧二进制与升级前备份。
- 发布前在数据库副本上演练迁移。

### 14.4 日志与监控

- 前台模式日志输出 stdout，由用户自行重定向或交给系统服务管理；后台模式（`note web start`）写入数据库同目录的 `note.log`（追加、不轮转）。
- 不提供远程遥测、指标上报或云服务。
- 进程存活以 `note web status`（状态文件 + `/api/info` 探测）为准；不是通用健康检查端点。

---

## 15. 实施进度

### 15.1 M1：基础能力（已完成）

- Go 模块、`config/note.yaml` 三级加载与严格校验、结构化日志、HTTP 服务骨架。
- SQLite 启动检测与自动建库、PRAGMA、`sql/0001`–`0003` 迁移、CRUD、搜索、活动统计。
- API：列表、创建、详情、更新、删除、CSRF、统计、设置。
- Svelte 脚手架、自写 History 路由、首页与列表/详情/编辑视图、API 客户端、三主题。
- Vite → Go embed 构建链路与 justfile。

### 15.2 M2：加密与笔记应用（已完成，方案改为浏览器端）

- 浏览器 WebCrypto：PBKDF2-HMAC-SHA256 + AES-256-GCM，`lib/crypto.ts` / `lib/session-key.ts`。
- 全局主密码首次配置（`POST` 只允许一次）、助记问题、忘记口令后唯一出路是删除笔记（修改口令自 v1.4 起提供，见 15.5）。
- 加密创建、编辑重加密、加密不可逆；服务端只搬运密文。
- 分类：`sql/0004` + store 分类层 + CRUD API + 列表页右侧分类树（`components/CategoryTree.svelte`）。
- Markdown 写作与预览：`marked` + `DOMPurify`，正文只存原始 Markdown。
- 2000 码点正文上限、200 码点搜索上限等校验。

### 15.3 M3：加固与验证（已完成）

- Host 白名单、安全头与 CSP、日志脱敏、CSRF 双提交。
- `secure_delete`、WAL checkpoint、哨兵串残留验证。
- Go 单测/集成测、前端 Vitest 单测、端到端脚本与 CLI 跨客户端解密验证。
- 文档：PRD v1.3、本技术方案 v1.3、BUILD.md、CHANGELOG.md。

### 15.4 M4 之后仍未做的

- 跨浏览器手工验证（Chrome/Firefox）与响应式的真机走查。
- 数据量基准（10k 笔记列表/搜索）与冷启动计时。
- 明文笔记的回收站、导出导入。
- `go test -race` 与 fuzz 目标尚未纳入 `just test`。

### 15.5 M5：信息页与改密（v1.4，已完成）

- `internal/build` + `justfile` 的 `ldflags` 注入与 `debug.ReadBuildInfo` 回落；`GET /api/info` 与 `Store.SchemaVersion`。
- 密文迁移端点：`GET /api/notes/encrypted` 清单与 `PUT /api/notes/{id}/cipher` 单条替换（`store.ListEncryptedCiphers` / `UpdateNoteCipher`）。
- 助记问题单独修改：`PUT /api/master-password/question`（不换盐）。
- 前端：`/info` 四张卡、`lib/reencrypt.ts` 迁移引擎、`lib/info.ts` 展示摊平、引导页两步式与 `lib/onboarding.ts` 判据、`settings` 并行读两端点。
- 验证：Go 与前端单测（`just test`）、`just check`、`.run/info-e2e.mjs` 的 57 项端到端断言（含中断续跑与口令残留检索）。

### 15.6 M6：CLI 子命令化（v1.5，已完成）

- `internal/cli`：`Parse` 子命令分发与全局 `-c`、bubbletea 菜单（模型不含服务资源，退出后由 `cmd/note` 接手）、`FormatInfo` 渲染、动态帮助文本。
- `internal/config`：默认配置/数据目录迁移到用户目录（`paths.go`，XDG 惯例）、`-c` 短参数、`-p/--port` 只覆盖端口、`--remote` 别名、`ConfigFound`。
- `cmd/note`：`run` 分发 + `runWeb`（原服务生命周期不变）+ `runInfo`（数据库存在才开库）；`scripts/dev.*` 改为 `go run ./cmd/note web`。
- 依赖：新增 `charmbracelet/bubbletea`（纯 Go，无 CGO），直接引用其随包依赖 `mattn/go-isatty`（终端检测）与 `rivo/uniseg`（CJK 宽度）。
- 验证：`just test`（Go 7 包 108 项 + 前端 100 项）、`just check`、真实二进制冒烟（真控制台菜单存活、`info -c` 输出对齐、`web --port` 覆盖生效并打印 URL、HTTP/CSRF/`/api/info` 全通、无终端裸 `note` 退化为帮助、`note foo` 退出码 2）。

### 15.7 M7：后台运行（v1.6，已完成）

- `internal/daemon`：`State`/`StatePath`/`LogPath`/`NewToken`/`Load`/`Save`/`RemoveIfPid`（状态文件 0600 + rename 原子写）、`ControlAddr`（通配→回环）、`ProcessAlive`（Unix `kill(pid,0)`，Windows `OpenProcess(SYNCHRONIZE)` + `WaitForSingleObject(0)`）、`Probe`/`RequestStop`（2s 超时、令牌头）、`RunDetached`（Unix `Setsid`，Windows 分离进程组 + 隐藏窗口，日志 fd 接管）。
- `internal/web`：`Control{Token, Shutdown}` + `NewWithControl`；`/api/shutdown` 仅在 Token 非空时注册（POST + GET 两路注册，GET 返 405 + Allow），空令牌 403 `shutdown_disabled`、令牌不符 403 `shutdown_denied`、成功 202 后异步关停；CSRF 只豁免这一条路径。
- `internal/cli`：`WebAction`（前台/start/stop/status）与未知子命令错误；`StatusData`/`FormatStatus` 三态渲染（运行中/未运行/进程在但不响应）；`info` 新增日志行。
- `cmd/note`：`runWebStart`（幂等、就绪轮询 100ms/10s、失败时回显日志尾 20 行）、`runWebStop`（未运行时提示退出 0、前台实例拒绝退出 1、10s 后强杀并说明 WAL）、`runWebStatus`（退出码 0/1/3、残留状态文件清理）、`exitError`（错误已打印时不再重复输出）；菜单项改后台启动。
- 验证：`just test`（Go 8 包 139 项含新增 windows-only 用例）、`just check`、真实二进制冒烟 12 项（start 幂等、stop 0.26s 优雅退出且日志有 `shutting down`、status 三态、端口占用回显日志尾、前台实例拒绝 stop、残留与损坏状态文件、同目录另一个库归属校验）。
- 实现中发现并修复：Windows 上 `os.FindProcess` 对已退出但进程对象未被回收的 pid 仍返回成功，导致 `stop` 白等 10 秒后强杀、`start` 误判「进程在但不响应」；改为等待句柄判定并补 `pid_windows_test.go` 复现。

---

## 16. 决策记录（ADR）

| 编号   | 决策                                                                                                                                                                                                                    | 理由                                                                                                                                                                                                                                                 | 代价                                                                                                                                                                                                                                                                                                            |
| ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ADR-01 | 前端使用 Svelte 5 + Vite，不使用 SvelteKit                                                                                                                                                                              | 轻量、无 SSR/文件路由负担，产物直接嵌入 Go                                                                                                                                                                                                           | 需要自写路由                                                                                                                                                                                                                                                                                                    |
| ADR-02 | 自写约 85 行 History 路由，不引入路由库                                                                                                                                                                                 | 10 个视图，减少依赖与体积                                                                                                                                                                                                                            | 路由功能有限，复杂需求需扩展                                                                                                                                                                                                                                                                                    |
| ADR-03 | JSON API + SPA 回退                                                                                                                                                                                                     | 前后端解耦，便于独立前端构建                                                                                                                                                                                                                         | 需要维护 API 契约                                                                                                                                                                                                                                                                                               |
| ADR-04 | SQLite 单连接                                                                                                                                                                                                           | 避免并发写与 PRAGMA 状态问题，个人场景足够                                                                                                                                                                                                           | 查询串行化                                                                                                                                                                                                                                                                                                      |
| ADR-05 | 不保存口令哈希或校验值                                                                                                                                                                                                  | 减少离线爆破目标；GCM tag 已能验证口令                                                                                                                                                                                                               | 校验口令必须完整派生 + 解密                                                                                                                                                                                                                                                                                     |
| ADR-06 | 全局主密码 + 页签级会话密钥，而不是每条笔记独立口令                                                                                                                                                                     | 用户只需记一个口令；一次解锁通用于整会话                                                                                                                                                                                                             | 单点失败：忘记主密码则全部加密笔记永久打不开                                                                                                                                                                                                                                                                    |
| ADR-07 | 加解密在浏览器完成，服务端零密码学代码                                                                                                                                                                                  | 明文永不出本机网络，服务端不持有可解密材料；密文自描述，未来 CLI 可直接读库解密                                                                                                                                                                      | 依赖 WebCrypto 与安全上下文；服务端无法提供解密/搜索密文能力                                                                                                                                                                                                                                                    |
| ADR-08 | PBKDF2-HMAC-SHA256 150k 迭代，而非 Argon2id                                                                                                                                                                             | WebCrypto 原生支持、零依赖、跨客户端易复现                                                                                                                                                                                                           | 抗 GPU 爆破弱于内存硬化 KDF；弱口令风险更高（PRD 风险 R4）                                                                                                                                                                                                                                                      |
| ADR-09 | 标题、分类名、时间、助记问题为明文                                                                                                                                                                                      | 支持列表、筛选、搜索与提示展示                                                                                                                                                                                                                       | 元数据泄露                                                                                                                                                                                                                                                                                                      |
| ADR-10 | `dist` 不提交，构建时生成                                                                                                                                                                                               | 避免产物与源码漂移，单二进制发布                                                                                                                                                                                                                     | 直接 `go build` 前必须先构建前端                                                                                                                                                                                                                                                                                |
| ADR-11 | 不提供取消加密；修改主密码自 v1.4 移出非目标（ADR-16）                                                                                                                                                                  | 取消加密会破坏「存储侧不可逆」的语义；改密不破坏它，因为明文始终只在浏览器内存里                                                                                                                                                                     | 仍需新建笔记才能把某条加密笔记换成明文                                                                                                                                                                                                                                                                          |
| ADR-12 | 错误口令与数据损坏统一为一句提示                                                                                                                                                                                        | 避免泄露解密细节                                                                                                                                                                                                                                     | 用户无法区分两者                                                                                                                                                                                                                                                                                                |
| ADR-13 | 未分类既存为 `category_id IS NULL`，又在 `categories` 里放一行 id=1「未分类」                                                                                                                                           | 老库无需回填即可工作；分类树与筛选有一致的展示锚点                                                                                                                                                                                                   | 两处表示同一含义，列表与计数必须把 NULL 归入默认分类（见 5.4，本轮修复的缺陷）                                                                                                                                                                                                                                  |
| ADR-14 | 默认分类的「不可改名、不可删除」守卫放在应用层，不写 SQL 触发器                                                                                                                                                         | 触发器无法向用户回传语义化错误码                                                                                                                                                                                                                     | 守卫只在经由应用的写入路径上生效，手工改库不受约束                                                                                                                                                                                                                                                              |
| ADR-15 | Markdown 渲染只用 `marked` + `dompurify`，`{@html}` 全仓唯一出口                                                                                                                                                        | 依赖面小、净化点单一、审计容易                                                                                                                                                                                                                       | 富文本能力受这两个库限制                                                                                                                                                                                                                                                                                        |
| ADR-16 | 改密用「浏览器逐条试解」当校验器与进度判据，不加 `key_version` 列、不做迁移状态机                                                                                                                                       | GCM 认证标签已经是可靠的口令校验器；试解同时给出「这条笔记当前由哪把口令保护」，天然幂等、可续跑，无需任何持久化状态与新迁移                                                                                                                         | 验证与规划各需一次派生与解密；清单里混着多代口令时，一次续跑只能推进一类，另一类如实列为失败                                                                                                                                                                                                                    |
| ADR-17 | 换口令只走 `PUT /api/notes/{id}/cipher` 独立端点，且不动 `updated_at`/`rev`、不换全局盐                                                                                                                                 | `UpdateNote` 无条件重写标题与更新时间，加 `reencrypt` 标志等于在一个校验函数里塞第二套语义；改一次口令不该造成列表顺序的假修订；换盐会让未迁移的笔记当场不可解                                                                                       | 多一个端点与一条 store 方法；密文替换的校验与 `normalizeBody` 必须保持同值（共用 `decodeCipher`）                                                                                                                                                                                                               |
| ADR-18 | 版本注入用 `-ldflags -X`，未注入时回落 `debug.ReadBuildInfo()` 的 vcs 字段                                                                                                                                              | `just build` 产物有真实 hash 与构建日期，`go run`/`go test` 也拿得到可用信息，信息页不会空白；不在仓库内构建时命令失败退化为空值，不阻断构建                                                                                                         | 两个来源的 hash 长度与 dirty 语义需对齐（`shortRevision` 收成 7 位）；just 在 Windows 只保留末行输出，分支写法受限                                                                                                                                                                                              |
| ADR-19 | 引导页两步、初始主密码必填，半态由「已配置则只解锁」分支兜住                                                                                                                                                            | 避免「想加密时才发现还没主密码」的断链；只按 `nameSet` 判断会让用户绕过必填                                                                                                                                                                          | 启动多读一个端点；步骤 2 有两种形态，需要端到端脚本用双页签才能确定性地复现                                                                                                                                                                                                                                     |
| ADR-20 | 后端 CLI 子命令化：bubbletea 只承担交互式命令（v1.9 起为 `note add`，此前的裸 `note` 菜单已删除），子命令/参数用标准库 flag（`cli.Parse` 手写全局层）；默认配置/数据位置迁到用户目录，旧位置不做隐式回退                | 服务生命周期留在 `cmd/note`，向导模型可纯单测；stdin 非终端时报错退出而非挂起；默认位置与其他开源项目一致，隐式回退会引入难以排查的双源配置                                                                                                          | 非终端环境验证不了 TUI 交互（winpty 在无尺寸控制台断言失败），真 TTY 行为依赖人工双击 exe 验证；迁移对老用户是破坏性变更，靠文档与 `-c/--db` 兼容旧文件                                                                                                                                                         |
| ADR-21 | 后台化用「自我 re-exec + 分离进程组」，不用 systemd/计划任务等外部守护                                                                                                                                                  | 单二进制零依赖是项目底线；前台与后台共用同一份服务代码，只是入口不同                                                                                                                                                                                 | Go 无 fork，只能重执行自身；子进程靠环境变量 `NOTE_DAEMON_STATE` 认领身份，日志由父进程 fd 接管                                                                                                                                                                                                                 |
| ADR-22 | 关停走业务同端口的 `POST /api/shutdown`，令牌由子进程生成、只存 `note.json`                                                                                                                                             | 不额外开控制端口或命名管道，Unix 与 Windows 行为一致；令牌随机且只对本地可读文件可见，CSRF 豁免局限在单一路径                                                                                                                                        | 令牌是文件形式的本地凭据；中间件为这一条路径开豁免，边界必须写进硬性约定                                                                                                                                                                                                                                        |
| ADR-23 | 日志与状态文件固定放在 `dir(DBPath)`，不新增配置项                                                                                                                                                                      | 与数据库同生命周期、同权限目录，备份/迁移只需处理一个目录；`note info` 与 `web status` 共用一个纯函数推导路径                                                                                                                                        | 同一目录只能有一个后台实例（按 `db` 字段校验归属）；日志不轮转，长期运行需用户自行清理                                                                                                                                                                                                                          |
| ADR-24 | `note add` 的多行正文用 `bubbles/textarea`，不自写编辑器；裸 `note` 从交互菜单改为打印帮助                                                                                                                              | `gum write` 用的就是同一套组件：软换行、CJK 宽度、粘贴、`$EDITOR` 外接都是现成的，自写等于重写一遍且必然有边角 bug；裸 `note` 打印帮助比只有三项的菜单更能让用户发现命令                                                                             | 多一个纯 Go 依赖 `bubbles`（间接带入 `clipboard` / `lipgloss`）；裸 `note` 是破坏性变更，迁移靠 help 与 CHANGELOG                                                                                                                                                                                               |
| ADR-24 | `status` 用「状态文件 + `/api/info` 探测」双保险，不只看 pid                                                                                                                                                            | 前台实例、残留状态文件、端口被占三种情况都能说清楚；Windows 上 `os.FindProcess` 对已退出进程仍成功，pid 存活必须用等待句柄判断                                                                                                                       | 多一个探测请求与 2s 超时；状态判定分在 `daemon.Probe` 与 `cli.FormatStatus` 两处                                                                                                                                                                                                                                |
| ADR-25 | CLI 渲染内嵌 `glamour`（glow 的渲染内核）而不是调外部 `glow` 命令；`note list` 与 `note add` 分类步共用 `bubbles/list` 包出的 `picker`；列表数据一次性全量读进内存                                                      | glow 本体是 `package main`，Go 里无法当库引用，glamour 是它真正用的那层；外部命令要求用户自己装 glow（Windows 未必有）且每次要落临时文件再 exec；逐键查库会经 `RecordSearch` 污染搜索历史，而 `ListNotes` 每页只有 20 条，过滤会退化成「只筛第一页」 | 新增 glamour 及其传递依赖（goldmark/chroma/bluemonday/reflow，均纯 Go），并把 lipgloss 抬到伪版本 `v1.1.1-0.20250404…`；`muesli/termenv` 因为测试里要强制色彩档位而从间接依赖变为直接依赖；全量载入的内存随库大小线性增长（个人库量级在 MB 以内）；`picker` 与 `bubbles` 的键位语义耦合，升依赖时需重跑键位用例 |
| ADR-26 | AI 调用一律由服务端代发（`internal/ai` 是唯一对外 HTTP 出口），不做浏览器直连：`api_key` 明文只允许存在于 `note.yaml`，浏览器与网页代码都不该看到它；CLI 复用同一份实现，因此 `note add` 不要求 `note web` 在运行       |
| ADR-27 | 优化结果是「建议」而不是「自动改写」：网页与 CLI 都先预览、用户确认后才替换，超 2000 码点禁止替换而不截断，失败/超时/改过正文都不动原文；`ai.prompt` 用 `{{content}}` 模板 + 内置默认提示词，避免空模板导致正文无处代入 |

---

## 17. 已确认的问题

PRD 第 13 节的开放问题在本轮已全部落定，技术方案按结论实现：

| 编号 | 问题                    | 结论                                                                           |
| ---- | ----------------------- | ------------------------------------------------------------------------------ |
| Q1   | 单向加密的语义          | 口令正确才能查看；无找回、无后门                                               |
| Q2   | 独立口令还是全局主密码  | 全局主密码，首次配置一次后在信息页可修改（v1.4），会话级解锁                   |
| Q3   | 元数据明文              | 接受：标题、分类、时间、助记问题明文，界面明确告知                             |
| Q4   | 解锁后搜索加密正文      | 不做；密文对服务端是黑盒                                                       |
| Q5   | 修改口令 / 导出加密备份 | 修改口令 v1.4 起做（需旧口令，浏览器端批量重加密，仍无找回）；导出加密备份不做 |
| Q6   | 应用启动口令            | 不做，仅回环监听                                                               |
| Q7   | 加解密放哪儿            | 浏览器端 WebCrypto，服务端零密码学代码，CLI 可直读库解密                       |

---

_文档结束。_
