#!/usr/bin/env bash
set -euo pipefail

# KhajuBridge firewall apply script
# - Resolves Conduit systemd cgroup ID dynamically
# - Injects it into nftables rules
# - Loads CIDR sets safely (bulk update)
# - Guardrails: refuses to apply if template looks wrong
# - Prints counts of CIDRs loaded into each set
#
# NOTE: This script does NOT "flush ruleset". It only replaces the inet khajubridge table.

NFT_TEMPLATE="nftables/conduit-region.nft"
CIDR_DIR="/etc/khajubridge"

REGION_V4="${CIDR_DIR}/region_ipv4.cidr"
REGION_V6="${CIDR_DIR}/region_ipv6.cidr"

TABLE_NAME="khajubridge"
SERVICE_NAME="conduit.service"

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

# Accept "#!/usr/sbin/nft -f" exactly, as in your template.
if ! head -n 1 "$NFT_TEMPLATE" | grep -qE '^#!/usr/sbin/nft[[:space:]]+-f[[:space:]]*$'; then
  echo "ERROR: $NFT_TEMPLATE does not look like an nftables file (expected '#!/usr/sbin/nft -f' on line 1)."
  echo "Refusing to apply firewall."
  exit 1
fi

if ! grep -q '__CGROUP_ID__' "$NFT_TEMPLATE"; then
  echo "ERROR: $NFT_TEMPLATE is missing __CGROUP_ID__ placeholder."
  echo "Refusing to apply firewall."
  exit 1
fi

# ---- Resolve Conduit cgroup ID ----
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

echo "Resolved cgroup ID for $SERVICE_NAME: $CGROUP_ID"

# ---- Helpers: load/sanitize CIDRs and build bulk nft commands ----
count_and_collect_cidrs() {
  # Reads CIDRs from file, strips blank lines and comments, trims whitespace,
  # and prints them one-per-line to stdout. Also echoes count to stderr as "COUNT=<n>".
  local file="$1"
  local out
  local cnt

  # Remove comments, trim whitespace, drop empty lines
  # shellcheck disable=SC2002
  out="$(cat "$file" \
    | sed -E 's/[[:space:]]*#.*$//' \
    | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//' \
    | sed -E '/^$/d')"

  if [[ -z "$out" ]]; then
    echo "COUNT=0" >&2
    return 0
  fi

  # Count lines robustly
  cnt="$(printf '%s\n' "$out" | wc -l | tr -d '[:space:]')"
  echo "COUNT=$cnt" >&2
  printf '%s\n' "$out"
}

build_add_element_block() {
  # Build one or more "add element ... { ... }" lines with chunking to avoid overly long commands.
  # Args: table set family addrcmd_prefix elements...
  # Usage: build_add_element_block "inet khajubridge" "region_ipv4" "add element inet khajubridge region_ipv4" <elements on stdin>
  local family_table="$1"
  local setname="$2"
  local prefix="$3"
  local max_per_line=200  # conservative chunk size; adjust if needed

  local chunk=()
  local n=0
  local line

  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    chunk+=("$line")
    n=$((n + 1))

    if [[ "$n" -ge "$max_per_line" ]]; then
      # Emit chunk
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
sed "s/__CGROUP_ID__/$CGROUP_ID/g" "$NFT_TEMPLATE" > "$TMP_NFT_RULES"

echo "Replacing nftables table inet $TABLE_NAME (scoped; does not flush global ruleset)..."
sudo nft delete table inet "$TABLE_NAME" 2>/dev/null || true
sudo nft -f "$TMP_NFT_RULES"

# ---- Build set population snippet (bulk update) ----
echo "Preparing CIDR set updates..."

V4_COUNT=0
V6_COUNT=0

# Collect IPv4 CIDRs
V4_CIDRS="$(count_and_collect_cidrs "$REGION_V4" 2> >(tee /dev/stderr) )" || true
# Extract COUNT from the helper's stderr line
V4_COUNT="$(grep -Eo 'COUNT=[0-9]+' /dev/stderr 2>/dev/null | tail -n1 | cut -d= -f2 || true)"
# The above COUNT extraction via /dev/stderr is brittle in some shells; do it in a safer way:
# Recompute count from V4_CIDRS:
if [[ -n "${V4_CIDRS:-}" ]]; then
  V4_COUNT="$(printf '%s\n' "$V4_CIDRS" | wc -l | tr -d '[:space:]')"
else
  V4_COUNT=0
fi

# Collect IPv6 CIDRs if present
V6_CIDRS=""
if [[ "$HAS_V6" -eq 1 ]]; then
  V6_CIDRS="$(count_and_collect_cidrs "$REGION_V6" 2> >(tee /dev/stderr) )" || true
  if [[ -n "${V6_CIDRS:-}" ]]; then
    V6_COUNT="$(printf '%s\n' "$V6_CIDRS" | wc -l | tr -d '[:space:]')"
  else
    V6_COUNT=0
  fi
fi

# Compose nft snippet for sets (flush + add in bulk)
{
  echo "#!/usr/sbin/nft -f"
  echo ""
  echo "flush set inet $TABLE_NAME region_ipv4"
  if [[ "$V4_COUNT" -gt 0 ]]; then
    printf '%s\n' "$V4_CIDRS" \
      | build_add_element_block "inet $TABLE_NAME" "region_ipv4" "add element inet $TABLE_NAME region_ipv4"
  fi

  if [[ "$HAS_V6" -eq 1 ]]; then
    echo ""
    echo "flush set inet $TABLE_NAME region_ipv6"
    if [[ "$V6_COUNT" -gt 0 ]]; then
      printf '%s\n' "$V6_CIDRS" \
        | build_add_element_block "inet $TABLE_NAME" "region_ipv6" "add element inet $TABLE_NAME region_ipv6"
    fi
  fi
} > "$TMP_NFT_SETS"

# ---- Apply set updates ----
echo "Updating region CIDR sets (bulk)..."
sudo nft -f "$TMP_NFT_SETS"

echo "Loaded IPv4 CIDRs: $V4_COUNT"
if [[ "$HAS_V6" -eq 1 ]]; then
  echo "Loaded IPv6 CIDRs: $V6_COUNT"
else
  echo "IPv6 CIDR file missing or empty; skipping IPv6 set population."
fi

echo "Firewall rules applied successfully."

