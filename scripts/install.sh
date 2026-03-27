#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────
#  LOKA Installer
#
#  Online:  curl -fsSL https://vyprai.github.io/loka/install.sh | bash
#  Local:   ./install.sh --local /path/to/release/dir
#
#  Environment variables:
#    LOKA_VERSION       Release version (default: latest)
#    LOKA_INSTALL_DIR   Binary install dir (default: /usr/local/bin)
#    LOKA_DATA_DIR      Data directory (default: /var/loka)
#    LOKA_LOCAL_DIR     Path to extracted release dir (skip download)
#    CH_VERSION         Cloud Hypervisor version (default: v44.0)
# ──────────────────────────────────────────────────────────
set -euo pipefail

VERSION="${LOKA_VERSION:-latest}"
INSTALL_DIR="${LOKA_INSTALL_DIR:-/usr/local/bin}"
DATA_DIR="${LOKA_DATA_DIR:-/var/loka}"
CH_VERSION="${CH_VERSION:-v44.0}"
LOCAL_DIR="${LOKA_LOCAL_DIR:-}"

# Parse --local flag.
while [ $# -gt 0 ]; do
  case "$1" in
    --local)
      LOCAL_DIR="$2"
      shift 2
      ;;
    --local=*)
      LOCAL_DIR="${1#--local=}"
      shift
      ;;
    *)
      shift
      ;;
  esac
done

# ── Helpers ───────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

info()  { echo -e "${CYAN}==>${NC} $*"; }
ok()    { echo -e "${GREEN}  ✓${NC} $*"; }
warn()  { echo -e "${YELLOW}  !${NC} $*"; }
fail()  { echo -e "${RED}  ✗ $*${NC}"; exit 1; }

need_cmd() {
  if ! command -v "$1" &>/dev/null; then
    fail "$1 is required but not installed"
  fi
}

# Determine how to run privileged commands.
SUDO=""
setup_sudo() {
  if [ "$(id -u)" -eq 0 ]; then
    SUDO=""
    return
  fi

  local needs_sudo=false
  if ! { [ -w "$INSTALL_DIR" ] || mkdir -p "$INSTALL_DIR" 2>/dev/null; }; then
    needs_sudo=true
  fi
  for bin in loka lokad loka-supervisor; do
    if [ -f "${INSTALL_DIR}/$bin" ] && [ ! -w "${INSTALL_DIR}/$bin" ]; then
      needs_sudo=true
      break
    fi
  done
  if [ "$needs_sudo" = false ]; then
    SUDO=""
    return
  fi

  if command -v sudo &>/dev/null; then
    info "This installer needs elevated privileges to install binaries to ${INSTALL_DIR}."
    if ! sudo -v 2>/dev/null; then
      fail "sudo access required. Run with sudo or as root."
    fi
    SUDO="sudo"
    (while true; do sudo -n true 2>/dev/null || sudo -v; sleep 50; done) &
    SUDO_KEEPALIVE_PID=$!
    trap 'kill $SUDO_KEEPALIVE_PID 2>/dev/null; wait $SUDO_KEEPALIVE_PID 2>/dev/null' EXIT
  else
    fail "sudo is required but not found. Run as root instead."
  fi
}

# ── Detect platform ──────────────────────────────────────

detect_platform() {
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)

  case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) fail "Unsupported architecture: $ARCH" ;;
  esac

  case "$OS" in
    linux|darwin) ;;
    *) fail "Unsupported OS: $OS" ;;
  esac

  PLATFORM="${OS}-${ARCH}"
}

# ── Download LOKA binaries ───────────────────────────────

