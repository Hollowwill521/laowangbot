#!/usr/bin/env bash
set -euo pipefail
REPO=$(cd "$(dirname "$0")/../.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
cat > "$WORK/fixture" <<'BOT'
#!/usr/bin/env bash
set -euo pipefail
mode=$1; shift
root=; source=
while [ $# -gt 0 ]; do
  case "$1" in --root) root=$2; shift 2;; --from) shift 2;; *) source=$1; shift;; esac
done
case "$mode" in
  --migrate) [ -z "$(ls -A "$root")" ]; cp "$source/config.json" "$root/config.json";;
  --check) [ -f "$root/config.json" ]; [ ! -f "$root/fail-check" ];;
  *) exit 1;;
esac
BOT
chmod +x "$WORK/fixture"
mkdir "$WORK/source"
printf '{"session":"fixture"}\n' > "$WORK/source/config.json"
bash "$REPO/scripts/install.sh" --no-service --binary "$WORK/fixture" --migrate "$WORK/source" --root "$WORK/new"
cmp "$WORK/source/config.json" "$WORK/new/config.json"
[ -x "$WORK/new/laowangbot" ]
if bash "$REPO/scripts/install.sh" --no-service --binary "$WORK/fixture" --migrate "$WORK/source" --root "$WORK/new"; then echo 'Occupied migration accepted' >&2; exit 1; fi
cp "$WORK/fixture" "$WORK/v2"; printf '\n# version two\n' >> "$WORK/v2"
bash "$REPO/scripts/install.sh" --no-service --binary "$WORK/v2" --root "$WORK/new"
cmp "$WORK/fixture" "$WORK/new/laowangbot.previous"
cmp "$WORK/v2" "$WORK/new/laowangbot"
touch "$WORK/new/fail-check"
if bash "$REPO/scripts/install.sh" --no-service --binary "$WORK/fixture" --root "$WORK/new"; then echo 'Invalid update accepted' >&2; exit 1; fi
cmp "$WORK/v2" "$WORK/new/laowangbot"
cmp "$WORK/source/config.json" "$WORK/new/config.json"
bash -n "$REPO/scripts/install.sh" "$REPO/scripts/install-service.sh"
echo 'Offline deployment tests passed: migration, occupied destination, update backup, rejected update preservation'
# Exercise service-start failure and rollback without touching real launchd/systemd.
mkdir -p "$WORK/mock" "$WORK/home/Library/LaunchAgents" "$WORK/service"
cp "$WORK/source/config.json" "$WORK/service/config.json"
cp "$WORK/fixture" "$WORK/service/laowangbot"
printf '<string>%s</string>\n' "$WORK/service" > "$WORK/home/Library/LaunchAgents/io.github.laowangbot.plist"
cp "$WORK/home/Library/LaunchAgents/io.github.laowangbot.plist" "$WORK/original.plist"
cat > "$WORK/mock/uname" <<'MOCK'
#!/usr/bin/env bash
case "$1" in -s) echo Darwin;; -m) echo arm64;; esac
MOCK
cat > "$WORK/mock/launchctl" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$MOCK_LOG"
if [ "$1" = bootstrap ] && [ ! -f "$MOCK_ONCE" ]; then touch "$MOCK_ONCE"; exit 1; fi
MOCK
chmod +x "$WORK/mock/"*
if PATH="$WORK/mock:$PATH" HOME="$WORK/home" MOCK_LOG="$WORK/log" MOCK_ONCE="$WORK/once" bash "$REPO/scripts/install.sh" --binary "$WORK/v2" --root "$WORK/service"; then echo 'Failed service start accepted' >&2; exit 1; fi
cmp "$WORK/fixture" "$WORK/service/laowangbot"
cmp "$WORK/original.plist" "$WORK/home/Library/LaunchAgents/io.github.laowangbot.plist"
[ "$(grep -c '^bootstrap ' "$WORK/log")" = 2 ]
echo 'Mock launchd rollback test passed'
