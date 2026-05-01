#!/usr/bin/env bash
set -euo pipefail

# KhajuBridge — top-level installer
# Installs nftables + curl, deploys scripts, and sets up systemd units.
# Run as root or with sudo.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPTS_DEST="/opt/khajubridge/scripts"
NFT_DEST="/opt/khajubridge/nftables"
CIDR_DIR="/etc/khajubridge"

# ── Colour helpers ─────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
ok()   { echo -e "${GREEN}[✓]${NC} $*"; }
info() { echo -e "${BLUE}[*]${NC} $*"; }
warn() { echo -e "${YELLOW}[!]${NC} $*"; }
err()  { echo -e "${RED}[✗]${NC} $*" >&2; }

# ── Root check ─────────────────────────────────────────────────────────────────
if [[ "$EUID" -ne 0 ]]; then
  err "This script must be run as root (use sudo)."
  exit 1
fi

# ── OS detection ───────────────────────────────────────────────────────────────
OS="unknown"; PKG_MGR="unknown"; HAS_SYSTEMD=false

if [[ -f /etc/os-release ]]; then
  # shellcheck source=/dev/null
  . /etc/os-release
  OS="${ID:-unknown}"
fi

case "$OS" in
  ubuntu|debian|linuxmint|pop|elementary|kali|raspbian)
    PKG_MGR="apt" ;;
  rhel|centos|fedora|rocky|almalinux|oracle|amazon|amzn)
    command -v dnf &>/dev/null && PKG_MGR="dnf" || PKG_MGR="yum" ;;
  arch|manjaro|endeavouros|garuda)
    PKG_MGR="pacman" ;;
  opensuse|opensuse-leap|opensuse-tumbleweed|sles)
    PKG_MGR="zypper" ;;
  alpine)
    PKG_MGR="apk" ;;
  *)
    warn "Unrecognised distro '$OS'. Skipping package installation — install nftables and curl manually." ;;
esac

if command -v systemctl &>/dev/null && [[ -d /run/systemd/system ]]; then
  HAS_SYSTEMD=true
fi

info "Detected OS: $OS  (package manager: $PKG_MGR, systemd: $HAS_SYSTEMD)"

# ── Install dependencies ───────────────────────────────────────────────────────
install_pkg() {
  local pkg="$1"
  info "Installing $pkg..."
  case "$PKG_MGR" in
    apt)
      # Wait for dpkg lock if held by unattended-upgrades
      local tries=0
      while fuser /var/lib/dpkg/lock-frontend &>/dev/null 2>&1; do
        [[ $tries -eq 0 ]] && warn "Waiting for dpkg lock..."
        tries=$((tries+1)); [[ $tries -ge 30 ]] && { err "dpkg lock held too long"; return 1; }
        sleep 2
      done
      DEBIAN_FRONTEND=noninteractive apt-get install -y -q "$pkg"
      ;;
    dnf)  dnf install -y -q "$pkg" ;;
    yum)  yum install -y -q "$pkg" ;;
    pacman) pacman -Sy --noconfirm "$pkg" ;;
    zypper) zypper install -y "$pkg" ;;
    apk)  apk add --quiet "$pkg" ;;
    *)    warn "Cannot auto-install $pkg — install it manually." ;;
  esac
}

# nftables package name varies by distro
NFT_PKG="nftables"
[[ "$OS" == "alpine" ]] && NFT_PKG="nftables"
[[ "$PKG_MGR" == "pacman" ]] && NFT_PKG="nftables"

for dep in "$NFT_PKG" curl; do
  if ! command -v "${dep%%tables}" &>/dev/null && ! command -v nft &>/dev/null; then
    install_pkg "$dep" && ok "$dep installed" || warn "Could not install $dep"
  fi
done
command -v nft   &>/dev/null && ok "nftables present" || warn "nft not found — install nftables manually"
command -v curl  &>/dev/null && ok "curl present"     || warn "curl not found — install curl manually"

# ── Enable nftables service ────────────────────────────────────────────────────
if [[ "$HAS_SYSTEMD" == true ]]; then
  systemctl enable nftables 2>/dev/null && ok "nftables service enabled" || true
fi

# ── Deploy scripts ─────────────────────────────────────────────────────────────
info "Deploying scripts to $SCRIPTS_DEST..."
mkdir -p "$SCRIPTS_DEST" "$NFT_DEST" "$CIDR_DIR"

install -m 0755 "$REPO_ROOT/scripts/apply_firewall.sh"      "$SCRIPTS_DEST/apply_firewall.sh"
install -m 0755 "$REPO_ROOT/scripts/update_region_cidrs.sh" "$SCRIPTS_DEST/update_region_cidrs.sh"
install -m 0755 "$REPO_ROOT/scripts/apply_option_a.sh"      "$SCRIPTS_DEST/apply_option_a.sh"
install -m 0644 "$REPO_ROOT/nftables/conduit-region.nft"    "$NFT_DEST/conduit-region.nft"
ok "Scripts and nftables template deployed"

# ── Systemd units ──────────────────────────────────────────────────────────────
if [[ "$HAS_SYSTEMD" == true ]]; then
  info "Installing systemd units..."

  install -m 0644 "$REPO_ROOT/systemd/khajubridge-cidr-refresh.service" \
    /etc/systemd/system/khajubridge-cidr-refresh.service
  install -m 0644 "$REPO_ROOT/systemd/khajubridge-cidr-refresh.timer" \
    /etc/systemd/system/khajubridge-cidr-refresh.timer

  mkdir -p /etc/systemd/system/conduit.service.d
  install -m 0644 "$REPO_ROOT/systemd/conduit.service.d/khajubridge.conf" \
    /etc/systemd/system/conduit.service.d/khajubridge.conf

  systemctl daemon-reload
  ok "Systemd units installed"
else
  warn "systemd not detected — skipping unit installation. Set up periodic CIDR refresh manually."
fi

# ── Sudoers ────────────────────────────────────────────────────────────────────
if [[ ! -f /etc/sudoers.d/khajubridge ]]; then
  info "Installing sudoers rules..."
  install -m 0440 "$REPO_ROOT/docs/sudoers.d/khajubridge" /etc/sudoers.d/khajubridge
  ok "Sudoers rules installed (/etc/sudoers.d/khajubridge)"
  warn "Review /etc/sudoers.d/khajubridge — update the %khajubridge group to match your setup."
else
  info "Sudoers file already exists — skipping (not overwriting)."
fi

# ── Done ───────────────────────────────────────────────────────────────────────
echo ""
ok "KhajuBridge installed."
echo ""
echo "Next steps:"
echo "  1. Fetch Iran CIDRs:        sudo $SCRIPTS_DEST/update_region_cidrs.sh"
echo "  2. Apply firewall:          sudo $SCRIPTS_DEST/apply_firewall.sh"
if [[ "$HAS_SYSTEMD" == true ]]; then
echo "  3. Enable weekly refresh:   sudo systemctl enable --now khajubridge-cidr-refresh.timer"
fi
echo "  4. (Optional) Web console:  cd console && sudo bash install.sh"
echo "  5. (Optional) Option A:     sudo DEDICATED_IP=<ip> INTERFACE=<iface> $SCRIPTS_DEST/apply_option_a.sh"
echo ""
echo "  Verify:  sudo nft list table inet khajubridge"
echo "  Sudoers: review /etc/sudoers.d/khajubridge"
