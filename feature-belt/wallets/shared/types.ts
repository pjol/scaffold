/**
 * Wallets DTOs, mirroring backend/internal/wallets JSON exactly (snake_case).
 * Amounts are base units as decimal strings; format them with `formatUnits`.
 */
import type { PageParams } from "../api/params";

export type WalletKind = "external" | "embedded";

/** A linked address as its owner sees it. `address` is EIP-55 checksummed. */
export interface Wallet {
  id: string;
  address: string;
  kind: WalletKind;
  label: string;
  is_primary: boolean;
  created_at: string;
}

/** An ERC-20 on a chain. `decimals` is a digit count (6 for USDC). */
export interface WalletToken {
  address: string;
  symbol: string;
  name: string;
  decimals: number;
}

/** A configured chain. RPC secrets never reach clients; `public_rpc_url` is keyless when set. */
export interface WalletChain {
  id: number;
  name: string;
  public_rpc_url: string | null;
  explorer_url: string | null;
  native_symbol: string;
  native_decimals: number;
  testnet: boolean;
  tokens: WalletToken[];
}

/** `GET /v1/wallets/config` (public). No chains = wallets aren't set up. */
export interface WalletsConfig {
  chains: WalletChain[];
  default_chain_id: number | null;
  embedded: {
    enabled: boolean;
    provider: "privy" | null;
    /** Public by design: the Privy client SDK needs it. */
    privy_app_id: string | null;
  };
  max_wallets: number;
  /** Web page where wallets are linked (mobile opens it). */
  link_url: string;
}

/** `POST /v1/wallets/link/challenge` → the EIP-4361 message to sign with personal_sign. */
export interface WalletChallenge {
  message: string;
  nonce: string;
  expires_at: string;
}

export interface WalletChallengeRequest {
  address: string;
  /** The chain the wallet is on; defaults to the config's default chain. */
  chain_id?: number;
}

export interface LinkWalletRequest {
  message: string;
  signature: string;
  label?: string;
}

/** `POST /v1/wallets/embedded`: a Privy identity token (`getIdentityToken()`, not the access token). */
export interface RegisterEmbeddedWalletRequest {
  identity_token: string;
  address?: string;
  label?: string;
}

/** `PATCH /v1/wallets/{id}`. `is_primary` only accepts true. */
export interface UpdateWalletRequest {
  label?: string;
  is_primary?: true;
}

export type WalletBalanceError = "rpc_unavailable" | "invalid_response";

/** One asset of one wallet on one chain. `amount` is null when the read failed. */
export interface WalletBalance {
  wallet_id: string;
  address: string;
  chain_id: number;
  /** null: the chain's native coin. */
  token_address: string | null;
  symbol: string;
  decimals: number;
  amount: string | null;
  error: WalletBalanceError | null;
}

export interface WalletBalances {
  items: WalletBalance[];
  fetched_at: string;
}

export type TransferDirection = "in" | "out" | "self";

/** An ERC-20 transfer touching the viewer's wallets, direction from their view. */
export interface WalletTransfer {
  id: string;
  chain_id: number;
  tx_hash: string;
  log_index: number;
  block_number: number;
  token_address: string;
  symbol: string;
  decimals: number;
  from: string;
  to: string;
  amount: string;
  direction: TransferDirection;
  explorer_url: string | null;
  occurred_at: string;
}

/** Realtime `wallets.changed` on the user topic. */
export interface WalletChangedEvent {
  wallet_id: string;
  action: "linked" | "updated" | "unlinked";
}

export type WalletProof = "siwe" | "eip1271" | "privy";

/** A linked wallet as staff see it (`GET /v1/admin/wallets`). */
export interface AdminWallet {
  id: string;
  address: string;
  kind: WalletKind;
  label: string;
  is_primary: boolean;
  proof: WalletProof;
  verified_chain_id: number | null;
  provider_subject: string | null;
  user_id: string;
  user_email: string;
  user_display_name: string;
  created_at: string;
}

/** `?wallet_id=&chain_id=` filters. */
export interface WalletFilter {
  wallet_id?: string | undefined;
  chain_id?: number | undefined;
}

export interface WalletActivityParams extends WalletFilter, PageParams {}

/** `GET /v1/admin/wallets?address=&user_id=`: a full address or a 4+ digit hex prefix. */
export interface AdminWalletListParams extends PageParams {
  address?: string | undefined;
  user_id?: string | undefined;
}

/** Wallet error codes clients branch on. */
export type WalletErrorCode =
  | "wallets_address_taken"
  | "wallets_already_linked"
  | "wallets_limit_reached"
  | "wallets_challenge_invalid"
  | "wallets_message_invalid"
  | "wallets_signature_invalid"
  | "wallets_embedded_unconfigured"
  | "wallets_identity_token_invalid"
  | "wallets_no_embedded_wallet"
  | "wallets_not_in_token"
  | "wallets_privy_account_taken"
  | "wallets_chain_unknown";

declare module "../types" {
  interface RealtimeEventMap {
    "wallets.transfer": WalletTransfer;
    "wallets.changed": WalletChangedEvent;
  }
}
