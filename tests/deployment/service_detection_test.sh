#!/usr/bin/env bash
set -euo pipefail
REPO=$(cd "$(dirname "$0")/../.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/mock" "$WORK/old" "$WORK/unrelated"
printf '{"session":"fixture"}\n' > "$WORK/old/config.json"
cat > "$WORK/mock/uname" <<'MOCK'
#!/usr/bin/env bash
case "$1" in -s) echo Linux;; -m) echo x86_64;; esac
MOCK
cat > "$WORK/mock/systemctl" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$SERVICE_LOG"
case "$1" in
 list-units) [ "${MODE:-one}" != failed ] || exit 1
  printf 'laowangbot.service loaded active running new\nunrelated.service loaded active running unrelated\n'
  [ "${MODE:-one}" != none ] || exit 0
  printf 'custom-old.service loaded active running old\n'
  [ "${MODE:-one}" != many ] || printf 'other-old.service loaded inactive dead old\n';;
 list-unit-files) [ "${MODE:-one}" != failed ] || exit 1
  [ "${MODE:-one}" != none ] || exit 0
  printf 'custom-old.service enabled\n'
  [ "${MODE:-one}" != alias ] || printf 'alias-old.service enabled\n';;
 show)
  if [ "$4" = TriggeredBy ]; then echo "${SOURCE_TRIGGERS:-}"
   elif [ "$4" = UnitFileState ]; then if [ -f "$STOP_FILE.disabled" ]; then echo disabled; else echo "${SOURCE_ENABLED:-enabled}"; fi
   elif [ "$4" = WorkingDirectory ]; then
   case "$2" in custom-old.service|other-old.service|mibot-lite.service|alias-old.service) printf '%s\n' "$SOURCE_ROOT";; *) printf '%s\n' "$OTHER_ROOT";; esac
  elif [ "$4" = Id ]; then if [ "$2" = alias-old.service ];then echo custom-old.service;else echo "$2";fi
  elif [ -f "$STOP_FILE" ]; then echo inactive
  else echo "${SOURCE_STATE:-activating}"; fi;;
 disable) touch "$STOP_FILE.disabled"; [ "${DISABLE_FAIL:-0}" = 0 ];;
 enable) rm -f "$STOP_FILE.disabled";;
 stop) case "$2" in custom-old.service|other-old.service|mibot-lite.service) ;; *) exit 9;; esac; touch "$STOP_FILE";;
 start) case "$2" in custom-old.service|other-old.service|mibot-lite.service) ;; *) exit 9;; esac; rm -f "$STOP_FILE";;
esac
MOCK
cat > "$WORK/binary" <<'BOT'
#!/usr/bin/env bash
set -euo pipefail
mode=$1;shift;root=;source=
while [ $# -gt 0 ];do case "$1" in --root)root=$2;shift 2;; --from)shift 2;; *)source=$1;shift;;esac;done
case "$mode" in --migrate) [ "${MIGRATE_FAIL:-0}" = 0 ];cp "$source/config.json" "$root/config.json";; --check)test -f "$root/config.json";; *)exit 1;;esac
BOT
chmod +x "$WORK/mock/"* "$WORK/binary"
export PATH="$WORK/mock:$PATH" SOURCE_ROOT="$WORK/old" OTHER_ROOT="$WORK/unrelated" SERVICE_LOG="$WORK/log" STOP_FILE="$WORK/stopped"
# Only bot selection and path input: no stop-mode/service-name input at all.
printf '1\n%s\n' "$WORK/old" | bash "$REPO/scripts/install.sh" --wizard --no-service --binary "$WORK/binary" --root "$WORK/new"
grep -q '^stop custom-old.service$' "$WORK/log"
cmp "$WORK/old/config.json" "$WORK/new/config.json"
for mode in none many failed;do
 : > "$WORK/log"
 if printf '1\n%s\n' "$WORK/old" | MODE=$mode bash "$REPO/scripts/install.sh" --wizard --no-service --binary "$WORK/binary" --root "$WORK/$mode";then echo "Accepted $mode";exit 1;fi
 if grep -q '^stop ' "$WORK/log";then echo 'Stopped ambiguous/unmatched service';exit 1;fi
done
# Manual-stop still discovers and disables the matching boot service.
: > "$WORK/log"
printf '1\n%s\n' "$WORK/old" | MODE=one bash "$REPO/scripts/install.sh" --wizard --source-stopped --no-service --binary "$WORK/binary" --root "$WORK/manual"
grep -q '^disable custom-old.service$' "$WORK/log"
# Failed migration resumes precisely the automatically stopped source.
rm -f "$STOP_FILE" "$STOP_FILE.disabled"
: > "$WORK/log"
if printf '1\n%s\n' "$WORK/old" | MIGRATE_FAIL=1 bash "$REPO/scripts/install.sh" --wizard --no-service --binary "$WORK/binary" --root "$WORK/failure";then exit 1;fi
grep -q '^start custom-old.service$' "$WORK/log"
echo 'Automatic source service detection tests passed'

