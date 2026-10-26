# 开发环境后台启动/停止（Windows）：后端 go run（:8080）+ 前端 vite dev（:5173）。
# PID 与日志记录在 .run/ 下，供 stop 与排查使用。
$ErrorActionPreference = 'Stop'
Set-Location (Join-Path (Split-Path -Parent $PSCommandPath) '..')
New-Item -ItemType Directory -Force .run | Out-Null

function Test-DevRunning([string]$name) {
    $pidfile = ".run/$name.pid"
    if (-not (Test-Path $pidfile)) { return $false }
    [bool](Get-Process -Id (Get-Content $pidfile) -ErrorAction SilentlyContinue)
}

function Start-DevProcess([string]$name, [string]$commandLine) {
    if (Test-DevRunning $name) {
        Write-Host "$name 已在运行 (pid $(Get-Content ".run/$name.pid"))"
        return
    }
    # 经 WMI 创建进程：子进程挂在 WmiPrvSE 下，不继承当前脚本的 stdout 管道句柄，
    # 否则调用方（如 just）会一直等管道 EOF 而挂起。重定向由 cmd 内部完成。
    $inner = "cmd.exe /c `"$commandLine > .run\$name.log 2>&1`""
    $result = ([wmiclass]'Win32_Process').Create($inner, (Get-Location).Path)
    if ($result.ReturnValue -ne 0) {
        throw "启动 $name 失败，WMI ReturnValue = $($result.ReturnValue)"
    }
    $result.ProcessId | Out-File ".run/$name.pid" -Encoding ascii
    Write-Host "已启动 $name (pid $($result.ProcessId)): $commandLine"
}

function Stop-DevProcess([string]$name) {
    $pidfile = ".run/$name.pid"
    if (-not (Test-Path $pidfile)) {
        Write-Host "$name 未在运行"
        return
    }
    $procId = Get-Content $pidfile
    # /T 杀整棵进程树：go run 与 npm(cmd) 都派生子进程
    & taskkill.exe /T /F /PID $procId 2>$null | Out-Null
    Write-Host "已停止 $name (pid $procId)"
    Remove-Item $pidfile -Force
}

switch ($args[0]) {
    'start' {
        if (-not (Test-Path web/node_modules)) { npm --prefix web ci }
        if (-not (Test-Path internal/web/dist)) { npm --prefix web run build }
        Start-DevProcess backend 'go run ./cmd/note'
        Start-DevProcess frontend 'npm --prefix web run dev'
        Write-Host '后端 http://127.0.0.1:8080 · 前端 http://localhost:5173 （/api 已代理到后端）'
        Write-Host '日志：.run/backend.log .run/frontend.log'
    }
    'stop' {
        Stop-DevProcess frontend
        Stop-DevProcess backend
    }
    default {
        Write-Error '用法: dev.ps1 {start|stop}'
        exit 1
    }
}
