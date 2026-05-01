#!/usr/bin/env bash
set -euo pipefail

DATA_DIR="/etc/khajubridge"
REGION_V4="${DATA_DIR}/region_ipv4.cidr"
REGION_V6="${DATA_DIR}/region_ipv6.cidr"

# Minimum acceptable CIDR counts — if a fetch returns fewer, it is treated as failure.
MIN_V4=100
MIN_V6=10

IP_SOURCES_V4=(
  "https://www.ipdeny.com/ipblocks/data/countries/ir.zone"
  "https://raw.githubusercontent.com/herrbischoff/country-ip-blocks/master/ipv4/ir.cidr"
)

IP_SOURCES_V6=(
  "https://www.ipdeny.com/ipv6/ipaddresses/blocks/ir.zone"
  "https://raw.githubusercontent.com/herrbischoff/country-ip-blocks/master/ipv6/ir.cidr"
)

mkdir -p "${DATA_DIR}"

# Write to a temp file, validate count, then atomically replace the real file.
# If all sources fail or result is below the minimum, the existing file is kept intact.
fetch_and_validate() {
  local output="$1"
  local min_count="$2"
  shift 2
  local sources=("$@")

  local tmp
  tmp="$(mktemp)"
  trap 'rm -f "$tmp"' RETURN

  local fetch_ok=0
  for url in "${sources[@]}"; do
    echo "Fetching ${url}"
    if curl -fsSL --max-time 30 "${url}" \
        | sed 's/\r$//' \
        | grep -E '^[0-9a-fA-F:.]+/[0-9]+' \
        >> "${tmp}"; then
      fetch_ok=1
    else
      echo "WARN: fetch failed for ${url}" >&2
    fi
  done

  if [[ "$fetch_ok" -eq 0 ]]; then
    echo "ERROR: all sources failed, keeping existing file: ${output}" >&2
    return 1
  fi

  sort -u "${tmp}" -o "${tmp}"

  local count
  count="$(wc -l < "${tmp}" | tr -d '[:space:]')"

  if [[ "$count" -lt "$min_count" ]]; then
    echo "ERROR: only ${count} CIDRs fetched (minimum ${min_count}), keeping existing file: ${output}" >&2
    return 1
  fi

  mv "${tmp}" "${output}"
  echo "OK: ${count} CIDRs written to ${output}"
}

echo "Updating region CIDR lists..."

v4_ok=0
v6_ok=0

fetch_and_validate "${REGION_V4}" "${MIN_V4}" "${IP_SOURCES_V4[@]}" && v4_ok=1 || true
fetch_and_validate "${REGION_V6}" "${MIN_V6}" "${IP_SOURCES_V6[@]}" && v6_ok=1 || true

echo ""
echo "Update complete:"
[[ "$v4_ok" -eq 1 ]] && echo "  IPv4 ranges: $(wc -l < "${REGION_V4}")" || echo "  IPv4: FAILED (existing file preserved)"
[[ "$v6_ok" -eq 1 ]] && echo "  IPv6 ranges: $(wc -l < "${REGION_V6}")" || echo "  IPv6: FAILED (existing file preserved)"

if [[ "$v4_ok" -eq 0 ]]; then
  exit 1
fi
