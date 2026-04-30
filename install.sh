#!/usr/bin/env sh
# Stancil install script
# Usage: curl -fsSL https://raw.githubusercontent.com/stancil-gen/stancil/main/install.sh | sh
#
# Supports: macOS (arm64, amd64), Linux (amd64, arm64)
# Installs to: /usr/local/bin (with sudo fallback) or ~/.local/bin

set -e

REPO="stancil-gen/stancil"
BINARY="stencil"

# ─── Colours ──────────────────────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
RESET='\033[0m'

info()    { printf "${BOLD}%s${RESET}\n" "$1"; }
success() { printf "${GREEN}✓${RESET} %s\n" "$1"; }
warn()    { printf "${YELLOW}! %s${RESET}\n" "$1"; }
die()     { printf "${RED}error: %s${RESET}\n" "$1" >&2; exit 1; }

# ─── Detect OS and arch ───────────────────────────────────────────────────────

detect_os() {
    case "$(uname -s)" in
        Darwin) echo "Darwin" ;;
        Linux)  echo "Linux"  ;;
        *)      die "Unsupported OS: $(uname -s). Only macOS and Linux are supported." ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64)         echo "x86_64" ;;
        amd64)          echo "x86_64" ;;
        arm64|aarch64)  echo "arm64"  ;;
        *)              die "Unsupported architecture: $(uname -m)." ;;
    esac
}

# ─── Fetch latest release tag ─────────────────────────────────────────────────

latest_version() {
    if [ -n "${STENCIL_VERSION:-}" ]; then
        echo "$STENCIL_VERSION"
        return
    fi

    if command -v curl >/dev/null 2>&1; then
        VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
            | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
    elif command -v wget >/dev/null 2>&1; then
        VERSION=$(wget -qO- "https://api.github.com/repos/${REPO}/releases/latest" \
            | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')
    else
        die "curl or wget is required to download stencil."
    fi

    [ -n "$VERSION" ] || die "Could not determine latest release version."
    echo "$VERSION"
}

# ─── Download ─────────────────────────────────────────────────────────────────

download() {
    URL="$1"
    DEST="$2"

    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$URL" -o "$DEST"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$DEST" "$URL"
    else
        die "curl or wget is required."
    fi
}

# ─── Verify checksum ──────────────────────────────────────────────────────────

verify_checksum() {
    ARCHIVE="$1"
    CHECKSUMS="$2"
    ARCHIVE_NAME="$(basename "$ARCHIVE")"

    EXPECTED=$(grep "$ARCHIVE_NAME" "$CHECKSUMS" | awk '{print $1}')
    [ -n "$EXPECTED" ] || die "Could not find checksum for $ARCHIVE_NAME."

    if command -v sha256sum >/dev/null 2>&1; then
        ACTUAL=$(sha256sum "$ARCHIVE" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        ACTUAL=$(shasum -a 256 "$ARCHIVE" | awk '{print $1}')
    else
        warn "sha256sum/shasum not found — skipping checksum verification."
        return
    fi

    [ "$ACTUAL" = "$EXPECTED" ] || die "Checksum mismatch for $ARCHIVE_NAME. Expected $EXPECTED, got $ACTUAL."
    success "Checksum verified"
}

# ─── Install location ─────────────────────────────────────────────────────────

pick_install_dir() {
    # Prefer /usr/local/bin — check if writable without sudo first
    if [ -w "/usr/local/bin" ]; then
        echo "/usr/local/bin"
        return
    fi

    # Try with sudo
    if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
        echo "/usr/local/bin"
        return
    fi

    # Fall back to ~/.local/bin
    echo "${HOME}/.local/bin"
}

install_binary() {
    SRC="$1"
    INSTALL_DIR="$2"

    mkdir -p "$INSTALL_DIR"
    chmod +x "$SRC"

    if [ -w "$INSTALL_DIR" ]; then
        mv "$SRC" "$INSTALL_DIR/$BINARY"
    elif command -v sudo >/dev/null 2>&1; then
        sudo mv "$SRC" "$INSTALL_DIR/$BINARY"
    else
        die "Cannot write to $INSTALL_DIR. Run as root or install manually."
    fi
}

check_path() {
    INSTALL_DIR="$1"
    case ":$PATH:" in
        *":$INSTALL_DIR:"*) ;;
        *)
            warn "$INSTALL_DIR is not in your PATH."
            warn "Add this to your shell profile (~/.zshrc or ~/.bashrc):"
            warn "  export PATH=\"$INSTALL_DIR:\$PATH\""
            ;;
    esac
}

# ─── Main ─────────────────────────────────────────────────────────────────────

main() {
    info "Installing stencil..."

    OS=$(detect_os)
    ARCH=$(detect_arch)
    VERSION=$(latest_version)

    ARCHIVE_NAME="stencil_${OS}_${ARCH}.tar.gz"
    BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
    DOWNLOAD_URL="${BASE_URL}/${ARCHIVE_NAME}"
    CHECKSUMS_URL="${BASE_URL}/checksums.txt"

    info "Version : $VERSION"
    info "Platform: $OS/$ARCH"

    # Work in a temp directory
    TMP=$(mktemp -d)
    trap 'rm -rf "$TMP"' EXIT

    info "Downloading $ARCHIVE_NAME..."
    download "$DOWNLOAD_URL"  "$TMP/$ARCHIVE_NAME"
    download "$CHECKSUMS_URL" "$TMP/checksums.txt"

    verify_checksum "$TMP/$ARCHIVE_NAME" "$TMP/checksums.txt"

    info "Extracting..."
    tar -xzf "$TMP/$ARCHIVE_NAME" -C "$TMP"

    INSTALL_DIR=$(pick_install_dir)
    install_binary "$TMP/$BINARY" "$INSTALL_DIR"

    success "stencil $VERSION installed to $INSTALL_DIR/$BINARY"

    check_path "$INSTALL_DIR"

    # Confirm it works
    if command -v stencil >/dev/null 2>&1; then
        printf "\n"
        stencil version
    fi
}

main
