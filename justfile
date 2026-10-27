# https://just.systems

set unstable
# Unix/macOS 使用 bash；Windows 由 windows-shell 覆盖，避免硬编码 Git 安装路径
set shell := ["bash", "-uc"]
set windows-shell := ["powershell.exe", "-NoLogo", "-Command"]

# SQLite 驱动为纯 Go 实现，前端与后端构建都不需要 CGO
export CGO_ENABLED := "0"

# Windows 必须有 .exe 扩展名才能直接执行
binary := if os() == "windows" { "note.exe" } else { "note" }

default:
    just --list

# 按 package-lock.json 安装前端依赖
install:
    npm --prefix web ci

# 构建前端产物到 internal/web/dist，供 go:embed 使用
frontend: install
    npm --prefix web run build

# 后台启动前后端开发服务：go run 后端(:8080) + vite dev 前端(:5173)，日志写入 .run/
# 子进程经 WMI 创建，不继承 just 的管道句柄，因此 start 能立即返回
start:
    {{ if os() == "windows" { "powershell.exe -NoLogo -ExecutionPolicy Bypass -File scripts/dev.ps1 start" } else { "bash scripts/dev.sh start" } }}

# 停止 start 启动的后台前后端（按 .run/*.pid 杀进程树）
stop:
    {{ if os() == "windows" { "powershell.exe -NoLogo -ExecutionPolicy Bypass -File scripts/dev.ps1 stop" } else { "bash scripts/dev.sh stop" } }}

# 拉取所有远程并变基到远端最新提交；有冲突则中止（保留 rebase 现场手工解决）
pull:
    git fetch --all --prune
    git pull --rebase

# 推送到所有远程：先像 pull 一样 fetch + rebase，有冲突立即停止，再逐个远程推送
push:
    git fetch --all --prune
    git pull --rebase
    {{ if os() == "windows" { "git remote | ForEach-Object { git push $_ }" } else { "git remote | xargs -n1 git push" } }}

# 构建单二进制
build: frontend
    go build -trimpath -ldflags "-s -w" -o {{binary}} ./cmd/note

# 运行后端与前端测试
test: frontend
    go test ./...
    npm --prefix web test

# Go 与前端类型检查
check:
    go vet ./...
    npm --prefix web run check

# 格式化 Go 与前端/文档文件
format:
    go fmt ./...
    npx --yes --registry https://registry.npmmirror.com prettier --write --list-different .
