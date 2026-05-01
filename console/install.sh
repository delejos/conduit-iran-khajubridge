#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$ROOT_DIR/.." && pwd)"
SCRIPTS_DEST="/opt/khajubridge/scripts"
NFT_DEST="/opt/khajubridge/nftables"
BINARY_DEST="/usr/local/bin/khajubridge"
SERVICE_DEST="/etc/systemd/system/khajubridge.service"
ENV_FILE="/etc/khajubridge/console.env"

echo "[*] Building KhajuBridge console..."
cd "$ROOT_DIR/cmd/khajubridge"
go build -o khajubridge .

echo "[*] Installing binary to $BINARY_DEST..."
sudo install -m 0755 khajubridge "$BINARY_DEST"

echo "[*] Installing firewall scripts to $SCRIPTS_DEST..."
sudo mkdir -p "$SCRIPTS_DEST"
sudo install -m 0755 "$REPO_ROOT/scripts/apply_firewall.sh"    "$SCRIPTS_DEST/apply_firewall.sh"
sudo install -m 0755 "$REPO_ROOT/scripts/update_region_cidrs.sh" "$SCRIPTS_DEST/update_region_cidrs.sh"

echo "[*] Installing nftables template to $NFT_DEST..."
sudo mkdir -p "$NFT_DEST"
sudo install -m 0644 "$REPO_ROOT/nftables/conduit-region.nft" "$NFT_DEST/conduit-region.nft"

echo "[*] Installing systemd units..."
sudo install -m 0644 "$ROOT_DIR/khajubridge.service" "$SERVICE_DEST"
sudo install -m 0644 "$REPO_ROOT/systemd/khajubridge-cidr-refresh.service" \
  /etc/systemd/system/khajubridge-cidr-refresh.service
sudo install -m 0644 "$REPO_ROOT/systemd/khajubridge-cidr-refresh.timer" \
  /etc/systemd/system/khajubridge-cidr-refresh.timer

echo "[*] Installing conduit.service drop-in..."
sudo mkdir -p /etc/systemd/system/conduit.service.d
sudo install -m 0644 "$REPO_ROOT/systemd/conduit.service.d/khajubridge.conf" \
  /etc/systemd/system/conduit.service.d/khajubridge.conf

echo "[*] Installing config..."
sudo mkdir -p /etc/khajubridge
if [[ -f "$ENV_FILE" ]]; then
  echo "    Config already exists at $ENV_FILE — skipping (not overwriting)."
else
  sudo install -m 0640 "$ROOT_DIR/env.example" "$ENV_FILE"
  echo "    Written: $ENV_FILE"
fi

echo "[*] Reloading systemd..."
sudo systemctl daemon-reload

echo ""
echo "[✓] Installed."
echo ""
echo "Next steps:"
echo "  1. Edit config:   sudo nano $ENV_FILE"
echo "  2. Fetch CIDRs:   sudo $SCRIPTS_DEST/update_region_cidrs.sh"
echo "  3. Apply firewall: sudo $SCRIPTS_DEST/apply_firewall.sh"
echo "  4. Enable timer:  sudo systemctl enable --now khajubridge-cidr-refresh.timer"
echo "  5. Enable console: sudo systemctl enable --now khajubridge"
echo "  6. See sudoers:   docs/sudoers.d/khajubridge"
