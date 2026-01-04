#!/bin/sh
# Thank you so much for using Orla!
# This script installs Orla on Linux and macOS.
# It does not support Windows yet :-(
# Usage:
#   ./install.sh
#   Homebrew mode:
#   ./install.sh --homebrew
#   This mode is useful if you want to use Orla with Homebrew's version of Ollama. It will not install
#   Orla's binary, but will install Ollama and set everything up for you. The Orla binary will be installed
#   by Homebrew directly.

set -eu

# Check for --homebrew flag
HOMEBREW_INSTALL=0
if [ "${1:-}" = "--homebrew" ]; then
    HOMEBREW_INSTALL=1
    export HOMEBREW_INSTALL=1
fi

status() { echo "STATUS: $*" >&2; }

success() { echo "SUCCESS:$*" >&2; }

error() {
    echo "ERROR: $*" >&2
    exit 1
}

warning() { echo "WARNING: $*" >&2; }

available() { command -v "$1" >/dev/null 2>&1; }

check_os() {
    OS=$(uname -s)
    case "$OS" in
    Linux) ;;
    Darwin) ;;
    *) error "Unsupported operating system: $OS. Orla supports Linux and macOS only for now." ;;
    esac
}

check_os

check_curl() {
    if ! available curl; then
        error "curl is not installed, please install it first"
    fi
}

check_curl