download_and_install() {
  local pkg_dir=""
  local tmp=""

  # If --local was given, use that directory directly (no download).
  if [ -n "$LOCAL_DIR" ]; then
    if [ ! -d "$LOCAL_DIR" ]; then
      fail "Local directory not found: $LOCAL_DIR"
    fi
    pkg_dir="$LOCAL_DIR"
    info "Installing from local directory: $LOCAL_DIR"
  else
    local platform="${OS}-${ARCH}"
    local url
    if [ "$VERSION" = "latest" ]; then
      url="https://github.com/vyprai/loka/releases/latest/download/loka-${platform}.tar.gz"
    else
      url="https://github.com/vyprai/loka/releases/download/${VERSION}/loka-${platform}.tar.gz"
    fi

    tmp=$(mktemp -d)

    info "Downloading loka-${platform}.tar.gz ..."

    if curl -fsSL "$url" -o "$tmp/loka.tar.gz" 2>/dev/null; then
      tar -xzf "$tmp/loka.tar.gz" -C "$tmp"
      # Find the extracted directory (loka-<platform>/).
      pkg_dir=$(find "$tmp" -maxdepth 1 -type d -name "loka-*" | head -1)
      if [ -z "$pkg_dir" ]; then
        pkg_dir="$tmp"
      fi
    else
      warn "Pre-built release not found. Building from source..."
      need_cmd go
      need_cmd git

      git clone --depth 1 https://github.com/vyprai/loka "$tmp/loka-src" 2>/dev/null
      cd "$tmp/loka-src"
      GOOS=$OS GOARCH=$ARCH go build -trimpath -ldflags "-s -w" -o "$tmp/lokad" ./cmd/lokad
      GOOS=$OS GOARCH=$ARCH go build -trimpath -ldflags "-s -w" -o "$tmp/loka-supervisor" ./cmd/loka-supervisor
      GOOS=$OS GOARCH=$ARCH go build -trimpath -ldflags "-s -w" -o "$tmp/loka" ./cmd/loka
      cd - >/dev/null
      pkg_dir="$tmp"
    fi
  fi

  # Install binaries.
  info "Installing binaries to ${INSTALL_DIR}"
  for bin in lokad loka-supervisor loka; do
    if [ -f "$pkg_dir/$bin" ]; then
      $SUDO install -m 755 "$pkg_dir/$bin" "${INSTALL_DIR}/$bin"
      ok "$bin → ${INSTALL_DIR}/$bin"
    fi
  done

  # Install VM assets (kernel + initramfs) from the release package.
  local vm_dir="$HOME/.loka/vm"
  mkdir -p "$vm_dir"
  if [ -d "$pkg_dir/vm" ]; then
    info "Installing VM assets"
    if [ -f "$pkg_dir/vm/vmlinux-lokavm" ]; then
      cp "$pkg_dir/vm/vmlinux-lokavm" "$vm_dir/vmlinux-lokavm"
      ok "kernel → $vm_dir/vmlinux-lokavm"
    fi
    if [ -f "$pkg_dir/vm/initramfs.cpio.gz" ]; then
      cp "$pkg_dir/vm/initramfs.cpio.gz" "$vm_dir/initramfs.cpio.gz"
      ok "initramfs → $vm_dir/initramfs.cpio.gz"
    fi
  else
    warn "Release package does not contain VM assets"
    warn "Build from source: make kernel && make initramfs"
  fi

  if [ -n "$tmp" ]; then rm -rf "$tmp"; fi
}

# ── Install Cloud Hypervisor (Linux only) ────────────────

install_cloud_hypervisor() {
  if [ "$OS" != "linux" ]; then
    return
  fi

  info "Installing Cloud Hypervisor ${CH_VERSION}"

  local ch_arch
  ch_arch=$(uname -m)  # x86_64 or aarch64

  local tmp
  tmp=$(mktemp -d)

  local ch_url="https://github.com/cloud-hypervisor/cloud-hypervisor/releases/download/${CH_VERSION}/cloud-hypervisor-static-${ch_arch}"

  if curl -fsSL "$ch_url" -o "$tmp/cloud-hypervisor" 2>/dev/null; then
    chmod +x "$tmp/cloud-hypervisor"
    $SUDO install -m 755 "$tmp/cloud-hypervisor" "${INSTALL_DIR}/cloud-hypervisor"
    ok "cloud-hypervisor → ${INSTALL_DIR}/cloud-hypervisor"
  else
    warn "Failed to download Cloud Hypervisor. Install manually:"
    warn "  https://github.com/cloud-hypervisor/cloud-hypervisor/releases"
  fi

  rm -rf "$tmp"
}

    # VM assets are included in the release tar.gz and installed by download_and_install().

