#!/usr/bin/env bash
set -euo pipefail

# KhajuBridge firewall apply script
# - Resolves Conduit systemd cgroup ID dynamically (for meta cgroup matches)
# - Resolves Conduit systemd cgroup path dynamically (for socket cgroupv2 matches)
# - Injects both into nftables rules
# - Loads CIDR sets safely (bulk update)
# - Writes state file to /etc/khajubridge/state.json on success (read by console)
#
# NOTE: This script does NOT "flush ruleset". It only replaces the inet khajubridge table.

NFT_TEMPLATE="nftables/conduit-region.nft"
CIDR_DIR="/etc/khajubridge"
STATE_FILE="${CIDR_DIR}/state.json"

REGION_V4="${CIDR_DIR}/region_ipv4.cidr"
REGION_V6="${CIDR_DIR}/region_ipv6.cidr"

TABLE_NAME="khajubridge"
SERVICE_NAME="${CONDUIT_UNIT:-conduit.service}"

echo "Applying KhajuBridge firewall rules..."

# ---- Pre-flight checks ----
if ! command -v nft >/dev/null 2>&1; then
  echo "ERROR: nftables (nft) is not installed."
  exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
  echo "ERROR: systemd (systemctl) is not available."
  exit 1
fi

if ! systemctl is-active --quiet "$SERVICE_NAME"; then
  echo "ERROR: $SERVICE_NAME is not running."
  exit 1
fi

if [[ ! -f "$REGION_V4" ]] || [[ ! -s "$REGION_V4" ]]; then
  echo "ERROR: Missing or empty IPv4 CIDR file: $REGION_V4"
  echo "Run: scripts/update_region_cidrs.sh first"
  exit 1
fi

HAS_V6=0
if [[ -f "$REGION_V6" ]] && [[ -s "$REGION_V6" ]]; then
  HAS_V6=1
fi

# ---- Guardrails: template sanity checks ----
if [[ ! -f "$NFT_TEMPLATE" ]]; then
  echo "ERROR: Missing nftables template: $NFT_TEMPLATE"
  exit 1
fi

if ! head -n 1 "$NFT_TEMPLATE" | grep -qE '^#!/usr/sbin/nft[[:space:]]+-f[[:space:]]*$'; then
  echo "ERROR: $NFT_TEMPLATE does not look like an nftables file (expected '#!/usr/sbin/nft -f' on line 1)."
  exit 1
fi

if ! grep -q '__CGROUP_ID__' "$NFT_TEMPLATE"; then
  echo "ERROR: $NFT_TEMPLATE is missing __CGROUP_ID__ placeholder."
  exit 1
fi

if ! grep -q '__CGROUP_PATH__' "$NFT_TEMPLATE"; then
  echo "ERROR: $NFT_TEMPLATE is missing __CGROUP_PATH__ placeholder."
  exit 1
fi

# ---- Resolve Conduit cgroup path + ID ----
CGROUP_PATH="$(systemctl show -p ControlGroup --value "$SERVICE_NAME" || true)"
if [[ -z "$CGROUP_PATH" ]]; then
  echo "ERROR: Failed to read ControlGroup for $SERVICE_NAME"
  exit 1
fi

CGROUP_FS_PATH="/sys/fs/cgroup${CGROUP_PATH}"
if [[ ! -e "$CGROUP_FS_PATH" ]]; then
  echo "ERROR: cgroup path does not exist: $CGROUP_FS_PATH"
  exit 1
fi

CGROUP_ID="$(stat -c %i "$CGROUP_FS_PATH" || true)"
if [[ -z "$CGROUP_ID" ]]; then
  echo "ERROR: Failed to resolve cgroup ID for $SERVICE_NAME"
  exit 1
fi

echo "Resolved cgroup path for $SERVICE_NAME: $CGROUP_PATH"
echo "Resolved cgroup ID   for $SERVICE_NAME: $CGROUP_ID"

# ---- Sanitize CIDRs from a file ----
read_cidrs() {
  local file="$1"
  sed -E 's/[[:space:]]*#.*$//; s/^[[:space:]]+//; s/[[:space:]]+$//; /^$/d' "$file"
}

