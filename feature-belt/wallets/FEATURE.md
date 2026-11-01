# Wallets

## What it does

- A signed-in user links EVM wallets to their account. Clerk stays the identity provider: a wallet is an add-on, never a sign-in method. The server never holds private keys.
- External wallets (MetaMask, Rabby, Coinbase Wallet) prove control with Sign-In with Ethereum (EIP-4361): the server issues a single-use message valid 10 minutes, the wallet signs it with `personal_sign`, the server recovers the signer (secp256k1 + Keccak). Contract wallets (Safe, Coinbase Smart Wallet) verify through EIP-1271 on a configured chain.
- Embedded wallets (Privy) register from a Privy identity token verified on the server; off unless `PRIVY_APP_ID` and `PRIVY_VERIFICATION_KEY` are set (503 `wallets_embedded_unconfigured`).
- Balances: native coin and configured ERC-20s on every configured chain, read over JSON-RPC and cached 15 s. Amounts travel as base-unit strings plus `decimals`; clients format them with string math.
- Activity: ERC-20 transfers to and from linked wallets, pushed by an indexer webhook, live in the open screen, with a "Received" notification.
- One address belongs to one user; labels and one primary wallet per user. With no chains configured every screen says "Wallets aren't set up".

## Data

| Table | Purpose |
|---|---|
| `wallets` | linked address (lowercase, unique), kind `external`/`embedded`, proof `siwe`/`eip1271`/`privy`, label, primary flag, Privy user id |
| `wallet_nonces` | SIWE challenges: user, address, chain, expiry, used_at (pruned a day after expiry) |
| `wallet_transfers` | ERC-20 transfers, unique `(chain_id, tx_hash, log_index)`, amount `numeric(78,0)` base units, token symbol/decimals copied at ingest, `from_user_id`/`to_user_id` owners when recorded |

Chains and tokens are configuration (`WALLETS_CHAINS`), not tables. `migrations/1500_wallets_init.sql` is the exact schema.

## API

| Method | Path | Guard | Purpose |
|---|---|---|---|
| GET | `/v1/wallets/config` | public | chains (no RPC secrets), tokens, default chain, embedded on/off + Privy app id, `max_wallets`, `link_url` |
| GET | `/v1/wallets` | user | linked wallets, primary first (one `Page`) |
| POST | `/v1/wallets/link/challenge` | user, active, 20/min | `{address, chain_id?}` → `{message, nonce, expires_at}` |
| POST | `/v1/wallets/link` | user, active, 20/min | `{message, signature, label?}` → 201 `Wallet` (200 if already yours) |
| POST | `/v1/wallets/embedded` | user, active, 20/min | `{identity_token, address?, label?}` → `Page<Wallet>` |
| PATCH / DELETE | `/v1/wallets/{id}` | user, active | `{label?, is_primary: true?}` / unlink (the oldest remaining wallet becomes primary) |
| GET | `/v1/wallets/balances` | user, 30/min | `?wallet_id=&chain_id=` → `{items: Balance[], fetched_at}`; a failed read is `amount: null` + `error` |
| GET | `/v1/wallets/activity` | user | `?wallet_id=&chain_id=&limit=&cursor=` → `Page<Transfer>`, keyset on block time `(occurred_at, id)` |
| GET | `/v1/admin/wallets` | support | `?address=` (full or 4+ digit prefix) `&user_id=`; audited `wallets.lookup` |
| DELETE | `/v1/admin/wallets/{id}` | support | unlink; audited `wallets.unlink` |
| POST | `/webhooks/wallets/indexer` | HMAC | transfers from the indexer (below) |

Error codes: `wallets_address_taken`, `wallets_already_linked`, `wallets_limit_reached`, `wallets_challenge_invalid`, `wallets_message_invalid` (`details.reason`), `wallets_signature_invalid`, `wallets_embedded_unconfigured`, `wallets_identity_token_invalid`, `wallets_no_embedded_wallet`, `wallets_not_in_token`, `wallets_privy_account_taken`, `wallets_chain_unknown`, `wallets_indexer_unconfigured`.

