# 新建笔记的 Markdown 骨架（模板）设计

日期：2026-10-29
状态：待确认

## 意图与为什么

新建笔记现在是纯白板：正文框空着，用户得自己想着敲 `#`、`- `、三反引号，
也不知道这应用到底吃哪些 markdown 语法。改成：**新笔记的正文预填一份简单的
markdown 骨架**，骨架存在配置文件旁边（`note-example.md`），用户可以自己改，
改的入口就在新建笔记页上。

关键取舍（已与用户确认）：

- 骨架是**实打实写进正文**的预填内容，不是 placeholder；不需要就自己删。
- 骨架放在**文件系统**里（和 `note.yaml` 同一目录），不是数据库、不是常量：
  用户可以用任何编辑器改，也可以只靠页面改。
- 页面上的入口是**新建笔记页的一个链接 → 独立模板编辑页**，不是弹窗。

## 目标

- `note web` 启动时，若 `<配置文件目录>/note-example.md` 不存在，就把内置默认模板写过去；存在则**绝不覆盖**。
- 新建笔记页打开时，正文预填模板内容（有草稿时草稿优先）。
- 新建笔记页提供「编辑模板」入口，进入 `/template`：改模板、看渲染预览、保存、恢复默认。
- 模板改动立即对之后的新建笔记生效；已存在的笔记不受任何影响。

## 非目标

- 不做模板变量/占位符替换（`{{date}}` 之类），不做多套模板、不做按分类选模板。
- 不改数据库结构、不加迁移、模板内容不进 SQLite。
- 不改加密流程：模板是明文，勾了加密保存时照常走浏览器加密。
- 不加 `note template` CLI 子命令（终端编辑可以等以后）。
- 不改 `/api/info`、不改日志字段。

## 约束

- 模板会原样变成笔记正文，因此**长度上限就是正文上限**：2000 码点
  （`internal/web/handlers_notes.go` 的 `maxContentRunes`，前端 `MAX_CONTENT_CODEPOINTS`）。
  超限的模板会让"新建 → 保存"必然失败，所以 PUT 时就拒绝。
- 模板内容属于用户输入：前端只做文本插值与 `Markdown.svelte` 预览（走既有
  `DOMPurify` 管道），不得引入新的 `{@html}`。
- 写文件必须原子（临时文件 + `rename`），沿用 `config.SaveUserName` 的做法。
- `PUT /api/template` 是状态变更接口，照旧受 `csrfProtect` 双提交校验，不加豁免。
- 配置文件目录不可写时**不能让服务起不来**：启动只告警，GET 回退到内置默认值。

## 做法（已选 A1 + B1）

### 1. 文件怎么落地

| 方案           | 内容                                                                     | 取舍                                                                                 |
| -------------- | ------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| **A1（选它）** | `go:embed` 内置默认模板；`note web` 启动时缺文件就补一份；之后一律读磁盘 | 用户能在文件管理器里看见并直接改；缺点是启动会写用户目录（已有 `SaveUserName` 先例） |
| A2             | 懒初始化：不主动落盘，GET 时文件缺失就返回内置默认，只有用户保存才写     | 配置目录更干净，但"初始化时复制过去"的要求落空，用户找不到文件                       |
| A3             | `note template` CLI 子命令初始化/编辑                                    | 贴合"配置文件"精神，但用户要的是页面按钮，且要处理 `$EDITOR` 与终端差异              |

### 2. 按钮放哪

| 方案           | 内容                                                                                                  | 取舍                                                                                      |
| -------------- | ----------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| **B1（选它）** | 新建笔记页正文标签行加「编辑模板」→ 跳 `/template` 独立页（textarea + markdown 预览 + 保存/恢复默认） | 在最容易被发现的地方；预览复用 `Markdown.svelte`；跳转用既有 `editorDraft` 交棒，不丢草稿 |
| B2             | 编辑器内弹窗                                                                                          | 不换路由，但要新写 modal 且没地方放预览                                                   |
| B3             | 塞进信息页（姓名设置现在在那儿）                                                                      | 不打断编辑，但用户发现骨架不合心意时找不到入口                                            |

## 组件与接口

### 新包 `internal/template`

```go
Path(configPath string) string          // dir(configPath)/note-example.md，纯函数
Default() string                        // //go:embed default.md
Ensure(path string) (created bool, err error) // 仅缺失时写入 Default()；已存在则 no-op
Load(path string) (content string, fromDefault bool, err error) // 读失败回退 Default()；err 只用于日志，调用方照样返回 200
Save(path, content string) error        // 目录 0700 + 临时文件 + rename
```

- 默认模板内容是 `internal/template/default.md`（一个简单 markdown 笔记骨架：
  `#` 标题、要点列表、待办、引用、代码块）。
- 文件权限 `0644`：内容不是秘密（不含正文/密码），而且 0700 的配置目录本身就挡住了别人。

### HTTP