get_latest_release() {
    status "fetching latest orla release from github"
    if [ -n "${GITHUB_TOKEN:-}" ]; then
        LATEST_RELEASE=$(curl -fsSL -H "Authorization: token ${GITHUB_TOKEN}" https://api.github.com/repos/dorcha-inc/orla/releases/latest 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || echo "")
    else
        LATEST_RELEASE=$(curl -fsSL https://api.github.com/repos/dorcha-inc/orla/releases/latest 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || echo "")
    fi
    if [ -z "$LATEST_RELEASE" ]; then
        error "failed to determine latest orla release version from github"
    fi
    status "latest orla release version: $LATEST_RELEASE"
    echo "$LATEST_RELEASE"
}

get_download_url() {
    LOCAL_LATEST_RELEASE="$1"
    PLATFORM="$2"
    ARCH="$3"

    if [ "$PLATFORM" = "darwin" ]; then
        BINARY_NAME="orla-darwin-${ARCH}.tar.gz"
    else
        BINARY_NAME="orla-linux-${ARCH}.tar.gz"
    fi

    status "fetching download url for orla release $LOCAL_LATEST_RELEASE for $PLATFORM $ARCH from github"
    DOWNLOAD_URL="https://github.com/dorcha-inc/orla/releases/download/${LOCAL_LATEST_RELEASE}/${BINARY_NAME}"
    status "download url: $DOWNLOAD_URL"
    echo "$DOWNLOAD_URL"
}

get_install_dir() {
    USER_INSTALL_DIR="/usr/local/bin"
    if [ ! -w "$USER_INSTALL_DIR" ]; then
        error "cannot write to user install directory: $USER_INSTALL_DIR, please run with sudo or as root"
    fi
    echo "$USER_INSTALL_DIR"
}

install_orla() {
    DOWNLOAD_URL="$1"
    INSTALL_DIR="$2"
    PLATFORM="$3"

    # Download the archive
    TEMP_FILE=$(mktemp)
    if ! curl -fsSL "$DOWNLOAD_URL" -o "$TEMP_FILE"; then
        error "failed to download orla from github releases :-("
    fi

    # Extract the binary from the tar.gz archive
    if ! tar -xzf "$TEMP_FILE" -C "$INSTALL_DIR" orla 2>/dev/null; then
        error "failed to extract orla binary from archive"
    fi

    # Verify the extracted binary exists
    if [ ! -f "$INSTALL_DIR/orla" ]; then
        error "orla binary not found after extraction"
    fi

    chmod +x "$INSTALL_DIR/orla"

    # Verify it's executable
    if [ ! -x "$INSTALL_DIR/orla" ]; then
        error "failed to make orla executable"
    fi

    rm -f "$TEMP_FILE"
    success "orla installed successfully :-)"
}

install_ollama() {
    platform="$1"
    status "checking for ollama..."
    if ! available ollama; then
        status "ollama is not installed. installing ollama..."
        if [ "$platform" = "brew" ]; then
            status "installing ollama via homebrew..."
            brew install ollama
            success "ollama installed successfully :-)"
        else
            status "installing ollama via curl..."
            curl -fsSL https://ollama.ai/install.sh | sh
            success "ollama installed successfully :-)"
        fi
    fi
    success "ollama is installed :-)"
}

install_ollama_on_macos() {
    status "macos detected"
    status "checking for ollama..."
    install_ollama "brew"
}

install_ollama_on_linux() {
    status "linux detected"
    status "checking for ollama..."
    install_ollama "curl"
}

run_ollama_service() {
    platform="$1"
    status "checking if ollama is running..."

    # Check if Ollama is already running
    if curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
        success "ollama is running :-)"
        return 0
    fi

    # Ollama is not running, try to start it
    status "ollama is not running. starting ollama service..."
    if [ "$platform" = "brew" ]; then
        if brew services start ollama; then
            success "ollama service started successfully :-)"
        else
            warning "failed to start ollama service. start it manually with: brew services start ollama"
        fi
    else
        if systemctl --user start ollama 2>/dev/null || systemctl start ollama 2>/dev/null; then
            success "ollama service started successfully :-)"
        else
            warning "failed to start ollama service. start it manually with: systemctl --user start ollama"
        fi
    fi
}

run_ollama_service_on_macos() {
    run_ollama_service "brew"
}

run_ollama_service_on_linux() {
    run_ollama_service "systemctl"
}

get_shell_config() {
    # Detect shell config file
    if [ -n "$ZSH_VERSION" ]; then
        echo "$HOME/.zshrc"
    elif [ -n "$BASH_VERSION" ]; then
        if [ -f "$HOME/.bash_profile" ]; then
            echo "$HOME/.bash_profile"
        else
            echo "$HOME/.bashrc"
        fi
    else
        # Default to .bashrc
        echo "$HOME/.bashrc"
    fi
}

add_to_path_in_file() {
    PATH_TO_ADD="$1"
    CONFIG_FILE="$2"

    # Check if already in PATH
    if grep -q "export PATH.*$PATH_TO_ADD" "$CONFIG_FILE" 2>/dev/null; then
        return 0
    fi

    # Add to config file
    {
        echo ""
        echo "# Added by Orla installer"
        echo "export PATH=\"\$PATH:$PATH_TO_ADD\""
    } >>"$CONFIG_FILE"
}

add_orla_to_path() {
    INSTALL_DIR="$1"

    # Check if orla is already in PATH
    if available orla; then
        return 0
    fi

    status "adding orla to PATH..."

    # Add to current session
    export PATH="$PATH:$INSTALL_DIR"

    # Add to shell config file
    CONFIG_FILE=$(get_shell_config)
    if [ -f "$CONFIG_FILE" ] || touch "$CONFIG_FILE" 2>/dev/null; then
        add_to_path_in_file "$INSTALL_DIR" "$CONFIG_FILE"
        success "added orla to PATH in $CONFIG_FILE :-)"
        warning "run 'source $CONFIG_FILE' or restart your terminal to use orla in new sessions"
    else
        warning "could not add orla to PATH automatically. add this to your shell config:"
        echo "  export PATH=\$PATH:$INSTALL_DIR"
    fi
}

add_orla_to_path_macos() {
    # Check if orla is already in PATH
    if available orla; then
        return 0
    fi

    status "adding orla to PATH..."

    if ! available go; then
        warning "go is not available, cannot determine orla install location"
        return 1
    fi

    GOPATH=$(go env GOPATH 2>/dev/null || echo "$HOME/go")
    ORLA_BIN_DIR="$GOPATH/bin"

    # Add to current session
    export PATH="$PATH:$ORLA_BIN_DIR"

    # Add to shell config file
    CONFIG_FILE=$(get_shell_config)
    if [ -f "$CONFIG_FILE" ] || touch "$CONFIG_FILE" 2>/dev/null; then
        add_to_path_in_file "$ORLA_BIN_DIR" "$CONFIG_FILE"
        success "added orla to PATH in $CONFIG_FILE :-)"
        warning "run 'source $CONFIG_FILE' or restart your terminal to use orla in new sessions"
    else
        warning "could not add orla to PATH automatically. add this to your shell config:"
        echo "  export PATH=\$PATH:$ORLA_BIN_DIR"
    fi
}

check_default_model() {
    DEFAULT_MODEL="qwen3:0.6b"

    status "checking for default model..."

    if ! available ollama; then
        error "ollama is not installed somehow (even though we verified it earlier) please start a github issue at https://github.com/dorcha-inc/orla/issues"
    fi

    # Wait for Ollama API to be ready before checking/pulling models
    status "waiting for ollama API to be ready..."
    max_attempts=60
    attempt=0
    while [ $attempt -lt $max_attempts ]; do
        if curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
            break
        fi
        sleep 1
        attempt=$((attempt + 1))
    done

    if [ $attempt -eq $max_attempts ]; then
        warning "ollama API is not ready yet. model will need to be pulled manually later."
        return 0
    fi

    if ollama list 2>/dev/null | grep -q "^$DEFAULT_MODEL"; then
        success "model '$DEFAULT_MODEL' is available :-)"
        return 0
    fi

    status "pulling model '$DEFAULT_MODEL' (this may take a while)..."
    if ollama pull "$DEFAULT_MODEL"; then
        success "model '$DEFAULT_MODEL' pulled successfully :-)"
    else
        error "failed to pull model. you can pull it later with: ollama pull \"$DEFAULT_MODEL\""
    fi

    success "model '$DEFAULT_MODEL' is available :-)"
}

detect_architecture() {
    MACHINE=$(uname -m)
    case "$MACHINE" in
    x86_64 | amd64)
        echo "amd64"
        ;;
    aarch64 | arm64)
        echo "arm64"
        ;;
    *)
        error "Unsupported architecture: $MACHINE"
        ;;
    esac
}

