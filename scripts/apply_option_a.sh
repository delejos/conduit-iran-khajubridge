#!/usr/bin/env bash
set -euo pipefail

# KhajuBridge — Option A: Strict Iran-Only (Dedicated IP + cgroup-scoped SNAT)
#
# This script enforces hard Iran-only access by:
#   1. Source-NAT-ing all Conduit outbound traffic to a dedicated IP
#      (uses meta cgroup, not UID — consistent with the main firewall)
#   2. Adding inbound rules scoped to that dedicated IP, allowing only Iran CIDRs
#
# Prerequisites:
#   - KhajuBridge Layer 1 already applied (apply_firewall.sh)
#   - A secondary IP assigned to the host interface (see docs/OPTION_A_DEDICATED_IP.md)
#   - DEDICATED_IP and INTERFACE set below or via environment variables

DEDICATED_IP="${DEDICATED_IP:?Set DEDICATED_IP to the secondary IP address for Conduit (e.g. 192.0.2.20)}"
INTERFACE="${INTERFACE:?Set INTERFACE to the network interface (e.g. eth0)}"
SERVICE_NAME="${CONDUIT_UNIT:-conduit.service}"
TABLE_NAME="khajubridge"

echo "Applying KhajuBridge Option A (Strict Iran-Only)..."

# ---- Verify Layer 1 is loaded ----
if ! nft list table inet "$TABLE_NAME" >/dev/null 2>&1; then
  echo "ERROR: inet $TABLE_NAME table not found. Run apply_firewall.sh first."
  exit 1
fi

if ! systemctl is-active --quiet "$SERVICE_NAME"; then
  echo "ERROR: $SERVICE_NAME is not running."
  exit 1
fi

# ---- Resolve Conduit cgroup ID (same method as apply_firewall.sh) ----
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

CGROUP_ID="$(stat -c %i "$CGROUP_FS_PATH")"
echo "Using cgroup ID: $CGROUP_ID"
echo "Dedicated IP:    $DEDICATED_IP"
echo "Interface:       $INTERFACE"

# ---- Verify the dedicated IP exists on the interface ----
if ! ip addr show dev "$INTERFACE" | grep -q "$DEDICATED_IP"; then
  echo "ERROR: $DEDICATED_IP is not assigned to $INTERFACE."
  echo "Assign it first: sudo ip addr add $DEDICATED_IP/<PREFIX> dev $INTERFACE"
  exit 1
fi

# ---- Create/replace the NAT table (cgroup-scoped SNAT) ----
nft delete table ip conduit_nat 2>/dev/null || true

nft add table ip conduit_nat
nft 'add chain ip conduit_nat postrouting { type nat hook postrouting priority srcnat; policy accept; }'
nft add rule ip conduit_nat postrouting meta cgroup "$CGROUP_ID" snat to "$DEDICATED_IP"

echo "SNAT rule installed: Conduit (cgroup $CGROUP_ID) → $DEDICATED_IP"

# ---- Add inbound rules scoped to the dedicated IP in the khajubridge table ----
# Allow only Iran CIDRs to reach the dedicated IP; drop everything else.
# These rules are appended to the existing input chain.
nft add rule inet "$TABLE_NAME" input ip daddr "$DEDICATED_IP" ip saddr @region_ipv4 accept
nft add rule inet "$TABLE_NAME" input ip daddr "$DEDICATED_IP" counter drop

echo "Inbound rules: allow Iran → $DEDICATED_IP, drop all else."
echo ""
echo "Option A applied successfully."
echo ""
echo "Verify with:"
echo "  sudo nft list table ip conduit_nat"
echo "  sudo nft list chain inet $TABLE_NAME input"
echo ""
echo "Rollback:"
echo "  sudo nft delete table ip conduit_nat"
echo "  sudo nft flush chain inet $TABLE_NAME input && sudo ./scripts/apply_firewall.sh"
