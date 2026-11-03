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
install: format
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

# 重启后台开发服务：先 stop 再 start（依赖按顺序执行）
restart: stop start

# 拉取所有远程并变基到远端最新提交；有冲突则中止（保留 rebase 现场手工解决）
pull:
    git fetch --all --prune
    git pull --rebase

# 推送到所有远程：先像 pull 一样 fetch + rebase，有冲突立即停止，再逐个远程推送
push:
    git fetch --all --prune
    git pull --rebase
    {{ if os() == "windows" { "git remote | ForEach-Object { git push $_ }" } else { "git remote | xargs -n1 git push" } }}

# 构建时注入 git 短 hash 与构建时间；时间取构建机本地时区（带偏移，如 +08:00），
# 不是 UTC——信息页与 `note info` 里看到的就是「我刚打这个包的时刻」。
# git 不可用或不在仓库内时退化成空值，绝不能让 build 因此失败
# （internal/build 会回落到 debug.ReadBuildInfo）。
ldflags := if os() == "windows" { `$h = git rev-parse --short HEAD 2>$null; $now = [DateTimeOffset]::Now.ToString('yyyy-MM-ddTHH:mm:sszzz'); $a = '-X note/internal/build.GitHash=' + $h + ' -X note/internal/build.BuildDate=' + $now; if (git status --porcelain 2>$null) { $a += ' -X note/internal/build.GitDirty=true' }; $a` } else { `t=$(date +%Y-%m-%dT%H:%M:%S); z=$(date +%z); d="$t${z:0:3}:${z:3:2}"; a="-X note/internal/build.GitHash=$(git rev-parse --short HEAD 2>/dev/null) -X note/internal/build.BuildDate=$d"; if test -n "$(git status --porcelain 2>/dev/null)"; then a="$a -X note/internal/build.GitDirty=true"; fi; printf %s "$a"` }

# 构建单二进制
build: frontend
    go build -trimpath -ldflags "-s -w {{ldflags}}" -o {{binary}} ./cmd/note

# 构建并安装到本机（Windows）：关停后台 note web → 删旧版本 → 拷新版本 → 写用户 PATH
[windows]
install-win: build
    powershell.exe -NoLogo -ExecutionPolicy Bypass -File scripts/install-win.ps1

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
