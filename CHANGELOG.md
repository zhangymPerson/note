# Changelog

本项目所有值得关注的变更都记录在此文件中。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。只记录影响需求、架构、构建或使用方式的关键变更，不记录日常格式化与零散改动。

## [Unreleased]

### Added

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
