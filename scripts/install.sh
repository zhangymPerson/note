#!/usr/bin/env bash
# 把构建出来的 note 安装到本机（macOS / Linux）。
#
# 顺序是固定的，不能调换：
#   1. 确认构建产物存在（just install-mac / install-linux 会先跑 just build）；
#   2. 检查 note web 是否在运行——替换前先关停，替换完才能用新版本重启；
#   3. 删除安装目录里的旧版本；
#   4. 复制新版本进去，并就地跑一次 --help 校验；
#   5. 把安装目录写进 shell 配置（幂等，已经在里面就不动）；
#   6. 启动后台服务（note web start，已在运行则直接打印信息）。
#
# 任何一步失败都不会继续往下走：关停不成功时绝不删旧版本，宁可让用户看到
# 「note web 还在运行」也不留下一个被删掉的安装。

set -euo pipefail

# ── 参数 ────────────────────────────────────────────────────────────────────
# $1 平台：macos / linux（由 justfile 传入）；$2 可选安装目录，缺省 ~/.local/bin
platform="${1:-}"
case "$platform" in
macos | linux) ;;
*)
  echo "用法: $0 {macos|linux} [安装目录]" >&2
  exit 2
  ;;
esac

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_bin="$repo_root/note"
install_dir="${2:-$HOME/.local/bin}"
target="$install_dir/note"

# ── 输出辅助 ────────────────────────────────────────────────────────────────
log() { printf '\033[36m==> %s\033[0m\n' "$*"; }
ok() { printf '\033[32m    %s\033[0m\n' "$*"; }
note_() { printf '\033[33m    %s\033[0m\n' "$*"; }
die() {
  printf '\033[31m错误：%s\033[0m\n' "$*" >&2
  exit 1
}

# 运行命令，stdout+stderr 存进全局 $captured，退出码作为本函数退出码。
# set -e 下直接调用非零退出会中断脚本，调用方必须放在 if / case 等条件里。
run() {
  set +e
  captured="$("$@" 2>&1)"
  local code=$?
  set -e
  return "$code"
}

# ── 1. 构建产物 ──────────────────────────────────────────────────────────────
if [ ! -f "$source_bin" ]; then
  die "找不到构建产物 $source_bin。先跑 just build（just install-mac / install-linux 会自动构建）。"
fi

log "安装 $source_bin → $target"

# ── 2. 关停运行中的 note web ────────────────────────────────────────────────
# 只有旧版本存在时才有「在运行」一说；新装的没有旧版本，直接跳到复制。
if [ -e "$target" ]; then
  log "检查 note web 是否在运行"
  if run "$target" web status; then
    status_code=0
  else
    status_code=$?
  fi

  case "$status_code" in
  3)
    ok "未在运行"
    ;;
  0 | 1)
    note_ "检测到运行中的实例，先关停"
    if ! run "$target" web stop; then
      die "关停请求失败：$(printf '%s' "$captured" | tail -n 3)"
    fi
    [ -n "$captured" ] && printf '%s\n' "$captured"

    # 关停后等进程真正退出：status 退出码变成 3 才算退干净。
    deadline=$((SECONDS + 20))
    stopped=0
    while ((SECONDS < deadline)); do
      if run "$target" web status; then
        : # 0：还在运行，继续等
      elif [ $? -eq 3 ]; then
        stopped=1
        break
      fi
      sleep 0.3
    done
    if [ "$stopped" -eq 0 ]; then
      die "note web 还在运行，替换不了正在使用的可执行文件。请回到它的终端按 Ctrl+C 再重试。"
    fi
    ok "已关停"
    ;;
  *)
    die "note web status 以退出码 $status_code 结束（$(printf '%s' "$captured" | tail -n 3)），先手动确认服务状态再安装。"
    ;;
  esac
fi

# ── 3. 建目录并删除旧版本 ────────────────────────────────────────────────────
if [ ! -d "$install_dir" ]; then
  mkdir -p "$install_dir"
  ok "已创建 $install_dir"
fi

if [ -e "$target" ]; then
  log "删除旧版本"
  rm -f "$target" || die "删除旧版本失败（文件仍被占用？）。"
  ok "已删除"
fi

# ── 4. 复制新版本并校验 ──────────────────────────────────────────────────────
same_file() {
  local d1 d2
  d1="$(cd "$(dirname "$1")" 2>/dev/null && pwd -P)" || return 1
  d2="$(cd "$(dirname "$2")" 2>/dev/null && pwd -P)" || return 1
  [ "$d1/$(basename "$1")" = "$d2/$(basename "$2")" ]
}
if same_file "$source_bin" "$target"; then
  die "构建产物与安装目标同一个文件，别把安装目录当仓库根目录用。"
fi

cp "$source_bin" "$target"
if run "$target" --help; then
  : # 校验通过
else
  help_code=$?
  die "复制过去的 note 跑不起来（--help 退出码 $help_code），旧版本已经被删，请重跑 just build 后再装一次。"
fi
ok "已复制，并试跑 --help 通过"

# ── 5. shell 配置里的 PATH ──────────────────────────────────────────────────
# 用户级安装目录可能不在 PATH 里：挑一个已存在的 rc 文件（zsh 优先，macOS
# 默认 shell），把 export 追加进去；全部不存在时退回 ~/.profile。幂等写入。
rc_file=""
for f in "$HOME/.zshrc" "$HOME/.bashrc" "$HOME/.profile"; do
  if [ -f "$f" ]; then
    rc_file="$f"
    break
  fi
done
[ -z "$rc_file" ] && rc_file="$HOME/.profile"

log "检查安装目录是否在 PATH 中"
# 当前会话 PATH 与 rc 文件都查：同一会话重复安装时 rc 里已有就不再追加
if grep -Fq ":$install_dir:" <<<":$PATH:" || grep -Fq "export PATH=\"$install_dir:" "$rc_file" 2>/dev/null; then
  ok "已在 PATH 中"
else
  mkdir -p "$(dirname "$rc_file")"
  {
    printf '\n# note 安装目录（just install-mac / install-linux 自动写入）\n'
    printf 'export PATH="%s:$PATH"\n' "$install_dir"
  } >>"$rc_file"
  ok "已写入 $rc_file"
  note_ "新开的终端才会生效（当前窗口可临时执行：export PATH=\"$install_dir:\$PATH\"）"
fi

# ── 6. 启动后台服务 ──────────────────────────────────────────────────────────
log "启动后台服务"
if ! run "$target" web start; then
  die "启动失败：$(printf '%s' "$captured" | tail -n 5)"
fi
[ -n "$captured" ] && printf '%s\n' "$captured"

printf '\n\033[32m安装完成：%s\033[0m\n' "$target"
printf '新开一个终端后可以直接用 note 命令：\n'
printf '  note --help\n'
printf '状态与停止：note web status / note web stop\n'
