#!/usr/bin/env bash
set -euo pipefail

# KhajuBridge — Option A: Strict Iran-Only (Dedicated IP + cgroup-scoped SNAT)
#
# Enforces hard Iran-only access for Conduit by:
#   1. Source-NAT-ing all Conduit outbound traffic to a dedicated IP
#      (uses meta cgroup, consistent with the main firewall)
#   2. Adding inbound rules for that dedicated IP, allowing only Iran CIDRs
#
# IPv6 is supported optionally via DEDICATED_IP6 + INTERFACE6.
#
# Prerequisites:
#   - KhajuBridge Layer 1 already applied (apply_firewall.sh)
#   - A secondary IPv4 (and optionally IPv6) address assigned to the host interface
#   - See docs/OPTION_A_DEDICATED_IP.md

DEDICATED_IP="${DEDICATED_IP:?Set DEDICATED_IP to the secondary IPv4 for Conduit (e.g. 192.0.2.20)}"
INTERFACE="${INTERFACE:?Set INTERFACE to the network interface (e.g. eth0)}"
DEDICATED_IP6="${DEDICATED_IP6:-}"   # optional; leave empty to skip IPv6
INTERFACE6="${INTERFACE6:-$INTERFACE}"
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
echo "Dedicated IPv4:  $DEDICATED_IP  (interface: $INTERFACE)"
[[ -n "$DEDICATED_IP6" ]] && echo "Dedicated IPv6:  $DEDICATED_IP6  (interface: $INTERFACE6)" || echo "IPv6:            not configured (set DEDICATED_IP6 to enable)"

# ---- Verify dedicated IPs are assigned ----
if ! ip -4 addr show dev "$INTERFACE" | grep -qF "$DEDICATED_IP"; then
  echo "ERROR: $DEDICATED_IP is not assigned to $INTERFACE."
  echo "Assign it first: sudo ip addr add $DEDICATED_IP/<PREFIX> dev $INTERFACE"
  exit 1
fi

if [[ -n "$DEDICATED_IP6" ]]; then
  if ! ip -6 addr show dev "$INTERFACE6" | grep -qF "$DEDICATED_IP6"; then
    echo "ERROR: $DEDICATED_IP6 is not assigned to $INTERFACE6."
    echo "Assign it first: sudo ip -6 addr add $DEDICATED_IP6/<PREFIX> dev $INTERFACE6"
    exit 1
  fi
fi

# ---- IPv4: create/replace NAT table with cgroup-scoped SNAT ----
nft delete table ip conduit_nat 2>/dev/null || true
nft add table ip conduit_nat
nft 'add chain ip conduit_nat postrouting { type nat hook postrouting priority srcnat; policy accept; }'
nft add rule ip conduit_nat postrouting meta cgroup "$CGROUP_ID" snat to "$DEDICATED_IP"
echo "IPv4 SNAT: Conduit (cgroup $CGROUP_ID) → $DEDICATED_IP"

# Inbound: allow Iran CIDRs to reach the dedicated IPv4; drop everything else.
nft add rule inet "$TABLE_NAME" input ip daddr "$DEDICATED_IP" ip saddr @region_ipv4 accept
nft add rule inet "$TABLE_NAME" input ip daddr "$DEDICATED_IP" counter drop
echo "IPv4 inbound: allow @region_ipv4 → $DEDICATED_IP, drop all else."

# ---- IPv6: optional ----
if [[ -n "$DEDICATED_IP6" ]]; then
  nft delete table ip6 conduit_nat6 2>/dev/null || true
  nft add table ip6 conduit_nat6
  nft 'add chain ip6 conduit_nat6 postrouting { type nat hook postrouting priority srcnat; policy accept; }'
  nft add rule ip6 conduit_nat6 postrouting meta cgroup "$CGROUP_ID" snat to "$DEDICATED_IP6"
  echo "IPv6 SNAT: Conduit (cgroup $CGROUP_ID) → $DEDICATED_IP6"

  nft add rule inet "$TABLE_NAME" input ip6 daddr "$DEDICATED_IP6" ip6 saddr @region_ipv6 accept
  nft add rule inet "$TABLE_NAME" input ip6 daddr "$DEDICATED_IP6" counter drop
  echo "IPv6 inbound: allow @region_ipv6 → $DEDICATED_IP6, drop all else."
else
  echo "IPv6 skipped — set DEDICATED_IP6 to enable."
fi

echo ""
echo "Option A applied successfully."
echo ""
echo "Verify:"
echo "  sudo nft list table ip conduit_nat"
[[ -n "$DEDICATED_IP6" ]] && echo "  sudo nft list table ip6 conduit_nat6"
echo "  sudo nft list chain inet $TABLE_NAME input"
echo ""
echo "Rollback:"
echo "  sudo nft delete table ip conduit_nat"
[[ -n "$DEDICATED_IP6" ]] && echo "  sudo nft delete table ip6 conduit_nat6"
echo "  sudo nft flush chain inet $TABLE_NAME input"
echo "  sudo ./scripts/apply_firewall.sh   # re-apply Layer 1 cleanly"
