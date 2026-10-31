# wallets: a local EVM chain (Anvil) for development.
#
# Does nothing unless .env.dev has WALLETS_LOCAL_CHAIN=true and Foundry's
# `anvil` is on PATH (https://getfoundry.sh). Then, before the env files are
# written, it starts Anvil on the block's spare port +6 (chain id
# WALLETS_LOCAL_CHAIN_ID, default 31337; state kept in .data/anvil/state.json)
# and hands the backend WALLETS_CHAINS (that one chain, native ETH) and
# WALLETS_DEFAULT_CHAIN_ID, replacing any WALLETS_CHAINS from .env.dev for
# this run. Anvil's 10 default accounts hold 10000 ETH each; the seeded
# wallets (Sam, Jordan) are accounts 1 and 2. Their keys are public: never use
# them on a real network.
# Menu: `e` shows the chain and sets any address's balance to 10 ETH.

wallets_hook_enabled() {
  case "$(env_get "$ROOT/.env.dev" WALLETS_LOCAL_CHAIN)" in
    true|TRUE|True|1|yes) ;;
    *) return 1 ;;
  esac
  if ! command -v anvil >/dev/null 2>&1; then
    c_yellow "  wallets: WALLETS_LOCAL_CHAIN=true but anvil isn't installed (https://getfoundry.sh); no local chain"
    return 1
  fi
  return 0
}

wallets_hook_rpc() { # wallets_hook_rpc <url> <json body>
  curl -s --max-time 3 -H 'Content-Type: application/json' -d "$2" "$1" 2>/dev/null
}

hook_wallets_prepare() {
  local chain_id port rpc deadline
  wallets_hook_enabled || return 0
  if [[ "${RUN_BACKEND:-1}" != "1" ]]; then
    c_dim "  wallets: backend skipped, so no local chain"
    return 0
  fi
  chain_id="$(env_get "$ROOT/.env.dev" WALLETS_LOCAL_CHAIN_ID)"
  chain_id="${chain_id:-31337}"
  if ! [[ "$chain_id" =~ ^[0-9]+$ ]]; then
    c_red "  wallets: WALLETS_LOCAL_CHAIN_ID must be a number (got $chain_id)"
    return 1
  fi
  port="$(hook_port 6)" || return 1
  mkdir -p "$DATA_DIR/anvil"
  start_bg anvil "$ROOT" anvil --host 127.0.0.1 --port "$port" --chain-id "$chain_id" \
    --state "$DATA_DIR/anvil/state.json" || return 1
  rpc="http://127.0.0.1:$port"
  # Anvil answers JSON-RPC over POST only, so poll eth_chainId.
  deadline=$((SECONDS + 30))
  until wallets_hook_rpc "$rpc" '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' | grep -q '"result"'; do
    if ! kill -0 "$LAST_PID" 2>/dev/null; then
      c_red "  wallets: anvil exited (logs/anvil.log):"
      tail -n 20 "$LOG_DIR/anvil.log"
      return 1
    fi
    if [[ $SECONDS -ge $deadline ]]; then
      c_red "  wallets: anvil did not answer on $rpc within 30 s (logs/anvil.log):"
      tail -n 20 "$LOG_DIR/anvil.log"
      return 1
    fi
    sleep 0.5
  done
  hook_env backend WALLETS_CHAINS "[{\"id\":$chain_id,\"name\":\"Local chain\",\"rpc_url\":\"$rpc\",\"public_rpc_url\":\"$rpc\",\"native_symbol\":\"ETH\",\"testnet\":true,\"tokens\":[]}]" || return 1
  hook_env backend WALLETS_DEFAULT_CHAIN_ID "$chain_id" || return 1
  c_green "  wallets: local chain $chain_id at $rpc"
}

hook_wallets_started() {
  wallets_hook_enabled || return 0
  menu_add e "wallets: local chain, set a balance" wallets_hook_menu
}

wallets_hook_menu() {
  local port rpc chain addr res
  port=$((PORT_BASE + 6))
  rpc="http://127.0.0.1:$port"
  chain="$(wallets_hook_rpc "$rpc" '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' | sed -nE 's/.*"result":"(0x[0-9a-fA-F]+)".*/\1/p')"
  if [[ -z "$chain" ]]; then
    c_yellow "  wallets: the local chain isn't running (WALLETS_LOCAL_CHAIN=true in .env.dev, then restart dev-up)"
    return 0
  fi
  echo "  Local chain $((chain))   RPC $rpc"
  echo "  Seeded wallets: Sam 0x70997970C51812dc3A010C7d01b50e0d17dc79C8, Jordan 0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC"
  echo "  Test keys for accounts 0-9 are at the top of logs/anvil.log"
  printf '  Set an address to 10 ETH (blank to skip): '
  read -r addr || return 0
  [[ -n "$addr" ]] || return 0
  if ! [[ "$addr" =~ ^0x[0-9a-fA-F]{40}$ ]]; then
    c_yellow "  not an address"
    return 0
  fi
  res="$(wallets_hook_rpc "$rpc" "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"anvil_setBalance\",\"params\":[\"$addr\",\"0x8ac7230489e80000\"]}")"
  case "$res" in
    *'"error"'*) c_red "  anvil refused: $res" ;;
    *) c_green "  $addr now holds 10 ETH" ;;
  esac
}