Realtime on `user:<id>`: `wallets.transfer` (a `Transfer`, direction from that user's view; the shared handler puts it at the top of cached activity and refreshes balances) and `wallets.changed` (`{wallet_id, action}`). Notification kind `wallets.received`: recipient only, transfers under 1 hour old, not self-transfers. Agent tools (read-only): `wallets_list`, `wallets_balances`, `wallets_activity`.

**Indexer contract.** `POST /webhooks/wallets/indexer`, header `X-Signature: sha256=<hex HMAC-SHA256(raw body, WALLETS_INDEXER_SECRET)>`, body `{"transfers":[{"chain_id":8453,"token_address":"0x…","tx_hash":"0x…","log_index":3,"block_number":123,"block_timestamp":1730000000,"from":"0x…","to":"0x…","amount":"1500000"}]}` → `{received, recorded, duplicates, ignored}`. At most 500 per request; `amount` in base units (string, or a JSON integer); `block_timestamp` in seconds. Unknown chains/tokens and transfers between unlinked addresses are ignored. Each `(chain, tx, log)` is processed once (`webhooks.Once`); a replay only attaches owners who linked an address since. A malformed item rejects the batch (400, `details.index`). Unset secret → 503, bad signature → 401. History starts when an address is linked.

## Screens

| Surface | Where | What |
|---|---|---|
| web | `/wallets` (nav "Wallet" + Home tile) | balances per network and token summed across wallets (wallet and network filters), activity grouped by day with keyset paging and live rows, link button in the empty state |
| web | `/settings/wallets` (settings "Wallets") | linked wallets: label, Primary/Embedded badges, menu (copy address, rename, make primary, view on explorer, unlink with confirm), link dialog |
| web | link dialog | injected EIP-1193 provider (`window.ethereum`): connect → pick account → label → challenge, `personal_sign`, verify; handles no wallet, declined, pending requests |
| web | `/admin/wallets` (admin "Wallets") | search by address, 0x prefix or user id (in the URL), table with user links, unlink with confirm; `?q=<user id>` deep-links from a user |
| mobile | Home entry "Wallet" → `/wallets` | balances per network, activity grouped by day, pull to refresh; header button to manage |
| mobile | Settings "Wallets" → `/wallets/manage` | linked wallets: rename, make primary, share address, unlink; "Link a wallet on the web" opens `link_url` |

Every view has skeleton, empty and error states, plus the "Wallets aren't set up" state.

## Config

| Variable | Default | Purpose |
|---|---|---|
| `WALLETS_CHAINS` | empty | JSON array of chains: `id, name, rpc_url, public_rpc_url?, explorer_url?, native_symbol?, native_decimals?, testnet?, tokens[{address, symbol, name?, decimals}]` (secret: RPC URLs). `decimals` is a digit count and required |
| `WALLETS_DEFAULT_CHAIN_ID` | first chain | default chain for clients and SIWE |
| `WALLETS_SIWE_URI` | `PUBLIC_WEB_URL` | origin named in SIWE messages; also `link_url`; https in production |
| `WALLETS_MAX_PER_USER` / `WALLETS_RPC_TIMEOUT` / `WALLETS_BALANCE_CACHE_TTL` | 10 / 5s / 15s | link limit, one chain round trip, balance reuse |
| `WALLETS_INDEXER_SECRET` | empty | webhook HMAC key |
| `PRIVY_APP_ID`, `PRIVY_VERIFICATION_KEY` | empty | embedded wallets (both or neither; the key is the dashboard's public verification key) |
| `WALLETS_LOCAL_CHAIN`, `WALLETS_LOCAL_CHAIN_ID` (dev) | false, 31337 | dev-up starts Anvil and hands the backend its chain |

Example: `WALLETS_CHAINS=[{"id":8453,"name":"Base","rpc_url":"https://base-mainnet.g.alchemy.com/v2/KEY","public_rpc_url":"https://mainnet.base.org","explorer_url":"https://basescan.org","tokens":[{"address":"0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913","symbol":"USDC","decimals":6}]}]`. Bad values fail the boot through `config.Problem` with the exact cause (a `decimals` of `1000000` is refused as a multiplier; local RPC URLs are refused in production).

Go dependencies (added by `go mod tidy`): `github.com/decred/dcrd/dcrec/secp256k1/v4` (verified v4.4.1) and `golang.org/x/crypto` (Keccak-256). No go-ethereum, no client npm packages.

**Dev-up hook** (`scripts/dev-up/hooks/wallets.sh`): with `WALLETS_LOCAL_CHAIN=true` in `.env.dev` and Foundry's `anvil` on PATH, `hook_wallets_prepare` starts Anvil on `hook_port 6` (state in `.data/anvil/`) and passes `WALLETS_CHAINS` and `WALLETS_DEFAULT_CHAIN_ID` through `hook_env backend`; `hook_wallets_started` adds menu `e` (show the chain, set any address to 10 ETH). The seeded wallets (Sam, Jordan) are Anvil accounts 1 and 2, so their balances are real. Otherwise it does nothing.

## Compliance

- Deletion: wallets and challenges are deleted. Transfers are copies of public chain data, not this app's financial records: the user's link is removed and rows no other linked user needs are deleted. Apps that move money keep their own ledger (payments) and retain that.
- Export: linked wallets (address, kind, proof, label, Privy user id) and every transfer kept for the user.
- Admin lookups that tie addresses to people are audited; so are admin unlinks.
- Privy (when embedded wallets are on) and the RPC providers process addresses: name them in the privacy policy. No tracking; no keys on the server; RPC URLs never reach clients.
- Linking moves no funds; the dialog says so. Apps that let users send or buy crypto take on store and licensing duties (Apple 3.1.5, Play's crypto policy) beyond this feature.

## Stitching

1. Configure `WALLETS_CHAINS` per environment; use `public_rpc_url` only for keyless endpoints.
2. Run an indexer watching the configured tokens that posts the contract above (Ponder, Goldsky, Alchemy webhooks through a relay).
3. Placement: the web nav entry, Home tile and mobile Home entry are wired; drop them from the registries if wallets are secondary (settings stay).
4. If `notifications` is installed, register the kind in `internal/wallets` `New`: `notifications.RegisterKind(notifications.Kind{ID: "wallets.received", Label: "Payments received", Push: true})` (this feature doesn't require notifications, so it only calls `d.Notify.Send`).
5. Mobile linking (not shipped): add WalletConnect (Reown AppKit for React Native) and call `useLinkWallet({ address, chainId, sign })` with its `personal_sign`, setting the dapp metadata URL to `WALLETS_SIWE_URI`'s origin; or Privy (`@privy-io/expo`, loaded only when an app id is set) and send `getIdentityToken()` to `useRegisterEmbeddedWallet()`. Until then mobile opens `link_url`.
6. Embedded wallets on web: add `@privy-io/react-auth` behind `config.embedded.enabled`, create the wallet, then `useRegisterEmbeddedWallet({ identity_token })`.
7. To link from an admin user page, add a link to `/admin/wallets?q=<user id>`.

## Extending

- `ParseChains`/`Registry` is the one chain-config parser (Go); `walletsEndpoints`, `walletKeys` and the hooks are the client contract; `formatUnits`/`parseUnits`/`addUnits` are the only amount math (string arithmetic, never floats).
- `useLinkWallet` takes any `sign(message)`, so new wallet connectors plug in without server changes.
- Smart accounts: link the account address as embedded (Privy smart wallets are read from the token) or through SIWE + EIP-1271 once deployed; undeployed accounts (EIP-6492) aren't verified.
- Sending transactions belongs on the client (wallet or embedded signer); never add server-held keys here.

## Provenance

Distilled from SFLuv (server-served runtime chain config, Ponder → backend transfer webhook with a fail-closed secret and idempotent claims, Privy ES256 verification, resilient Privy storage idea), THASSA (local Privy verification with tolerant PEM parsing) and MOX (Privy loaded only when an app id is set). Fixed: token decimals are a required digit count, not SFLuv's `TOKEN_DECIMALS` multiplier, and no silent 18-decimal fallback; one config parser instead of three; standard `Authorization: Bearer` instead of `Access-Token`; Privy `aud` checked without panicking on an empty claim; indexer replays don't re-notify; amounts never pass through floats.

Verified: Go tests, every route with curl (SIWE with real `cast` signatures, EIP-1271, Privy tokens, indexer, realtime over WebSocket, deletion, export), web flows in Playwright (link through an injected provider, live transfer, manage, admin) at 375/1440 in light and dark, `next build`, the dev-up hook on a real dev-up boot, mobile typecheck and `expo export`. Not verified: mobile screens on a device or simulator; Privy and EIP-1271 against production services.

## Changes

- 1.0.0: shared client (types, endpoints, hooks, base-unit helpers), web and mobile screens, admin page, `link_url` in config, `config.Problem` for bad settings, two-phase dev-up hook.
- 0.1.0: backend, migration, dev-up hook.
