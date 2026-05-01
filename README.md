## Conduit for Iran – KhajuBridge

## TL;DR: This is **not** a plug-and-play VPN firewall, and all changes require explicit operator action.

KhajuBridge is a Linux-native firewall layer and Conduit bridge for Iran, designed
to improve Psiphon Conduit reliability, bypass DPI, and optimize traffic under
Iranian network censorship.

It provides region-aware connection prioritization for Conduit by using nftables and
systemd cgroup scoping, allowing high-bandwidth TCP access while selectively enabling
UDP where it is most effective inside Iran.

KhajuBridge mirrors the behavior of existing Psiphon Conduit firewall deployments
without modifying Conduit itself.

KhajuBridge is a firewall wrapper (scripts + nftables rules), not a compiled build. You need:

- A Linux system that uses **systemd** and supports **nftables**
- Basic familiarity with the Linux command line (running scripts, reading output)
- **sudo/root** access (to load nftables rules and manage `/etc/khajubridge`)
- **Psiphon Conduit installed and running as a systemd service**
  - default expected unit name: `conduit.service`
- `curl` (used by `scripts/update_region_cidrs.sh` to fetch CIDR lists)

---

## Conduit Optimization for Iran

KhajuBridge is specifically designed to operate under Iranian network conditions,
where aggressive filtering, throttling, and DPI affect Conduit performance.

---

## 🚀 Overview

KhajuBridge provides a transparent, non-invasive way to apply region-based network
controls to Psiphon Conduit on Linux systems.

KhajuBridge improves Conduit behavior under Iranian network conditions but does not,
by itself, provide strict Iran-only exclusivity. Optional deployment layers can be
used to enforce stronger regional isolation when required.

Instead of patching or wrapping Conduit, KhajuBridge enforces policy entirely at the
firewall level. Rules are scoped specifically to the Conduit process using its
systemd cgroup, ensuring:

- No port-based assumptions
- No UID-based filtering
- No impact on other system traffic

The firewall can be safely applied, updated, or removed at any time.

---

## ⚙️ How It Works

KhajuBridge uses a three-stage model:

**1. Fetch Region CIDR Ranges**
A helper script downloads IPv4 and IPv6 CIDR ranges for Iran from multiple public
sources. CIDRs are written atomically — if a fetch fails or returns fewer than the
minimum expected entries, the existing file is preserved.

**2. Define Firewall Policy**
An nftables ruleset defines traffic handling for Conduit only:

- TCP from Conduit: allowed globally (mirrors Windows Psiphon behavior)
- UDP from Conduit: allowed only to Iran CIDRs
- Inbound to Conduit: allowed from Iran CIDRs only; all other UDP/TCP dropped
- All other system traffic: unaffected (policy accept)

**3. Apply Rules Safely**
A helper script:

- Dynamically resolves Conduit's systemd cgroup ID at runtime
- Injects it into the nftables template
- Replaces only the `inet khajubridge` table (never the global ruleset)
- Bulk-loads CIDR sets efficiently
- Writes `/etc/khajubridge/state.json` on success (timestamp, ruleset hash, CIDR counts)
- Can be safely re-run at any time

### Optional Web Console

KhajuBridge includes an optional, lightweight web console for local monitoring:

- Real assurance panel (live enforcement status, last apply time, ruleset hash)
- Conduit stats, peer country breakdown with Iran share %
- Action buttons: Apply Firewall, Update CIDRs, Restart Conduit
- LAN-only, stateless, environment-configured

See [`console/`](console/) for installation and configuration.

---

## ✨ Features

- **Region-Restricted UDP** — CIDR-based allowlists for precise geographic control
- **Global TCP Connectivity** — matches existing Windows Psiphon firewall behavior
- **Process-Scoped Filtering** — uses systemd cgroups instead of ports or UIDs
- **Dual-Stack Support** — full IPv4 and IPv6 support
- **High Performance** — nftables interval sets for efficient CIDR lookups
- **Non-Invasive** — does not modify, wrap, or patch Psiphon Conduit
- **Auto-Reapply on Restart** — systemd drop-in re-applies rules when Conduit restarts
- **Weekly CIDR Refresh** — optional systemd timer keeps Iran IP ranges up to date

---

## 🛠️ Requirements

- Linux system with nftables support
- systemd-based distribution (Debian 11/12 or compatible)
- Root or sudo privileges
- Psiphon Conduit running as a systemd service (`conduit.service`)

---

## ⚡ Quick Start (Manual)

After cloning, make scripts executable:

```bash
chmod +x scripts/*.sh
```

