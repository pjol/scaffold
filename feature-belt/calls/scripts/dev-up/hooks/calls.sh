# calls: a local LiveKit media server for development.
#
# Before the env files are written, prepare starts livekit-server with its
# API and signalling bound to 127.0.0.1 (plus the LAN IP with --lan/--tunnel,
# so phones can reach it) on the block's spare ports:
#   +7  signalling (HTTP/WebSocket, what LIVEKIT_URL points at)
#   +8  media: WebRTC over UDP (single-port mux) and the TCP fallback
# Media listens on the machine's network interfaces: LiveKit gathers its ICE
# candidates there (browsers don't use loopback for WebRTC), and media is
# useless without a session from signalling, which stays local.
# (+6 is the wallets hook's Anvil; +9 stays free.) It hands the backend
# LIVEKIT_URL, LIVEKIT_API_KEY and LIVEKIT_API_SECRET with hook_env; clients
# get the URL from the API with each join token, so they need no setting and
# never see the secret. The key pair is generated once per project
# (.data/livekit/keys, mode 0600), and LiveKit's webhooks go to
# $API_PORT/webhooks/livekit so attendance history works locally.
#
# When livekit-server isn't on PATH it uses .data/bin/livekit-server-<ver>,
# fetching the pinned release first (CALLS_LIVEKIT_DOWNLOAD=false opts out):
#   Linux  the release tarball from github.com/livekit/livekit, sha256-pinned
#   macOS  LiveKit ships no macOS binary (Homebrew builds it), so the pinned
#          release source tarball is sha256-checked and built with the local
#          Go toolchain and Xcode's clang (about 2 minutes, once; module
#          downloads are verified by go.sum and the Go checksum database).
# Skipped when CALLS_LOCAL_LIVEKIT=false or LIVEKIT_URL is set in .env.dev
# (LiveKit Cloud keys pasted there win). Without a server the call routes
# answer 503 calls_unconfigured; nothing here fails the boot.
# Menu: `v` shows the server, its rooms and the logs.

CALLS_LK_VERSION="1.13.11"
CALLS_LK_SRC_SHA256="c8d9b7ba9d66c8b5f314a438c0c2493815e7e70085e37706b888cab3c4ab8f8d"
CALLS_LK_LINUX_AMD64_SHA256="1e9ee90ca301e1842b2887fc77cb122759756a9c8aeb4334ee91de3325ec43fb"
CALLS_LK_LINUX_ARM64_SHA256="2af6ce091d6b3c79d1677f8ff1bbb91a8007f40b2325c059b52d38383c0830b6"
CALLS_LK_LINUX_ARMV7_SHA256="5f663a6498c3d81ef2dce55a3a1e7815aacfe78207e469abc0bd9c5b5ca5070b"

calls_hook_flag() { # calls_hook_flag <KEY> <default>: true when the .env.dev flag is on
  local v
  v="$(env_get "$ROOT/.env.dev" "$1")"
  case "${v:-$2}" in
    true|TRUE|True|1|yes) return 0 ;;
  esac
  return 1
}

calls_hook_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

calls_hook_fetch() { # calls_hook_fetch <url> <dest> <sha256>
  if ! curl -fsSL --retry 2 --connect-timeout 15 -o "$2" "$1"; then
    c_red "  calls: download failed: $1" >&2
    return 1
  fi
  local got
  got="$(calls_hook_sha256 "$2")"
  if [[ "$got" != "$3" ]]; then
    c_red "  calls: checksum mismatch for $1 (got $got); not using it" >&2
    rm -f "$2"
    return 1
  fi
}

