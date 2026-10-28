#!/usr/bin/env bash
#
# Render one end-to-end case into a Bootwright input directory.
#
#     e2e/render.sh rhel
#     e2e/render.sh sno --set security.fips=true --out-dir ~/bootwright-e2e/sno-fips
#     e2e/render.sh ceph --values ~/bootwright-e2e/my-input.yaml
#
# e2e/input.yaml is always applied first; each --values file and --set
# overrides it in order. host.address "auto" takes the source address of this
# host's default route. The output directory is replaced on every run and
# defaults to ~/bootwright-e2e/<case>.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cases=(rhel ceph sno stretch stretch-arbiter stretch-ceph compact)

usage() {
  cat <<EOF
usage: e2e/render.sh CASE [--values FILE]... [--set KEY=VALUE]... [--out-dir DIR]

cases: ${cases[*]}
EOF
}

(($#)) || { usage >&2; exit 1; }
case_name="$1"
shift
[[ "$case_name" == -h || "$case_name" == --help ]] && { usage; exit 0; }
case " ${cases[*]} " in
  *" $case_name "*) ;;
  *) echo "render.sh: unknown case: $case_name" >&2; usage >&2; exit 1 ;;
esac

overrides=()
out_dir="$HOME/bootwright-e2e/$case_name"
while (($#)); do
  case "$1" in
    --values | --set)
      [[ $# -ge 2 && -n "$2" ]] || { echo "render.sh: $1 requires a value" >&2; exit 1; }
      if [[ "$1" == --values && ! -f "$2" ]]; then
        echo "render.sh: values file not found: $2" >&2
        exit 1
      fi
      overrides+=("$1" "$2")
      shift 2
      ;;
    --out-dir)
      [[ $# -ge 2 && -n "$2" ]] || { echo "render.sh: --out-dir requires a value" >&2; exit 1; }
      out_dir="$2"
      shift 2
      ;;
    -h | --help) usage; exit 0 ;;
    *) echo "render.sh: unexpected argument: $1" >&2; usage >&2; exit 1 ;;
  esac
done

command -v helm >/dev/null || { echo "render.sh: helm is required" >&2; exit 1; }

detected="$(ip -4 route get 192.0.2.1 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p')"
stream="$(helm template e2e "$here/chart" --values "$here/input.yaml" "${overrides[@]}" \
  --set "case=$case_name" --set "host.detectedAddress=$detected")"

# Only an empty directory or one an earlier render marked is ever replaced.
abs_out="$(realpath -m -- "$out_dir")"
marker=".bootwright-e2e-render"
repo="$(realpath -m -- "$here/..")"
case "$abs_out" in
  "$repo" | "$repo"/*)
    echo "render.sh: refusing to render inside the repository: $abs_out" >&2
    exit 1
    ;;
esac
if [[ -e "$abs_out" ]]; then
  if [[ ! -d "$abs_out" || -L "$abs_out" ]]; then
    echo "render.sh: $abs_out is not a directory" >&2
    exit 1
  fi
  if [[ ! -f "$abs_out/$marker" && -n "$(ls -A -- "$abs_out")" ]]; then
    echo "render.sh: refusing to replace $abs_out: not empty and not an earlier render" >&2
    exit 1
  fi
  rm -rf -- "$abs_out"
fi
mkdir -p -- "$abs_out"
: >"$abs_out/$marker"

# Every document opens with "# bw-path: <relative file>", the file it is
# written to: one document per file, never outside the output directory.
paths="$(printf '%s\n' "$stream" | sed -n 's/^# bw-path: //p')"
[[ -n "$paths" ]] || { echo "render.sh: no bw-path markers in the render" >&2; exit 1; }
if duplicate="$(sort <<<"$paths" | uniq -d | grep .)"; then
  echo "render.sh: two documents name $duplicate" >&2
  exit 1
fi
while IFS= read -r path; do
  if [[ ! "$path" =~ ^[a-z0-9][a-z0-9./-]*\.yaml$ || "$path" == *..* ]]; then
    echo "render.sh: unsafe bw-path: $path" >&2
    exit 1
  fi
  mkdir -p -- "$abs_out/$(dirname -- "$path")"
done <<<"$paths"

printf '%s\n' "$stream" | awk -v out="$abs_out" '
  /^# Source:/ || /^---$/ { next }
  /^# bw-path: / {
    if (file != "") close(file)
    file = out "/" $3
    started = 0
    blank = 0
    next
  }
  file == "" { next }
  /^[[:space:]]*$/ { blank = started; next }
  {
    if (blank) print "" > file
    print > file
    started = 1
    blank = 0
  }
'

if grep -rn '{{' "$abs_out" >/dev/null; then
  echo "render.sh: template syntax survived the render in $abs_out" >&2
  exit 1
fi

echo "render.sh: $case_name rendered to $abs_out"
