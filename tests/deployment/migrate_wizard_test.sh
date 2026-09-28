#!/usr/bin/env bash
set -euo pipefail
REPO=$(cd "$(dirname "$0")/../.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/old bot" "$WORK/mock"
printf '{"session":"fixture"}\n' > "$WORK/old bot/config.json"
cat > "$WORK/fixture" <<'BOT'
#!/usr/bin/env bash
set -euo pipefail
mode=$1; shift; root=; source=; kind=
while [ $# -gt 0 ]; do
 case "$1" in --root) root=$2;shift 2;; --from) kind=$2;shift 2;; *) source=$1;shift;; esac
done
case "$mode" in
 --migrate) [ -z "$(ls -A "$root")" ]; printf '%s\n' "$kind" >> "$WIZARD_LOG"; [ "${MIGRATE_FAIL:-0}" = 0 ]; cp "$source/config.json" "$root/config.json"; echo '{}' > "$root/migration-report.json";;
 --check) test -f "$root/config.json";;
 *) exit 1;;
esac
BOT
chmod +x "$WORK/fixture"
for choice in 1 2 3; do
 printf '%s\n%s\n%s\n' "$choice" "$WORK/old bot" 1 | WIZARD_LOG="$WORK/log" bash "$REPO/scripts/install.sh" --wizard --no-service --binary "$WORK/fixture" --root "$WORK/new-$choice" --source-stopped
 cmp "$WORK/old bot/config.json" "$WORK/new-$choice/config.json"
 test -x "$WORK/new-$choice/laowangbot"
done
printf 'mibot-lite\nmibox\ntelebox\n' > "$WORK/expected"
cmp "$WORK/log" "$WORK/expected"
# EOF cancels before touching the deployment.
if WIZARD_LOG="$WORK/log" bash "$REPO/scripts/install.sh" --wizard --no-service --binary "$WORK/fixture" --root "$WORK/eof" < /dev/null; then exit 1; fi
test ! -e "$WORK/eof"
# A nonempty destination is rejected before any source service action.
if printf '3\n%s\n' "$WORK/old bot" | WIZARD_LOG="$WORK/log" bash "$REPO/scripts/install.sh" --wizard --no-service --binary "$WORK/fixture" --root "$WORK/new-3"; then exit 1; fi
cat > "$WORK/mock/uname" <<'MOCK'
#!/usr/bin/env bash
case "$1" in -s) echo Linux;; -m) echo x86_64;; esac
MOCK
cat > "$WORK/mock/systemctl" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$SERVICE_LOG"
case "$1" in
 show)
   if [ "$4" = WorkingDirectory ]; then printf '%s\n' "$SOURCE_ROOT"
   elif [ -f "$STOP_FILE" ]; then echo inactive
   else echo "${SOURCE_STATE:-active}"; fi;;
 is-active) [ "${SOURCE_STATE:-active}" = active ] && test ! -f "$STOP_FILE";;
 stop) touch "$STOP_FILE";;
 start) rm -f "$STOP_FILE";;
esac
MOCK
chmod +x "$WORK/mock/"*
if printf '1\n%s\noldbot.service\n' "$WORK/old bot" | PATH="$WORK/mock:$PATH" SOURCE_ROOT="$WORK/old bot" STOP_FILE="$WORK/stopped" SERVICE_LOG="$WORK/service.log" WIZARD_LOG="$WORK/log" MIGRATE_FAIL=1 bash "$REPO/scripts/install.sh" --wizard --no-service --binary "$WORK/fixture" --root "$WORK/failed"; then exit 1; fi
# Migration fails after stop: restore only the service stopped by the installer.
grep -q '^stop oldbot.service$' "$WORK/service.log"
grep -q '^start oldbot.service$' "$WORK/service.log"
test ! -e "$WORK/stopped"
test ! -e "$WORK/failed/laowangbot"
echo 'Migration wizard tests passed'

# A mismatched service must never be stopped.
: > "$WORK/service.log"
if printf '1\n%s\nother.service\n' "$WORK/old bot" | PATH="$WORK/mock:$PATH" SOURCE_ROOT="$WORK/new-1" STOP_FILE="$WORK/stopped" SERVICE_LOG="$WORK/service.log" WIZARD_LOG="$WORK/log" bash "$REPO/scripts/install.sh" --wizard --no-service --binary "$WORK/fixture" --root "$WORK/mismatch"; then exit 1; fi
if grep -q '^stop ' "$WORK/service.log"; then echo 'Stopped mismatched service' >&2; exit 1; fi
if printf '1\n%s\n' "$WORK/old bot" | WIZARD_LOG="$WORK/log" bash "$REPO/scripts/install.sh" --wizard --no-service --binary "$WORK/fixture" --root "$WORK/old bot/nested"; then exit 1; fi
test ! -e "$WORK/old bot/nested"
echo 'Source-service matching and source containment tests passed'

# Transitional services may still have live processes even if is-active is nonzero.
: > "$WORK/service.log"
if printf '1\n%s\noldbot.service\n' "$WORK/old bot" | PATH="$WORK/mock:$PATH" SOURCE_STATE=activating SOURCE_ROOT="$WORK/old bot" STOP_FILE="$WORK/stopped" SERVICE_LOG="$WORK/service.log" WIZARD_LOG="$WORK/log" MIGRATE_FAIL=1 bash "$REPO/scripts/install.sh" --wizard --no-service --binary "$WORK/fixture" --root "$WORK/transitional"; then exit 1; fi
grep -q '^stop oldbot.service$' "$WORK/service.log"
grep -q '^start oldbot.service$' "$WORK/service.log"
echo 'Transitional source stop test passed'

# Failing before the target starts must still resume the source service.
cat > "$WORK/mock/id" <<'MOCK'
#!/usr/bin/env bash
echo 0
MOCK
cat > "$WORK/mock/mkdir" <<'MOCK'
#!/usr/bin/env bash
for arg in "$@"; do [ "$arg" != /etc/systemd/system ] || exit 1; done
exec /bin/mkdir "$@"
MOCK
cat > "$WORK/mock/systemctl" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$SERVICE_LOG"
case "$1" in
 show) if [ "$4" = WorkingDirectory ]; then echo "$SOURCE_ROOT"; elif [ -f "$STOP_FILE" ]; then echo inactive; else echo active; fi;;
 stop) [ "$2" != laowangbot ] || exit 5; touch "$STOP_FILE";;
 start) rm -f "$STOP_FILE";;
esac
MOCK
chmod +x "$WORK/mock/"*
: > "$WORK/service.log"
# Existing real units are outside this test's scope.
if [ ! -f /etc/systemd/system/laowangbot.service ]; then
 if printf '1\n%s\noldbot.service\n' "$WORK/old bot" | PATH="$WORK/mock:$PATH" SOURCE_ROOT="$WORK/old bot" STOP_FILE="$WORK/stopped" SERVICE_LOG="$WORK/service.log" WIZARD_LOG="$WORK/log" bash "$REPO/scripts/install.sh" --wizard --binary "$WORK/fixture" --root "$WORK/before-start"; then exit 1; fi
 grep -q '^start oldbot.service$' "$WORK/service.log"
 if grep -q '^stop laowangbot$' "$WORK/service.log"; then echo 'Stopped target that never started';exit 1;fi
 test ! -e "$WORK/stopped"
fi
echo 'Pre-start rollback test passed'
