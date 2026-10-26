# Changelog

本项目所有值得关注的变更都记录在此文件中。

格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。只记录影响需求、架构、构建或使用方式的关键变更，不记录日常格式化与零散改动。

## [Unreleased]

### Added

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