# calls_hook_binary prints the path of a livekit-server to run (fetching the
# pinned release when allowed). Messages go to stderr.
calls_hook_binary() {
  local bin os arch url sha work
  if command -v livekit-server >/dev/null 2>&1; then
    command -v livekit-server
    return 0
  fi
  bin="$DATA_DIR/bin/livekit-server-$CALLS_LK_VERSION"
  if [[ -x "$bin" ]]; then
    echo "$bin"
    return 0
  fi
  if ! calls_hook_flag CALLS_LIVEKIT_DOWNLOAD true; then
    c_yellow "  calls: livekit-server isn't installed (brew install livekit, or https://docs.livekit.io/home/self-hosting/local/); calls answer 503" >&2
    return 1
  fi
  mkdir -p "$DATA_DIR/bin" "$DATA_DIR/livekit"
  work="$(mktemp -d "$DATA_DIR/livekit/fetch.XXXXXX")" || return 1
  os="$(uname -s)"
  arch="$(uname -m)"
  case "$os" in
    Linux)
      case "$arch" in
        x86_64|amd64) arch=amd64 sha="$CALLS_LK_LINUX_AMD64_SHA256" ;;
        aarch64|arm64) arch=arm64 sha="$CALLS_LK_LINUX_ARM64_SHA256" ;;
        armv7l) arch=armv7 sha="$CALLS_LK_LINUX_ARMV7_SHA256" ;;
        *) c_yellow "  calls: no LiveKit release for linux/$arch; install livekit-server yourself" >&2; rm -rf "$work"; return 1 ;;
      esac
      url="https://github.com/livekit/livekit/releases/download/v$CALLS_LK_VERSION/livekit_${CALLS_LK_VERSION}_linux_${arch}.tar.gz"
      c_dim "  calls: downloading LiveKit $CALLS_LK_VERSION for linux/$arch (verified by sha256) into .data/bin" >&2
      calls_hook_fetch "$url" "$work/livekit.tar.gz" "$sha" || { rm -rf "$work"; return 1; }
      tar -xzf "$work/livekit.tar.gz" -C "$work" livekit-server || { rm -rf "$work"; return 1; }
      mv "$work/livekit-server" "$bin"
      ;;
    Darwin)
      if ! command -v go >/dev/null 2>&1 || ! command -v cc >/dev/null 2>&1; then
        c_yellow "  calls: LiveKit has no macOS release binary; install it (brew install livekit) or install Go and Xcode's command line tools so dev-up can build it" >&2
        rm -rf "$work"
        return 1
      fi
      url="https://github.com/livekit/livekit/archive/refs/tags/v$CALLS_LK_VERSION.tar.gz"
      c_dim "  calls: LiveKit ships no macOS binary; building release $CALLS_LK_VERSION from its verified source with Go (once, ~2 min, logs/livekit-build.log)" >&2
      calls_hook_fetch "$url" "$work/src.tar.gz" "$CALLS_LK_SRC_SHA256" || { rm -rf "$work"; return 1; }
      tar -xzf "$work/src.tar.gz" -C "$work" || { rm -rf "$work"; return 1; }
      if ! (cd "$work/livekit-$CALLS_LK_VERSION" && CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o "$bin" ./cmd/server) >"$LOG_DIR/livekit-build.log" 2>&1; then
        c_red "  calls: building LiveKit failed (logs/livekit-build.log):" >&2
        tail -n 15 "$LOG_DIR/livekit-build.log" >&2
        rm -rf "$work"
        return 1
      fi
      ;;
    *)
      c_yellow "  calls: no LiveKit build for $os; install livekit-server yourself" >&2
      rm -rf "$work"
      return 1
      ;;
  esac
  rm -rf "$work"
  chmod +x "$bin"
  c_green "  calls: LiveKit $CALLS_LK_VERSION ready at .data/bin/$(basename "$bin")" >&2
  echo "$bin"
}

# calls_hook_keys prints "<key> <secret>", generating the pair once.
calls_hook_keys() {
  local f="$DATA_DIR/livekit/keys"
  mkdir -p "$DATA_DIR/livekit"
  if [[ ! -s "$f" ]]; then
    (
      umask 077
      printf 'API%s %s\n' "$(LC_ALL=C tr -dc 'a-zA-Z0-9' </dev/urandom | head -c 12)" "$(LC_ALL=C tr -dc 'a-zA-Z0-9' </dev/urandom | head -c 48)" >"$f"
    )
  fi
  cat "$f"
}

