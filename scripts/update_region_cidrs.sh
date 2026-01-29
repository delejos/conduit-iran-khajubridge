#!/usr/bin/env bash
set -euo pipefail

# Directory to store downloaded CIDR lists
DATA_DIR="/etc/khajubridge"
REGION_V4="${DATA_DIR}/region_ipv4.cidr"
REGION_V6="${DATA_DIR}/region_ipv6.cidr"

# CIDR sources (swap these later if you change region/provider)
IP_SOURCES_V4=(
  "https://www.ipdeny.com/ipblocks/data/countries/ir.zone"
)

IP_SOURCES_V6=(
  "https://www.ipdeny.com/ipv6/ipaddresses/blocks/ir.zone"
)

echo "Updating region CIDR lists..."

# NOTE: On a real Linux machine this path needs root permissions.
# For testing, you can temporarily change DATA_DIR to "./data".
mkdir -p "${DATA_DIR}"

fetch_and_clean() {
  local output="$1"
  shift
  : > "${output}"

  for url in "$@"; do
    echo "Fetching ${url}"
    curl -fsSL --max-time 30 "${url}" \
      | sed 's/\r$//' \
      | grep -E '^[0-9a-fA-F:.]+/[0-9]+' \
      >> "${output}" || true
  done

  sort -u "${output}" -o "${output}"
}

fetch_and_clean "${REGION_V4}" "${IP_SOURCES_V4[@]}"
fetch_and_clean "${REGION_V6}" "${IP_SOURCES_V6[@]}"

echo "Update complete:"
echo "IPv4 ranges: $(wc -l < "${REGION_V4}")"
echo "IPv6 ranges: $(wc -l < "${REGION_V6}")"
