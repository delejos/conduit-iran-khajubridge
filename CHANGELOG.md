# Changelog

## v2.0.0 — 2026-05-01

### Critical Fixes

- **nftables INPUT chain bug** — `ct state established,related accept` was placed
  after the socket-scoped drop rules, silently killing all of Conduit's inbound TCP
  replies. Rule order corrected; `ct state invalid drop` added.

### Reliability

- **`update_region_cidrs.sh`** — rewrote to use atomic temp-file writes: existing
  CIDR files are never overwritten unless the fetch succeeds and passes a minimum
  count check (100 IPv4 / 10 IPv6 ranges). Added a second fallback source
  (herrbischoff/country-ip-blocks) so a single provider outage does not leave the
  node without data.
- **`apply_firewall.sh`** — simplified CIDR count logic; removed the fragile
  `COUNT=N` stderr/stdout interleave pattern. Writes
  `/etc/khajubridge/state.json` on success (enforcement status, UTC timestamp,
  ruleset SHA-256 prefix, CIDR counts, service name, cgroup ID).
- **Systemd drop-in** (`systemd/conduit.service.d/khajubridge.conf`) — re-applies
  firewall automatically after every Conduit restart. The cgroup ID changes on each
  start, which previously left stale rules in place indefinitely.
- **Weekly CIDR refresh** (`systemd/khajubridge-cidr-refresh.{service,timer}`) —
  operator-installable systemd timer that fetches updated Iran CIDRs and re-applies
  firewall rules weekly (Sunday 03:00 UTC, ±1 h random jitter).

### Strictness

- **`scripts/apply_option_a.sh`** — replaces the manual commands in the Option A
  docs with a proper script. Uses `meta cgroup` SNAT (not UID-based, which breaks
  when Conduit runs as root) for consistency with the main firewall. Verifies the
  dedicated IP is assigned before making any changes and prints rollback commands.

### Operational

- **Console renamed** `khajunbgui` → `khajubridge` (binary, service file, Go
  module, directory). Fixes the missing-`c` typo and aligns with the repo name.
- **Console: real assurance panel** — live `nft list table inet khajubridge` check
  plus state.json read, replacing the all-UNKNOWN placeholder. Shows enforcement
  status, last apply timestamp, ruleset hash, and CIDR source/counts.
- **Console: response caching** — journalctl, peer stats, and assurance results are
  cached (8–30 s TTL per endpoint) to avoid hammering sudo on every poll interval.
- **Console: ANSI stripping** — log output is now stripped of escape sequences
  before being sent to the browser, preventing display corruption.
- **Console: dynamic status pill** — sidebar pill now polls `/status-pill` every
  10 s and reflects actual Conduit service state instead of being hardcoded green.
- **Console: action buttons** — Apply Firewall, Update CIDRs, and Restart Conduit
  buttons added with HTMX confirm dialogs; output rendered inline.
- **Console: peer country breakdown** — reads Conduit's
  `traffic_stats/cumulative_data` file (country|from_bytes|to_bytes format) and
  shows top-10 inbound/outbound peer countries with Iran share percentage. Path
  configurable via `TRAFFIC_STATS` env var.
- **`console/install.sh`** — now deploys firewall scripts and nftables template to
  `/opt/khajubridge/`, installs all systemd units and the conduit drop-in, and
  guards the existing `/etc/khajubridge/console.env` from silent overwrite.
- **`docs/sudoers.d/khajubridge`** — example `/etc/sudoers.d/` file documenting
  the exact passwordless sudo rules required by the console and scripts.
- **`console/env.example`** — added `STATE_FILE` and `TRAFFIC_STATS` variables.

---

## Previous (Initial Release)

- Introduced Linux-native firewall layer for Psiphon Conduit using nftables and
  systemd cgroup scoping.
- Region-aware UDP allowlist (Iran CIDRs) with global TCP pass-through.
- Initial optional web console for local monitoring.
- Documentation for Layer 1 (default) and Option A (dedicated IP) deployments.