: > "$WORK/log"
printf '1\n%s\n2\n' "$WORK/old" | MODE=many SOURCE_STATE=inactive SOURCE_ENABLED=disabled bash "$REPO/scripts/install.sh" --wizard --no-service --binary "$WORK/binary" --root "$WORK/multiple-picked"
grep -q '^stop other-old.service$' "$WORK/log"
: > "$WORK/log"
printf '1\n%s\n\n' "$WORK/old" | MODE=none bash "$REPO/scripts/install.sh" --wizard --no-service --binary "$WORK/binary" --root "$WORK/fallback-default"
grep -q '^stop mibot-lite.service$' "$WORK/log"
echo 'Multiple matching units and default-name fallback passed'

: > "$WORK/log"
printf '1\n%s\n' "$WORK/old" | MODE=alias bash "$REPO/scripts/install.sh" --wizard --no-service --binary "$WORK/binary" --root "$WORK/alias"
grep -q '^stop custom-old.service$' "$WORK/log"
if grep -q '^stop alias-old.service$' "$WORK/log";then exit 1;fi
echo 'Canonical service alias deduplication passed'

# Boot simulation: only enabled units start again; migrated sources stay inactive.
rm -f "$STOP_FILE" "$STOP_FILE.disabled"
: > "$WORK/log"
bash "$REPO/scripts/install.sh" --migrate "$WORK/old" --no-service --binary "$WORK/binary" --root "$WORK/noninteractive"
test -f "$STOP_FILE.disabled"
if [ "$(systemctl show custom-old.service -p UnitFileState --value)" = enabled ]; then systemctl start custom-old.service; fi
test -f "$STOP_FILE"

# Preserve original persistent/runtime enablement and original activity on failure.
for enabled in enabled enabled-runtime disabled; do
 for active in active inactive; do
  rm -f "$STOP_FILE" "$STOP_FILE.disabled"
  : > "$WORK/log"
  if SOURCE_ENABLED=$enabled SOURCE_STATE=$active MIGRATE_FAIL=1 bash "$REPO/scripts/install.sh" --migrate "$WORK/old" --no-service --binary "$WORK/binary" --root "$WORK/restore-$enabled-$active"; then exit 1; fi
  case "$enabled" in
   enabled) grep -q '^enable custom-old.service$' "$WORK/log"; test ! -e "$STOP_FILE.disabled";;
   enabled-runtime) grep -q '^enable --runtime custom-old.service$' "$WORK/log"; test ! -e "$STOP_FILE.disabled";;
   disabled) if grep -q '^enable ' "$WORK/log"; then exit 1; fi;;
  esac
  if [ "$active" = active ]; then grep -q '^start custom-old.service$' "$WORK/log"
  elif grep -q '^start ' "$WORK/log"; then echo 'Started originally inactive source'; exit 1; fi
 done
done

# Partially failed disable must restore enablement and running source before exit.
rm -f "$STOP_FILE" "$STOP_FILE.disabled"
: > "$WORK/log"
if DISABLE_FAIL=1 bash "$REPO/scripts/install.sh" --migrate "$WORK/old" --no-service --binary "$WORK/binary" --root "$WORK/disable-failed"; then exit 1; fi
grep -q '^enable custom-old.service$' "$WORK/log"
grep -q '^start custom-old.service$' "$WORK/log"
test ! -e "$WORK/disable-failed"

# Another enabled/active unit or a timer can restart the old deployment; refuse it.
for mode in duplicate trigger; do
 rm -f "$STOP_FILE" "$STOP_FILE.disabled"
 : > "$WORK/log"
 if [ "$mode" = duplicate ]; then
  if printf '1\n' | MODE=many bash "$REPO/scripts/install.sh" --migrate "$WORK/old" --no-service --binary "$WORK/binary" --root "$WORK/reject-$mode"; then exit 1; fi
 else
  if SOURCE_TRIGGERS=old.timer bash "$REPO/scripts/install.sh" --migrate "$WORK/old" --no-service --binary "$WORK/binary" --root "$WORK/reject-$mode"; then exit 1; fi
 fi
 if grep -Eq '^(stop|disable) ' "$WORK/log"; then echo 'Changed source before rejecting unsafe activation'; exit 1; fi
done
echo 'Source boot prevention, original-state rollback, and activation guards passed'