| 方法 | 路径            | 请求              | 响应                                                        |
| ---- | --------------- | ----------------- | ----------------------------------------------------------- |
| GET  | `/api/template` | 无                | 200 `{"content":"…","path":"…","source":"file"｜"default"}` |
| PUT  | `/api/template` | `{"content":"…"}` | 200 同上（`source` 恒为 `"file"`）                          |

- GET 在文件缺失或不可读时不报错：200 + `source:"default"`，并 `slog.Warn` 一行（不含内容）。
- 超过 2000 码点 → 400 `validation_failed`，`fields.content` = "模板请控制在 2000 字以内，当前 N 字"。
- 写失败（目录只读等）→ 500 `internal`，文案说"无法写入模板文件，请检查配置目录是否可写"。
- 空内容合法（等于"不预填"）。
- `path` 已在 GET 里出现，属于与 `/api/info` 的 `db_path` 同尺度的信息公开。

### 启动初始化

`cmd/note` 的 `serve(cfg)`（前台与后台子进程共用）在开库前调用
`template.Ensure(template.Path(cfg.ConfigPath))`：成功创建打一条 info 日志，
失败只 `slog.Warn` 不影响启动。

### 前端

- `lib/template.ts`：`getTemplate()` / `saveTemplate(content)` / `MAX_TEMPLATE_CODEPOINTS`，
  走既有 `lib/api.ts`（PUT 自动带 `X-CSRF-Token`）。
- `NoteEditorView.svelte`：新建且无草稿时拉一次模板，正文仍为空就填进去
  （`dirty` 保持 `false`——骨架不是用户写的东西，不该触发 `beforeunload` 提醒）；
  拉取失败静默降级为空正文，不能让编辑器打不开。
- 正文标签行右侧（计数器旁）加「编辑模板」链接，**只在新建时显示**：点击先把
  当前表单存进 `editorDraft`，再 `navigate('/template?next=/notes/new')`。
- 新页 `views/TemplateView.svelte`（路由 `/template`）：左 textarea、右 markdown 预览
  （300ms 防抖，与编辑器一致），顶部显示模板文件真实路径，
  底部「保存模板」「恢复默认」「返回」。恢复默认只把默认内容填进 textarea，
  **不直接落盘**——误点不该覆盖用户改过的文件；要保存再点保存。

## 要改的文件

1. **新增** `internal/template/template.go`、`internal/template/default.md`、`internal/template/template_test.go`
2. **新增** `internal/web/handlers_template.go`、`internal/web/template_api_test.go`
3. **改** `internal/web/server.go`（两条路由）、`internal/web/handlers_settings.go`
   （`application` 增加 `templatePath` 字段，`newApplication` 里由 `template.Path(cfg.ConfigPath)` 推导）
4. **改** `cmd/note/main.go`（`serve` 里调用 `Ensure`）
5. **新增** `web/src/lib/template.ts`、`web/src/lib/template.test.ts`
6. **新增** `web/src/views/TemplateView.svelte`
7. **改** `web/src/views/NoteEditorView.svelte`（预填 + 入口链接 + 交棒）
8. **改** `web/src/router/router.ts`（`template` 路由）、`web/src/router/router.test.ts`、`web/src/App.svelte`（标题与视图分支）
9. **改** `web/src/app.css`（模板页布局；尽量复用 `.editor-grid` / `.preview`）
10. **改** `docs/PRD.md`（API 表 + FR/AC 补充）、`docs/TECH_DESIGN.md`（4.2 文件位置、4.3 路由清单、8.3 端点表、目录树）
11. **改** `CHANGELOG.md`（`just test` 与 `just check` 通过后写 `[Unreleased]`）

## 测试

- `internal/template/template_test.go`：`Path` 纯函数；`Ensure` 首次创建、二次不覆盖
  （先写入自定义内容再 `Ensure`，内容不变）；`Load` 在文件缺失/不可读时回退默认且
  `fromDefault=true`；`Save` 原子替换后内容一致、权限正确；目录不可写时报错。
- `internal/web/template_api_test.go`：GET 在文件缺失时回退默认并标 `source=default`；
  PUT 后 GET 读到新内容且 `source=file`；超 2000 码点 400；无 CSRF 403；
  `path` 落在配置目录下且文件名为 `note-example.md`。
- 前端 `lib/template.test.ts`：GET/PUT 调用路径、错误映射（400 的 `fields.content` 透传）。
- 前端 `router.test.ts`：`/template` 映射。
- 手测：`note web` 首启生成文件 → 页面改模板 → 新建笔记看到新骨架 → 重启后模板不被覆盖。

## 留给实现者的判断

- 默认模板的具体文案与结构（保持"简单"，6–12 行即可，别做成说明书）。
- 模板页是否复用 `.editor-grid` 两栏比例，还是单独一套更窄的布局。
- 「编辑模板」链接的措辞与位置（正文标签行右侧最省事）。
- 是否需要组件级测试（`TemplateView.test.ts`）；现有前端测试以 `lib/` + 路由为主，
  组件测试要单开 mock，可选。
