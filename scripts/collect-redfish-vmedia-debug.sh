#!/usr/bin/env bash
set -Eeuo pipefail

IFS=$'\n\t'
umask 077

program_name=${0##*/}
cluster_dir=""
cluster_dir_explicit=0
output_parent=""
bootwright_cli="${BOOTWRIGHT_BIN:-}"
bootwright_context="${BOOTWRIGHT_CONTEXT:-}"
discovered_context_dir=""
context_input_dir=""
context_state_dir=""
context_secrets_dir=""
bootwright_root_dir="/var/lib/bootwright"
context_source=""
iso_url="${BOOTWRIGHT_ISO_URL:-}"
iso_path="${BOOTWRIGHT_AGENT_ISO_PATH:-${BOOTWRIGHT_ISO_PATH:-}}"
iso_path_explicit=0
iso_path_status="not discovered"
iso_urls_file=""
bmc_url="${BOOTWRIGHT_REDFISH_BASE_URL:-}"
system_uri="${BOOTWRIGHT_REDFISH_SYSTEM_URI:-}"
vmedia_uri="${BOOTWRIGHT_REDFISH_VMEDIA_URI:-}"
task_url="${BOOTWRIGHT_REDFISH_TASK_URL:-}"
security_uri="${BOOTWRIGHT_REDFISH_SECURITY_URI:-}"
bmc_user="${BOOTWRIGHT_REDFISH_USER:-${BMC_USER:-}}"
bmc_password="${BOOTWRIGHT_REDFISH_PASSWORD:-${BMC_PASSWORD:-${BMC_PASS:-}}}"
bmc_password_file="${BOOTWRIGHT_REDFISH_PASSWORD_FILE:-${BMC_PASSWORD_FILE:-}}"
bmc_credentials_ref=""
provider_disable_certificate_verification=""
redfish_ca=""
redfish_insecure=0
redfish_tls_explicit=0
redfish_tls_reason="default"
prompt_for_password=1
redfish_insert_attempt=1
redfish_eject_first=0
state_change_yes=0
curl_timeout=25
max_text_copy_bytes=$((20 * 1024 * 1024))
work_dir=""
tmp_dir=""
auth_config=""
notes_file=""
last_redfish_headers=""
last_redfish_body=""

usage() {
  cat <<EOF
Usage:
  ${program_name} [--cluster-dir PATH] [options]

Context Discovery:
  The collector uses --cluster-dir first. When omitted, it reads
  BOOTWRIGHT_CONTEXT or runs bootwright print-env --sensitive, then derives the
  fixed context path from /var/lib/bootwright/contexts/<context>.

Options:
  --cluster-dir PATH              Bootwright context/base directory that contains state/
  --bootwright PATH               bootwright binary to use for current-context discovery
  --output-dir PATH               Directory where the debug bundle directory is created
  --iso-path PATH                 Staged agent ISO file or directory containing agent-*.iso
  --iso-url URL                   Agent ISO URL; auto-detected from ansible-output.log when possible
  --bmc-url URL                   Redfish base URL; auto-detected from the failed task URL when possible
  --system-uri URI                Redfish ComputerSystem URI; auto-detected when possible
  --vmedia-uri URI                VirtualMedia URI; auto-detected from ansible-output.log when possible
  --task-url URL                  Redfish task URL; auto-detected from ansible-output.log when possible
  --security-uri URI              Manager SecurityService URI; auto-detected from ansible-output.log when possible
  --bmc-user USER                 Redfish username
  --bmc-password-file PATH        File containing password or username:password
  --redfish-insecure              Disable Redfish BMC TLS certificate verification
  --redfish-secure                Verify the Redfish BMC TLS certificate
  --redfish-ca PATH               CA bundle for Redfish BMC TLS verification
  --no-redfish-insert             Skip the diagnostic InsertMedia attempt
  --eject-first                   Eject virtual media before the diagnostic InsertMedia attempt
  --yes                           Consent to state-changing Redfish calls
  --no-prompt                     Do not prompt for missing Redfish credentials
  --timeout SECONDS               Curl max-time for each network request; default: ${curl_timeout}
  -h, --help                      Show this help

Environment:
  BOOTWRIGHT_BIN, BOOTWRIGHT_CONTEXT, BOOTWRIGHT_AGENT_ISO_PATH, BOOTWRIGHT_ISO_PATH
  BOOTWRIGHT_REDFISH_USER, BOOTWRIGHT_REDFISH_PASSWORD, BOOTWRIGHT_REDFISH_PASSWORD_FILE
  BOOTWRIGHT_REDFISH_BASE_URL, BOOTWRIGHT_REDFISH_SYSTEM_URI
  BOOTWRIGHT_REDFISH_VMEDIA_URI, BOOTWRIGHT_ISO_URL

The collector performs local checks, ISO file/URL probes, authenticated
Redfish GETs when possible, and one diagnostic virtual-media attach sequence
when enough Redfish and ISO data is available. It also reads the provider
desired state and context-local credentialRef file when available, without
copying credential values into the bundle. State-changing Redfish calls are
skipped unless --yes is set. With --yes, it powers the system off, inserts the
ISO, sets one-time CD boot, and powers the system on. Use --no-redfish-insert
to skip Redfish writes.
EOF
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

info() {
  printf '%s\n' "$*"
}

have() {
  command -v "$1" >/dev/null 2>&1
}

find_bootwright_cli() {
  local script_dir

  if [[ -n "${bootwright_cli}" ]]; then
    [[ -x "${bootwright_cli}" ]] || die "--bootwright is not executable: ${bootwright_cli}"
    return 0
  fi

  if have bootwright; then
    bootwright_cli=$(command -v bootwright)
    return 0
  fi

  script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
  if [[ -x "${script_dir}/../bin/bootwright" ]]; then
    bootwright_cli="${script_dir}/../bin/bootwright"
    return 0
  fi

  if [[ -x "./bin/bootwright" ]]; then
    bootwright_cli="./bin/bootwright"
    return 0
  fi

  return 1
}

decode_shell_export_value() {
  local raw=$1
  local decoded

  if [[ "${raw}" == "''" ]]; then
    printf ''
    return 0
  fi
  if [[ "${raw}" =~ ^[A-Za-z0-9_@%+=:,./-]+$ ]]; then
    printf '%s' "${raw}"
    return 0
  fi
  if [[ "${raw}" == \'*\' ]]; then
    decoded=$(eval "printf '%s' ${raw}") || return 1
    printf '%s' "${decoded}"
    return 0
  fi
  return 1
}

apply_bootwright_export_line() {
  local line=$1
  local key raw value

  [[ "${line}" =~ ^export[[:space:]]+([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]] || return 0
  key=${BASH_REMATCH[1]}
  raw=${BASH_REMATCH[2]}
  case "${key}" in
    BOOTWRIGHT_CONTEXT|HTTP_PROXY|HTTPS_PROXY|NO_PROXY|http_proxy|https_proxy|no_proxy)
      ;;
    *)
      return 0
      ;;
  esac

  value=$(decode_shell_export_value "${raw}") || return 1
  case "${key}" in
    BOOTWRIGHT_CONTEXT)
      bootwright_context=${bootwright_context:-${value}}
      ;;
    HTTP_PROXY|HTTPS_PROXY|NO_PROXY|http_proxy|https_proxy|no_proxy)
      export "${key}=${value}"
      ;;
  esac
}

load_bootwright_print_env() {
  local line output status

  find_bootwright_cli || return 1
  output=$("${bootwright_cli}" print-env --sensitive 2>&1) || {
    status=$?
    printf '%s\n' "bootwright print-env --sensitive failed with exit code ${status}:" >&2
    printf '%s\n' "${output}" >&2
    return "${status}"
  }
  while IFS= read -r line; do
    apply_bootwright_export_line "${line}"
  done <<<"${output}"
  context_source="bootwright print-env --sensitive"
}

resolve_context_dir() {
  if [[ "${cluster_dir_explicit}" -eq 1 ]]; then
    context_source="--cluster-dir"
  elif [[ -n "${bootwright_context}" ]]; then
    cluster_dir="${bootwright_root_dir}/contexts/${bootwright_context}"
    context_source="BOOTWRIGHT_CONTEXT"
  else
    load_bootwright_print_env || die "could not discover current Bootwright context; pass --cluster-dir, set BOOTWRIGHT_CONTEXT, or install bootwright in PATH"
    if [[ -n "${bootwright_context}" ]]; then
      cluster_dir="${bootwright_root_dir}/contexts/${bootwright_context}"
    fi
  fi

  [[ -n "${cluster_dir}" ]] || die "current Bootwright context could not be resolved"
  [[ -d "${cluster_dir}" ]] || die "context directory does not exist: ${cluster_dir}"
  cluster_dir=$(cd "${cluster_dir}" && pwd -P)
  discovered_context_dir=${discovered_context_dir:-${cluster_dir}}
  context_input_dir=${context_input_dir:-${cluster_dir}/input-files}
  context_state_dir=${context_state_dir:-${cluster_dir}/state}
  context_secrets_dir=${context_secrets_dir:-${cluster_dir}/secrets}
}

cleanup() {
  if [[ -n "${tmp_dir}" && -d "${tmp_dir}" ]]; then
    rm -rf "${tmp_dir}"
  fi
}

trap cleanup EXIT

while [[ $# -gt 0 ]]; do
  case "$1" in
    --cluster-dir)
      [[ $# -ge 2 ]] || die "--cluster-dir needs a value"
      cluster_dir=$2
      cluster_dir_explicit=1
      shift 2
      ;;
    --bootwright)
      [[ $# -ge 2 ]] || die "--bootwright needs a value"
      bootwright_cli=$2
      shift 2
      ;;
    --output-dir)
      [[ $# -ge 2 ]] || die "--output-dir needs a value"
      output_parent=$2
      shift 2
      ;;
    --iso-path)
      [[ $# -ge 2 ]] || die "--iso-path needs a value"
      iso_path=$2
      iso_path_explicit=1
      shift 2
      ;;
    --iso-url)
      [[ $# -ge 2 ]] || die "--iso-url needs a value"
      iso_url=$2
      shift 2
      ;;
    --bmc-url)
      [[ $# -ge 2 ]] || die "--bmc-url needs a value"
      bmc_url=$2
      shift 2
      ;;
    --system-uri)
      [[ $# -ge 2 ]] || die "--system-uri needs a value"
      system_uri=$2
      shift 2
      ;;
    --vmedia-uri)
      [[ $# -ge 2 ]] || die "--vmedia-uri needs a value"
      vmedia_uri=$2
      shift 2
      ;;
    --task-url)
      [[ $# -ge 2 ]] || die "--task-url needs a value"
      task_url=$2
      shift 2
      ;;
    --security-uri)
      [[ $# -ge 2 ]] || die "--security-uri needs a value"
      security_uri=$2
      shift 2
      ;;
    --bmc-user)
      [[ $# -ge 2 ]] || die "--bmc-user needs a value"
      bmc_user=$2
      shift 2
      ;;
    --bmc-password-file)
      [[ $# -ge 2 ]] || die "--bmc-password-file needs a value"
      bmc_password_file=$2
      shift 2
      ;;
    --redfish-secure)
      redfish_insecure=0
      redfish_tls_explicit=1
      redfish_tls_reason="explicit_secure"
      shift
      ;;
    --redfish-insecure)
      redfish_insecure=1
      redfish_tls_explicit=1
      redfish_tls_reason="explicit_insecure"
      shift
      ;;
    --redfish-ca)
      [[ $# -ge 2 ]] || die "--redfish-ca needs a value"
      redfish_ca=$2
      redfish_insecure=0
      redfish_tls_explicit=1
      redfish_tls_reason="explicit_ca"
      shift 2
      ;;
    --no-redfish-insert)
      redfish_insert_attempt=0
      shift
      ;;
    --eject-first)
      redfish_eject_first=1
      shift
      ;;
    --yes)
      state_change_yes=1
      shift
      ;;
    --no-prompt)
      prompt_for_password=0
      shift
      ;;
    --timeout)
      [[ $# -ge 2 ]] || die "--timeout needs a value"
      curl_timeout=$2
      [[ "${curl_timeout}" =~ ^[0-9]+$ ]] || die "--timeout must be an integer"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

resolve_context_dir

if [[ -z "${output_parent}" ]]; then
  output_parent=$PWD
fi
mkdir -p "${output_parent}"
output_parent=$(cd "${output_parent}" && pwd -P)

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
work_dir="${output_parent}/bootwright-redfish-vmedia-debug-${timestamp}"
tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/bootwright-redfish-debug.XXXXXX")
notes_file="${tmp_dir}/notes.txt"
iso_urls_file="${tmp_dir}/iso-urls.txt"
: >"${notes_file}"
: >"${iso_urls_file}"
mkdir -p "${work_dir}/commands" "${work_dir}/files" "${work_dir}/redfish" "${work_dir}/iso"

safe_sed_literal() {
  printf '%s' "$1" | sed -e 's/[\/&]/\\&/g'
}

redact_stream() {
  local sed_args=()

  if [[ -n "${bmc_password}" ]]; then
    sed_args+=("-e" "s/$(safe_sed_literal "${bmc_password}")/<redacted>/g")
  fi

  sed "${sed_args[@]}" \
    -e 's/\([Aa]uthorization:[[:space:]]*\)\(Basic\|Bearer\)[[:space:]][A-Za-z0-9+\/._=-]\+/\1<redacted>/g' \
    -e 's/\([Pp]assword[[:space:]]*[:=][[:space:]]*\)["'\'']\{0,1\}[^"'\''[:space:],}]\+/\1<redacted>/g' \
    -e 's/\([Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd][[:space:]]*[:=][[:space:]]*\)["'\'']\{0,1\}[^"'\''[:space:],}]\+/\1<redacted>/g' \
    -e 's/\([Pp]asswd[[:space:]]*[:=][[:space:]]*\)["'\'']\{0,1\}[^"'\''[:space:],}]\+/\1<redacted>/g' \
    -e 's/\([Tt]oken[[:space:]]*[:=][[:space:]]*\)["'\'']\{0,1\}[^"'\''[:space:],}]\+/\1<redacted>/g' \
    -e 's/\([Tt][Oo][Kk][Ee][Nn][[:space:]]*[:=][[:space:]]*\)["'\'']\{0,1\}[^"'\''[:space:],}]\+/\1<redacted>/g' \
    -e 's/\([Ss]ecret[[:space:]]*[:=][[:space:]]*\)["'\'']\{0,1\}[^"'\''[:space:],}]\+/\1<redacted>/g' \
    -e 's/\([Ss][Ee][Cc][Rr][Ee][Tt][[:space:]]*[:=][[:space:]]*\)["'\'']\{0,1\}[^"'\''[:space:],}]\+/\1<redacted>/g' \
    -e 's#\(https\{0,1\}://\)[^/@[:space:]]\+:[^/@[:space:]]\+@#\1<redacted>@#g'
}

redact_file() {
  local src=$1
  local dest=$2
  mkdir -p "$(dirname "${dest}")"
  if [[ -s "${src}" ]]; then
    redact_stream <"${src}" >"${dest}"
  else
    : >"${dest}"
  fi
}

run_cmd() {
  local name=$1
  shift
  local raw="${tmp_dir}/${name}.raw"
  local out="${work_dir}/commands/${name}.txt"
  local status
  {
    printf '$'
    printf ' %q' "$@"
    printf '\n\n'
    "$@"
    printf '\nexit_code=%s\n' "$?"
  } >"${raw}" 2>&1 || {
    status=$?
    printf '\nexit_code=%s\n' "${status}" >>"${raw}"
  }
  redact_file "${raw}" "${out}"
}

first_ipv4() {
  getent ahostsv4 "$1" 2>/dev/null | awk 'NR == 1 {print $1}'
}

run_openssl_s_client() {
  local name=$1
  local host=$2
  local port=$3

  if have timeout; then
    run_cmd "${name}" timeout "${curl_timeout}" openssl s_client -connect "${host}:${port}" -servername "${host}" -showcerts
  else
    run_cmd "${name}" openssl s_client -connect "${host}:${port}" -servername "${host}" -showcerts
  fi
}

append_note() {
  if [[ -n "${notes_file}" ]]; then
    printf '%s\n' "$*" >>"${notes_file}"
  elif [[ -n "${work_dir}" ]]; then
    printf '%s\n' "$*" >>"${work_dir}/summary.txt"
  fi
}

record_iso_url() {
  local url=$1

  [[ -n "${url}" ]] || return 0
  [[ "${url}" =~ ^https?:// ]] || return 0
  grep -Fx -- "${url}" "${iso_urls_file}" >/dev/null 2>&1 || printf '%s\n' "${url}" >>"${iso_urls_file}"
}

python_url_part() {
  local url=$1
  local part=$2
  if have python3; then
    python3 - "$url" "$part" <<'PY'
import sys
from urllib.parse import urlsplit

url = sys.argv[1]
part = sys.argv[2]
parsed = urlsplit(url)
if part == "origin":
    if not parsed.scheme or not parsed.netloc:
        sys.exit(1)
    print(f"{parsed.scheme}://{parsed.netloc}")
elif part == "scheme":
    if parsed.scheme:
        print(parsed.scheme)
    else:
        sys.exit(1)
elif part == "host":
    if parsed.hostname:
        print(parsed.hostname)
    else:
        sys.exit(1)
elif part == "port":
    if parsed.port:
        print(parsed.port)
    elif parsed.scheme == "https":
        print("443")
    elif parsed.scheme == "http":
        print("80")
    else:
        sys.exit(1)
else:
    sys.exit(1)
PY
  else
    return 1
  fi
}

url_basename() {
  local url=$1
  if have python3; then
    python3 - "$url" <<'PY'
import posixpath
import sys
from urllib.parse import unquote, urlsplit

path = urlsplit(sys.argv[1]).path
name = posixpath.basename(path)
if name:
    print(unquote(name))
PY
  fi
}

sanitize_name() {
  local value=$1
  value=${value//[^A-Za-z0-9_.-]/_}
  printf '%s\n' "${value}"
}

redfish_absolute_url() {
  local ref=$1
  if [[ "${ref}" =~ ^https?:// ]]; then
    printf '%s' "${ref}"
  elif [[ "${ref}" == /* ]]; then
    printf '%s%s' "${bmc_url}" "${ref}"
  else
    printf '%s/%s' "${bmc_url}" "${ref}"
  fi
}

json_members() {
  local file=$1
  have python3 || return 0
  [[ -s "${file}" ]] || return 0
  python3 - "${file}" <<'PY' 2>/dev/null || true
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)
for member in data.get("Members", []):
    ref = member.get("@odata.id") if isinstance(member, dict) else ""
    if ref:
        print(ref)
PY
}

json_top_field() {
  local file=$1
  local field=$2
  have python3 || return 0
  [[ -s "${file}" ]] || return 0
  python3 - "${file}" "${field}" <<'PY' 2>/dev/null || true
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)
value = data.get(sys.argv[2])
if isinstance(value, (str, int, float, bool)):
    print(value)
PY
}

json_has_top_field() {
  local file=$1
  local field=$2
  have python3 || return 1
  [[ -s "${file}" ]] || return 1
  python3 - "${file}" "${field}" <<'PY' >/dev/null 2>&1
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)
sys.exit(0 if sys.argv[2] in data else 1)
PY
}

header_value() {
  local file=$1
  local name=$2
  awk -v want="$(tr '[:upper:]' '[:lower:]' <<<"${name}")" '
    BEGIN { FS = ":" }
    {
      key = tolower($1)
      if (key == want) {
        sub(/^[^:]*:[[:space:]]*/, "")
        sub(/\r$/, "")
        print
        exit
      }
    }
  ' "${file}" 2>/dev/null || true
}

etag_from_response() {
  local body=$1
  local headers=$2
  local etag

  etag=$(json_top_field "${body}" "@odata.etag" | tail -n 1 || true)
  if [[ -z "${etag}" ]]; then
    etag=$(header_value "${headers}" ETag | tail -n 1 || true)
  fi
  printf '%s' "${etag:-*}"
}

redfish_task_url_from_response() {
  local body=$1
  local headers=$2
  local ref

  if have python3 && [[ -s "${body}" ]]; then
    ref=$(python3 - "${body}" <<'PY' 2>/dev/null || true
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)
for key in ("@odata.id", "TaskMonitor"):
    value = data.get(key)
    if isinstance(value, str) and value:
        print(value)
        break
PY
)
  fi
  if [[ -z "${ref}" ]]; then
    ref=$(header_value "${headers}" Location | tail -n 1 || true)
  fi
  ref=${ref/\/TaskService\/TaskMonitors\//\/TaskService\/Tasks\/}
  if [[ -n "${ref}" ]]; then
    redfish_absolute_url "${ref}"
  fi
}

redfish_transfer_protocol() {
  local scheme

  scheme=$(python_url_part "${iso_url}" scheme 2>/dev/null || true)
  if [[ -z "${scheme}" ]]; then
    scheme=${iso_url%%:*}
  fi
  printf '%s' "${scheme^^}"
}

candidate_iso_roots() {
  printf '%s\n' \
    "${bootwright_root_dir}/artifacts-server" \
    "${bootwright_root_dir}/bmc" \
    "${cluster_dir}/state" \
    "${cluster_dir}"
}

resolve_iso_path_from_directory() {
  local dir=$1
  local basename=${2:-}

  [[ -d "${dir}" ]] || return 0
  if [[ -n "${basename}" ]]; then
    find "${dir}" -type f -name "${basename}" -print 2>/dev/null | sort | tail -n 1
    return 0
  fi
  find "${dir}" -type f -name 'agent-*.iso' -printf '%T@ %p\n' 2>/dev/null | sort -n | awk 'END { $1=""; sub(/^ /, ""); print }'
}

discover_iso_path() {
  local basename root found requested

  if [[ -n "${iso_path}" ]]; then
    requested=${iso_path}
    if [[ -d "${iso_path}" ]]; then
      basename=$(url_basename "${iso_url}" 2>/dev/null || true)
      found=$(resolve_iso_path_from_directory "${iso_path}" "${basename}" || true)
      [[ -n "${found}" ]] || found=$(resolve_iso_path_from_directory "${iso_path}" || true)
      if [[ -n "${found}" ]]; then
        iso_path=${found}
      fi
    fi
    if [[ "${iso_path_explicit}" -eq 1 && ! -f "${iso_path}" ]]; then
      iso_path_status="explicit path not found"
      append_note "iso_path_explicit_not_found=${requested}"
    elif [[ -f "${iso_path}" ]]; then
      iso_path_status="explicit"
    fi
    return 0
  fi

  basename=$(url_basename "${iso_url}" 2>/dev/null || true)
  while IFS= read -r root; do
    found=$(resolve_iso_path_from_directory "${root}" "${basename}" || true)
    [[ -n "${found}" ]] || found=$(resolve_iso_path_from_directory "${root}" || true)
    if [[ -n "${found}" ]]; then
      iso_path=${found}
      iso_path_status="auto-discovered"
      return 0
    fi
  done < <(candidate_iso_roots)
}

discover_from_logs() {
  local value
  [[ -d "${cluster_dir}/state" ]] || return 0

  if have python3; then
    while IFS= read -r value; do
      record_iso_url "${value}"
    done < <(find "${cluster_dir}/state" -path '*/ansible-output.log' -type f -exec python3 -c '
import re
import sys

for path in sys.argv[1:]:
    try:
        with open(path, encoding="utf-8", errors="replace") as fh:
            text = fh.read()
    except OSError:
        continue
    for url in re.findall(r"https?://[^\s\"'"'"',;\]\}]+/agent-[^\s\"'"'"',;\]\}]+\.iso", text):
        print(url)
' {} + 2>/dev/null || true)
  fi
  if [[ -z "${iso_url}" ]]; then
    value=$(find "${cluster_dir}/state" -path '*/ansible-output.log' -type f -exec sed -nE 's/.*Expected Image=(https?:\/\/[^;[:space:]]+).*/\1/p' {} + 2>/dev/null | tail -n 1 || true)
    iso_url=${value:-${iso_url}}
  fi
  record_iso_url "${iso_url}"
  if [[ -z "${task_url}" ]]; then
    value=$(find "${cluster_dir}/state" -path '*/ansible-output.log' -type f -exec sed -nE 's/.*task URL=(https?:\/\/[^,[:space:]]+).*/\1/p' {} + 2>/dev/null | tail -n 1 || true)
    task_url=${value:-${task_url}}
  fi
  if [[ -z "${vmedia_uri}" ]]; then
    value=$(find "${cluster_dir}/state" -path '*/ansible-output.log' -type f -exec sed -nE 's/.*Redfish did not attach the requested agent ISO for (\/redfish\/v1\/[^[:space:]]+) after.*/\1/p' {} + 2>/dev/null | tail -n 1 || true)
    vmedia_uri=${value:-${vmedia_uri}}
  fi
  if [[ -z "${security_uri}" ]]; then
    value=$(find "${cluster_dir}/state" -path '*/ansible-output.log' -type f -exec sed -nE 's/.*SecurityService=(\/redfish\/v1\/[^,[:space:]]+).*/\1/p' {} + 2>/dev/null | tail -n 1 || true)
    security_uri=${value:-${security_uri}}
  fi
  if [[ -z "${bmc_url}" && -n "${task_url}" ]]; then
    bmc_url=$(python_url_part "${task_url}" origin 2>/dev/null || true)
  fi
}

discover_iso_url_from_files() {
  local value

  if have python3; then
    while IFS= read -r value; do
      record_iso_url "${value}"
      iso_url=${iso_url:-${value}}
    done < <(find "${cluster_dir}" \
      \( -path "${cluster_dir}/secrets" -o -path "${cluster_dir}/state/runtime" -o -path "${cluster_dir}/auth" \) -prune \
      -o -type f \( -name '*.yml' -o -name '*.yaml' -o -name '*.json' -o -name '*.log' -o -name '*.txt' \) \
      -exec python3 -c '
import re
import sys

seen = set()
for path in sys.argv[1:]:
    try:
        with open(path, encoding="utf-8", errors="replace") as fh:
            text = fh.read()
    except OSError:
        continue
    for url in re.findall(r"https?://[^\s\"'"'"',;\]\}]+/agent-[^\s\"'"'"',;\]\}]+\.iso", text):
        if url not in seen:
            print(url)
            seen.add(url)
' {} + 2>/dev/null || true)
    return 0
  fi

  [[ -z "${iso_url}" ]] || {
    record_iso_url "${iso_url}"
    return 0
  }
  value=$(find "${cluster_dir}" \
    \( -path "${cluster_dir}/secrets" -o -path "${cluster_dir}/state/runtime" -o -path "${cluster_dir}/auth" \) -prune \
    -o -type f \( -name '*.yml' -o -name '*.yaml' -o -name '*.json' -o -name '*.log' -o -name '*.txt' \) \
    -exec sed -nE 's#.*(https?://[^[:space:]";,}]+/agent-[^[:space:]";,}]+\.iso).*#\1#p' {} + 2>/dev/null | tail -n 1 || true)
  iso_url=${value:-${iso_url}}
  record_iso_url "${iso_url}"
}

discover_from_desired_state() {
  local discovery
  local key value

  have python3 || return 0
  discovery=$(python3 - "${cluster_dir}" "${context_input_dir}" "${context_state_dir}" "${bmc_url}" "${system_uri}" <<'PY' 2>/dev/null || true
import os
import re
import sys
from urllib.parse import urlsplit

cluster_dir, input_dir, state_dir, wanted_base, wanted_system = sys.argv[1:]
roots = []
for root in (input_dir, os.path.join(state_dir, "effective"), os.path.join(state_dir, "ansible", "artifacts"), cluster_dir):
    if root and os.path.isdir(root) and root not in roots:
        roots.append(root)

def iter_files():
    seen = set()
    for root in roots:
        for current, dirs, files in os.walk(root):
            dirs[:] = [d for d in dirs if d not in {"secrets", "runtime", "auth", ".git"}]
            for name in files:
                if not name.endswith((".yaml", ".yml", ".json", ".log", ".txt")):
                    continue
                path = os.path.join(current, name)
                if path in seen:
                    continue
                seen.add(path)
                yield path

def scalar(raw):
    value = raw.split("#", 1)[0].strip()
    if value.startswith(("'", '"')) and value.endswith(("'", '"')) and len(value) >= 2:
        value = value[1:-1]
    return value.strip()

def normalize_address(value):
    if "+" in value and "http" in value.split("+", 1)[1]:
        value = value.split("+", 1)[1]
    match = re.search(r"https?://[^\s\"',}]+", value)
    if not match:
        return ""
    return match.group(0).rstrip("/")

def origin(url):
    parsed = urlsplit(url)
    if parsed.scheme and parsed.netloc:
        return f"{parsed.scheme}://{parsed.netloc}"
    return ""

def system_uri(url):
    parsed = urlsplit(url)
    path = parsed.path.rstrip("/")
    if path.startswith("/redfish/v1/Systems/"):
        return path
    return ""

def host(value):
    parsed = urlsplit(value)
    return parsed.hostname or ""

records = []
for path in iter_files():
    try:
        with open(path, encoding="utf-8", errors="replace") as fh:
            lines = fh.readlines()
    except OSError:
        continue
    for idx, line in enumerate(lines):
        if "address:" not in line or "redfish" not in line.lower():
            continue
        address = normalize_address(scalar(line.split("address:", 1)[1]))
        if not address:
            continue
        parent_indent = 0
        for prev in range(idx - 1, max(-1, idx - 12), -1):
            if re.match(r"^\s*bmc:\s*$", lines[prev]):
                parent_indent = len(lines[prev]) - len(lines[prev].lstrip())
                break
        credential_ref = ""
        disable_cert = ""
        for follow in lines[idx + 1:idx + 40]:
            if not follow.strip() or follow.lstrip().startswith("#"):
                continue
            indent = len(follow) - len(follow.lstrip())
            if indent <= parent_indent:
                break
            if "credentialsRef:" in follow or "credentialRef:" in follow:
                inline = re.search(r"name\s*:\s*([^,}\s]+)", follow)
                if inline:
                    credential_ref = scalar(inline.group(1))
                    continue
            if credential_ref == "" and re.match(r"^\s*name\s*:", follow):
                credential_ref = scalar(follow.split(":", 1)[1])
            if "disableCertificateVerification:" in follow:
                disable_cert = scalar(follow.split("disableCertificateVerification:", 1)[1]).lower()
        records.append({
            "file": path,
            "line": str(idx + 1),
            "address": address,
            "base": origin(address),
            "host": host(address),
            "system_uri": system_uri(address),
            "credential_ref": credential_ref,
            "disable_cert": disable_cert,
        })

if not records:
    sys.exit(0)

wanted_origin = origin(wanted_base) if wanted_base.startswith(("http://", "https://")) else wanted_base.rstrip("/")
wanted_host = host(wanted_origin)
wanted_system = wanted_system.rstrip("/")

def score(record):
    value = 0
    if wanted_origin and record["base"] == wanted_origin:
        value += 100
    if wanted_host and record["host"] == wanted_host:
        value += 50
    if wanted_system and record["system_uri"] == wanted_system:
        value += 25
    return value

chosen = sorted(records, key=score, reverse=True)[0]
if score(chosen) == 0 and (wanted_origin or wanted_system):
    chosen = records[0]

for key in ("base", "system_uri", "credential_ref", "disable_cert", "file", "line", "address"):
    value = chosen.get(key, "")
    if value:
        print(f"{key}={value}")
PY
)
  [[ -n "${discovery}" ]] || return 0
  while IFS='=' read -r key value; do
    case "${key}" in
      base)
        bmc_url=${bmc_url:-${value}}
        ;;
      system_uri)
        system_uri=${system_uri:-${value}}
        ;;
      credential_ref)
        bmc_credentials_ref=${bmc_credentials_ref:-${value}}
        ;;
      disable_cert)
        provider_disable_certificate_verification=${provider_disable_certificate_verification:-${value}}
        ;;
      file)
        append_note "desired_state_bmc_source=${value}"
        ;;
      line)
        append_note "desired_state_bmc_line=${value}"
        ;;
      address)
        append_note "desired_state_bmc_address=${value}"
        ;;
    esac
  done <<<"${discovery}"

  if [[ -z "${bmc_password_file}" && -n "${bmc_credentials_ref}" && -f "${context_secrets_dir}/${bmc_credentials_ref}" ]]; then
    bmc_password_file="${context_secrets_dir}/${bmc_credentials_ref}"
    append_note "redfish_credentials_ref_loaded=${bmc_credentials_ref}"
  fi
}

normalize_base_url() {
  if [[ -n "${bmc_url}" ]]; then
    if [[ "${bmc_url}" =~ ^(https?://[^/]+)(/redfish/v1/Systems/[^/]+)/?$ ]]; then
      system_uri=${system_uri:-${BASH_REMATCH[2]}}
      bmc_url=${BASH_REMATCH[1]}
    fi
    bmc_url=${bmc_url%/}
  fi
}

resolve_redfish_tls_mode() {
  if [[ "${redfish_tls_explicit}" -eq 1 ]]; then
    return 0
  fi
  case "${provider_disable_certificate_verification}" in
    true)
      redfish_insecure=1
      redfish_tls_reason="desired_state_disableCertificateVerification"
      append_note "redfish_tls_verification=disabled_from_desired_state"
      ;;
    false)
      redfish_insecure=0
      redfish_tls_reason="desired_state_verify"
      ;;
    *)
      redfish_insecure=0
      redfish_tls_reason="default_secure"
      ;;
  esac
}

load_password() {
  local raw user pass
  if [[ -n "${bmc_password_file}" ]]; then
    [[ -f "${bmc_password_file}" ]] || die "--bmc-password-file does not exist: ${bmc_password_file}"
    raw=$(tr -d '\r\n' <"${bmc_password_file}")
    if [[ "${raw}" == *:* ]]; then
      user=${raw%%:*}
      pass=${raw#*:}
      bmc_user=${bmc_user:-${user}}
      bmc_password=${pass}
    else
      bmc_password=${raw}
    fi
  fi
  if [[ -n "${bmc_url}" && -n "${bmc_user}" && -z "${bmc_password}" && "${prompt_for_password}" -eq 1 && -t 0 ]]; then
    printf 'Redfish password for %s: ' "${bmc_user}" >&2
    stty -echo
    read -r bmc_password
    stty echo
    printf '\n' >&2
  fi
}

curl_quote() {
  local s=$1
  s=${s//\\/\\\\}
  s=${s//\"/\\\"}
  s=${s//$'\n'/}
  printf '%s' "${s}"
}

prepare_auth_config() {
  [[ -n "${bmc_user}" && -n "${bmc_password}" ]] || return 0
  auth_config="${tmp_dir}/curl-redfish-auth.conf"
  {
    printf 'user = "%s:%s"\n' "$(curl_quote "${bmc_user}")" "$(curl_quote "${bmc_password}")"
    printf 'basic\n'
  } >"${auth_config}"
  chmod 600 "${auth_config}"
}

curl_redfish_args() {
  local args=(-sS --connect-timeout 10 --max-time "${curl_timeout}")
  if [[ "${redfish_insecure}" -eq 1 ]]; then
    args+=(--insecure)
  elif [[ -n "${redfish_ca}" ]]; then
    args+=(--cacert "${redfish_ca}")
  fi
  if [[ -n "${auth_config}" ]]; then
    args+=(--config "${auth_config}")
  fi
  printf '%s\0' "${args[@]}"
}

json_pretty_or_copy() {
  local src=$1
  local dest=$2
  if [[ ! -f "${src}" ]]; then
    : >"${dest}"
    return 0
  fi
  if have python3; then
    python3 -m json.tool <"${src}" >"${dest}" 2>/dev/null || cp "${src}" "${dest}"
  else
    cp "${src}" "${dest}"
  fi
}

redfish_get() {
  local name=$1
  local url=$2
  local dir="${work_dir}/redfish/${name}"
  local headers="${tmp_dir}/${name}.headers"
  local body="${tmp_dir}/${name}.body"
  local meta="${tmp_dir}/${name}.meta"
  local pretty="${tmp_dir}/${name}.pretty"
  local args=()
  local status

  [[ -n "${url}" ]] || return 0
  mkdir -p "${dir}"
  while IFS= read -r -d '' arg; do
    args+=("${arg}")
  done < <(curl_redfish_args)

  {
    printf 'url=%s\n' "${url}"
    curl "${args[@]}" -D "${headers}" -o "${body}" -w 'http_code=%{http_code}\nremote_ip=%{remote_ip}\nssl_verify_result=%{ssl_verify_result}\ntime_connect=%{time_connect}\ntime_appconnect=%{time_appconnect}\ntime_total=%{time_total}\n' "${url}"
    printf 'exit_code=%s\n' "$?"
  } >"${meta}" 2>&1 || {
    status=$?
    printf 'exit_code=%s\n' "${status}" >>"${meta}"
  }

  json_pretty_or_copy "${body}" "${pretty}"
  redact_file "${headers}" "${dir}/headers.txt"
  redact_file "${pretty}" "${dir}/body.json"
  redact_file "${meta}" "${dir}/curl.txt"
  last_redfish_headers="${headers}"
  last_redfish_body="${body}"
}

redfish_request_json() {
  local name=$1
  local method=$2
  local url=$3
  local request_body=$4
  shift 4
  local dir="${work_dir}/redfish/${name}"
  local headers="${tmp_dir}/${name}.headers"
  local body="${tmp_dir}/${name}.body"
  local meta="${tmp_dir}/${name}.meta"
  local pretty="${tmp_dir}/${name}.pretty"
  local request_file="${tmp_dir}/${name}.request.json"
  local args=()
  local curl_headers=()
  local status

  mkdir -p "${dir}"
  printf '%s\n' "${request_body}" >"${request_file}"
  while [[ $# -gt 0 ]]; do
    curl_headers+=(-H "$1: $2")
    shift 2
  done
  while IFS= read -r -d '' arg; do
    args+=("${arg}")
  done < <(curl_redfish_args)

  {
    printf 'method=%s\n' "${method}"
    printf 'url=%s\n' "${url}"
    printf 'request_body='
    cat "${request_file}"
    printf '\n'
    curl "${args[@]}" -X "${method}" -H 'Content-Type: application/json' "${curl_headers[@]}" --data-binary @"${request_file}" -D "${headers}" -o "${body}" -w 'http_code=%{http_code}\nremote_ip=%{remote_ip}\nssl_verify_result=%{ssl_verify_result}\ntime_connect=%{time_connect}\ntime_appconnect=%{time_appconnect}\ntime_total=%{time_total}\n' "${url}"
    printf 'exit_code=%s\n' "$?"
  } >"${meta}" 2>&1 || {
    status=$?
    printf 'exit_code=%s\n' "${status}" >>"${meta}"
  }

  json_pretty_or_copy "${body}" "${pretty}"
  redact_file "${request_file}" "${dir}/request.json"
  redact_file "${headers}" "${dir}/headers.txt"
  redact_file "${pretty}" "${dir}/body.json"
  redact_file "${meta}" "${dir}/curl.txt"
  last_redfish_headers="${headers}"
  last_redfish_body="${body}"
}

iso_probe() {
  local url=$1
  local name=$2
  shift 2
  local headers="${tmp_dir}/iso-${name}.headers"
  local meta="${tmp_dir}/iso-${name}.meta"
  local body="${tmp_dir}/iso-${name}.body"
  local args=(-sS --connect-timeout 10 --max-time "${curl_timeout}" "$@")
  local status

  [[ -n "${url}" ]] || return 0
  {
    printf 'url=%s\n' "${url}"
    curl "${args[@]}" -D "${headers}" -o "${body}" -w 'http_code=%{http_code}\nremote_ip=%{remote_ip}\nssl_verify_result=%{ssl_verify_result}\nsize_download=%{size_download}\ntime_connect=%{time_connect}\ntime_appconnect=%{time_appconnect}\ntime_starttransfer=%{time_starttransfer}\ntime_total=%{time_total}\n' "${url}"
    printf 'exit_code=%s\n' "$?"
  } >"${meta}" 2>&1 || {
    status=$?
    printf 'exit_code=%s\n' "${status}" >>"${meta}"
  }
  redact_file "${headers}" "${work_dir}/iso/${name}.headers.txt"
  redact_file "${meta}" "${work_dir}/iso/${name}.curl.txt"
  if [[ -s "${body}" ]]; then
    wc -c "${body}" >"${work_dir}/iso/${name}.body-size.txt" 2>&1 || true
  fi
}

copy_text_file() {
  local src=$1
  local dest=$2
  local size

  [[ -f "${src}" ]] || return 0
  if [[ ! -r "${src}" ]]; then
    printf 'skipped_unreadable %s\n' "${src}" >>"${work_dir}/files/skipped.txt"
    return 0
  fi
  case "${src}" in
    */secrets/*|*/state/runtime/*|*/auth/*|*kubeconfig*|*pull-secret*|*.key|*.pem|*.p12|*.pfx)
      printf 'skipped_sensitive %s\n' "${src}" >>"${work_dir}/files/skipped.txt"
      return 0
      ;;
  esac

  size=$(stat -c '%s' "${src}" 2>/dev/null || printf '0')
  mkdir -p "$(dirname "${dest}")"
  if [[ "${size}" -gt "${max_text_copy_bytes}" ]]; then
    {
      printf 'truncated_from=%s\n' "${src}"
      printf 'original_bytes=%s\n\n' "${size}"
      tail -n 5000 "${src}"
    } >"${tmp_dir}/copy-truncated"
    redact_file "${tmp_dir}/copy-truncated" "${dest}"
  else
    redact_file "${src}" "${dest}"
  fi
}

copy_selected_files() {
  local rel src

  if [[ -d "${cluster_dir}/input-files" ]]; then
    while IFS= read -r -d '' src; do
      rel=${src#"${cluster_dir}/"}
      copy_text_file "${src}" "${work_dir}/files/${rel}"
    done < <(find "${cluster_dir}/input-files" -type f \( -name '*.yaml' -o -name '*.yml' \) -print0 2>/dev/null)
  fi

  for src in \
    "${cluster_dir}/state/ansible/artifacts/cluster/ansible-output.log" \
    "${cluster_dir}/state/ansible/artifacts/infra/ansible-output.log" \
    "${cluster_dir}/state/ansible/artifacts/all/ansible-output.log"; do
    if [[ -f "${src}" ]]; then
      rel=${src#"${cluster_dir}/"}
      copy_text_file "${src}" "${work_dir}/files/${rel}"
    fi
  done

  if [[ -d "${cluster_dir}/state/ansible/artifacts" ]]; then
    while IFS= read -r -d '' src; do
      rel=${src#"${cluster_dir}/"}
      copy_text_file "${src}" "${work_dir}/files/${rel}"
    done < <(find "${cluster_dir}/state/ansible/artifacts" -type f \( -name '*.log' -o -name '*.txt' -o -name '*.json' \) -print0 2>/dev/null)
  fi

  if [[ -d "${cluster_dir}/state/artifacts" ]]; then
    while IFS= read -r -d '' src; do
      rel=${src#"${cluster_dir}/"}
      copy_text_file "${src}" "${work_dir}/files/${rel}"
    done < <(find "${cluster_dir}/state/artifacts" -type f \( -name '*.log' -o -name '*.crt' -o -name 'openssl.cnf' \) -print0 2>/dev/null)
  fi

  if [[ -d "${cluster_dir}/state/effective" ]]; then
    while IFS= read -r -d '' src; do
      rel=${src#"${cluster_dir}/"}
      copy_text_file "${src}" "${work_dir}/files/${rel}"
    done < <(find "${cluster_dir}/state/effective" -type f \( -name '*.yaml' -o -name '*.yml' -o -name '*.json' \) -print0 2>/dev/null)
  fi

  if [[ -d "${cluster_dir}/state/ansible-bundle/roles/openshift/boot_redfish" ]]; then
    while IFS= read -r -d '' src; do
      rel=${src#"${cluster_dir}/"}
      copy_text_file "${src}" "${work_dir}/files/${rel}"
    done < <(find "${cluster_dir}/state/ansible-bundle/roles/openshift/boot_redfish" -type f \( -name '*.yml' -o -name '*.yaml' \) -print0 2>/dev/null)
  fi
}

copy_host_artifact_files() {
  local phase=$1
  local rel src

  if [[ -d "${bootwright_root_dir}/artifacts-server" ]]; then
    while IFS= read -r -d '' src; do
      case "${src}" in
        *.iso|*.img|*.qcow2|*.raw)
          continue
          ;;
      esac
      rel=${src#"${bootwright_root_dir}/"}
      copy_text_file "${src}" "${work_dir}/files/host-state-${phase}/${rel}"
    done < <(find "${bootwright_root_dir}/artifacts-server" -maxdepth 6 -type f \( -name '*.log' -o -name '*.txt' -o -name '*.json' -o -name '*.crt' -o -name 'openssl.cnf' -o -name '*.service' \) -print0 2>/dev/null)
  fi

  if [[ -d "${bootwright_root_dir}/bmc" ]]; then
    while IFS= read -r -d '' src; do
      case "${src}" in
        *.iso|*.img|*.qcow2|*.raw|*.key|*.pem|*.p12|*.pfx)
          continue
          ;;
      esac
      rel=${src#"${bootwright_root_dir}/"}
      copy_text_file "${src}" "${work_dir}/files/host-state-${phase}/${rel}"
    done < <(find "${bootwright_root_dir}/bmc" -maxdepth 6 -type f \( -name '*.log' -o -name '*.txt' -o -name '*.json' -o -name '*.crt' -o -name 'openssl.cnf' -o -name '*.service' \) -print0 2>/dev/null)
  fi
}

write_file_listing() {
  {
    printf 'cluster_dir=%s\n' "${cluster_dir}"
    printf 'generated_at_utc=%s\n\n' "${timestamp}"
    find "${cluster_dir}" -maxdepth 5 -type f 2>/dev/null \
      | sed "s#^${cluster_dir}#<cluster-dir>#" \
      | sort
  } >"${tmp_dir}/file-list"
  redact_file "${tmp_dir}/file-list" "${work_dir}/files/source-file-list.txt"
}

collect_iso_url_endpoint() {
  local prefix=$1
  local url=$2
  local host="" port="" ip=""

  host=$(python_url_part "${url}" host 2>/dev/null || true)
  port=$(python_url_part "${url}" port 2>/dev/null || true)
  [[ -n "${host}" ]] || return 0

  run_cmd "${prefix}_getent_hosts" getent hosts "${host}"
  run_cmd "${prefix}_getent_ahostsv4" getent ahostsv4 "${host}"
  if have python3; then
    run_cmd "${prefix}_dns_python" python3 -c 'import socket, sys; print(socket.getaddrinfo(sys.argv[1], int(sys.argv[2]), proto=socket.IPPROTO_TCP))' "${host}" "${port:-443}"
  fi
  if have ip; then
    ip=$(first_ipv4 "${host}" || true)
    [[ -n "${ip}" ]] && run_cmd "${prefix}_ip_route" ip route get "${ip}"
  fi
  if have nc && [[ -n "${port}" ]]; then
    run_cmd "${prefix}_tcp_nc" nc -vz -w "${curl_timeout}" "${host}" "${port}"
  fi
  if have tracepath; then
    run_cmd "${prefix}_tracepath" tracepath -n "${host}"
  fi
  if [[ -n "${port}" ]] && have openssl; then
    run_openssl_s_client "${prefix}_openssl_s_client" "${host}" "${port}"
  fi
}

collect_system() {
  local iso_host="" iso_port="" bmc_host="" bmc_port=""
  local iso_ip="" bmc_ip=""
  local index url

  run_cmd date date -u
  run_cmd hostname hostname -f
  run_cmd uname uname -a
  run_cmd id id
  run_cmd cwd pwd
  run_cmd env_proxy sh -c 'env | sort | grep -E "^(HTTP_PROXY|HTTPS_PROXY|NO_PROXY|http_proxy|https_proxy|no_proxy|BOOTWRIGHT_)" || true'
  if [[ -n "${bootwright_cli}" && -x "${bootwright_cli}" ]]; then
    run_cmd bootwright_print_env_sensitive "${bootwright_cli}" print-env --sensitive
  elif have bootwright; then
    run_cmd bootwright_print_env_sensitive bootwright print-env --sensitive
  fi

  if [[ -n "${iso_url}" ]]; then
    iso_host=$(python_url_part "${iso_url}" host 2>/dev/null || true)
    iso_port=$(python_url_part "${iso_url}" port 2>/dev/null || true)
  fi
  if [[ -n "${bmc_url}" ]]; then
    bmc_host=$(python_url_part "${bmc_url}" host 2>/dev/null || true)
    bmc_port=$(python_url_part "${bmc_url}" port 2>/dev/null || true)
  fi

  [[ -n "${iso_host}" ]] && run_cmd iso_getent_hosts getent hosts "${iso_host}"
  [[ -n "${bmc_host}" ]] && run_cmd bmc_getent_hosts getent hosts "${bmc_host}"
  [[ -n "${iso_host}" ]] && run_cmd iso_getent_ahostsv4 getent ahostsv4 "${iso_host}"
  [[ -n "${bmc_host}" ]] && run_cmd bmc_getent_ahostsv4 getent ahostsv4 "${bmc_host}"

  if have python3 && [[ -n "${iso_host}" ]]; then
    run_cmd iso_dns_python python3 -c 'import socket, sys; print(socket.getaddrinfo(sys.argv[1], int(sys.argv[2]), proto=socket.IPPROTO_TCP))' "${iso_host}" "${iso_port:-443}"
  fi
  if have python3 && [[ -n "${bmc_host}" ]]; then
    run_cmd bmc_dns_python python3 -c 'import socket, sys; print(socket.getaddrinfo(sys.argv[1], int(sys.argv[2]), proto=socket.IPPROTO_TCP))' "${bmc_host}" "${bmc_port:-443}"
  fi

  if have ip && [[ -n "${iso_host}" ]]; then
    iso_ip=$(first_ipv4 "${iso_host}" || true)
    [[ -n "${iso_ip}" ]] && run_cmd iso_ip_route ip route get "${iso_ip}"
  fi
  if have ip && [[ -n "${bmc_host}" ]]; then
    bmc_ip=$(first_ipv4 "${bmc_host}" || true)
    [[ -n "${bmc_ip}" ]] && run_cmd bmc_ip_route ip route get "${bmc_ip}"
  fi
  if have nc && [[ -n "${iso_host}" && -n "${iso_port}" ]]; then
    run_cmd iso_tcp_nc nc -vz -w "${curl_timeout}" "${iso_host}" "${iso_port}"
  fi
  if have nc && [[ -n "${bmc_host}" && -n "${bmc_port}" ]]; then
    run_cmd bmc_tcp_nc nc -vz -w "${curl_timeout}" "${bmc_host}" "${bmc_port}"
  fi
  if have tracepath && [[ -n "${iso_host}" ]]; then
    run_cmd iso_tracepath tracepath -n "${iso_host}"
  fi
  if have tracepath && [[ -n "${bmc_host}" ]]; then
    run_cmd bmc_tracepath tracepath -n "${bmc_host}"
  fi

  have ip && run_cmd ip_addr ip addr
  have ip && run_cmd ip_link ip link
  have ip && run_cmd ip_route ip route
  have ip && run_cmd ip_rule ip rule
  have ip && run_cmd ip_neigh ip neigh
  have bridge && run_cmd bridge_link bridge link
  have ss && run_cmd ss_listen ss -ltnp
  have ps && run_cmd ps_ef ps -ef
  [[ -f /etc/resolv.conf ]] && run_cmd etc_resolv_conf sed -n '1,200p' /etc/resolv.conf
  [[ -f /etc/hosts ]] && run_cmd etc_hosts sed -n '1,200p' /etc/hosts
  have resolvectl && run_cmd resolvectl_status resolvectl status
  have nmcli && run_cmd nmcli_device nmcli device show
  have nft && run_cmd nft_ruleset nft list ruleset
  have iptables && run_cmd iptables_filter iptables -S
  have iptables && run_cmd iptables_nat iptables -t nat -S

  if have systemctl; then
    run_cmd systemctl_bootwright_artifacts systemctl list-units --all 'bootwright-artifacts-*.service'
    run_cmd systemctl_bootwright_artifacts_files systemctl list-unit-files 'bootwright-artifacts-*.service'
    run_cmd systemctl_bootwright_artifacts_status systemctl status --no-pager 'bootwright-artifacts-*.service'
    run_cmd systemctl_bootwright_artifacts_cat systemctl cat 'bootwright-artifacts-*.service'
    run_cmd systemctl_failed systemctl --failed
  fi
  if have journalctl; then
    run_cmd journal_bootwright_artifacts journalctl --no-pager -n 300 -u 'bootwright-artifacts-*.service'
  fi
  if have firewall-cmd; then
    run_cmd firewall_cmd_state firewall-cmd --state
    run_cmd firewall_cmd_list_all firewall-cmd --list-all
  fi
  if [[ -d "${bootwright_root_dir}/artifacts-server" ]]; then
    run_cmd bootwright_host_artifacts_find find "${bootwright_root_dir}/artifacts-server" -maxdepth 4 -type f -printf '%M %u %g %s %TY-%Tm-%Td %TH:%TM %p\n'
  fi
  if [[ -d "${bootwright_root_dir}/bmc" ]]; then
    run_cmd bootwright_host_bmc_find find "${bootwright_root_dir}/bmc" -maxdepth 5 -type f -printf '%M %u %g %s %TY-%Tm-%Td %TH:%TM %p\n'
  fi
  copy_host_artifact_files before_redfish

  if [[ -n "${iso_host}" && -n "${iso_port}" ]] && have openssl; then
    run_openssl_s_client iso_openssl_s_client "${iso_host}" "${iso_port}"
  fi
  if [[ -n "${bmc_host}" && -n "${bmc_port}" ]] && have openssl; then
    run_openssl_s_client bmc_openssl_s_client "${bmc_host}" "${bmc_port}"
  fi
  index=0
  while IFS= read -r url; do
    [[ -n "${url}" ]] || continue
    index=$((index + 1))
    collect_iso_url_endpoint "iso_url_${index}" "${url}"
  done <"${iso_urls_file}"
}

collect_iso_url() {
  local url=$1
  local prefix=$2

  iso_probe "${url}" "${prefix}_head_secure" -I
  iso_probe "${url}" "${prefix}_head_insecure" --insecure -I
  iso_probe "${url}" "${prefix}_head_insecure_no_proxy" --noproxy '*' --insecure -I
  iso_probe "${url}" "${prefix}_range_secure" -H 'Range: bytes=0-0'
  iso_probe "${url}" "${prefix}_range_insecure" --insecure -H 'Range: bytes=0-0'
  iso_probe "${url}" "${prefix}_range_insecure_no_proxy" --noproxy '*' --insecure -H 'Range: bytes=0-0'
  iso_probe "${url}" "${prefix}_verbose_insecure" --insecure -v -I
}

collect_iso() {
  local index=0
  local url

  record_iso_url "${iso_url}"
  [[ -s "${iso_urls_file}" ]] || return 0
  redact_file "${iso_urls_file}" "${work_dir}/iso/urls.txt"
  while IFS= read -r url; do
    [[ -n "${url}" ]] || continue
    index=$((index + 1))
    collect_iso_url "${url}" "url_${index}"
  done <"${iso_urls_file}"
}

collect_iso_file() {
  [[ -n "${iso_path}" && -f "${iso_path}" ]] || return 0
  run_cmd iso_file_ls ls -l "${iso_path}"
  run_cmd iso_file_stat stat "${iso_path}"
  have sha256sum && run_cmd iso_file_sha256 sha256sum "${iso_path}"
  have file && run_cmd iso_file_type file "${iso_path}"
}

collect_redfish_action_info() {
  local action_targets="${tmp_dir}/redfish-actioninfo-uris.txt"
  local vmedia_body
  local action_url

  have python3 || return 0
  : >"${action_targets}"

  while IFS= read -r vmedia_body; do
    python3 - "${vmedia_body}" >>"${action_targets}" 2>/dev/null <<'PY' || true
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)

def walk(value):
    if isinstance(value, dict):
        info = value.get("@Redfish.ActionInfo")
        if isinstance(info, str) and info:
            print(info)
        for child in value.values():
            walk(child)
    elif isinstance(value, list):
        for child in value:
            walk(child)

walk(data.get("Actions", {}))
PY
  done < <(find "${work_dir}/redfish" -path '*/body.json' -type f -print 2>/dev/null)

  while IFS= read -r action_url; do
    [[ -n "${action_url}" ]] || continue
    if [[ "${action_url}" =~ ^https?:// ]]; then
      redfish_get "actioninfo_$(printf '%s' "${action_url}" | sed 's#[^A-Za-z0-9_.-]#_#g')" "${action_url}"
    elif [[ "${action_url}" == /* && -n "${bmc_url}" ]]; then
      redfish_get "actioninfo_$(printf '%s' "${action_url}" | sed 's#[^A-Za-z0-9_.-]#_#g')" "${bmc_url}${action_url}"
    fi
  done < <(sort -u "${action_targets}" 2>/dev/null)
}

json_registry_uris() {
  local file=$1
  have python3 || return 0
  [[ -s "${file}" ]] || return 0
  python3 - "${file}" <<'PY' 2>/dev/null || true
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)
for location in data.get("Location", []):
    if not isinstance(location, dict):
        continue
    for key in ("Uri", "ArchiveUri"):
        value = location.get(key)
        if isinstance(value, str) and value:
            print(value)
PY
}

collect_redfish_registries() {
  local registries_body=$1
  local registry_member registry_name registry_uri

  while IFS= read -r registry_member; do
    [[ -n "${registry_member}" ]] || continue
    registry_name=$(sanitize_name "${registry_member}")
    redfish_get "registry_${registry_name}" "$(redfish_absolute_url "${registry_member}")"
    while IFS= read -r registry_uri; do
      [[ -n "${registry_uri}" ]] || continue
      redfish_get "registry_file_$(sanitize_name "${registry_uri}")" "$(redfish_absolute_url "${registry_uri}")"
    done < <(json_registry_uris "${last_redfish_body}")
  done < <(json_members "${registries_body}")
}

collect_log_services() {
  local prefix=$1
  local services_body=$2
  local service_member service_name

  while IFS= read -r service_member; do
    [[ -n "${service_member}" ]] || continue
    service_name=$(sanitize_name "${service_member}")
    redfish_get "${prefix}_log_service_${service_name}" "$(redfish_absolute_url "${service_member}")"
    redfish_get "${prefix}_log_entries_${service_name}" "$(redfish_absolute_url "${service_member}/Entries")"
  done < <(json_members "${services_body}")
}

collect_task_collection_members() {
  local collection_body=$1
  local task_member task_name count=0

  while IFS= read -r task_member; do
    [[ -n "${task_member}" ]] || continue
    count=$((count + 1))
    [[ "${count}" -le 30 ]] || break
    task_name=$(sanitize_name "${task_member}")
    redfish_get "task_${task_name}" "$(redfish_absolute_url "${task_member}")"
  done < <(json_members "${collection_body}")
}

resolve_insert_action_from_vmedia() {
  local file=$1
  have python3 || return 0
  [[ -s "${file}" ]] || return 0
  python3 - "${file}" <<'PY' 2>/dev/null || true
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)

standard = ""
vmm = ""

def walk(value):
    global standard, vmm
    if isinstance(value, dict):
        for key, child in value.items():
            if key == "#VirtualMedia.InsertMedia" and isinstance(child, dict) and not standard:
                target = child.get("target")
                if isinstance(target, str):
                    standard = target
            if key == "#VirtualMedia.VmmControl" and isinstance(child, dict) and not vmm:
                target = child.get("target")
                if isinstance(target, str):
                    vmm = target
            walk(child)
    elif isinstance(value, list):
        for child in value:
            walk(child)

walk(data.get("Actions", {}))
if vmm:
    print("vmm " + vmm)
elif standard:
    print("standard " + standard)
PY
}

resolve_eject_action_from_vmedia() {
  local file=$1
  have python3 || return 0
  [[ -s "${file}" ]] || return 0
  python3 - "${file}" <<'PY' 2>/dev/null || true
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)

target = ""

def walk(value):
    global target
    if isinstance(value, dict):
        for key, child in value.items():
            if key == "#VirtualMedia.EjectMedia" and isinstance(child, dict) and not target:
                ref = child.get("target")
                if isinstance(ref, str):
                    target = ref
            walk(child)
    elif isinstance(value, list):
        for child in value:
            walk(child)

walk(data.get("Actions", {}))
if target:
    print(target)
PY
}

poll_redfish_task() {
  local url=$1
  local attempt state status

  [[ -n "${url}" ]] || return 0
  for attempt in $(seq 1 20); do
    redfish_get "insert_task_poll_${attempt}" "${url}"
    state=$(json_top_field "${last_redfish_body}" TaskState | tail -n 1 || true)
    status=$(json_top_field "${last_redfish_body}" TaskStatus | tail -n 1 || true)
    case "${state}" in
      Completed|Cancelled|Exception|Interrupted|Killed)
        append_note "insert_task_terminal_state=${state:-unknown}, status=${status:-unknown}, poll=${attempt}"
        return 0
        ;;
    esac
    sleep 2
  done
  append_note "insert_task_terminal_state=not reached after polling"
}

resolve_reset_action_from_system() {
  local file=$1
  have python3 || return 0
  [[ -s "${file}" ]] || return 0
  python3 - "${file}" <<'PY' 2>/dev/null || true
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    data = json.load(fh)
target = data.get("Actions", {}).get("#ComputerSystem.Reset", {}).get("target")
if isinstance(target, str) and target:
    print(target)
PY
}

poll_system_power_state() {
  local expected=$1
  local attempt state system_url

  [[ -n "${system_uri}" ]] || return 0
  system_url=$(redfish_absolute_url "${system_uri}")
  for attempt in $(seq 1 30); do
    redfish_get "system_power_poll_${expected}_${attempt}" "${system_url}"
    state=$(json_top_field "${last_redfish_body}" PowerState | tail -n 1 || true)
    if [[ "${state}" == "${expected}" ]]; then
      append_note "system_power_state=${state}, expected=${expected}, poll=${attempt}"
      return 0
    fi
    sleep 2
  done
  append_note "system_power_state=${state:-unknown}, expected=${expected}, poll=timeout"
}

redfish_system_reset() {
  local reset_type=$1
  local system_url reset_ref reset_url body

  [[ -n "${system_uri}" ]] || return 0
  system_url=$(redfish_absolute_url "${system_uri}")
  redfish_get "system_before_reset_${reset_type}" "${system_url}"
  reset_ref=$(resolve_reset_action_from_system "${last_redfish_body}" | tail -n 1 || true)
  [[ -n "${reset_ref}" ]] || reset_ref="${system_uri}/Actions/ComputerSystem.Reset"
  reset_url=$(redfish_absolute_url "${reset_ref}")
  body=$(printf '{"ResetType":"%s"}' "${reset_type}")
  redfish_request_json "system_reset_${reset_type}" POST "${reset_url}" "${body}"
}

redfish_set_cd_boot_once() {
  local system_url system_etag

  [[ -n "${system_uri}" ]] || return 0
  system_url=$(redfish_absolute_url "${system_uri}")
  redfish_get system_before_boot_override "${system_url}"
  system_etag=$(etag_from_response "${last_redfish_body}" "${last_redfish_headers}")
  redfish_request_json system_set_cd_boot_once PATCH "${system_url}" '{"Boot":{"BootSourceOverrideEnabled":"Once","BootSourceOverrideTarget":"Cd"}}' If-Match "${system_etag}"
  redfish_get system_after_boot_override "${system_url}"
}

attempt_redfish_boot_iso() {
  local protocol insert_ref insert_style insert_target insert_url insert_body
  local eject_ref eject_url vmedia_url vmedia_etag security_url security_etag
  local new_task_url

  [[ "${redfish_insert_attempt}" -eq 1 ]] || {
    append_note "redfish_insert_attempt=skipped_by_option"
    return 0
  }
  if [[ "${state_change_yes}" -ne 1 ]]; then
    append_note "state_changing_redfish_steps=skipped_missing_yes"
    return 0
  fi
  if [[ -z "${bmc_url}" || -z "${system_uri}" || -z "${vmedia_uri}" || -z "${iso_url}" ]]; then
    append_note "state_changing_redfish_steps=skipped_missing_bmc_system_vmedia_or_iso_url"
    return 0
  fi
  if [[ -z "${auth_config}" ]]; then
    append_note "state_changing_redfish_steps=skipped_missing_credentials"
    return 0
  fi

  protocol=$(redfish_transfer_protocol)
  append_note "state_changing_redfish_steps=started"
  redfish_system_reset ForceOff
  poll_system_power_state Off

  vmedia_url=$(redfish_absolute_url "${vmedia_uri}")
  redfish_get virtual_media_before_insert "${vmedia_url}"
  vmedia_etag=$(etag_from_response "${last_redfish_body}" "${last_redfish_headers}")

  if [[ "${protocol}" == "HTTPS" && -n "${security_uri}" ]]; then
    security_url=$(redfish_absolute_url "${security_uri}")
    redfish_get security_service_before_insert "${security_url}"
    if json_has_top_field "${last_redfish_body}" HttpsTransferCertVerification; then
      security_etag=$(etag_from_response "${last_redfish_body}" "${last_redfish_headers}")
      redfish_request_json security_service_disable_https_transfer PATCH "${security_url}" '{"HttpsTransferCertVerification":false}' If-Match "${security_etag}"
    else
      append_note "security_service_https_transfer_cert_verification=not advertised"
    fi
  fi

  if [[ "${protocol}" == "HTTPS" ]]; then
    redfish_request_json virtual_media_disable_verify_certificate PATCH "${vmedia_url}" '{"VerifyCertificate":false}' If-Match "${vmedia_etag}"
    redfish_get virtual_media_after_verify_certificate_patch "${vmedia_url}"
    vmedia_etag=$(etag_from_response "${last_redfish_body}" "${last_redfish_headers}")
  fi

  if [[ "${redfish_eject_first}" -eq 1 ]]; then
    eject_ref=$(resolve_eject_action_from_vmedia "${last_redfish_body}" | tail -n 1 || true)
    [[ -n "${eject_ref}" ]] || eject_ref="${vmedia_uri}/Actions/VirtualMedia.EjectMedia"
    eject_url=$(redfish_absolute_url "${eject_ref}")
    redfish_request_json virtual_media_eject_before_insert POST "${eject_url}" '{}'
    redfish_get virtual_media_after_eject "${vmedia_url}"
    vmedia_etag=$(etag_from_response "${last_redfish_body}" "${last_redfish_headers}")
  fi

  insert_ref=$(resolve_insert_action_from_vmedia "${last_redfish_body}" | tail -n 1 || true)
  if [[ "${insert_ref}" == vmm\ * ]]; then
    insert_style=vmm
    insert_target=${insert_ref#vmm }
    insert_body=$(printf '{"VmmControlType":"Connect","Image":"%s"}' "${iso_url}")
  else
    insert_style=standard
    insert_target=${insert_ref#standard }
    [[ -n "${insert_target}" ]] || insert_target="${vmedia_uri}/Actions/VirtualMedia.InsertMedia"
    insert_body=$(printf '{"Image":"%s","Inserted":true,"TransferProtocolType":"%s"}' "${iso_url}" "${protocol}")
  fi
  insert_url=$(redfish_absolute_url "${insert_target}")
  append_note "redfish_insert_attempt=started style=${insert_style} target=${insert_url}"
  redfish_request_json virtual_media_insert POST "${insert_url}" "${insert_body}"

  new_task_url=$(redfish_task_url_from_response "${last_redfish_body}" "${last_redfish_headers}" | tail -n 1 || true)
  if [[ -n "${new_task_url}" ]]; then
    task_url=${new_task_url}
    poll_redfish_task "${new_task_url}"
  else
    append_note "redfish_insert_task_url=not reported"
  fi

  redfish_get virtual_media_after_insert "${vmedia_url}"
  redfish_set_cd_boot_once
  redfish_system_reset On
  poll_system_power_state On
}

collect_redfish() {
  local manager_uri="" manager_safe member member_url collection_url collection_body vm_member vm_name
  local managers_body="" systems_body="" task_collection_body="" registries_body="" log_services_body=""

  [[ -n "${bmc_url}" ]] || return 0
  if [[ -z "${auth_config}" ]]; then
    append_note "redfish_auth=missing; Redfish requests are unauthenticated"
  fi

  redfish_get service_root "${bmc_url}/redfish/v1"
  redfish_get managers "${bmc_url}/redfish/v1/Managers"
  managers_body=${last_redfish_body}
  redfish_get systems "${bmc_url}/redfish/v1/Systems"
  systems_body=${last_redfish_body}
  redfish_get task_service "${bmc_url}/redfish/v1/TaskService"
  redfish_get task_collection "${bmc_url}/redfish/v1/TaskService/Tasks"
  task_collection_body=${last_redfish_body}
  collect_task_collection_members "${task_collection_body}"
  redfish_get registries "${bmc_url}/redfish/v1/Registries"
  registries_body=${last_redfish_body}
  collect_redfish_registries "${registries_body}"

  while IFS= read -r member; do
    [[ -n "${member}" ]] || continue
    member_url=$(redfish_absolute_url "${member}")
    manager_safe=$(sanitize_name "${member}")
    redfish_get "manager_${manager_safe}" "${member_url}"
    redfish_get "manager_network_protocol_${manager_safe}" "${member_url}/NetworkProtocol"
    redfish_get "manager_ethernet_${manager_safe}" "${member_url}/EthernetInterfaces"
    redfish_get "manager_log_services_${manager_safe}" "${member_url}/LogServices"
    log_services_body=${last_redfish_body}
    collect_log_services "manager_${manager_safe}" "${log_services_body}"
    redfish_get "manager_eventlog_entries_${manager_safe}" "${member_url}/LogServices/EventLog/Entries"
    redfish_get "manager_redfish_events_${manager_safe}" "${member_url}/LogServices/RedfishEvents/Entries"
    redfish_get "manager_log1_entries_${manager_safe}" "${member_url}/LogServices/Log1/Entries"
    collection_url="${member_url}/VirtualMedia"
    redfish_get "manager_virtual_media_collection_${manager_safe}" "${collection_url}"
    collection_body=${last_redfish_body}
    while IFS= read -r vm_member; do
      [[ -n "${vm_member}" ]] || continue
      vm_name=$(sanitize_name "${vm_member}")
      redfish_get "virtual_media_${vm_name}" "$(redfish_absolute_url "${vm_member}")"
      if [[ -z "${vmedia_uri}" && "${vm_member}" =~ /([Cc][Dd]|[Dd][Vv][Dd]|CD1|DVD1)$ ]]; then
        vmedia_uri=${vm_member}
      fi
    done < <(json_members "${collection_body}")
  done < <(json_members "${managers_body}")

  while IFS= read -r member; do
    [[ -n "${member}" ]] || continue
    if [[ -z "${system_uri}" ]]; then
      system_uri=${member}
    fi
    member_url=$(redfish_absolute_url "${member}")
    manager_safe=$(sanitize_name "${member}")
    redfish_get "system_${manager_safe}" "${member_url}"
    redfish_get "system_log_services_${manager_safe}" "${member_url}/LogServices"
    log_services_body=${last_redfish_body}
    collect_log_services "system_${manager_safe}" "${log_services_body}"
    redfish_get "system_virtual_media_collection_${manager_safe}" "${member_url}/VirtualMedia"
    collection_body=${last_redfish_body}
    while IFS= read -r vm_member; do
      [[ -n "${vm_member}" ]] || continue
      vm_name=$(sanitize_name "${vm_member}")
      redfish_get "system_virtual_media_${vm_name}" "$(redfish_absolute_url "${vm_member}")"
      if [[ -z "${vmedia_uri}" && "${vm_member}" =~ /([Cc][Dd]|[Dd][Vv][Dd]|CD1|DVD1)$ ]]; then
        vmedia_uri=${vm_member}
      fi
    done < <(json_members "${collection_body}")
  done < <(json_members "${systems_body}")

  if [[ -n "${vmedia_uri}" ]]; then
    redfish_get virtual_media "$(redfish_absolute_url "${vmedia_uri}")"
    manager_uri=${vmedia_uri%/VirtualMedia/*}
  fi
  if [[ -n "${security_uri}" ]]; then
    redfish_get security_service "$(redfish_absolute_url "${security_uri}")"
  fi
  if [[ -n "${task_url}" ]]; then
    redfish_get insert_task "${task_url}"
  fi
  if [[ -n "${manager_uri}" ]]; then
    manager_safe=$(sanitize_name "${manager_uri}")
    redfish_get "manager_${manager_safe}" "$(redfish_absolute_url "${manager_uri}")"
    redfish_get "manager_network_protocol_${manager_safe}" "$(redfish_absolute_url "${manager_uri}/NetworkProtocol")"
    redfish_get "manager_ethernet_${manager_safe}" "$(redfish_absolute_url "${manager_uri}/EthernetInterfaces")"
    redfish_get "manager_log_services_${manager_safe}" "$(redfish_absolute_url "${manager_uri}/LogServices")"
    log_services_body=${last_redfish_body}
    collect_log_services "manager_${manager_safe}" "${log_services_body}"
    redfish_get "manager_eventlog_entries_${manager_safe}" "$(redfish_absolute_url "${manager_uri}/LogServices/EventLog/Entries")"
    redfish_get "manager_redfish_events_${manager_safe}" "$(redfish_absolute_url "${manager_uri}/LogServices/RedfishEvents/Entries")"
    redfish_get "manager_log1_entries_${manager_safe}" "$(redfish_absolute_url "${manager_uri}/LogServices/Log1/Entries")"
  fi

  collect_redfish_action_info
}

write_summary() {
  {
    printf 'generated_at_utc=%s\n' "${timestamp}"
    printf 'context_source=%s\n' "${context_source:-unknown}"
    printf 'bootwright_context=%s\n' "${bootwright_context:-not detected}"
    printf 'cluster_dir=%s\n' "${cluster_dir}"
    printf 'context_input_dir=%s\n' "${context_input_dir:-not detected}"
    printf 'context_state_dir=%s\n' "${context_state_dir:-not detected}"
    printf 'bootwright_root_dir=%s\n' "${bootwright_root_dir}"
    printf 'iso_url=%s\n' "${iso_url:-not detected}"
    printf 'iso_urls:\n'
    if [[ -s "${iso_urls_file}" ]]; then
      sed 's/^/- /' "${iso_urls_file}"
    else
      printf '%s\n' '- not detected'
    fi
    printf 'iso_path=%s\n' "${iso_path:-not detected}"
    printf 'iso_path_status=%s\n' "${iso_path_status}"
    printf 'bmc_url=%s\n' "${bmc_url:-not detected}"
    printf 'system_uri=%s\n' "${system_uri:-not detected}"
    printf 'vmedia_uri=%s\n' "${vmedia_uri:-not detected}"
    printf 'task_url=%s\n' "${task_url:-not detected}"
    printf 'security_uri=%s\n' "${security_uri:-not detected}"
    printf 'redfish_credentials_ref=%s\n' "${bmc_credentials_ref:-not detected}"
    printf 'provider_disable_certificate_verification=%s\n' "${provider_disable_certificate_verification:-not detected}"
    printf 'redfish_user=%s\n' "${bmc_user:+provided}"
    printf 'redfish_password=%s\n' "${bmc_password:+provided}"
    printf 'redfish_tls_verification=%s\n' "$([[ "${redfish_insecure}" -eq 1 ]] && printf 'disabled_for_diagnostics' || printf 'enabled')"
    printf 'redfish_tls_reason=%s\n' "${redfish_tls_reason}"
    printf 'redfish_insert_attempt=%s\n' "$([[ "${redfish_insert_attempt}" -eq 1 ]] && printf 'enabled' || printf 'disabled')"
    printf 'redfish_eject_first=%s\n' "$([[ "${redfish_eject_first}" -eq 1 ]] && printf 'enabled' || printf 'disabled')"
    printf 'state_change_consent=%s\n' "$([[ "${state_change_yes}" -eq 1 ]] && printf 'yes' || printf 'no')"
    printf '\n'
    printf 'next_interpretation_notes:\n'
    printf '%s\n' '- If ISO head/range probes fail here, fix artifact publication before retrying Redfish.'
    printf '%s\n' '- If ISO probes pass here but the Redfish task still reports ConnectionFailed, verify BMC-network DNS, routing, firewall, and HTTPS certificate verification to the ISO URL.'
    printf '%s\n' '- If VirtualMedia shows VerifyCertificate unsupported and SecurityService HttpsTransferCertVerification still true, the BMC may reject the self-signed artifact endpoint.'
    if [[ -s "${notes_file}" ]]; then
      printf '\n'
      printf 'diagnostic_notes:\n'
      sed 's/^/- /' "${notes_file}"
    fi
  } >"${work_dir}/summary.txt"
}

create_bundle() {
  local tarball="${work_dir}.tar.gz"
  printf '%s\n' "${tarball}" >"${work_dir}/bundle-path.txt"
  tar -C "${output_parent}" -czf "${tarball}" "$(basename "${work_dir}")"
  info "created ${tarball}"
}

discover_from_logs
discover_iso_url_from_files
record_iso_url "${iso_url}"
normalize_base_url
discover_from_desired_state
normalize_base_url
resolve_redfish_tls_mode
load_password
prepare_auth_config
discover_iso_path
write_file_listing
copy_selected_files
collect_system
collect_iso_file
collect_iso
collect_redfish
attempt_redfish_boot_iso
copy_host_artifact_files after_redfish
write_summary
create_bundle
