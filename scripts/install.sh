#!/usr/bin/env bash
# Downloaded installations require a published GitHub release. --binary is an explicit trusted local override.
set -euo pipefail
umask 077
ROOT=${LAOWANGBOT_ROOT:-$HOME/laowangbot}; REPO=OrionG-hub/laowangbot; VERSION=latest
BINARY=; MIGRATE=; FROM=auto; RESTORE=; SERVICE=1; WIZARD=0; SOURCE_SERVICE=; SOURCE_STOPPED=0
while [ $# -gt 0 ]; do
  case "$1" in
    --root|--repo|--version|--binary|--migrate|--from|--restore)
      [ $# -ge 2 ] || { echo "Missing value for $1" >&2; exit 2; }
      case "$1" in --root) ROOT=$2;; --repo) REPO=$2;; --version) VERSION=$2;; --binary) BINARY=$2;; --migrate) MIGRATE=$2;; --from) FROM=$2;; --restore) RESTORE=$2;; esac; shift 2;;
    --wizard) WIZARD=1; shift;;
    --no-service) SERVICE=0; shift;;
    --help|-h) echo 'Usage: install.sh [--wizard] [--root DIR] [--version TAG] [--binary PATH] [--migrate DIR --from auto|mibot-lite|mibox|telebox] [--restore FILE] [--no-service]'; exit 0;;
    *) echo "Unknown argument: $1" >&2; exit 2;;
  esac
done
die() { echo "$*" >&2; exit 1; }
[ -z "$MIGRATE" ] || [ -z "$RESTORE" ] || die 'Choose migration or restore'
case "$FROM" in auto|mibot-lite|mibox|telebox) ;; *) die 'Unsupported migration source';; esac
case "$(uname -s)" in Linux) OS=linux;; Darwin) OS=darwin;; *) die 'Use install.ps1 on Windows';; esac
case "$(uname -m)" in x86_64|amd64) ARCH=amd64;; arm64|aarch64) ARCH=arm64;; *) die 'Unsupported architecture';; esac
ask() { printf '%s' "$1" >&2; IFS= read -r ANSWER || die '输入结束，迁移已取消'; }
if [ "$WIZARD" = 1 ]; then
  [ -z "$MIGRATE$RESTORE" ] || die '--wizard 不能和 --migrate / --restore 同时使用'
  printf '%s\n' '迁移到 laowangbot：无需手动复制配置、会话和数据。' '1) mibot-lite' '2) MiBox' '3) TeleBox' '0) 取消' >&2
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
if [ "$WIZARD" = 1 ]; then
  printf '旧目录：%s\n新目录：%s\n旧目录将保留；不支持的插件会归档并列入报告。\n' "$MIGRATE" "$ROOT" >&2
  ask '旧实例停止方式：systemd（脚本停止）或 manual（已自行停止）[manual]：'
  case "${ANSWER:-manual}" in
    systemd)
      [ "$OS" = linux ] || die '此平台请使用 manual 并先停止旧实例'
      command -v systemctl >/dev/null || die '找不到 systemctl'
      ask '旧 systemd 服务名称（例如 mibot-lite.service）：'
      SOURCE_SERVICE=$ANSWER
      [[ "$SOURCE_SERVICE" =~ ^[A-Za-z0-9_@][A-Za-z0-9_.@-]*$ ]] || die '服务名称无效'
      case "$SOURCE_SERVICE" in laowangbot|laowangbot.service) die '不能把新服务当作旧服务停止';; esac
      OLD_ROOT=$(systemctl show "$SOURCE_SERVICE" -p WorkingDirectory --value)
      [ -d "$OLD_ROOT" ] && [ "$(cd -- "$OLD_ROOT" && pwd -P)" = "$MIGRATE" ] || die '旧服务的 WorkingDirectory 与所选部署目录不一致，未停止服务'
      ;;
    manual)
      ask '请确认旧人形已停止（包括 Docker/PM2/后台进程），输入 y 继续：'
      case "$ANSWER" in y|Y) ;; *) die '已取消，请先停止旧实例';; esac
      ;;
    *) die '停止方式无效，已取消';;
  esac
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
    if [ "$SOURCE_STOPPED" = 1 ]; then
      if [ "$SAFE_TO_RESUME" = 1 ]; then systemctl start "$SOURCE_SERVICE" || echo "旧服务恢复失败：$SOURCE_SERVICE" >&2
      else echo "无法确认新服务已停止，未恢复旧服务，避免双实例运行。" >&2; fi
    fi
    [ "$STOPPED" = 0 ] || service_start || echo 'Previous service could not restart; inspect service logs' >&2
  fi
  rm -rf "$WORK"
  exit "$rc"
}
trap cleanup EXIT
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
  OLD_STATE=$(systemctl show "$SOURCE_SERVICE" -p ActiveState --value) || die '无法查询旧服务状态，拒绝迁移'
  case "$OLD_STATE" in
    active|activating|reloading|deactivating) SOURCE_STOPPED=1;;
    inactive|failed) ;;
    *) die '旧服务状态未知，拒绝迁移';;
  esac
  systemctl stop "$SOURCE_SERVICE"
  OLD_STATE=$(systemctl show "$SOURCE_SERVICE" -p ActiveState --value) || die '无法确认旧服务已停止'
  [ "$OLD_STATE" = inactive ] || die '旧服务未完全停止，拒绝迁移'
fi
mkdir -p "$ROOT"
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
printf 'Installed %s (local configuration checked; Telegram connectivity not verified)\n' "$ROOT/laowangbot"

if [ -n "$MIGRATE" ]; then
  printf '迁移完成。旧部署保留在：%s\n迁移报告：%s/migration-report.json\n' "$MIGRATE" "$ROOT"
  if [ -n "$SOURCE_SERVICE" ]; then printf '旧服务保持停止：%s；旧服务自启动设置未更改，请避免重启服务器后双实例运行。\n' "$SOURCE_SERVICE"; fi
fi
