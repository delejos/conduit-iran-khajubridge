# KhajuBridge: A Linux-based Firewall Layer for Psiphon Conduit

KhajuBridge is a Linux-based firewall layer for **Psiphon Conduit** that enables region-restricted access using `nftables`. 

It mirrors the behavior of existing Windows firewall implementations by allowing global TCP connectivity while restricting UDP traffic to a configurable region using CIDR-based filtering. Both IPv4 and IPv6 are fully supported.

---

## 🚀 Overview

KhajuBridge provides a simple and transparent way to apply region-based network restrictions to Psiphon Conduit on Linux systems. 

The project is designed as a lightweight wrapper around **nftables** and does not modify Conduit itself. All filtering is applied at the firewall level and can be safely enabled, updated, or disabled.

---

## ⚙️ How It Works

KhajuBridge follows a three-step model:

1.  **Fetch Region CIDR Ranges**: A script downloads IPv4 and IPv6 CIDR ranges for a specific region from public sources.
2.  **Define Firewall Rules**: An `nftables` ruleset defines traffic handling:
    * **TCP** traffic to Conduit ports is allowed **globally**.
    * **UDP** traffic to Conduit ports is **restricted** to the configured region.
    * All other traffic remains unaffected.
3.  **Apply Rules Safely**: A helper script loads the rules and populates nftables sets atomically, allowing for updates without interrupting existing connections.

---

## ✨ Features

* **Region-Restricted Access**: CIDR-based filtering for precise control.
* **Dual-Stack Support**: Supports both IPv4 and IPv6.
* **Performance**: Uses `nftables` sets for high-efficiency lookups.
* **Non-Invasive**: Does not modify or patch Psiphon Conduit.
* **Distro Friendly**: Designed for Debian-based Linux systems (Debian 11/12, etc.).


---

## 🛠️ Requirements
Linux system with nftables support.

Debian 11 / 12 or compatible distribution.

Root or sudo privileges.

Psiphon Conduit installed and running.

---


##⚡ Quick Start (Manual)

Install dependencies:

Bash
sudo apt install nftables curl
Fetch region CIDR ranges:

Bash
sudo ./scripts/update_region_cidrs.sh
Apply firewall rules:

Bash
sudo ./scripts/apply_firewall.sh
Verify rules:

Bash
sudo nft list table inet khajubridge

---

## 🛡️ Safety & Modes
Currently Supported:
Normal Mode: TCP traffic is allowed globally; UDP traffic is restricted to the configured region.

Future versions may introduce a Strict Mode where both TCP and UDP are region-restricted.

## Notes:
KhajuBridge only affects traffic matching the configured Conduit ports.

CIDR lists change over time; regular updates via the provided script are recommended.

Always test firewall rules on non-critical systems before production use.

---

## 📝 Credits
This project is inspired by existing Windows-based firewall implementations for Psiphon Conduit and adapts those core principles for the Linux ecosystem using nftables.
