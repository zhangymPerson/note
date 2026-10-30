# AI 美化笔记：网页按钮 + CLI 快捷键（设计）

日期：2026-10-30
状态：待批准（未开始实现）

## 1. 意图与为什么

新建一条笔记时，用户往往只把想到的东西按顺序敲下来——没有小标题、没有列表、段落糊成一团。把格式整理好是一件「机械但花时间」的活，正好适合交给模型做。

所以：在**新建笔记**的地方（网页编辑器的正文区、`note add` 的正文步）放一个入口，一键把当前正文交给已配置的 AI 按 Markdown 笔记的标准美化一遍，**结果先预览、用户确认后才替换**，原文在确认前一个字都不动。

这条需求把应用从「从不联网」推进到「用户主动触发时才联网」，因此隐私边界要在界面上说清楚，而不是藏在文档里。

## 2. 目标 / 非目标 / 反目标

**目标**

- G1 网页新建笔记的正文旁有按钮，CLI `note add` 正文步有快捷键，两处语义一致（同一个提示词模板、同一个模型选择规则）。
- G2 提示词是**模板**，用户笔记用变量 `{{content}}` 代入；内置一份默认模板，随时可恢复默认。
- G3 模型是**列表**（`ai.models`），默认用第一个；网页可临时选另一个（只影响本次请求）。
- G4 结果是预览，不是直接落笔：确认才替换正文，取消则原文原样。
- G5 失败只影响这次优化：正文不动、不阻塞保存、不改变退出码。

**非目标**

- 流式输出（`stream: true`）——本地单用户，等 5 秒不难受，先不做增量渲染。
- 多轮对话 / 追问 / 撤销重做历史。只保留「最近一次结果」。
- 自动优化（保存时顺手跑一遍）、定时批量优化、编辑旧笔记时的 AI 改写按钮。
- 服务端保存任何优化结果、历史、用量统计。
- 多模型自动 fallback 重试（见 §5.2，理由：重复计费 + 掩盖真实错误）。

**反目标**（技术上可用但等于失败）

- 正文或密钥出现在日志、`/api/info`、错误响应、`GET /api/ai-config`。
- 浏览器拿到明文 `api_key`（那会把「密钥只在 note.yaml」这条不变量拆掉）。
- 用户没点按钮就发生任何外部网络请求。
- 优化结果无声覆盖正文（没有「替换」这一步就是失败）。

## 3. 约束（沿用现有硬性约定）

- `api_key` 明文只允许在 `note.yaml`（文件 `0600`、目录 `0700`）；任何 API 响应、网页、日志只有掩码。→ 因此**只能服务端代发**。
- 配置写入只有 `internal/config.SetValues` 一个入口（`yaml.Node` 级改写保留注释 → 编码 → 严格复核 → 临时文件 + rename）。
- 含正文的响应 `Cache-Control: no-store`（`writeJSON` 已统一设置）。
- 状态变更类 API 必须过 CSRF 双提交。
- 正文上限 2000 码点、标题 200、分类名 30，一律 `utf8.RuneCountInString` 口径。
- `{@html}` 只允许在 `web/src/components/Markdown.svelte`，输入先过 DOMPurify。

## 4. 方案与取舍

**A. 服务端代发（采用）**：浏览器把正文 POST 给 Go，Go 用 `note.yaml` 里的密钥调 `{base_url}/chat/completions`，把结果回给浏览器预览。密钥不出机器、浏览器与上游之间不涉及 CORS、CLI 与网页能共用同一份客户端代码。

**B. 浏览器直连上游**（否）：`api_key` 必须下发到浏览器，直接违反安全不变量；还要处理 CORS，且在半数兼容网关上根本过不去。

**C. 只把「提示词 + 正文」复制到剪贴板**（否，用户已明确否掉）：零网络请求，但把「贴到别的工具、再贴回来」的手工步骤留给用户。

