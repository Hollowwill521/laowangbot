#!/usr/bin/env bash
# Downloaded installations require a published GitHub release. --binary is an explicit trusted local override.
set -euo pipefail
umask 077
# Color only interactive terminals; NO_COLOR disables styling for accessible output.
C_RESET=; C_TITLE=; C_OK=; C_WARN=; C_ERROR=
if [ -t 2 ] && [ "${TERM:-dumb}" != dumb ] && [ -z "${NO_COLOR+x}" ]; then
  C_RESET=$'\033[0m'; C_TITLE=$'\033[1;36m'; C_OK=$'\033[1;32m'; C_WARN=$'\033[1;33m'; C_ERROR=$'\033[1;31m'
fi
ui() { local color=$1; shift; printf '%s%s%s\n' "$color" "$*" "$C_RESET" >&2; }
info() { ui "$C_TITLE" "$*"; }
success() { ui "$C_OK" "$*"; }
warn() { ui "$C_WARN" "$*"; }
error() { ui "$C_ERROR" "$*"; }
die() { error "$*"; exit 1; }
ROOT=${LAOWANGBOT_ROOT:-$HOME/laowangbot}; REPO=OrionG-hub/laowangbot; VERSION=latest
BINARY=; MIGRATE=; FROM=auto; RESTORE=; SERVICE=1; WIZARD=0; SOURCE_SERVICE=; SOURCE_STOPPED=0; SOURCE_ALREADY_STOPPED=0; SOURCE_ENABLE_STATE=; SOURCE_DISABLED=0
while [ $# -gt 0 ]; do
  case "$1" in
    --root|--repo|--version|--binary|--migrate|--from|--restore)
      [ $# -ge 2 ] || { error "Missing value for $1"; exit 2; }
      case "$1" in --root) ROOT=$2;; --repo) REPO=$2;; --version) VERSION=$2;; --binary) BINARY=$2;; --migrate) MIGRATE=$2;; --from) FROM=$2;; --restore) RESTORE=$2;; esac; shift 2;;
    --wizard) WIZARD=1; shift;;
    --source-stopped) SOURCE_ALREADY_STOPPED=1; shift;;
    --no-service) SERVICE=0; shift;;
    --help|-h) info 'Usage: install.sh [--wizard] [--source-stopped] [--root DIR] [--version TAG] [--binary PATH] [--migrate DIR --from auto|mibot-lite|mibox|telebox] [--restore FILE] [--no-service]'; exit 0;;
    *) error "Unknown argument: $1"; exit 2;;
  esac
done
[ -z "$MIGRATE" ] || [ -z "$RESTORE" ] || die 'Choose migration or restore'
case "$FROM" in auto|mibot-lite|mibox|telebox) ;; *) die 'Unsupported migration source';; esac
case "$(uname -s)" in Linux) OS=linux;; Darwin) OS=darwin;; *) die 'Use install.ps1 on Windows';; esac
case "$(uname -m)" in x86_64|amd64) ARCH=amd64;; arm64|aarch64) ARCH=arm64;; *) die 'Unsupported architecture';; esac
ask() { printf '%s%s%s' "$C_TITLE" "$1" "$C_RESET" >&2; IFS= read -r ANSWER || die '输入结束，迁移已取消'; }
if [ "$WIZARD" = 1 ]; then
  [ -z "$MIGRATE$RESTORE" ] || die '--wizard 不能和 --migrate / --restore 同时使用'
  info '迁移到 laowangbot：无需手动复制配置、会话和数据。'
  ui "$C_OK" '1) mibot-lite'
  ui "$C_OK" '2) MiBox'
  ui "$C_OK" '3) TeleBox'
  warn '0) 取消'
  ask '请选择旧人形 [1-3]：'
  case "$ANSWER" in 1) FROM=mibot-lite;; 2) FROM=mibox;; 3) FROM=telebox;; *) die '已取消，未更改旧部署';; esac
  case "$FROM" in mibot-lite) SUGGESTED="$HOME/mibot-lite";; mibox) SUGGESTED="$HOME/mibot";; telebox) SUGGESTED="$HOME/TeleBox";; esac
  ask "旧部署目录（包含 config.json）[$SUGGESTED]："
  MIGRATE=${ANSWER:-$SUGGESTED}
