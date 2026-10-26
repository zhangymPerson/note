# 构建指南

本文记录从空仓库到可运行单二进制的完整构建过程，全部步骤均可用命令行复现。`just` 只是命令聚合，任何一步都可以去掉 `just` 直接执行。

## 目标与原则

- **命令行优先**：脚手架、依赖安装、构建、测试都由 CLI 完成，不依赖 IDE 或图形化工具。
- **成熟开源方案**：Go 标准库、SQLite（`modernc.org/sqlite`）、Vite、Svelte 5，均为活跃维护的主流项目。
- **单二进制交付**：前端产物通过 `go:embed` 嵌入 Go 二进制，运行机只需要一个可执行文件。
- **无 CGO**：SQLite 使用纯 Go 驱动，交叉编译和 CI 不需要 C 工具链。

## 技术选型

| 用途      | 方案                          | 说明                                     |
| --------- | ----------------------------- | ---------------------------------------- |
| HTTP 服务 | Go `net/http` + `ServeMux`    | 标准库按方法注册路由，无第三方框架       |
| 数据库    | SQLite + `modernc.org/sqlite` | 纯 Go、无 CGO，WAL 模式                  |
| 配置      | YAML + `gopkg.in/yaml.v3`     | 严格字段校验，未知字段直接报错           |
| 前端      | Vite + Svelte 5 + TypeScript  | 不使用 SvelteKit，保持 SPA + 同源 API    |
| 测试      | Go `testing` + Vitest         | 前后端各自覆盖                           |
| 命令编排  | `just`                        | 跨平台 shell 配置见 `justfile`           |
| 格式化    | `go fmt` + Prettier           | Prettier 通过 `npx` 按需运行，不写入依赖 |

版本以 `go.mod`、`web/package.json` 为准：Go 1.26、Vite 8、Svelte 5、TypeScript 6、Vitest 5。

## 环境要求

| 工具    | 版本                         | 用途               |
| ------- | ---------------------------- | ------------------ |
| Go      | 1.26 及以上                  | 编译后端与单二进制 |
| Node.js | 22 LTS 及以上（建议 24 LTS） | 构建前端           |
| npm     | 随 Node 安装                 | 安装前端依赖       |
| `just`  | 1.0 及以上（可选）           | 聚合构建命令       |

检查环境：

```bash
go version
node --version
npm --version
just --version   # 可选
```

## 从零初始化

以下命令即本项目基座的生成过程，按顺序执行即可复现。

### 1. 生成前端脚手架

使用 Vite 官方脚手架创建 Svelte + TypeScript 工程：

```bash
npx --yes create-vite@latest web --template svelte-ts
cd web
npm install
cd ..
```

生成的 `web/` 与后端完全独立：Node/npm 只参与开发与构建，不进入运行时。

### 2. 初始化 Go 模块

在仓库根目录执行：

```bash
go mod init note
go get modernc.org/sqlite@v1.61.0
go get gopkg.in/yaml.v3@v3.0.1
go mod tidy
```

`modernc.org/sqlite` 是纯 Go 的 SQLite 移植，避免 CGO；`yaml.v3` 负责读取 `config/note.yaml`。`go mod tidy` 后确认 `go.mod` 中的 Go 版本与实际依赖要求一致（当前为 1.26）。

### 3. 建立后端目录

```bash
mkdir -p cmd/note config sql internal/config internal/store internal/web
```

目录职责：

| 路径               | 职责                                                     |
| ------------------ | -------------------------------------------------------- |
| `cmd/note/`        | 程序入口：加载配置、打开数据库、启动 HTTP 服务、优雅退出 |
| `config/`          | `note.yaml` 默认配置                                     |
| `sql/`             | 建表与迁移脚本，通过 `embed.FS` 编译进二进制             |
| `internal/config/` | 默认值、YAML、命令行参数的合并与校验                     |
| `internal/store/`  | SQLite 连接、PRAGMA、迁移执行、数据库自检                |
| `internal/web/`    | 路由、中间件、CSRF、静态资源与 SPA 回退                  |

### 4. 打通前端产物与 Go 嵌入

Vite 默认输出到 `web/dist`，本项目改为输出到 `internal/web/dist`，再由 Go 嵌入：

```ts
// web/vite.config.ts
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
        changeOrigin: false,
      },
    },
  },
});
```

```go
// internal/web/assets.go
//go:embed all:dist
var distFS embed.FS
```

`all:` 前缀确保以 `.` 开头的文件也会被嵌入。`internal/web/dist` 是构建产物，已被 `.gitignore` 忽略；**首次 `go build` 前必须先构建前端**，否则 `go:embed` 会因目录缺失而失败。

### 5. 配置加载顺序

配置按以下优先级合并，后者覆盖前者：

1. 内置默认值（`127.0.0.1:8080`、`./data/notes.db`）
2. `config/note.yaml`
3. 命令行参数 `--config`、`--addr`、`--db`、`--allow-remote`