**D. CLI 走网页端点 vs CLI 直连**（选直连）：`note add` 不该要求 `note web` 在跑。两边都调同一个 `internal/ai` 包，只是入口不同（`cmd/note/add.go` 从 `cfg.AI` 造 client，web 从 `app.cfg` 现读磁盘后造 client）。

## 5. 设计决定

### 5.1 配置：`ai.models`（列表）与 `ai.prompt`（模板）

```yaml
ai:
  base_url: https://api.deepseek.com/v1
  api_key: sk-…
  models:
    - deepseek-chat
    - gpt-4o-mini
  prompt: |
    你是 Markdown 笔记专家。……
    【笔记原文】
    {{content}}
    【笔记原文结束】
```

- **`ai.models`**：字符串列表，1–10 项；每项 trim 后非空、无空白/控制字符、≤200 字节；重复项去除（保留首现顺序）。空列表 = 未配置。默认模型 = 列表第一项。
  - 命令行写法：`note config set ai.models=deepseek-chat,gpt-4o-mini`（逗号分隔，也接受换行）；`note config set ai.models=` 清空。
  - YAML 里写成 `!!seq`（`models: []` 表示清空）。`setNode` 需要新增「列表叶子」分支，其余（保留注释、原子写、严格复核）不变。
  - 归一化与拆分只有一个函数 `config.SplitModels(text string) []string`，命令行与 `PUT /api/ai-config` 共用（网页也传逗号分隔的单串），避免「CLI 写得进去、网页报错」。
- **`ai.prompt`**：**模板**，用户笔记在 `{{content}}` 处代入；空串表示使用内置默认模板（`config.DefaultAIPrompt`）。
  - 非空时**必须包含** `{{content}}`，否则视为用法错误：命令行退出 2、网页 400 字段级；错误文案直接给出恢复默认的办法（「要恢复默认提示词，把值留空」）。
  - 上限仍是 2000 码点（含占位符）。
  - **默认提示词**（`config.DefaultAIPrompt`，Go 侧唯一来源）：

    > 你是 Markdown 笔记专家。请美化下面这段笔记的 Markdown 格式，让它更好读、更有条理。
    >
    > 【笔记原文】
    > {{content}}
    > 【笔记原文结束】
    >
    > 要求：
    >
    > 1. 不改变核心内容：不删减要点、不编造事实、不改动原意与结论。
    > 2. 可以调整：标题层级、段落划分、列表与表格、代码块语言标注、加粗与斜体、标点与全半角、以及描述性的措辞。
    > 3. 目标是「扫一眼就能读懂」：每个小节有说清主题的标题，并列的信息改成列表，结论与关键事实放在前面。
    > 4. 只输出优化后的 Markdown 正文：不要解释、不要前言或结语，也不要用代码块把整篇包起来。

- **恢复默认**：`ai.prompt=` 留空即恢复（命令行）；网页 AI 卡片在提示词下方加「恢复默认提示词」按钮，点了把文本框填成服务端返回的 `default_prompt`（**仍需点保存才落盘**，避免误触改文件）。`GET /api/ai-config` 新增 `models []string`、`default_prompt`、`prompt_variable`（`{{content}}`）三个非敏感字段，前端不硬编码常量。
- **空值的展示**：`ai.prompt` 为空时网页文本框显示为空、placeholder 写「留空 = 使用默认提示词」；`note config list` 对空 `prompt` 显示 `（默认提示词）`、空 `models` 显示 `（未配置）`。
- **手写 YAML 的容错**：`models` 被写成标量（`models: gpt-4o`）时严格解析报错并指到该键，不做「自动升格成列表」的猜测；只有 `setNode` 写入时才把已存在的标量叶子替换成 `!!seq`。
- **`ai.prompt` 旧语义的处置**：v1.13 把空串定义为「不追加提示词」。本版把空串升格为「用默认模板」，语义变了但不影响已有的非空值——非空提示词现在要求带 `{{content}}`，不带的老配置会在保存/启动时报错并给出改法（启动校验只警告还是硬失败？**硬失败**，与现有 `ValidateAI` 一致：坏配置在启动时就该炸，而不是等用户点了按钮）。这一条要写进 PRD 偏离表。

