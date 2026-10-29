#!/usr/bin/env bash
#
# Render one end-to-end case into a Bootwright input directory.
#
#     e2e/render.sh lab-rhel
#     e2e/render.sh lab-rhel --set security.fips=true --out-dir ~/bootwright-e2e/lab-rhel-fips
#     e2e/render.sh lab-3-nodes-ceph --values ~/bootwright-e2e/my-ceph.yaml
#
# A case is a directory beside this script holding values.yaml (its default
# input) and templates/ (one Bootwright object per file, laid out as the input
# directory). Each --values file and --set overrides values.yaml in order.
# host.address "auto" takes the source address of this host's default route.
# The output defaults to ~/bootwright-e2e/<case>; a render replaces only an
# empty directory or an earlier render, never anything inside the repository.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cases=()
for dir in "$here"/*/; do
  [[ -f "$dir/values.yaml" && -d "$dir/templates" ]] && cases+=("$(basename "$dir")")
done

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
fi

work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
mkdir -- "$work/chart"
cp -R -- "$here/$case_name/templates" "$here/$case_name/values.yaml" "$work/chart/"
printf 'apiVersion: v2\nname: %s\nversion: 0.1.0\n' "$case_name" >"$work/chart/Chart.yaml"

detected="$(ip -4 route get 192.0.2.1 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p')"
helm template "$case_name" "$work/chart" "${overrides[@]}" \
  --set "host.detectedAddress=$detected" --output-dir "$work/out" >/dev/null

# Helm writes each non-empty template under <case>/templates/ with a "# Source:"
# header; keep the documents, one blank line at most between lines. The render
# is assembled in the work directory and replaces the output only once whole.
rendered="$work/out/$case_name/templates"
staged="$work/input"
[[ -d "$rendered" ]] || { echo "render.sh: the case rendered no object" >&2; exit 1; }
while IFS= read -r -d '' file; do
  relative="${file#"$rendered"/}"
  mkdir -p -- "$staged/$(dirname -- "$relative")"
  awk '
    /^# Source:/ { next }
    /^---$/ && !started { next }
    /^[[:space:]]*$/ { blank = started; next }
    {
      if (blank) print ""
      print
      started = 1
      blank = 0
    }
  ' "$file" >"$staged/$relative"
done < <(find "$rendered" -type f -name '*.yaml' -print0)

if grep -rn '{{' "$staged" >/dev/null; then
  echo "render.sh: template syntax survived the render of $case_name" >&2
  exit 1
fi
: >"$staged/$marker"

rm -rf -- "$abs_out"
mkdir -p -- "$(dirname -- "$abs_out")"
cp -R -- "$staged" "$abs_out"

echo "render.sh: $case_name rendered to $abs_out"