# ── Install Linux dependencies ───────────────────────────

install_linux_deps() {
  info "Checking dependencies..."

  local pkg=""
  if command -v apt-get &>/dev/null; then
    pkg="apt"
  elif command -v dnf &>/dev/null; then
    pkg="dnf"
  elif command -v yum &>/dev/null; then
    pkg="yum"
  elif command -v apk &>/dev/null; then
    pkg="apk"
  fi

  local missing=()

  if ! command -v iptables &>/dev/null; then
    missing+=("iptables")
  else
    ok "iptables"
  fi

  if ! command -v ip &>/dev/null; then
    missing+=("iproute2")
  else
    ok "iproute2"
  fi

  # KVM.
  if [ ! -e /dev/kvm ]; then
    warn "/dev/kvm not found — Cloud Hypervisor requires KVM"
    warn "Enable KVM or run inside a VM with nested virtualization"
    $SUDO modprobe kvm 2>/dev/null || true
    $SUDO modprobe kvm_intel 2>/dev/null || $SUDO modprobe kvm_amd 2>/dev/null || true
    if [ -e /dev/kvm ]; then
      ok "KVM loaded"
    fi
  else
    ok "KVM available"
  fi

  # Install missing packages.
  if [ ${#missing[@]} -gt 0 ]; then
    info "Installing missing packages: ${missing[*]}"
    case "$pkg" in
      apt)
        $SUDO apt-get update -qq
        $SUDO apt-get install -y -qq "${missing[@]}"
        ;;
      dnf)
        $SUDO dnf install -y -q "${missing[@]}"
        ;;
      yum)
        $SUDO yum install -y -q "${missing[@]}"
        ;;
      apk)
        $SUDO apk add --quiet "${missing[@]}"
        ;;
      *)
        warn "Unknown package manager — install manually: ${missing[*]}"
        ;;
    esac
    for dep in "${missing[@]}"; do
      ok "$dep installed"
    done
  fi

  # Ensure current user can access /dev/kvm.
  if [ -e /dev/kvm ] && [ ! -w /dev/kvm ]; then
    info "Adding current user to kvm group..."
    $SUDO usermod -aG kvm "$(whoami)" 2>/dev/null || true
    $SUDO chmod 666 /dev/kvm 2>/dev/null || true
    ok "KVM access granted"
  fi
}

# ── Linux install ────────────────────────────────────────

install_linux() {
  install_linux_deps
  echo ""

  download_and_install
  echo ""

  install_cloud_hypervisor
  echo ""

  # Data dirs.
  info "Creating data directories"
  $SUDO mkdir -p "${DATA_DIR}"/{artifacts,worker,raft,tls}
  $SUDO chmod 755 "${DATA_DIR}"
  $SUDO chmod 700 "${DATA_DIR}/tls"
  ok "${DATA_DIR}"

  # Default config.
  local config_dir="/etc/loka"
  $SUDO mkdir -p "$config_dir"
  if [ ! -f "$config_dir/controlplane.yaml" ]; then
    info "Writing default config"
    $SUDO tee "$config_dir/controlplane.yaml" >/dev/null << YAML
role: all
mode: single
listen_addr: ":6840"
grpc_addr: ":6841"
database:
  driver: sqlite
  dsn: "${DATA_DIR}/loka.db"
coordinator:
  type: local
objectstore:
  type: local
  path: "${DATA_DIR}/artifacts"
scheduler:
  strategy: spread
YAML
    ok "$config_dir/controlplane.yaml"
  else
    ok "Config already exists, skipping"
  fi

  # Shell completion.
  if command -v loka &>/dev/null; then
    if [ -d /etc/bash_completion.d ]; then
      loka completion bash | $SUDO tee /etc/bash_completion.d/loka >/dev/null 2>&1 && ok "bash completion"
    fi
    if [ -d /usr/local/share/zsh/site-functions ]; then
      loka completion zsh | $SUDO tee /usr/local/share/zsh/site-functions/_loka >/dev/null 2>&1 && ok "zsh completion"
    fi
  fi

  echo ""
  echo -e "${GREEN}${BOLD}  LOKA installed successfully!${NC}"
  echo ""
  echo -e "  Get started:"
  echo -e "    ${CYAN}lokad${NC}                      Start the server"
  echo -e "    ${CYAN}loka session create${NC}        Create a session"
  echo -e "    ${CYAN}loka exec <id> -- echo hi${NC}  Run a command"
  echo ""
}

