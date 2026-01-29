#!/usr/bin/env bash
set -euo pipefail

# KhajuBridge firewall apply script
# Loads region CIDRs into nftables and applies rules safely

NFT_FILE="/etc/khajubridge/conduit-region.nft"
CIDR_DIR="/etc/khajubridge"

REGION_V4="${CIDR_DIR}/region_ipv4.cidr"
REGION_V6="${CIDR_DIR}/region_ipv6.cidr"

TABLE_NAME="khajubridge"

echo "Applying KhajuBridge firewall rules..."

# ---- Pre-flight checks ----
if ! command -v nft >/dev/null 2>&1; then
  echo "ERROR: nftables is not installed."
  exit 1
fi

if [[ ! -f "$REGION_V4" ]]; then
  echo "ERROR: Missing IPv4 CIDR file: $REGION_V4"
  echo "Run: scripts/update_region_cidrs.sh first"
  exit 1
fi

# IPv6 is optional
HAS_V6=0
if [[ -f "$REGION_V6" ]] && [[ -s "$REGION_V6" ]]; then
  HAS_V6=1
fi

# ---- Prepare temporary nft file ----
TMP_NFT="$(mktemp)"
trap 'rm -f "$TMP_NFT"' EXIT

echo "Generating nftables ruleset..."

{
  echo "flush ruleset"
  echo ""
  cat nftables/conduit-region.nft
} > "$TMP_NFT"

# ---- Load base rules ----
echo "Loading base nftables rules..."
sudo nft -f "$TMP_NFT"

# ---- Populate IPv4 region set ----
echo "Updating IPv4 region set..."
sudo nft flush set inet "$TABLE_NAME" region4
while read -r cidr; do
  [[ -z "$cidr" ]] && continue
  sudo nft add element inet "$TABLE_NAME" region4 "{ $cidr }"
done < "$REGION_V4"

# ---- Populate IPv6 region set (if present) ----
if [[ "$HAS_V6" -eq 1 ]]; then
  echo "Updating IPv6 region set..."
  sudo nft flush set inet "$TABLE_NAME" region6
  while read -r cidr; do
    [[ -z "$cidr" ]] && continue
    sudo nft add element inet "$TABLE_NAME" region6 "{ $cidr }"
  done < "$REGION_V6"
fi

echo "Firewall rules applied successfully."
