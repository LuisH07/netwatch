#!/usr/bin/env bash
set -euo pipefail

readonly REPO="LuisH07/netwatch"
readonly BINARY_NAME="netwatch"
readonly INSTALL_PATH="/usr/local/bin/${BINARY_NAME}"

log_info() { echo "[INFO] $1"; }
log_success() { echo "[SUCCESS] $1"; }
log_error() { echo "[ERROR] $1" >&2; exit 1; }

# 1. Validação de Sistema Operacional
if [ "$(uname -s)" != "Linux" ]; then
    log_error "O NetWatch é compatível exclusivamente com sistemas Linux."
fi

# 2. Decisão: Compilação Local vs Download da Release
if [ -f "go.mod" ] && command -v go &> /dev/null; then
    log_info "Repositório local detectado com Go instalado. Compilando do código-fonte..."
    go mod tidy
    go build -ldflags="-s -w" -o "${BINARY_NAME}" .
else
    log_info "Baixando o binário oficial da última release do GitHub..."
    
    ARCH="$(uname -m)"
    case "${ARCH}" in
        x86_64)  BINARY_ARCH="amd64" ;;
        aarch64) BINARY_ARCH="arm64" ;;
        *) log_error "Arquitetura não suportada: ${ARCH}" ;;
    es

    DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${BINARY_NAME}-linux-${BINARY_ARCH}"

    if ! curl -sL --fail "${DOWNLOAD_URL}" -o "${BINARY_NAME}"; then
        log_error "Falha ao baixar o binário. Verifique se há uma Release publicada no GitHub ou se o Go está instalado para compilar localmente."
    fi
fi

# 3. Instalação Global no Sistema
log_info "Instalando binário em ${INSTALL_PATH}..."

if [ -w "$(dirname "${INSTALL_PATH}")" ]; then
    mv "${BINARY_NAME}" "${INSTALL_PATH}"
else
    log_info "Permissões de superusuário necessárias para gravar em /usr/local/bin."
    sudo mv "${BINARY_NAME}" "${INSTALL_PATH}"
fi

sudo chmod +x "${INSTALL_PATH}"

log_success "NetWatch instalado com sucesso!"
log_info "Execute '${BINARY_NAME} --help' de qualquer lugar no terminal."