YAML 使用 `KnownFields(true)` 严格解析，拼错字段会直接启动失败。默认配置文件不存在时只打印警告；通过 `--config` 显式指定却不存在时视为错误。

### 6. 数据库自动初始化

启动时 `internal/store` 完成以下动作，无需手动建库：

1. 创建数据库所在目录（权限 `0700`）。
2. 以 WAL、`synchronous=NORMAL`、`secure_delete=ON`、`busy_timeout=5000` 打开 SQLite。
3. 对已有库执行 `PRAGMA quick_check`。
4. 读取 `PRAGMA user_version`，按 `sql/*.sql` 的数字前缀顺序在事务中执行未应用的迁移，并更新 `user_version`。
5. 数据库版本高于程序支持的版本时拒绝启动，避免旧程序破坏新结构。
6. 退出时执行 WAL checkpoint，并把数据库文件权限收紧为 `0600`。

## 日常命令

推荐使用 `just`：

```bash
just install    # npm ci，安装前端依赖
just frontend   # 构建前端到 internal/web/dist
just build      # 前端 + Go 单二进制
just test       # Go 测试 + Vitest
just check      # go vet + svelte-check/tsc
just format     # go fmt + Prettier
```

不使用 `just` 的等价命令：

```bash
# 构建
npm --prefix web ci
npm --prefix web run build
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o note ./cmd/note

# 测试与检查
go test ./...
npm --prefix web test
go vet ./...
npm --prefix web run check

# 格式化
go fmt ./...
npx --yes --registry https://registry.npmmirror.com prettier --write --list-different .
```

Windows PowerShell 下把 `CGO_ENABLED=0 go build ...` 写成：

```powershell
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w" -o note ./cmd/note
```

`justfile` 通过 `set windows-shell` 自动切换 PowerShell，因此 `just build` 在 Windows 上无需额外设置。

## 构建产物

执行 `just build` 后得到：

```text
note            # Linux/macOS 可执行文件；Windows 为 note.exe
```

前端 HTML、JS、CSS 已嵌入二进制。部署时只需要：

```text
note
config/note.yaml   # 可选，不存在则使用默认值
```

首次运行会在当前目录创建 `data/notes.db`。

## 运行

```bash
./note
# 或指定参数
./note --addr 127.0.0.1:9000 --db ./data/notes.db
```

打开 <http://127.0.0.1:8080>。默认只监听回环地址；绑定非回环地址必须显式加 `--allow-remote`，程序会打印安全警告。

### 前后端分离开发

Go 编译依赖 `internal/web/dist`，所以先构建一次前端，再开两个终端：

```bash
# 终端 1：后端
go run ./cmd/note

# 终端 2：Vite 开发服务器，/api 代理到 127.0.0.1:8080
npm --prefix web run dev
```

日常改前端时访问 Vite 输出的地址；需要验证嵌入产物时再执行 `just frontend` 并重启后端。

## 验证清单

构建完成后至少检查以下项目：

```bash
# 后端
go fmt ./...
go vet ./...
go test ./...
go test -race ./...

# 前端
npm --prefix web run check
npm --prefix web test
npm --prefix web run build
```

运行时手工验证：

| 检查项                | 预期                               |
| --------------------- | ---------------------------------- |
| `GET /`               | 返回 SPA 页面与安全响应头          |
| `GET /notes/demo`     | 返回 SPA 页面（History 路由回退）  |
| `GET /assets/<file>`  | 返回静态资源，带长缓存头           |
| `GET /api/unknown`    | 返回 JSON `{"error":{...}}` 与 404 |
| `GET /api/csrf`       | 下发 `csrf` Cookie 并返回 token    |
| 非法 `Host` 请求      | 被拒绝                             |
| 首次启动              | 自动创建 `data/notes.db`           |
| `PRAGMA user_version` | 等于已执行迁移数量                 |

## 常见问题

**`pattern all:dist: no matching files found`**

尚未构建前端。先执行 `just frontend` 或 `npm --prefix web run build`。

**前端类型检查通过但构建失败**

`web/package.json` 的 `build` 脚本已串联 `npm run check`，失败时按 `svelte-check` 输出修复类型错误。

**Windows 上 `just` 报命令不存在或语法错误**

`justfile` 顶部同时声明了 `set shell`（Unix）和 `set windows-shell`（PowerShell）。新增 recipe 时不要写 `VAR=value cmd`、`cd dir && cmd` 这类 bash 专有语法，改用 `export` 设置变量、`npm --prefix` 指定目录。

**数据库版本高于程序**

程序拒绝启动是预期行为：数据库由更新版本的程序迁移过，需升级程序而不是降级数据库。

## 依赖升级

```bash
# Go
go get -u ./...
go mod tidy
go test ./...

# 前端
npm --prefix web update
npm --prefix web run build
npm --prefix web test
```

升级后同步更新 `web/package.json` 中的版本说明，并在 `CHANGELOG.md` 记录影响构建或运行方式的变化。
