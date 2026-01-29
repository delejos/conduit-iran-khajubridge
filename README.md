KhajuBridge

A Linux Firewall Layer for Psiphon Conduit

KhajuBridge is a Linux-native firewall layer for Psiphon Conduit that enables region-restricted networking using nftables and systemd cgroup scoping.

It mirrors the behavior of existing Windows firewall deployments by allowing TCP globally while restricting UDP traffic to approved regions, without modifying Conduit itself.

🚀 Overview

KhajuBridge provides a transparent, non-invasive way to apply region-based network controls to Psiphon Conduit on Linux systems.

Instead of patching or wrapping Conduit, KhajuBridge enforces policy entirely at the firewall level. Rules are scoped specifically to the Conduit process using its systemd cgroup, ensuring:

No port-based assumptions

No UID-based filtering

No impact on other system traffic

The firewall can be safely applied, updated, or removed at any time.

⚙️ How It Works

KhajuBridge uses a three-stage model:

1. Fetch Region CIDR Ranges

A helper script downloads IPv4 and IPv6 CIDR ranges for one or more regions from public sources and stores them locally.

These CIDRs are treated as dynamic data and can be updated independently of firewall rules.

2. Define Firewall Policy

An nftables ruleset defines outbound traffic handling for Conduit only:

TCP traffic from Conduit is allowed globally

UDP traffic from Conduit is allowed only to configured regional CIDRs

All other UDP traffic from Conduit is dropped

All other system traffic is unaffected (policy accept)

No inbound rules are required; Conduit is outbound-only.

3. Apply Rules Safely

A helper script:

Dynamically resolves Conduit’s systemd cgroup ID

Injects it into the nftables template at runtime

Replaces only the KhajuBridge nftables table (never the global ruleset)

Bulk-loads CIDR sets efficiently

Can be safely re-run at any time

✨ Features

Region-Restricted UDP
CIDR-based allowlists for precise geographic control

Global TCP Connectivity
Matches existing Windows firewall behavior

Process-Scoped Filtering
Uses systemd cgroups instead of ports or UIDs

Dual-Stack Support
Full IPv4 and IPv6 support

High Performance
nftables interval sets for efficient lookups

Non-Invasive
Does not modify, wrap, or patch Psiphon Conduit

Distro-Friendly
Designed and tested on Debian-based systems

🛠️ Requirements

Linux system with nftables support

systemd-based distribution

Debian 11 / 12 or compatible

Root or sudo privileges

Psiphon Conduit installed and running as a systemd service

⚡ Quick Start (Manual)
1. Install dependencies
sudo apt install nftables curl

2. Fetch region CIDR ranges
sudo ./scripts/update_region_cidrs.sh

3. Apply firewall rules
sudo ./scripts/apply_firewall.sh

4. Verify
sudo nft list table inet khajubridge

🛡️ Safety & Modes
Current Mode (Normal)

TCP: allowed globally

UDP: restricted to configured regional CIDRs

Future Mode (Planned)

Strict mode where both TCP and UDP are region-restricted

Notes

KhajuBridge only affects traffic originating from the Conduit service

CIDR lists change over time; regular updates are recommended

Firewall rules can be removed by deleting the inet khajubridge table

Always test firewall changes on non-critical systems first

After cloning the repository, ensure scripts are executable:

chmod +x scripts/*.sh

🧠 Design Notes

Conduit is outbound-only; rules are applied in the OUTPUT hook

No port-based filtering is used

No global nftables state is modified

All cgroup IDs are resolved dynamically at runtime

No hardcoded values are required

📝 Credits

KhajuBridge is inspired by existing Windows-based firewall deployments for Psiphon Conduit and adapts the same core security model to Linux using nftables and systemd cgroups.