```bash
# 1. Install dependencies
sudo apt install nftables curl

# 2. Fetch Iran CIDR ranges
sudo ./scripts/update_region_cidrs.sh

# 3. Apply firewall rules
sudo ./scripts/apply_firewall.sh

# 4. (Optional) Enable weekly CIDR refresh
sudo cp systemd/khajubridge-cidr-refresh.service /etc/systemd/system/
sudo cp systemd/khajubridge-cidr-refresh.timer   /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now khajubridge-cidr-refresh.timer

# 5. (Optional) Auto-reapply on Conduit restart
sudo mkdir -p /etc/systemd/system/conduit.service.d
sudo cp systemd/conduit.service.d/khajubridge.conf \
  /etc/systemd/system/conduit.service.d/
sudo systemctl daemon-reload
```

---

## 🔒 Security & Operational Safety

⚠️ **Important:** This project interacts with system-level networking and firewall components.

- Review all changes carefully before applying them.
- Test updates in a non-production or isolated environment first.
- Ensure you have console or out-of-band access before applying firewall changes remotely.
- See [`docs/sudoers.d/khajubridge`](docs/sudoers.d/khajubridge) for the exact
  sudo rules required by the scripts and console.

Please do **not** include secrets, personal information, or live system data in issues or pull requests.  
See [`SECURITY.md`](SECURITY.md) for responsible disclosure guidelines.

---

## 🛡️ Safety & Modes

**Current Mode (Layer 1 — Default)**
- TCP: allowed globally
- UDP from Conduit: allowed only to Iran CIDRs
- Inbound to Conduit: allowed from Iran CIDRs; all other dropped
- All other system traffic: unaffected (policy accept)

**Layer 2 / Option A — Strict Iran-Only**
- Adds dedicated IP + SNAT for hard network-layer enforcement
- See `docs/OPTION_A_DEDICATED_IP.md`

**Notes**
- KhajuBridge only affects traffic scoped to the Conduit systemd cgroup.
- CIDR lists change over time; weekly automated refresh is recommended.
- Rules can be removed: `sudo nft delete table inet khajubridge`
- Always test firewall changes on non-critical systems first.

---

## 🌍 Deployment Options

### Layer 1 — Process-Scoped Traffic Shaping (Default)

The core KhajuBridge model. Safe, portable, non-invasive, and suitable for most
deployments. Improves Conduit reliability without strict regional exclusivity.

### Layer 2 — Network Identity Isolation (Option A)

Adds strict Iran-only enforcement via a dedicated IP and cgroup-scoped SNAT.
Intended for deployments where hard regional exclusivity is a requirement.

Deployed via `scripts/apply_option_a.sh`. See [`docs/OPTION_A_DEDICATED_IP.md`](docs/OPTION_A_DEDICATED_IP.md).

---

## 🧠 Design Notes

- **Outbound-only:** Conduit is outbound-only; output rules use `meta cgroup`.
- **Inbound filtering:** Inbound rules use `socket cgroupv2` to scope to Conduit's sockets.
- **Conntrack ordering:** `ct state established,related accept` runs before any drop
  rules to ensure Conduit's outbound TCP replies are not blocked.
- **No port filtering:** Conduit does not listen on a fixed port; filtering is not port-based.
- **No hardcoding:** cgroup IDs are resolved dynamically at apply time and injected
  into the nftables template.
- **State file:** `apply_firewall.sh` writes `/etc/khajubridge/state.json` on
  success, readable by the web console's assurance panel.
- **Option A SNAT** uses `meta cgroup` (not UID) for consistency and to avoid
  breakage when Conduit runs as root.

---

## 📁 Project Layout

```
nftables/conduit-region.nft                 nftables template (placeholders injected at runtime)
scripts/apply_firewall.sh                   core: resolve cgroup, load rules, populate CIDR sets
scripts/update_region_cidrs.sh              fetch Iran CIDRs atomically from multiple sources
scripts/apply_option_a.sh                   Option A: cgroup SNAT + dedicated IP inbound rules
systemd/conduit.service.d/khajubridge.conf  drop-in: re-apply firewall on Conduit restart
systemd/khajubridge-cidr-refresh.service    oneshot: update CIDRs + re-apply firewall
systemd/khajubridge-cidr-refresh.timer      weekly timer for the above
console/                                    optional Go web console
docs/OPTION_A_DEDICATED_IP.md              Layer 2 deployment guide
docs/sudoers.d/khajubridge                 example passwordless sudo rules
```

---

## 🔍 Verification

```bash
# Show active KhajuBridge rules
sudo nft list table inet khajubridge

# Confirm CIDR counts loaded
sudo nft list set inet khajubridge region_ipv4 | wc -l

# Check state file written by apply_firewall.sh
cat /etc/khajubridge/state.json

# Confirm Conduit health
sudo systemctl status conduit.service --no-pager
sudo journalctl -u conduit.service -n 20 --no-pager
```

---

## 📝 Credits

KhajuBridge is inspired by existing Windows-based firewall deployments for Psiphon
Conduit and adapts the same core security model to Linux using nftables and systemd
cgroups. Country breakdown in the web console uses the same peer data format as
[conduit-manager](https://github.com/SamNet-dev/conduit-manager).
