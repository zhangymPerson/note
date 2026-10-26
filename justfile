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
