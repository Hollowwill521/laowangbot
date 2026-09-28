#!/usr/bin/env bash
# Downloaded installations require a published GitHub release. --binary is an explicit trusted local override.
set -euo pipefail
umask 077
ROOT=${LAOWANGBOT_ROOT:-$HOME/laowangbot}; REPO=OrionG-hub/laowangbot; VERSION=latest
BINARY=; MIGRATE=; FROM=auto; RESTORE=; SERVICE=1
while [ $# -gt 0 ]; do
  case "$1" in
    --root|--repo|--version|--binary|--migrate|--from|--restore)
      [ $# -ge 2 ] || { echo "Missing value for $1" >&2; exit 2; }
      case "$1" in --root) ROOT=$2;; --repo) REPO=$2;; --version) VERSION=$2;; --binary) BINARY=$2;; --migrate) MIGRATE=$2;; --from) FROM=$2;; --restore) RESTORE=$2;; esac; shift 2;;
    --no-service) SERVICE=0; shift;;
    --help|-h) echo 'Usage: install.sh [--root DIR] [--version TAG] [--binary PATH] [--migrate DIR --from auto|mibot-lite|mibox|telebox] [--restore FILE] [--no-service]'; exit 0;;
    *) echo "Unknown argument: $1" >&2; exit 2;;
  esac
done
die() { echo "$*" >&2; exit 1; }
[ -z "$MIGRATE" ] || [ -z "$RESTORE" ] || die 'Choose migration or restore'
case "$FROM" in auto|mibot-lite|mibox|telebox) ;; *) die 'Unsupported migration source';; esac
case "$(uname -s)" in Linux) OS=linux;; Darwin) OS=darwin;; *) die 'Use install.ps1 on Windows';; esac
case "$(uname -m)" in x86_64|amd64) ARCH=amd64;; arm64|aarch64) ARCH=arm64;; *) die 'Unsupported architecture';; esac
case "$ROOT" in /*) ;; *) ROOT="$PWD/$ROOT";; esac
# Service formats have expansion syntax; reject it before any changes.
case "$ROOT" in *[!A-Za-z0-9._/-]*) [ "$SERVICE" = 0 ] || die 'Service root must contain only letters, numbers, / . _ -';; esac
if [ "$SERVICE" = 1 ] && [ "$OS" = linux ]; then
  [ "$(id -u)" = 0 ] || die 'Linux system service requires root; otherwise use --no-service'
  command -v systemctl >/dev/null || die 'systemctl is required'
fi
if [ -n "$MIGRATE$RESTORE" ] && [ -d "$ROOT" ] && [ -n "$(ls -A "$ROOT")" ]; then die 'Migration/restore destination must be empty'; fi
WORK=$(mktemp -d); CHANGED=0; STOPPED=0; SUCCESS=0; HAD_BINARY=0; HAD_UNIT=0; UNIT_CHANGED=0
UNIT=/etc/systemd/system/laowangbot.service
[ "$OS" != darwin ] || UNIT="$HOME/Library/LaunchAgents/io.github.laowangbot.plist"
service_stop() { if [ "$OS" = linux ]; then systemctl stop laowangbot; else launchctl bootout "gui/$(id -u)" "$UNIT"; fi; }
service_start() { if [ "$OS" = linux ]; then systemctl daemon-reload && systemctl enable --now laowangbot && systemctl is-active --quiet laowangbot; else launchctl bootstrap "gui/$(id -u)" "$UNIT"; fi; }
cleanup() {
  rc=$?
  if [ "$SUCCESS" = 0 ]; then
    if [ "$CHANGED" = 1 ]; then
      [ "$SERVICE" = 0 ] || service_stop >/dev/null 2>&1 || true
      if [ "$HAD_BINARY" = 1 ]; then cp "$WORK/previous" "$ROOT/laowangbot"; else rm -f "$ROOT/laowangbot"; fi
    fi
    if [ "$UNIT_CHANGED" = 1 ]; then
      if [ "$HAD_UNIT" = 1 ]; then cp "$WORK/unit" "$UNIT"; else rm -f "$UNIT"; fi
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
  service_start
fi
[ "$HAD_BINARY" = 0 ] || cp "$WORK/previous" "$ROOT/laowangbot.previous"
SUCCESS=1
printf 'Installed %s (local configuration checked; Telegram connectivity not verified)\n' "$ROOT/laowangbot"
