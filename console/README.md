# KhajuBridge Web Console

A lightweight, LAN-only web interface for KhajuBridge and Conduit.

Optional — not required to use KhajuBridge.

---

## Features

- **Real assurance panel** — live `nft` check + state file read shows actual
  enforcement status, last apply timestamp, ruleset hash, and CIDR counts
- **System overview** — uptime, CPU, RAM
- **Live Conduit stats** — active users, throughput, upload/download totals
- **Peer country breakdown** — top-10 inbound/outbound countries with Iran share %
  (reads Conduit's `traffic_stats/cumulative_data` if available)
- **Action buttons** — Apply Firewall, Update CIDRs, Restart Conduit (with confirm)
- **Live logs** — last N journalctl lines, ANSI-stripped
- **Dynamic status pill** — reflects actual Conduit service state
- Response caching (8–30 s TTL) so sudo is not called on every poll

---

## Install

```bash
cd console
sudo bash install.sh
```

The installer:
1. Builds the `khajubridge` binary (`go build`)
2. Installs it to `/usr/local/bin/khajubridge`
3. Deploys firewall scripts to `/opt/khajubridge/scripts/`
4. Deploys the nftables template to `/opt/khajubridge/nftables/`
5. Installs all systemd units (console, CIDR refresh timer, conduit drop-in)
6. Creates `/etc/khajubridge/console.env` from `env.example` — **only if it does not already exist**

After installing:

```bash
# 1. Review and edit config
sudo nano /etc/khajubridge/console.env

# 2. Fetch Iran CIDRs
sudo /opt/khajubridge/scripts/update_region_cidrs.sh

# 3. Apply firewall
sudo /opt/khajubridge/scripts/apply_firewall.sh

# 4. Enable weekly CIDR refresh
sudo systemctl enable --now khajubridge-cidr-refresh.timer

# 5. Start the console
sudo systemctl enable --now khajubridge
```

---

## Configuration

`/etc/khajubridge/console.env` — all values are optional with the defaults shown:

```env
# Address the web UI listens on
LISTEN_ADDR=:8080

# Path to deployed KhajuBridge scripts
KHAJUBRIDGE_SCRIPTS=/opt/khajubridge/scripts

# systemd unit name for Conduit
CONDUIT_UNIT=conduit.service

# How many journal lines to read for logs and stats
JOURNAL_TAIL=200

# Allowed client CIDRs for UI access (LAN-only by default)
ALLOW_CIDRS=127.0.0.1/32,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16

# Path to KhajuBridge state file (written by apply_firewall.sh)
STATE_FILE=/etc/khajubridge/state.json

# Path to Conduit traffic stats (for peer country breakdown)
TRAFFIC_STATS=/opt/conduit/traffic_stats/cumulative_data
```

---

## Sudo rules

The console calls `sudo -n` for `journalctl`, `nft`, and the KhajuBridge scripts.
Install the example sudoers file:

```bash
sudo cp ../docs/sudoers.d/khajubridge /etc/sudoers.d/khajubridge
sudo chmod 440 /etc/sudoers.d/khajubridge
```

Review it first — it grants passwordless access scoped to the exact commands needed.

---

## Design principles

- Single static Go binary, no npm, no database, no external dependencies
- Environment-based configuration
- LAN-only access by default
- All command output HTML-escaped and ANSI-stripped before display
- Stateless between restarts
- Easy to audit and easy to remove
