KhajuBridge
A Linux Firewall Layer for Psiphon Conduit

KhajuBridge is a Linux-native firewall layer for Psiphon Conduit that enables region-restricted networking using nftables and systemd cgroup scoping.

It mirrors the behavior of existing Windows firewall deployments by allowing TCP globally while restricting UDP traffic to approved regions, without modifying Conduit itself.
## Requirements / Prerequisites

KhajuBridge is a firewall wrapper (scripts + nftables rules), not a compiled build. You need:

- A Linux system that uses **systemd** and supports **nftables**
- **sudo/root** access (to load nftables rules and manage `/etc/khajubridge`)
- **Psiphon Conduit installed and running as a systemd service**
  - default expected unit name: `conduit.service`
- `curl` (used by `scripts/update_region_cidrs.sh` to fetch CIDR lists)


――――――――――――――――――――――――――――――

🚀 Overview

KhajuBridge provides a transparent, non-invasive way to apply region-based network controls to Psiphon Conduit on Linux systems.

Instead of patching or wrapping Conduit, KhajuBridge enforces policy entirely at the firewall level. Rules are scoped specifically to the Conduit process using its systemd cgroup, ensuring:

•	No port-based assumptions
•	No UID-based filtering
•	No impact on other system traffic

The firewall can be safely applied, updated, or removed at any time.

――――――――――――――――――――――――――――――

⚙️ How It Works

KhajuBridge uses a three-stage model:

1. Fetch Region CIDR Ranges
A helper script downloads IPv4 and IPv6 CIDR ranges for one or more regions from public sources and stores them locally.

These CIDRs are treated as dynamic data and can be updated independently of firewall rules.

2. Define Firewall Policy
An nftables ruleset defines outbound traffic handling for Conduit only:

•	TCP traffic from Conduit is allowed globally
•	UDP traffic from Conduit is allowed only to configured regional CIDRs
•	All other UDP traffic from Conduit is dropped
•	All other system traffic is unaffected (policy accept)

No inbound rules are required; Conduit is outbound-only.

3. Apply Rules Safely
A helper script:

•	Dynamically resolves Conduit’s systemd cgroup ID
•	Injects it into the nftables template at runtime
•	Replaces only the KhajuBridge nftables table (never the global ruleset)
•	Bulk-loads CIDR sets efficiently
•	Can be safely re-run at any time

――――――――――――――――――――――――――――――

✨ Features

•	Region-Restricted UDP
CIDR-based allowlists for precise geographic control

•	Global TCP Connectivity
Matches existing Windows firewall behavior

•	Process-Scoped Filtering
Uses systemd cgroups instead of ports or UIDs

•	Dual-Stack Support
Full IPv4 and IPv6 support

•	High Performance
nftables interval sets for efficient lookups

•	Non-Invasive
Does not modify, wrap, or patch Psiphon Conduit

•	Distro-Friendly
Designed and tested on Debian-based systems

――――――――――――――――――――――――――――――

🛠️ Requirements

•	Linux system with nftables support
•	systemd-based distribution
•	Debian 11 / 12 or compatible
•	Root or sudo privileges
•	Psiphon Conduit installed and running as a systemd service (`conduit.service`)

――――――――――――――――――――――――――――――

⚡ Quick Start (Manual)

1. Install dependencies
sudo apt install nftables curl

2. Fetch region CIDR ranges
sudo ./scripts/update_region_cidrs.sh

3. Apply firewall rules
sudo ./scripts/apply_firewall.sh

4. Verify
sudo nft list table inet khajubridge
sudo nft list chain inet khajubridge output
sudo journalctl -u conduit.service -n 20 --no-pager

After cloning the repository, ensure scripts are executable:
chmod +x scripts/*.sh

――――――――――――――――――――――――――――――

🛡️ Safety & Modes

Current Mode (Normal)
•	TCP: allowed globally
•	UDP: allowed only to configured regional CIDRs (IPv4 + IPv6)
•	All other system traffic: unaffected (policy accept)

Future Mode (Planned)
•	Strict mode where both TCP and UDP are region-restricted

Notes
•	KhajuBridge only affects traffic originating from the Conduit service (scoped by systemd cgroup).
•	CIDR lists change over time; regular updates are recommended.
•	Firewall rules can be removed by deleting the `inet khajubridge` table:
  sudo nft delete table inet khajubridge
•	Always test firewall changes on non-critical systems first.

――――――――――――――――――――――――――――――

🧠 Design Notes

•	Outbound-only: Conduit is outbound-only; rules are applied in the `OUTPUT` hook (not `INPUT`).
•	No port filtering: Conduit does not listen on ports; filtering is not port-based.
•	Process scoping: Traffic is scoped using `meta cgroup` (systemd cgroup ID), not UID or ports.
•	UDP allowlist: UDP is restricted using nftables CIDR sets (`region_ipv4`, `region_ipv6`).
•	TCP global: TCP is unrestricted to match Windows firewall-style behavior.
•	No hardcoding: cgroup IDs are resolved dynamically at apply time.

――――――――――――――――――――――――――――――

📁 Project Layout

•	`nftables/conduit-region.nft`
nftables template (not applied directly). Contains:
•	`define CONDUIT_CGROUP = __CGROUP_ID__`

•	`scripts/apply_firewall.sh`
Loads firewall rules and populates CIDR sets safely:
•	Validates template and placeholder
•	Resolves Conduit cgroup ID via systemd + `/sys/fs/cgroup`
•	Replaces only the `inet khajubridge` table (does not flush global ruleset)
•	Bulk-loads CIDR sets and prints counts

•	`scripts/update_region_cidrs.sh`
Fetches/updates CIDR allowlists and writes to:
•	`/etc/khajubridge/region_ipv4.cidr`
•	`/etc/khajubridge/region_ipv6.cidr`

――――――――――――――――――――――――――――――

🔍 Verification

Show active KhajuBridge rules:
sudo nft list chain inet khajubridge output

Confirm Conduit health:
sudo systemctl status conduit.service --no-pager
sudo journalctl -u conduit.service -n 20 --no-pager

――――――――――――――――――――――――――――――

📝 Credits

KhajuBridge is inspired by existing Windows-based firewall deployments for Psiphon Conduit and adapts the same core security model to Linux using nftables and systemd cgroups.