hook_calls_prepare() {
  local bin sig rtc key secret cfg host binds pair
  [[ "${DEVUP_MODE-boot}" == "boot" && "${RUN_BACKEND-1}" -eq 1 ]] || return 0
  if [[ -n "$(env_get "$ROOT/.env.dev" LIVEKIT_URL)" ]]; then
    c_dim "  calls: LIVEKIT_URL is set in .env.dev; using that server"
    return 0
  fi
  if ! calls_hook_flag CALLS_LOCAL_LIVEKIT true; then
    c_dim "  calls: CALLS_LOCAL_LIVEKIT=false; call routes answer 503 calls_unconfigured"
    return 0
  fi
  bin="$(calls_hook_binary)" || return 0
  sig="$(hook_port 7)" || return 0
  rtc="$(hook_port 8)" || return 0
  pair="$(calls_hook_keys)"
  key="${pair%% *}"
  secret="${pair#* }"
  host="127.0.0.1"
  binds='"127.0.0.1"'
  if [[ -n "${LAN_IP:-}" ]]; then
    host="$LAN_IP"
    binds="\"127.0.0.1\", \"$LAN_IP\""
  fi
  cfg="$DATA_DIR/livekit/livekit.yaml"
  (
    umask 077
    cat >"$cfg" <<EOF
# Written by dev-up's calls hook on every boot; edit the hook, not this file.
port: $sig
bind_addresses: [$binds]
rtc:
  tcp_port: $rtc
  udp_port: $rtc
  use_external_ip: false
keys:
  $key: $secret
webhook:
  api_key: $key
  urls: ["http://127.0.0.1:$API_PORT/webhooks/livekit"]
room:
  empty_timeout: 300
  departure_timeout: 20
logging:
  level: info
EOF
  )
  start_bg livekit "$ROOT" "$bin" --config "$cfg" || return 0
  if ! wait_for "http://127.0.0.1:$sig/" "livekit" 30 "$LAST_PID" "$LOG_DIR/livekit.log" ready; then
    c_yellow "  calls: LiveKit didn't start; call routes answer 503"
    return 0
  fi
  hook_env backend LIVEKIT_URL "ws://$host:$sig" || return 0
  hook_env backend LIVEKIT_API_KEY "$key" || return 0
  hook_env backend LIVEKIT_API_SECRET "$secret" || return 0
  c_green "  calls: LiveKit at ws://$host:$sig (media on $rtc tcp+udp), webhooks → :$API_PORT/webhooks/livekit"
}

hook_calls_started() {
  menu_add v "calls: LiveKit server" calls_hook_menu
}

calls_hook_menu() {
  local sig cfg
  sig=$((PORT_BASE + 7))
  cfg="$DATA_DIR/livekit/livekit.yaml"
  if ! curl -fs --max-time 2 "http://127.0.0.1:$sig/" >/dev/null 2>&1; then
    c_yellow "  calls: no local LiveKit server is running (CALLS_LOCAL_LIVEKIT, or LIVEKIT_URL in .env.dev)"
    return 0
  fi
  echo "  LiveKit  ws://127.0.0.1:$sig   media $((PORT_BASE + 8)) tcp+udp"
  echo "  Config   ${cfg#"$ROOT"/} (keys in .data/livekit/keys)   Log  logs/livekit.log"
  echo "  Rooms in the last log lines:"
  grep -E '"room": "[0-9a-f-]{36}"' "$LOG_DIR/livekit.log" 2>/dev/null | grep -oE '"room": "[0-9a-f-]{36}"' | sort -u | tail -n 5 | sed 's/^/    /'
}