# ---- Build chunked "add element" lines ----
build_add_element_block() {
  local prefix="$1"
  local max_per_line=200
  local chunk=()
  local n=0
  local line

  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    chunk+=("$line")
    n=$((n + 1))
    if [[ "$n" -ge "$max_per_line" ]]; then
      (IFS=,; echo "$prefix { ${chunk[*]} }")
      chunk=()
      n=0
    fi
  done

  if [[ "${#chunk[@]}" -gt 0 ]]; then
    (IFS=,; echo "$prefix { ${chunk[*]} }")
  fi
}

# ---- Prepare temporary files ----
TMP_NFT_RULES="$(mktemp)"
TMP_NFT_SETS="$(mktemp)"
trap 'rm -f "$TMP_NFT_RULES" "$TMP_NFT_SETS"' EXIT

# ---- Render and load rules (replace only our table) ----
echo "Rendering nftables table from template..."
sed -e "s/__CGROUP_ID__/$CGROUP_ID/g" \
    -e "s#__CGROUP_PATH__#$CGROUP_PATH#g" \
    "$NFT_TEMPLATE" > "$TMP_NFT_RULES"

echo "Replacing nftables table inet $TABLE_NAME..."
nft delete table inet "$TABLE_NAME" 2>/dev/null || true
nft -f "$TMP_NFT_RULES"

# ---- Count CIDRs ----
V4_CIDRS="$(read_cidrs "$REGION_V4")"
V4_COUNT="$(printf '%s\n' "$V4_CIDRS" | grep -c . || echo 0)"

V6_CIDRS=""
V6_COUNT=0
if [[ "$HAS_V6" -eq 1 ]]; then
  V6_CIDRS="$(read_cidrs "$REGION_V6")"
  V6_COUNT="$(printf '%s\n' "$V6_CIDRS" | grep -c . || echo 0)"
fi

# ---- Build and apply set population ----
echo "Preparing CIDR set updates..."
{
  echo "#!/usr/sbin/nft -f"
  echo ""
  echo "flush set inet $TABLE_NAME region_ipv4"
  if [[ "$V4_COUNT" -gt 0 ]]; then
    printf '%s\n' "$V4_CIDRS" \
      | build_add_element_block "add element inet $TABLE_NAME region_ipv4"
  fi

  if [[ "$HAS_V6" -eq 1 ]]; then
    echo ""
    echo "flush set inet $TABLE_NAME region_ipv6"
    if [[ "$V6_COUNT" -gt 0 ]]; then
      printf '%s\n' "$V6_CIDRS" \
        | build_add_element_block "add element inet $TABLE_NAME region_ipv6"
    fi
  fi
} > "$TMP_NFT_SETS"

echo "Updating region CIDR sets (bulk)..."
nft -f "$TMP_NFT_SETS"

echo "Loaded IPv4 CIDRs: $V4_COUNT"
if [[ "$HAS_V6" -eq 1 ]]; then
  echo "Loaded IPv6 CIDRs: $V6_COUNT"
else
  echo "IPv6 CIDR file missing or empty; skipping IPv6 set population."
fi

# ---- Compute ruleset hash ----
RULESET_HASH="$(nft list table inet "$TABLE_NAME" | sha256sum | cut -c1-12)"

# ---- Write state file for console ----
APPLIED_AT="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
CIDR_SOURCE="ipdeny.com+herrbischoff"

mkdir -p "$CIDR_DIR"
cat > "$STATE_FILE" <<EOF
{
  "enforcement_status": "ON",
  "applied_at_utc": "${APPLIED_AT}",
  "ruleset_hash": "${RULESET_HASH}",
  "cidr_source": "${CIDR_SOURCE}",
  "v4_count": ${V4_COUNT},
  "v6_count": ${V6_COUNT},
  "service": "${SERVICE_NAME}",
  "cgroup_id": "${CGROUP_ID}"
}
EOF

echo "Firewall rules applied successfully."
echo "State written to: $STATE_FILE"
