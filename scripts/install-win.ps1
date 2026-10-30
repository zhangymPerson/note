#Requires -Version 5.1
<#
.SYNOPSIS
    把构建出来的 note.exe 安装到本机，并让 note 在终端里直接可用。

.DESCRIPTION
    顺序是固定的，不能调换：
      1. 确认构建产物存在（just install-win 会先跑 just build）；
      2. 检查 note web 是否在运行——正在使用的可执行文件在 Windows 上删不掉，先关停；
      3. 删除安装目录里的旧版本；
      4. 复制新版本进去，并就地跑一次 --help 校验；
      5. 把安装目录写进用户 PATH（幂等，已经在里面就不动）。

    任何一步失败都不会继续往下走：关停不成功时绝不删旧版本，宁可让用户看到
    「note web 还在运行」也不留下一个被删掉的安装。

.PARAMETER Source
    构建产物路径，缺省是仓库根目录的 note.exe。

.PARAMETER InstallDir
    安装目录，缺省 %LOCALAPPDATA%\Programs\note（与 VS Code 等工具的安装位置惯例一致）。

.EXAMPLE
    powershell -NoLogo -ExecutionPolicy Bypass -File scripts/install-win.ps1

.EXAMPLE
    powershell -NoLogo -ExecutionPolicy Bypass -File scripts/install-win.ps1 -InstallDir D:\tools\note
#>
[CmdletBinding()]
param(
    [string]$Source,
    [string]$InstallDir = $(if ($env:LOCALAPPDATA) { Join-Path $env:LOCALAPPDATA 'Programs\note' } else { '' })
)

$ErrorActionPreference = 'Stop'

function Write-Step([string]$Text) { Write-Host "==> $Text" -ForegroundColor Cyan }
function Write-Done([string]$Text) { Write-Host "    $Text" -ForegroundColor Green }
function Write-Note([string]$Text) { Write-Host "    $Text" -ForegroundColor Yellow }

# 跑一次 note web status，返回退出码与原始输出。
# 退出码：0 运行中 / 1 进程在但不响应 / 3 未运行（见 cmd/note/web_service.go）。
function Get-WebStatus([string]$Exe) {
    $text = & $Exe web status 2>&1 | Out-String
    return [pscustomobject]@{ Code = $LASTEXITCODE; Text = $text }
}

# 关停后等进程真正退出：状态码变成 3 才算退干净。
function Wait-ServerStopped([string]$Exe, [int]$TimeoutSeconds = 20) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        if ((Get-WebStatus $Exe).Code -eq 3) { return $true }
        Start-Sleep -Milliseconds 300
    }
    return $false
}

if (-not $InstallDir) {
    throw '取不到 %LOCALAPPDATA%，请用 -InstallDir 显式指定安装目录。'
}

# ── 1. 构建产物 ────────────────────────────────────────────────────────────
if (-not $Source) {
    $Source = Join-Path (Split-Path -Parent $PSScriptRoot) 'note.exe'
}
$resolved = Resolve-Path -LiteralPath $Source -ErrorAction SilentlyContinue
if (-not $resolved -or -not (Test-Path -LiteralPath $resolved.Path -PathType Leaf)) {
    throw "找不到构建产物 $Source。先跑 just build（just install-win 会自动构建）。"
}
$Source = $resolved.Path

$InstallDir = [System.IO.Path]::GetFullPath($InstallDir)
$Target = Join-Path $InstallDir 'note.exe'
Write-Step "安装 $Source → $Target"

# ── 2. 关停运行中的 note web ───────────────────────────────────────────────
if (Test-Path -LiteralPath $Target) {
    Write-Step '检查 note web 是否在运行'
    $status = Get-WebStatus $Target
    switch ($status.Code) {
        3 {
            Write-Done '未在运行'
        }
        { $_ -eq 0 -or $_ -eq 1 } {
            Write-Note '检测到运行中的实例，先关停'
            $stopText = & $Target web stop 2>&1 | Out-String
            $stopCode = $LASTEXITCODE
            Write-Host $stopText.TrimEnd()
            if ($stopCode -ne 0 -or -not (Wait-ServerStopped $Target)) {
                throw @"
note web 还在运行，替换不了正在使用的可执行文件。
  如果它是前台实例（在某个终端里直接敲的 note web），回到那个终端按 Ctrl+C 再重试；
  关停输出：$($stopText.Trim())
"@
            }
            Write-Done '已关停'
        }
        default {
            throw @"
note web status 以退出码 $($status.Code) 结束，先手动确认服务状态再安装。
  $($status.Text.Trim())
"@
        }
    }
}

# ── 3. 建目录并删除旧版本 ──────────────────────────────────────────────────
if (-not (Test-Path -LiteralPath $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Write-Done "已创建 $InstallDir"
}

if (Test-Path -LiteralPath $Target) {
    Write-Step '删除旧版本'
    try {
        Remove-Item -LiteralPath $Target -Force
    }
    catch {
        throw "删除旧版本失败（文件仍被占用？）：$($_.Exception.Message)"
    }
    Write-Done '已删除'
}

# ── 4. 复制新版本并校验 ────────────────────────────────────────────────────
if ([System.IO.Path]::GetFullPath($Source) -ieq $Target) {
    throw '构建产物与安装目标同一个文件，别把安装目录当仓库根目录用。'
}

Copy-Item -LiteralPath $Source -Destination $Target -Force
$null = & $Target --help 2>&1
if ($LASTEXITCODE -ne 0) {
    throw "复制过去的 note.exe 跑不起来（--help 退出码 $LASTEXITCODE），旧版本已经被删，请重跑 just build 后再装一次。"
}
Write-Done '已复制，并试跑 --help 通过'

# ── 5. 用户 PATH ───────────────────────────────────────────────────────────
Write-Step '检查用户 PATH'
$environment = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $false)
$rawPath = ''
if ($environment) {
    $rawPath = [string]$environment.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
    $environment.Close()
}

$installed = @($rawPath -split ';' | Where-Object { $_ -ne '' -and $_.TrimEnd('\') -ieq $InstallDir.TrimEnd('\') })
if ($installed.Count -gt 0) {
    Write-Done '安装目录已在用户 PATH 中'
}
else {
    $newPath = if ($rawPath.Trim() -eq '') { $InstallDir } else { $rawPath.TrimEnd(';') + ';' + $InstallDir }
    # 写回 ExpandString：用户 PATH 里若有 %USERPROFILE% 之类的变量，改成 REG_SZ 会让它们失效
    $environment = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    $environment.SetValue('Path', $newPath, [Microsoft.Win32.RegistryValueKind]::ExpandString)
    $environment.Close()
    Write-Done "已加入用户 PATH：$InstallDir"
    Write-Note "新开的终端才会生效（当前窗口可临时执行：`$env:Path = `"$InstallDir;`$env:Path`"）"

}

$env:Path = "$InstallDir;$env:Path"

Write-Host ''
Write-Host "安装完成：$Target" -ForegroundColor Green
Write-Host '新开一个终端后可以直接用 note 命令：'
Write-Host '  note --help'
Write-Host '要把 Web 界面重新起起来：note web start'