install_on_linux() {
    status "linux detected"
    status "installing orla on linux..."

    # install ollama
    install_ollama_on_linux

    # start ollama service
    run_ollama_service_on_linux

    if [ "$HOMEBREW_INSTALL" = "1" ]; then
        status "homebrew mode: skipping binary installation (orla should already be installed)"
        if ! available orla; then
            error "orla is not installed. homebrew should have installed it."
        fi
        return 0
    fi

    # Detect architecture
    ARCH=$(detect_architecture)
    status "architecture detected: $ARCH"

    # get latest orla release version, fail if not found
    LATEST_RELEASE=$(get_latest_release)
    DOWNLOAD_URL=$(get_download_url "$LATEST_RELEASE" "linux" "$ARCH")

    # get the install directory
    ORLA_INSTALL_DIR=$(get_install_dir)

    # download and install orla binary to the install directory
    install_orla "$DOWNLOAD_URL" "$ORLA_INSTALL_DIR" "linux"

    # add orla to path
    add_orla_to_path "$ORLA_INSTALL_DIR"
}

check_homebrew() {
    status "checking for homebrew..."
    if ! available brew; then
        error "homebrew is not installed. please install homebrew first: https://brew.sh"
    fi
    success "homebrew is installed :-)"
}

check_go_version() {
    # orla requires go 1.25.0 or higher
    MIN_MAJOR=1
    MIN_MINOR=25
    MIN_PATCH=0

    status "checking go version..."

    GO_VERSION_STRING=$(go version | awk '{print $3}')
    # Remove 'go' prefix if present (e.g., "go1.25.0" -> "1.25.0")
    GO_VERSION=$(echo "$GO_VERSION_STRING" | sed 's/^go//')

    # Extract major, minor, and patch versions
    GO_MAJOR=$(echo "$GO_VERSION" | cut -d. -f1)
    GO_MINOR=$(echo "$GO_VERSION" | cut -d. -f2)
    GO_PATCH=$(echo "$GO_VERSION" | cut -d. -f3 | sed 's/[^0-9].*//')

    # Handle versions without patch (e.g., "1.25" -> patch is 0)
    if [ -z "$GO_PATCH" ]; then
        GO_PATCH=0
    fi

    # Compare versions
    if [ "$GO_MAJOR" -lt "$MIN_MAJOR" ] ||
        { [ "$GO_MAJOR" -eq "$MIN_MAJOR" ] && [ "$GO_MINOR" -lt "$MIN_MINOR" ]; } ||
        { [ "$GO_MAJOR" -eq "$MIN_MAJOR" ] && [ "$GO_MINOR" -eq "$MIN_MINOR" ] && [ "$GO_PATCH" -lt "$MIN_PATCH" ]; }; then
        error "go version $GO_VERSION is too old. orla requires go ${MIN_MAJOR}.${MIN_MINOR}.${MIN_PATCH} or higher. please upgrade go."
    fi

    status "go version $GO_VERSION meets requirement (>= ${MIN_MAJOR}.${MIN_MINOR}.${MIN_PATCH}) :-)"
}

