# AGENTS.md

本文件为在本仓库工作的 AI 编码代理提供项目说明与开发规则。

## 项目概览

个人本地笔记应用（单用户、单机、无登录体系）：

- **后端**：Go 单二进制（`net/http` + `ServeMux`，无框架），SQLite 持久化（纯 Go 驱动 `modernc.org/sqlite`，全程 CGO 关闭）。
- **前端**：Vite + Svelte 5 + TypeScript 的 History 路由 SPA，构建产物输出到 `internal/web/dist`，由 `go:embed` 内嵌；不使用 SvelteKit、UI 组件库、CSS 框架或状态管理库。
- **部署形态**：运行时只有一个可执行文件 + 一个 SQLite 数据文件 + 一个 YAML 配置（`config/note.yaml`），默认只监听 `127.0.0.1:8080`。

需求与架构细节以 `docs/PRD.md` 与 `docs/TECH_DESIGN.md` 为准，改动行为前先读这两份文档。

## 目录结构

```text
cmd/note/          入口与优雅退出
internal/config/   三级配置加载（内置默认值 → YAML → 命令行），严格解析
internal/store/    SQLite 打开、迁移与应用数据访问
internal/web/      HTTP 处理器、中间件、内嵌前端
sql/               编号迁移脚本（NNNN_name.sql，按 user_version 增量执行）
web/src/           Svelte 5 SPA（router、views、components、lib）
docs/              PRD、技术方案、构建指南
```

## 常用命令（just）

- `just install` / `just frontend` / `just build`：前端依赖、前端产物、单二进制（Windows 输出 `note.exe`）
- `just test`：`go test ./...` + 前端 Vitest
- `just check`：`go vet` + `svelte-check`/`tsc`
- 改动合入前必须 `just test` 与 `just check` 全部通过。

## 硬性约定

- 时间戳统一为 UTC 秒级文本 `2006-01-02T15:04:05Z`，可直接按字符串排序。
- 数据库结构变更只能新增 `sql/NNNN_*.sql` 迁移，禁止手改既有迁移。
- 状态变更类 API（PUT/POST/DELETE）必须通过 CSRF 双提交校验；前端经 `web/src/lib/api.ts` 统一携带 `X-CSRF-Token`。
- 安全不变量（见 TECH_DESIGN 1.2）：密码不落盘；日志不含正文/密码/Cookie；前端禁止 `{@html}`；解密与含正文响应 `Cache-Control: no-store`。
- 配置文件写入必须原子（临时文件 + rename），且保持 YAML 严格解析（未知键报错）。

## 开发流程规则

1. 每完成一部分开发，无论是否提交 git，都必须把实现过程与主要步骤逻辑（做了什么、改了哪些模块、关键设计决策与数据流）总结为条目，记录到 `CHANGELOG.md` 的 `[Unreleased]` 小节（按 Added / Changed / Fixed / Security 分类，Keep a Changelog 格式）。
2. 记录只写已验证的事实：先跑 `just test` 与 `just check`，通过后再写条目；测试未通过的状态不得写成"已完成"。
3. 不提交 git 的阶段性改动，同样适用规则 1，以便下次继续（人或 AI）能从 CHANGELOG 恢复上下文。
