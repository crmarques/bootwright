#!/usr/bin/env bash

set -o pipefail

required_samples=${1:-}
max_attempts=${2:-}
interval_seconds=${3:-}
deadline_seconds=${4:-}
quiet_seconds=${5:-}

case "$required_samples:$max_attempts:$interval_seconds:$deadline_seconds:$quiet_seconds" in
  *[!0-9:]*|:*|*::*|*:)
    exit 2
    ;;
esac

if [ "$required_samples" -lt 1 ] || [ "$max_attempts" -lt 1 ] || [ "$deadline_seconds" -lt 1 ]; then
  exit 2
fi

for dependency in awk head lvs ps pvs sed sleep sort timeout tr; do
  command -v "$dependency" >/dev/null 2>&1 || exit 127
done

started_at=$SECONDS

remaining_seconds() {
  local remaining=$((deadline_seconds - (SECONDS - started_at)))
  if [ "$remaining" -lt 1 ]; then
    return 75
  fi
  printf '%s\n' "$remaining"
}

bounded() {
  local remaining
  remaining=$(remaining_seconds) || return $?
  timeout --kill-after=2 "$remaining" "$@"
}

ceph_volume_writers() {
  bounded ps -eo pid=,args= | awk '$0 ~ /[c]eph-volume/ {print}'
}

scan_rows() {
  local pvs_rows lvs_rows pv vg tags fsid signed label
  pvs_rows=$(bounded pvs --noheadings --readonly --separator '|' --options pv_name,vg_name) || return $?
  lvs_rows=$(bounded lvs --noheadings --readonly --separator '|' --options vg_name,lv_tags) || return $?
  while IFS='|' read -r pv vg; do
    pv=$(printf '%s\n' "$pv" | awk '{$1=$1; print}')
    vg=$(printf '%s\n' "$vg" | awk '{$1=$1; print}')
    [ -n "$pv" ] || continue
    [ -n "$vg" ] || continue
    tags=$(printf '%s\n' "$lvs_rows" | awk -F '|' -v wanted="$vg" '
      {
        name=$1
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", name)
        if (name == wanted) {
          value=$2
          gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
          if (all != "" && value != "") all=all ","
          all=all value
        }
      }
      END { print all }
    ')
    fsid=$(printf '%s\n' "$tags" | tr ',' '\n' | sed -n 's/^[[:space:]]*ceph\.cluster_fsid=//p' | head -n 1)
    signed=no
    case "$vg" in ceph-*) signed=yes ;; esac
    if [ -n "$fsid" ]; then signed=yes; fi
    if [ "$signed" = no ]; then continue; fi
    label="$vg on $pv"
    if [ -n "$fsid" ]; then label="$vg (fsid $fsid) on $pv"; fi
    printf '{"vg":"%s","fsid":"%s","pv":"%s","label":"%s"}\n' "$vg" "$fsid" "$pv" "$label"
  done <<< "$pvs_rows" | LC_ALL=C sort -u
}

stable_samples=0
attempt=0
previous_rows=
quiet_started_at=
last_instability=

while [ "$attempt" -lt "$max_attempts" ]; do
  before=$(ceph_volume_writers) || exit $?
  first_rows=$(scan_rows) || exit $?
  rows=$(scan_rows) || exit $?
  after=$(ceph_volume_writers) || exit $?
  if [ -z "$before" ] && [ -z "$after" ] && [ "$first_rows" = "$rows" ]; then
    if [ "$stable_samples" -gt 0 ] && [ "$rows" = "$previous_rows" ]; then
      stable_samples=$((stable_samples + 1))
    else
      stable_samples=1
      quiet_started_at=$SECONDS
    fi
  else
    stable_samples=0
    quiet_started_at=
    if [ -n "$before" ] || [ -n "$after" ]; then
      last_instability=$(printf '%s\n%s\n' "$before" "$after" | sed '/^[[:space:]]*$/d' | LC_ALL=C sort -u)
    else
      last_instability="Ceph LVM rows changed within one final scan sample"
    fi
  fi
  previous_rows=$rows
  quiet_elapsed=0
  if [ -n "$quiet_started_at" ]; then quiet_elapsed=$((SECONDS - quiet_started_at)); fi
  if [ "$stable_samples" -ge "$required_samples" ] && [ "$quiet_elapsed" -ge "$quiet_seconds" ]; then
    if [ -n "$rows" ]; then printf '%s\n' "$rows"; fi
    exit 0
  fi
  attempt=$((attempt + 1))
  remaining_seconds >/dev/null || break
  if [ "$attempt" -lt "$max_attempts" ] && [ "$interval_seconds" -gt 0 ]; then
    sleep "$interval_seconds"
  fi
done

if [ -n "$last_instability" ]; then printf '%s\n' "$last_instability" >&2; fi
exit 75