install_go() {
    check_homebrew
    status "installing go via homebrew..."
    brew install go
    success "go installed successfully :-)"

    # verify go is installed
    if ! available go; then
        error "go installation completed but 'go' command is not available. please add go to your path and try again :-("
    fi

    # check go version
    status "checking go version..."
    check_go_version
    success "go version check passed :-)"
}

build_and_install_orla() {
    status "building and installing orla..."
    if go install github.com/dorcha-inc/orla/cmd/orla@latest; then
        success "orla installed successfully :-)"
    else
        error "failed to install orla :-("
    fi
}

install_on_macos() {
    status "macos detected"
    status "installing orla on macos..."

    # install ollama
    install_ollama_on_macos

    # start ollama service
    run_ollama_service_on_macos

    if [ "$HOMEBREW_INSTALL" = "1" ]; then
        status "homebrew mode: skipping binary installation (orla should already be installed)"
        if ! available orla; then
            error "orla is not installed. homebrew should have installed it."
        fi
        return 0
    fi

    # Detect architecture
    ARCH=$(detect_architecture)
    status "architecture detected: $ARCH"

    # get latest orla release version, fail if not found
    LATEST_RELEASE=$(get_latest_release)
    DOWNLOAD_URL=$(get_download_url "$LATEST_RELEASE" "darwin" "$ARCH")

    # get the install directory (use /usr/local/bin for macOS too)
    ORLA_INSTALL_DIR="/usr/local/bin"
    if [ ! -w "$ORLA_INSTALL_DIR" ]; then
        error "cannot write to install directory: $ORLA_INSTALL_DIR, please run with sudo or as root"
    fi

    # download and install orla binary to the install directory
    install_orla "$DOWNLOAD_URL" "$ORLA_INSTALL_DIR" "darwin"

    # add orla to path (already in /usr/local/bin which is typically in PATH)
    # But check and add to shell config if needed
    if ! available orla; then
        # /usr/local/bin might not be in PATH, add it
        add_orla_to_path_macos
    else
        success "orla is already in PATH :-)"
    fi
}

# installing orla
case "$OS" in
Linux) install_on_linux ;;
Darwin) install_on_macos ;;
esac

# check for default model
check_default_model

echo ""
success "Installation complete!"
echo ""
echo "Try it out:"
echo "  orla agent \"hello world\""
echo ""
echo "For more information, visit: https://github.com/dorcha-inc/orla"