fi
if [ -n "$MIGRATE" ]; then
  [ -d "$MIGRATE" ] && [ -f "$MIGRATE/config.json" ] || die '旧部署目录不存在或缺少 config.json'
  MIGRATE=$(cd -- "$MIGRATE" && pwd -P)
fi
case "$ROOT" in /*) ;; *) ROOT="$PWD/$ROOT";; esac
# Official binaries cannot preserve locally compiled plugins.
if [ -e "$ROOT/.compiled/current.json" ] || { [ -d "$ROOT/plugins" ] && [ -n "$(ls -A "$ROOT/plugins")" ]; }; then
  die '部署包含插件或源码构建记录，拒绝覆盖。请使用 .update run，或停止服务后运行 laowangbot --source-update TAG --root DIR；源码回滚使用 --source-rollback。'
fi
# Service formats have expansion syntax; reject it before any changes.
case "$ROOT" in *[!A-Za-z0-9._/-]*) [ "$SERVICE" = 0 ] || die 'Service root must contain only letters, numbers, / . _ -';; esac
if [ "$SERVICE" = 1 ] && [ "$OS" = linux ]; then
  [ "$(id -u)" = 0 ] || die 'Linux system service requires root; otherwise use --no-service'
  command -v systemctl >/dev/null || die 'systemctl is required'
fi
if [ -n "$MIGRATE$RESTORE" ] && [ -d "$ROOT" ] && [ -n "$(ls -A "$ROOT")" ]; then die 'Migration/restore destination must be empty'; fi
# Resolve destination through existing ancestors before stopping any source service.
if [ -n "$MIGRATE" ]; then
  cursor="$ROOT"; suffix=
  while [ ! -d "$cursor" ]; do
    [ "$cursor" != / ] || die '目标路径不可用'
    suffix="/$(basename -- "$cursor")$suffix"; cursor=$(dirname -- "$cursor")
  done
  ROOT="$(cd -- "$cursor" && pwd -P)$suffix"
  case "$ROOT" in *[!A-Za-z0-9._/-]*) [ "$SERVICE" = 0 ] || die 'Service root resolves to unsupported characters';; esac
  case "$ROOT/" in "$MIGRATE/"*) die '目标目录必须位于旧部署目录之外';; esac
  if [ -d "$ROOT" ] && [ -n "$(ls -A "$ROOT")" ]; then die 'Migration destination must be empty'; fi
fi
valid_source_unit() {
  [[ "$1" =~ ^[A-Za-z0-9_@][A-Za-z0-9_.@-]*$ ]] || return 1
  case "$1" in laowangbot|laowangbot.service|*@.service) return 1;; esac
}
source_matches() {
  local working
  working=$(systemctl show "$1" -p WorkingDirectory --value 2>/dev/null) || return 1
  [ -d "$working" ] && [ "$(cd -- "$working" && pwd -P)" = "$MIGRATE" ]
}
select_source_service() {
  local loaded installed unit rest canonical found candidate choice index
  local matches=()
  info '正在自动识别旧 systemd 服务…'
  loaded=$(systemctl list-units --type=service --all --plain --no-legend --no-pager 2>/dev/null) || die '无法读取 systemd 服务列表，未停止任何服务'
  installed=$(systemctl list-unit-files --type=service --no-legend --no-pager 2>/dev/null) || die '无法读取 systemd 服务文件列表，未停止任何服务'
  while read -r unit rest; do
    [ -n "$unit" ] || continue
    valid_source_unit "$unit" || continue
    source_matches "$unit" || continue
    canonical=$(systemctl show "$unit" -p Id --value 2>/dev/null) || continue
    valid_source_unit "$canonical" || continue
    # Aliases can appear in both listings; stop the canonical unit only once.
    found=0
    for candidate in "${matches[@]+"${matches[@]}"}"; do [ "$candidate" != "$canonical" ] || found=1; done
    [ "$found" = 1 ] || matches+=("$canonical")
  done <<< "$loaded
$installed"
  case "${#matches[@]}" in
    0)
      if [ "$SOURCE_ALREADY_STOPPED" = 1 ]; then
        warn '未找到匹配的 systemd 服务；请确保 Docker/PM2/其他启动器也已禁用旧实例自启动。'
        return
      fi
      warn "未找到工作目录为 $MIGRATE 的服务，请输入旧服务名。"
      case "$FROM" in mibot-lite) candidate=mibot-lite.service;; mibox) candidate=mibot.service;; telebox) candidate=telebox.service;; *) candidate=;; esac
      ask "旧 systemd 服务名称${candidate:+ [$candidate]}："
      SOURCE_SERVICE=${ANSWER:-$candidate}
      ;;
    1) SOURCE_SERVICE=${matches[0]}; success "自动识别到旧服务：$SOURCE_SERVICE";;
    *)
      info '找到多个匹配服务，请选择：'
      index=1
      for candidate in "${matches[@]}"; do ui "$C_OK" "$index) $candidate"; index=$((index+1)); done
      warn '0) 取消'
      ask '请选择服务序号（回车取消）：'
      choice=${ANSWER:-0}; SOURCE_SERVICE=
      index=1
      for candidate in "${matches[@]}"; do
        if [ "$choice" = "$index" ]; then SOURCE_SERVICE=$candidate; break; fi
        index=$((index+1))
      done
      [ -n "$SOURCE_SERVICE" ] || die '未选择有效服务，已取消'
      ;;
  esac
  valid_source_unit "$SOURCE_SERVICE" || die '服务名称无效，或不能将新服务作为旧服务'
  source_matches "$SOURCE_SERVICE" || die '旧服务的 WorkingDirectory 与所选部署目录不一致，未停止服务'
  for candidate in "${matches[@]+"${matches[@]}"}"; do
    [ "$candidate" != "$SOURCE_SERVICE" ] || continue
    local other_state other_enabled
    other_state=$(systemctl show "$candidate" -p ActiveState --value) || die '无法核验其他匹配服务'
    other_enabled=$(systemctl show "$candidate" -p UnitFileState --value) || die '无法核验其他匹配服务自启动'
    case "$other_state/$other_enabled" in inactive/disabled|inactive/masked|failed/disabled|failed/masked) ;;
      *) die "同一旧目录还被 $candidate 自动启动或运行，请先停止并禁用该服务";;
    esac
  done
}
if [ -n "$MIGRATE" ]; then
  info "旧目录：$MIGRATE"
  info "新目录：$ROOT"
  warn '旧目录将保留；迁移会停止匹配的旧服务并禁用其开机启动，失败时恢复原状态。'
  if [ "$OS" = linux ] && command -v systemctl >/dev/null; then
    select_source_service
  elif [ "$SOURCE_ALREADY_STOPPED" = 1 ]; then
    warn '请确保旧实例及其 Docker/PM2/其他启动器的自启动均已禁用。'
  elif [ "$WIZARD" = 1 ]; then
    warn '请先停止旧实例，并禁用其 Docker/PM2/launchd 等启动器的自动启动。'
    ui "$C_OK" '1) 已停止并禁用自动启动，继续迁移'
    warn '0) 取消'
    ask '请选择 [0-1，回车默认 0]：'
    case "${ANSWER:-0}" in 1|y|Y) ;; *) die '已取消，请先停止旧实例并禁用自动启动';; esac
  else
    die '请先停止旧实例并禁用自启动，然后加 --source-stopped 重试'
  fi
fi
WORK=$(mktemp -d); CHANGED=0; STOPPED=0; SUCCESS=0; HAD_BINARY=0; HAD_UNIT=0; UNIT_CHANGED=0; TARGET_START_ATTEMPTED=0
UNIT=/etc/systemd/system/laowangbot.service
[ "$OS" != darwin ] || UNIT="$HOME/Library/LaunchAgents/io.github.laowangbot.plist"
service_stop() { if [ "$OS" = linux ]; then systemctl stop laowangbot; else launchctl bootout "gui/$(id -u)" "$UNIT"; fi; }
service_start() { if [ "$OS" = linux ]; then systemctl daemon-reload && systemctl enable --now laowangbot && systemctl is-active --quiet laowangbot; else launchctl bootstrap "gui/$(id -u)" "$UNIT"; fi; }
cleanup() {
  rc=$?; SAFE_TO_RESUME=1
  if [ "$SUCCESS" = 0 ]; then
    if [ "$CHANGED" = 1 ]; then
      if [ "$TARGET_START_ATTEMPTED" = 1 ]; then service_stop >/dev/null 2>&1 || SAFE_TO_RESUME=0; fi
      if [ "$HAD_BINARY" = 1 ]; then cp "$WORK/previous" "$ROOT/laowangbot"; else rm -f "$ROOT/laowangbot"; fi
    fi
    if [ "$UNIT_CHANGED" = 1 ]; then
      if [ "$HAD_UNIT" = 1 ]; then cp "$WORK/unit" "$UNIT"; else rm -f "$UNIT"; fi
    fi
    if [ "$SOURCE_DISABLED" = 1 ] && [ "$SAFE_TO_RESUME" = 1 ]; then
      case "$SOURCE_ENABLE_STATE" in
        enabled) systemctl enable "$SOURCE_SERVICE" || { error "旧服务自启动恢复失败：$SOURCE_SERVICE"; SAFE_TO_RESUME=0; };;
        enabled-runtime) systemctl enable --runtime "$SOURCE_SERVICE" || { error "旧服务自启动恢复失败：$SOURCE_SERVICE"; SAFE_TO_RESUME=0; };;
      esac
    fi
    if [ "$SOURCE_STOPPED" = 1 ]; then
      if [ "$SAFE_TO_RESUME" = 1 ]; then systemctl start "$SOURCE_SERVICE" || error "旧服务恢复失败：$SOURCE_SERVICE"
      else error "无法确认新服务已停止，未恢复旧服务，避免双实例运行。"; fi
    fi
    [ "$STOPPED" = 0 ] || service_start || error '旧服务恢复失败，请检查服务日志'
  fi
  rm -rf "$WORK"
  exit "$rc"
}
trap cleanup EXIT
info "准备 laowangbot 安装文件…"
if [ -n "$BINARY" ]; then cp "$BINARY" "$WORK/new"; else
  ASSET=laowangbot-$OS-$ARCH
  if [ "$VERSION" = latest ]; then BASE="https://github.com/$REPO/releases/latest/download"; else BASE="https://github.com/$REPO/releases/download/$VERSION"; fi
  curl -fLsS "$BASE/$ASSET" -o "$WORK/new"
  curl -fLsS "$BASE/checksums.txt" -o "$WORK/checksums.txt"
  WANT=$(awk -v n="$ASSET" '$2==n || $2=="*"n {print $1}' "$WORK/checksums.txt")
  [ ${#WANT} = 64 ] || die 'Missing or ambiguous checksum'
  if command -v sha256sum >/dev/null; then GOT=$(sha256sum "$WORK/new"); else GOT=$(shasum -a 256 "$WORK/new"); fi
  [ "${GOT%% *}" = "$WANT" ] || die 'Checksum mismatch'
fi
chmod 755 "$WORK/new"
if [ "$SERVICE" = 1 ] && [ -f "$UNIT" ]; then
  if [ "$OS" = linux ]; then EXISTING=$(systemctl show laowangbot -p WorkingDirectory --value); [ "$EXISTING" = "$ROOT" ] || die 'Existing service points at another root'; else grep -Fq "<string>$ROOT</string>" "$UNIT" || die 'Existing launch agent points at another root'; fi
  cp "$UNIT" "$WORK/unit"; HAD_UNIT=1
fi
if [ -n "$SOURCE_SERVICE" ]; then
  source_matches "$SOURCE_SERVICE" || die '旧服务工作目录已变化，未停止服务'
  SOURCE_ENABLE_STATE=$(systemctl show "$SOURCE_SERVICE" -p UnitFileState --value) || die '无法查询旧服务自启动状态，拒绝迁移'
  case "$SOURCE_ENABLE_STATE" in
    enabled|enabled-runtime|disabled|masked) ;;
    *) die '旧服务由其他单元或启动器管理，请先禁用其自动启动后重试';;
  esac
  SOURCE_TRIGGERS=$(systemctl show "$SOURCE_SERVICE" -p TriggeredBy --value) || die '无法核验旧服务触发器'
  [ -z "$SOURCE_TRIGGERS" ] || die "旧服务存在触发器 $SOURCE_TRIGGERS，请先停用并移除触发关系后迁移"
  OLD_STATE=$(systemctl show "$SOURCE_SERVICE" -p ActiveState --value) || die '无法查询旧服务状态，拒绝迁移'
  case "$OLD_STATE" in
    active|activating|reloading|deactivating) SOURCE_STOPPED=1;;
    inactive|failed) ;;
    *) die '旧服务状态未知，拒绝迁移';;
  esac
  systemctl stop "$SOURCE_SERVICE"
  OLD_STATE=$(systemctl show "$SOURCE_SERVICE" -p ActiveState --value) || die '无法确认旧服务已停止'
  [ "$OLD_STATE" = inactive ] || die '旧服务未完全停止，拒绝迁移'
  if [ "$SOURCE_ENABLE_STATE" != masked ]; then
    SOURCE_DISABLED=1
    systemctl disable "$SOURCE_SERVICE"
    if [ "$SOURCE_ENABLE_STATE" = enabled-runtime ]; then systemctl disable --runtime "$SOURCE_SERVICE"; fi
    SOURCE_AFTER=$(systemctl show "$SOURCE_SERVICE" -p UnitFileState --value) || die '无法确认旧服务自启动已禁用'
    case "$SOURCE_AFTER" in disabled|masked) ;; *) die '旧服务自启动未成功禁用，拒绝迁移';; esac
  fi
fi
mkdir -p "$ROOT"
[ -z "$MIGRATE" ] || info "正在迁移 $FROM 配置、会话和数据…"
[ -z "$MIGRATE" ] || "$WORK/new" --migrate "$MIGRATE" --from "$FROM" --root "$ROOT"
[ -z "$RESTORE" ] || "$WORK/new" --restore "$RESTORE" --root "$ROOT"
if [ ! -f "$ROOT/config.json" ]; then
  if [ -t 0 ]; then "$WORK/new" --login --root "$ROOT"; else die "Login required: run installer in a terminal, or migrate an existing account"; fi
fi
"$WORK/new" --check --root "$ROOT"
if [ "$SERVICE" = 1 ] && [ "$HAD_UNIT" = 1 ]; then
  if [ "$OS" = linux ]; then
    if systemctl is-active --quiet laowangbot; then service_stop; STOPPED=1; fi
  elif launchctl print "gui/$(id -u)/io.github.laowangbot" >/dev/null 2>&1; then service_stop; STOPPED=1; fi
fi
if [ -f "$ROOT/laowangbot" ]; then cp "$ROOT/laowangbot" "$WORK/previous"; HAD_BINARY=1; fi
cp "$WORK/new" "$ROOT/.laowangbot.new"
mv "$ROOT/.laowangbot.new" "$ROOT/laowangbot"; CHANGED=1
if [ "$SERVICE" = 1 ]; then
  mkdir -p "$(dirname "$UNIT")"; UNIT_CHANGED=1
  if [ "$OS" = linux ]; then
    cat > "$UNIT" <<UNIT
[Unit]
Description=laowangbot
After=network-online.target
[Service]
WorkingDirectory=$ROOT
ExecStart=$ROOT/laowangbot --supervise --root $ROOT
Restart=on-failure
RestartSec=5
UMask=0077
NoNewPrivileges=yes
[Install]
WantedBy=multi-user.target
UNIT
  else
    cat > "$UNIT" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>io.github.laowangbot</string>
<key>ProgramArguments</key><array><string>$ROOT/laowangbot</string><string>--supervise</string><string>--root</string><string>$ROOT</string></array>
<key>WorkingDirectory</key><string>$ROOT</string>
<key>RunAtLoad</key><true/><key>KeepAlive</key><true/>
<key>StandardOutPath</key><string>$ROOT/service.log</string>
<key>StandardErrorPath</key><string>$ROOT/service.err.log</string>
</dict></plist>
PLIST
  fi
  TARGET_START_ATTEMPTED=1
  service_start
fi
[ "$HAD_BINARY" = 0 ] || cp "$WORK/previous" "$ROOT/laowangbot.previous"
SUCCESS=1
success "安装完成：$ROOT/laowangbot"
warn "本地配置检查通过；请在 Telegram 执行 .ping 确认连接。"

if [ -n "$MIGRATE" ]; then
  success "迁移完成。旧部署保留在：$MIGRATE"
  info "迁移报告：$ROOT/migration-report.json"
  if [ -n "$SOURCE_SERVICE" ]; then success "旧服务已停止并禁用开机启动：${SOURCE_SERVICE}"; fi
fi
