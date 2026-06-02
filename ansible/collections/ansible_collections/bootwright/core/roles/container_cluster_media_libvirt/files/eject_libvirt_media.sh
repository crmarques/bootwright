#!/usr/bin/env bash
set -u

uri=$1
domain=$2
state_arg=$3
retries=$4
delay=$5
scope=$6
shift 6
domblklist_args=("$@")
changed=0
absent_checks=0
tmp_files=()

trap 'for tmp in "${tmp_files[@]}"; do
  rm -f "$tmp"
done' EXIT

if [ "$state_arg" = "--live" ]; then
  domstate=$(virsh -c "$uri" domstate "$domain" 2>/dev/null || true)
  if [ "$domstate" != "running" ]; then
    echo "changed=false"
    exit 0
  fi
fi

read_targets() {
  local out rc
  if out=$(virsh -c "$uri" domblklist "$domain" "${domblklist_args[@]}" --details 2>&1); then
    printf '%s\n' "$out" | awk '$2 == "cdrom" && $4 != "-" && $4 != "" { print $3 }'
    return 0
  fi
  rc=$?
  printf 'domblklist failed for %s domain: rc=%d\n%s\n' "$scope" "$rc" "$out" >&2
  return "$rc"
}

load_targets() {
  local text line
  targets=()
  target_count=0
  text=$(read_targets) || return $?
  while IFS= read -r line; do
    if [ -n "$line" ]; then
      targets+=("$line")
      target_count=$((target_count + 1))
    fi
  done <<< "$text"
}

update_without_source() {
  local target=$1
  local xml tmp out rc
  if xml=$(virsh -c "$uri" change-media "$domain" "$target" --eject --force "$state_arg" --print-xml 2>&1); then
    tmp=$(mktemp)
    tmp_files+=("$tmp")
    printf '%s\n' "$xml" > "$tmp"
  else
    rc=$?
    printf 'change-media --print-xml %s failed: rc=%d\n%s\n' "$target" "$rc" "$xml" >&2
    return "$rc"
  fi
  if out=$(virsh -c "$uri" update-device "$domain" "$tmp" "$state_arg" --force 2>&1); then
    printf 'update-device %s succeeded\n%s\n' "$target" "$out"
    changed=1
    return 0
  fi
  rc=$?
  printf 'update-device %s failed: rc=%d\n%s\n' "$target" "$rc" "$out" >&2
  return "$rc"
}

eject_target() {
  local target=$1
  local out rc
  if out=$(virsh -c "$uri" change-media "$domain" "$target" --eject --force "$state_arg" 2>&1); then
    printf 'change-media %s succeeded\n%s\n' "$target" "$out"
    changed=1
    return 0
  fi
  rc=$?
  printf 'change-media %s failed: rc=%d\n%s\n' "$target" "$rc" "$out" >&2
  update_without_source "$target" || true
}

detach_target() {
  local target=$1
  local out rc
  if out=$(virsh -c "$uri" detach-disk "$domain" "$target" "$state_arg" 2>&1); then
    printf 'detach-disk %s succeeded\n%s\n' "$target" "$out"
    changed=1
    return 0
  fi
  rc=$?
  printf 'detach-disk %s failed: rc=%d\n%s\n' "$target" "$rc" "$out" >&2
  return "$rc"
}

for ((attempt = 1; attempt <= retries; attempt++)); do
  load_targets || exit 2
  if [ "$target_count" -gt 0 ]; then
    absent_checks=0
    printf 'attempt %d/%d source-backed targets: %s\n' "$attempt" "$retries" "${targets[*]}"
    for target in "${targets[@]}"; do
      eject_target "$target"
    done
  fi

  load_targets || exit 2
  if [ "$target_count" -gt 0 ]; then
    absent_checks=0
    printf 'attempt %d/%d remaining targets after eject: %s\n' "$attempt" "$retries" "${targets[*]}"
    for target in "${targets[@]}"; do
      detach_target "$target" || true
    done
  fi

  load_targets || exit 2
  if [ "$target_count" -eq 0 ]; then
    absent_checks=$((absent_checks + 1))
    if [ "$absent_checks" -ge 2 ]; then
      if [ "$changed" -eq 1 ]; then
        echo "changed=true"
      else
        echo "changed=false"
      fi
      exit 0
    fi
  else
    absent_checks=0
  fi

  if [ "$attempt" -lt "$retries" ]; then
    sleep "$delay"
  fi
done

printf 'source-backed libvirt cdrom targets still present after %s attempts: %s\n' "$retries" "${targets[*]}" >&2
virsh -c "$uri" domblklist "$domain" "${domblklist_args[@]}" --details >&2 || true
exit 1
