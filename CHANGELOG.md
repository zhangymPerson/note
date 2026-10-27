# Changelog

本项目所有值得关注的变更都记录在此文件中。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。只记录影响需求、架构、构建或使用方式的关键变更，不记录日常格式化与零散改动。

## [Unreleased]

### Added

- `just pull` / `just push` 多远程同步任务：
  - `pull` 先 `git fetch --all --prune` 再 `git pull --rebase`；`push` 复用同样的 fetch + rebase 前置，rebase 有冲突时该行非零退出、just 立即中止，不会带着冲突推送。
  - 推送遍历 `git remote` 的全部远程（本仓库为 gitcode/gitee/origin 三个），Windows 用 PowerShell `ForEach-Object`、Unix 用 `xargs -n1`，按 `os()` 分发。
  - 验证：`just --dry-run pull|push` 展开命令正确；`just test`（Go 22 项 + 前端 11 项）与 `just check` 通过，未实际执行网络推送。
- `just start` / `just stop` 后台开发任务（跨平台脚本，实现逻辑与关键决策）：
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

- 技术方案补充配置目录、数据库自动检测与迁移流程、`sql/` 迁移目录约定。
- 前端构建输出重定向到 `internal/web/dist`，由 `go:embed` 嵌入，部署不再需要 Node/npm。
- Go 模块版本提升到 1.26，SQLite 使用纯 Go 驱动，构建全程关闭 CGO。

### Fixed

- Windows 构建输出改为 `note.exe`，避免无扩展名的二进制无法在 Nushell、PowerShell 中直接执行。

### Security

- 默认只监听回环地址，绑定非回环地址必须显式 `--allow-remote` 并输出警告。
- 数据库文件权限收紧为 `0600`，启用 WAL、`secure_delete` 与 `quick_check`。
- 统一安全响应头、Host 白名单、CSRF Cookie，API 错误响应禁止缓存。