### 5.2 新包 `internal/ai`：唯一的外部网络出口

```go
type Client struct {
    BaseURL string
    APIKey  string
    Models  []string
    Prompt  string        // 空 = config.DefaultAIPrompt
    HTTP    *http.Client  // 默认 60s 超时，可注入测试
}

func (c Client) Configured() bool                     // base_url / api_key / models 都齐
func (c Client) DefaultModel() string                 // 列表第一项
func (c Client) Rendering() (host, model string, ok bool) // 给 CLI/网页显示「发送到哪」
func (c Client) Optimize(ctx context.Context, content, model string) (Result, error)
type Result struct{ Content, Model string }
```

- **提示词渲染** `BuildPrompt(tpl, content)`：`tpl` 为空用默认；`strings.ReplaceAll(tpl, "{{content}}", content)`。只对模板做一次替换，正文里若恰好含 `{{content}}` 不会被二次展开（`ReplaceAll` 不会重扫替换结果）。
- **请求**：`POST {base_url 去尾斜杠}/chat/completions`，头 `Authorization: Bearer <key>`、`Content-Type: application/json`，体 `{model, messages:[{role:"user",content:<渲染后的提示词>}], temperature:0.2, stream:false}`。示例端点都用单条 user 消息，不引入 system 角色差异（各家对 system 的支持并不一致）。
- **响应**：读 `choices[0].message.content`（字符串形态）；`Content-Type` 非 JSON 或结构不认识 → 报「上游返回格式不认识」。`io.LimitReader` 限 1 MiB。
- **后处理** `StripFence`：整段被单个 ``` / ~~~ 围栏包住时剥掉（模型有惯性），再 `strings.TrimRight` 掉尾部空白；其余原样。
- **错误**：`ErrNotConfigured`（缺哪几项）、`ErrModelNotConfigured`（请求的模型不在列表里）、`ErrUpstream{Status, Message}`（上游非 2xx；`Message` 取上游 `error.message` 截断 200 字，便于自查，不含密钥）、`ErrBadResponse`、`ErrTooLarge`。
- **不重试、不 fallback 到下一个模型**：多模型列表是「可选的模型」，不是「失败自动换」。自动换会重复计费并把真正的问题（地址错、密钥失效）藏起来；换模型是用户的显式选择（网页下拉）。
- **日志**：只记 `model`、`host`、`status`、`duration_ms`、错误文本；**不记正文、不记提示词渲染结果、不记密钥**。
- 全仓库只有这个包可以发起对外 HTTP 请求，这条要写进 AGENTS.md。

### 5.3 端点 `POST /api/ai/optimize`

请求 `{ "content": "...", "model": "可选，默认列表第一个" }`，响应 `{ "content": "...", "model": "实际使用的模型" }`。

| 情形                                 | 状态码 | code                                             |
| ------------------------------------ | ------ | ------------------------------------------------ |
| 成功                                 | 200    | —（`writeJSON` 自带 `no-store`）                 |
| 正文为空 / 超 2000 码点              | 400    | `validation_failed`，字段 `content`              |
| `model` 不在 `ai.models` 里          | 400    | `validation_failed`，字段 `model`                |
| 缺 `base_url` / `api_key` / `models` | 409    | `ai_not_configured`（消息列出缺哪几项 + 怎么补） |
| 上游非 2xx                           | 502    | `ai_upstream_failed`（带截断后的上游原因）       |
| 超时 / 取消                          | 504    | `ai_timeout`                                     |
| 未带 CSRF                            | 403    | 现有中间件行为，不新增豁免                       |

- 请求体上限 64 KiB（正文 2000 码点 ≈ 8 KiB，留足余量）。
- 每次请求**现读** `note.yaml` 的 AI 四项（与 `GET /api/ai-config` 同样的「改完刷新即生效」口径），不缓存 client 到 `application` 结构里。
- 服务端不写库、不写文件、不记录结果。
- 响应里的正文同样受 2000 码点约束吗？**不裁剪**：如实回给前端，由前端标注「超长」并禁用「替换正文」（悄悄截断等于偷偷改用户的笔记）。

### 5.4 网页（`NoteEditorView`）

- 前端 `web/src/lib/api.ts` 新增 `postJSON`（复用现有 `sendJSON`：首次先取 CSRF，403 时刷新令牌重试一次）；网络请求与响应类型放在新文件 `web/src/lib/ai.ts`（`optimizeNote`、`defaultModel`、`hostOf`）。
- 「正文」标签右侧那排（现有「编辑模板」「字数」旁）加按钮 **`用 AI 优化格式`**；新建与编辑两种模式都显示（同一个组件，编辑旧笔记时同样有用——这是本人的判断，如只想在新建时出现，删一个 `{#if}` 即可）。
- 旁边一个 **模型下拉**，选项来自 `GET /api/ai-config` 的 `models`，默认选中第一项，切换只影响本次请求（不写回配置）。只有一个模型时不渲染下拉。
- 未配置（`api_key_set` / `base_url` / `models` 任一为空）时按钮 `disabled`，旁边一行提示：「还没配置 AI —— 去信息页配置地址、密钥、模型」并链接到 `/info`。
- 按钮下方常驻一行灰字披露数据去向：`正文会发送到 {host}`（`host` 由 `base_url` 解析）；加密笔记多一句：`这条笔记是加密保存的，优化会把明文发出去`。
- 编辑器进入页面时（与分类、主密码、模板并列）额外加载一次 `GET /api/ai-config`，只留四个状态：`aiModels: string[]`、`aiConfigured`、`aiHost`、`aiModel`（下拉当前值，默认第一项）；加载失败只让按钮不可用并说明原因，编辑器其余部分照常可用。
- 按钮在 `content.trim() === ''` 时也禁用（请求空正文没有意义）。
- 点击后：按钮文案「优化中…」并禁用（模型下拉同时禁用），记下 `requestContent = content`。成功后展开结果面板：
  - 标题「AI 优化结果 · {model}」+ 字数 `n / 2000`（超限标红）；
  - 用 `Markdown.svelte` 渲染结果（已净化）；
  - 按钮「替换正文」「放弃」；
  - 若 `content !== requestContent` → 「替换正文」禁用并提示「正文在优化期间改过了，请重新优化」（防止用旧结果覆盖新输入）；
  - 结果超 2000 码点 → 「替换正文」禁用并提示「结果太长，保存不了；可重试或手动节选」。
- 「替换正文」：`content = result`、`dirty = true`、收起面板（正文进预览区走已有的 300ms 防抖）。「放弃」：只收起面板。
- 失败：面板换成一则错误（`ApiRequestError.message`）+「重新优化」按钮；正文不动。
- 刷新/离开页面结果即丢：不发任何保存请求。

### 5.5 CLI（`note add` 正文步）

- 正文步新增快捷键 **`Ctrl+O`**（`Ctrl+E` 被 `$EDITOR` 占了，`Ctrl+T` 被 textarea 的字符换位占，`Ctrl+O` 实测空闲）；help 行加一项 `{"Ctrl+O", "用 AI 优化"}`。
- 未配置（`AddDeps.Optimize == nil`）时按 `Ctrl+O` 只回一句 status：「还没配置 AI：note config set ai.base_url=/ai.api_key=/ai.models=」，不退出、不动正文。
- 已配置：status 显示「正在用 {model} 优化…（正文会发送到 {host}）」，期间**锁住正文编辑**与 `Ctrl+D`/`Ctrl+O`（避免并发请求与「发出去的版本 ≠ 屏幕上的版本」），光标停闪；请求在 `tea.Cmd` 里跑（`context.Background()`，超时由 client 的 60s 负责），不阻塞 UI。
- 成功 → 进入新步骤 `stepOptimized`：显示模型名、原字数 → 结果字数，结果用 `internal/markdown`（glamour）按当前宽度渲染**一次**并缓存到 `aiRendered`，放进 `bubbles/viewport`（`↑/↓`、`PgUp/PgDn` 滚动，超长结果不至于被裁掉一半）；底部键位 `Enter 采用 · Esc 保留原文 · Ctrl+O 重新优化`。
  - 新增模型字段：`aiBusy bool`、`aiResult string`、`aiModel string`、`aiRendered string`、`aiRequestContent string`；回执消息 `optimizeFinishedMsg{Content, Model string, Err error}`，`internal/cli` 用自己定义的 `OptimizeResult{Content, Model string}`，仍不 import `internal/ai`。
  - `Enter`：结果 ≤2000 码点才写入 `textarea`（光标置尾），回到正文步，status「已采用 AI 优化结果，Ctrl+D 保存」；超限则不采用，留在预览并提示。
  - `Esc`：回正文步，原文完全不变。
  - `Ctrl+O`：留在预览步重新请求。
- 失败：回正文步，status 显示错误（含上游截断原因或「配置不完整」），原文不变。
- 装配：`cmd/note/add.go` 用 `cfg.AI` 造 `ai.Client`，未配置则 `Optimize: nil`，并注入 `AIModel` / `AIHost` 两个展示字符串；`internal/cli` 依旧不 import `internal/config`、`internal/ai`（沿用 `AddDeps` 的依赖注入风格，测试注入假实现）。
- 配置文件本身坏掉时（例如提示词缺 `{{content}}`）`loadConfig` 会先失败，`note add` 与 `note web` 一并在启动时报错退出——与现有「坏 `base_url` 启动即报错」口径一致，`Ctrl+O` 只负责「没配置」这种合法状态。
- AI 的任何失败都不改变退出码（`Esc` 放弃仍是 130，成功仍是 0）。

### 5.6 安全与隐私（要写进 TECH_DESIGN 1.2 与 AGENTS.md）

1. 对外网络请求只有 `internal/ai` 一处；正文只在用户按下按钮/快捷键时发出。
2. 正文、渲染后的提示词、`api_key` 一律不进日志；日志只有 `model`/`host`/`status`/耗时/错误摘要。
3. 响应含正文 → `no-store`（`writeJSON` 已统一设置）。
4. 服务端不落盘优化结果；浏览器不把结果存 localStorage（只有用户点「替换」后进编辑器草稿）。
5. 加密笔记的正文是明文外发这件事，必须在触发点（按钮旁 / CLI status）明示目标主机。
6. 上游地址只来自 `note.yaml`（用户自己的机器），仍只允许 http(s) 绝对地址；不新增任何地址/代理参数。

### 5.7 测试

**Go**

- `internal/config`：`SplitModels` 拆分（逗号、换行、空项、重复、11 项拒绝、超 200 字节拒绝、空白拒绝）；`SetValues` 写 `ai.models` 为 `!!seq`、保留其它键与注释、空值写 `[]`、写后严格复核；`ValidatePrompt` 非空缺 `{{content}}` 报错、空串合法；`DefaultAIPrompt` 含占位符且 ≤2000 码点；启动校验坏配置失败。
- `internal/ai`（`httptest`）：路径拼接（尾斜杠不多出 `//`）、`Authorization` 头、默认模型/显式模型、请求体里是渲染后的提示词、正文含 `{{content}}` 不被二次展开、`StripFence` 三种形态、非 2xx → `ErrUpstream`（状态 + 截断消息）、超时、超大响应、`choices` 为空、未配置、模型不在列表。
- `internal/web`：表驱动覆盖 §5.3 每一行；断言响应里没有密钥、`Cache-Control: no-store`、无 CSRF 403、上游错误不把 key 带进响应/日志。
- `internal/cli`：`Ctrl+O` 未配置只出 status；成功进预览步、`Enter` 改正文、`Esc` 原文不变、`Ctrl+O` 重发；pending 期间 `Ctrl+D` 被忽略；超限不采用。

**前端（Vitest）**

- `lib/ai.ts`：`optimizeNote`、`defaultModel`、`hostOf`。
- `NoteEditorView.test.ts`：未配置时按钮禁用并给链接；点击后出现结果面板；「替换正文」写入正文并置 `dirty`；正文改动后按钮禁用；超长禁用；失败显示错误且正文不变；只有一个模型时不渲染下拉。
- `InfoView.test.ts`：`models` 输入框回填与保存（逗号串）、提示词下方「恢复默认提示词」填入 `default_prompt`、缺 `{{content}}` 标红、`prompt_variable` 提示文案。

### 5.8 文档与产物

- `docs/PRD.md`：版本 v1.14，新增 5.9「AI 美化笔记」（FR-7.5 ~ FR-7.9）、路由表补 `POST /api/ai/optimize`、`/api/ai-config` 字段变更、AC-60、里程碑 M13、偏离表补一行（`ai.prompt` 空串语义变更）。
- `docs/TECH_DESIGN.md`：目录树加 `internal/ai`、AI 四项与模板渲染、端点表、网页/CLI 段落、1.2 安全不变量加两条、测试矩阵。
- `AGENTS.md`：硬性约定加两条（唯一网络出口；提示词模板变量与恢复默认的唯一来源）。
- `CHANGELOG.md` `[Unreleased]`：按既有密度记录实现过程、关键决策、验证结果。

### 5.9 验收（人工冒烟）

1. 未配置 AI → 网页按钮禁用且链到信息页；`note add` 按 `Ctrl+O` 只出提示。
2. 配好（`base_url` 指向本地假上游 `httptest`/`python` 假服务、`models=a,b`、提示词留空→默认模板）：网页点按钮 → 面板出现、字数正确、替换后正文变化且可保存；下拉选 `b` 时请求体 `model == "b"`。
3. `ai.prompt` 改成带 `{{content}}` 的自定义模板 → 假上游收到的消息是替换后的文本；改成不带变量 → 保存 400、`note config set` 退出 2。
4. 网页点「恢复默认提示词」→ 文本域变成默认模板，保存后 `GET /api/ai-config` 的 `prompt` 等于默认模板。
5. 上游 500 / 超时 → 网页显示错误且正文不变；CLI status 显示错误且原文不变。
6. 检索：`.db`、`note.log`、浏览器 console、响应体里搜不到 `api_key` 明文与正文；响应头有 `no-store`。
7. `note add` 真终端：`Ctrl+O` → 预览屏 → `Enter` 采用 → `Ctrl+D` 保存，库里正文是优化后的版本。

## 6. 留给实现者的

- 上游兼容性只做「OpenAI 经典形态」（`choices[0].message.content` 为字符串）；遇到数组形态（部分新接口）报「上游返回格式不认识」，不做多形态猜测。
- 何时该把结果直接贴进编辑器而不进预览：不做。
- 网页按钮在编辑旧笔记时也显示（§5.4 的判断）；若产品上想收窄成「仅新建」，删一个条件即可。
- 提示词模板除了 `{{content}}` 不引入别的变量（日期、标题、分类）；将来要加时按同一处 `BuildPrompt` 扩展。

## 7. 我替用户做的决定（请在批准时一并核对）

1. 提示词空串 = 用默认模板（旧语义「不追加」废弃），非空必须含 `{{content}}`。
2. 默认提示词就是上面那段（内置常量，不是写进 `note.yaml` 的初始值）——`note.yaml` 里保持空，配置文件干净。
3. 模型失败不自动换下一个；换模型是网页下拉里的显式选择。
4. 网页按钮同时出现在新建与编辑两种模式。
5. 结果超 2000 码点不裁剪，交给用户节选。
6. 端点名字 `POST /api/ai/optimize`（而不是 `/api/notes/optimize`），因为它与具体笔记无关、也不写库。