# ── macOS install ────────────────────────────────────────

install_macos() {
  info "Installing LOKA for macOS (Apple Virtualization Framework)"

  echo ""
  download_and_install

  # Sign lokad with VZ entitlement.
  if [ -f "${INSTALL_DIR}/lokad" ]; then
    echo ""
    info "Signing lokad with virtualization entitlement"
    local ent_file
    ent_file=$(mktemp)
    cat > "$ent_file" << 'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>com.apple.security.virtualization</key><true/></dict></plist>
PLIST
    codesign --entitlements "$ent_file" --force -s - "${INSTALL_DIR}/lokad" 2>/dev/null && ok "lokad signed" || warn "codesign failed — VZ may not work"
    rm -f "$ent_file"
  fi

  # Shell completion.
  if command -v loka &>/dev/null; then
    if [ -d /usr/local/share/zsh/site-functions ]; then
      loka completion zsh | $SUDO tee /usr/local/share/zsh/site-functions/_loka >/dev/null 2>&1 && ok "zsh completion"
    fi
    local bash_comp_dir
    bash_comp_dir="$(brew --prefix 2>/dev/null)/etc/bash_completion.d" 2>/dev/null || true
    if [ -d "$bash_comp_dir" ]; then
      loka completion bash | tee "$bash_comp_dir/loka" >/dev/null 2>&1 && ok "bash completion"
    fi
  fi

  echo ""
  echo -e "${GREEN}${BOLD}  LOKA installed successfully!${NC}"
  echo ""
  echo -e "  Get started:"
  echo -e "    ${CYAN}lokad${NC}                      Start the server"
  echo -e "    ${CYAN}loka session create${NC}        Create a session"
  echo -e "    ${CYAN}loka exec <id> -- echo hi${NC}  Run a command"
  echo ""
}

# ── Uninstall previous installation ─────────────────────

uninstall_previous() {
  local found=false

  for bin in loka lokad loka-supervisor; do
    if [ -f "${INSTALL_DIR}/$bin" ]; then
      found=true
      break
    fi
  done

  if [ "$found" = false ]; then
    return
  fi

  echo ""
  info "Removing previous installation"

  # Stop running lokad.
  if pgrep -x lokad &>/dev/null; then
    echo -n "  Stopping lokad..."
    loka setup down 2>/dev/null || $SUDO pkill -x lokad 2>/dev/null || true
    sleep 1
    echo " done"
  fi

  # Remove binaries (including legacy ones).
  for bin in loka lokad loka-worker loka-supervisor lokavm firecracker; do
    [ -f "${INSTALL_DIR}/$bin" ] && $SUDO rm -f "${INSTALL_DIR}/$bin"
  done
  ok "Old binaries removed"

  [ -d "$DATA_DIR" ] && $SUDO rm -rf "$DATA_DIR" 2>/dev/null || true
  $SUDO rm -rf /tmp/loka-data 2>/dev/null || true
  [ "$OS" = "linux" ] && $SUDO rm -rf /etc/loka 2>/dev/null || true
  rm -rf "$HOME/.loka" 2>/dev/null || true
  $SUDO rm -f /etc/bash_completion.d/loka /usr/local/share/zsh/site-functions/_loka 2>/dev/null || true

  ok "Clean"
}

# ── Main ─────────────────────────────────────────────────

main() {
  detect_platform

  echo ""
  echo -e "${BOLD}  LOKA Installer${NC} — ${PLATFORM}"
  echo ""

  need_cmd curl
  need_cmd tar

  setup_sudo
  uninstall_previous

  case "$OS" in
    linux)  install_linux ;;
    darwin) install_macos ;;
  esac
}

main "$@"
