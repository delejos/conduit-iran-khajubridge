# KhajuBridge

KhajuBridge is a Linux-based firewall layer for Psiphon Conduit that enables region-restricted access using nftables.

It mirrors the behavior of an existing Windows firewall implementation by allowing global TCP connectivity while restricting UDP traffic to a configurable region using CIDR-based filtering. IPv4 and IPv6 are both supported.

---

## Overview

KhajuBridge provides a simple and transparent way to apply region-based network restrictions to Psiphon Conduit on Linux systems.

The project is designed as a lightweight wrapper around nftables and does not modify Conduit itself. All filtering is applied at the firewall level and can be safely enabled, updated, or disabled.

---

## How It Works

KhajuBridge follows a three-step model:

1. **Fetch region CIDR ranges**  
   A script downloads IPv4 and IPv6 CIDR ranges for a specific region from public sources.

2. **Define firewall rules**  
   An nftables ruleset defines how traffic should be handled:
   - TCP traffic to Conduit ports is allowed globally (normal mode)
   - UDP traffic to Conduit ports is only allowed from the configured region
   - All other traffic remains unaffected

3. **Apply rules safely**  
   A helper script loads the rules and populates nftables sets atomically, allowing safe re-application and updates without interrupting existing connections.

---

## Features

- Region-restricted access using CIDR-based filtering
- Supports both IPv4 and IPv6
- Uses nftables sets for efficient updates
- Safe to re-run and update at any time
- Does not modify or patch Psiphon Conduit
- Designed for Debian-based Linux systems

---

## Repository Structure

```text
KhajuBridge/
├─ scripts/
│  ├─ update_region_cidrs.sh   # Fetches region CIDR lists
│  └─ apply_firewall.sh        # Applies nftables rules and CIDRs
├─ nftables/
│  └─ conduit-region.nft       # Base nftables ruleset
├─ systemd/                    # systemd units (optional / future)
└─ README.md

Requirements:
- Linux system with nftables support
- Debian 11 / 12 or compatible distribution
- Root or sudo privileges
- Psiphon Conduit installed and running

Quick Start (Manual):
Install dependencies:
sudo apt install nftables curl

Fetch region CIDR ranges:
sudo ./scripts/update_region_cidrs.sh

Apply firewall rules:
sudo ./scripts/apply_firewall.sh

Verify rules:
sudo nft list table inet khajubridge

Modes:
Currently supported:
- Normal mode: TCP traffic is allowed globally, while UDP traffic is restricted to the configured region.

Future versions may introduce a strict mode where both TCP and UDP traffic are region-restricted.

Safety Notes:
- KhajuBridge only affects traffic matching the configured Conduit ports.
- Other services on the system are not modified.
- CIDR lists can change over time; regular updates are recommended.
- Always test firewall rules on non-critical systems before production use.

Credits:
This project is inspired by an existing Windows-based firewall implementation for Psiphon Conduit and adapts the same core ideas to Linux using nftables.

