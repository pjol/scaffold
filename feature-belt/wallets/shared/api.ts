import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { Page } from "../types";
import type {
  AdminWallet,
  AdminWalletListParams,
  LinkWalletRequest,
  RegisterEmbeddedWalletRequest,
  UpdateWalletRequest,
  Wallet,
  WalletActivityParams,
  WalletBalances,
  WalletChallenge,
  WalletChallengeRequest,
  WalletFilter,
  WalletsConfig,
  WalletTransfer,
} from "./types";

/** Wallets endpoints (`/v1/wallets…`, `/v1/admin/wallets…`). Hooks use `useEndpoints(walletsEndpoints)`. */
export function walletsEndpoints(http: HttpClient) {
  const W = "/v1/wallets";
  return {
    /** `GET /v1/wallets/config` (public): chains, tokens, embedded wallets on/off. */
    config: (o?: CallOptions) => http.get<WalletsConfig>(`${W}/config`, { ...o, auth: false }),
    /** `GET /v1/wallets`: every linked wallet, primary first (one page). */
    list: (o?: CallOptions) => http.get<Page<Wallet>>(W, o),
    /** `POST /v1/wallets/link/challenge`: the Sign-In with Ethereum message (single use, 10 minutes). */
    challenge: (body: WalletChallengeRequest, o?: CallOptions) => http.post<WalletChallenge>(`${W}/link/challenge`, body, o),
    /** `POST /v1/wallets/link`: the signed message → the linked wallet. */
    link: (body: LinkWalletRequest, o?: CallOptions) => http.post<Wallet>(`${W}/link`, body, o),
    /** `POST /v1/wallets/embedded`: register Privy embedded wallets (503 when not configured). */
    registerEmbedded: (body: RegisterEmbeddedWalletRequest, o?: CallOptions) => http.post<Page<Wallet>>(`${W}/embedded`, body, o),
    /** `PATCH /v1/wallets/{id}`: label, make primary. */
    update: (id: string, body: UpdateWalletRequest, o?: CallOptions) => http.patch<Wallet>(`${W}/${seg(id)}`, body, o),
    /** `DELETE /v1/wallets/{id}`: unlink. */
    unlink: (id: string, o?: CallOptions) => http.delete<void>(`${W}/${seg(id)}`, o),
    /** `GET /v1/wallets/balances?wallet_id=&chain_id=`: base-unit amounts, read live (cached 15 s on the server). */
    balances: (params: WalletFilter = {}, o?: CallOptions) =>
      http.get<WalletBalances>(`${W}/balances`, { ...o, query: { wallet_id: params.wallet_id, chain_id: params.chain_id } }),
    /** `GET /v1/wallets/activity`: ERC-20 transfers, newest first (block time), keyset pages. */
    activity: (params: WalletActivityParams = {}, o?: CallOptions) =>
      http.get<Page<WalletTransfer>>(`${W}/activity`, {
        ...o,
        query: { wallet_id: params.wallet_id, chain_id: params.chain_id, limit: params.limit, cursor: params.cursor },
      }),
    admin: {
      /** `GET /v1/admin/wallets?address=&user_id=` (support): audited lookup, newest first. */
      list: (params: AdminWalletListParams = {}, o?: CallOptions) =>
        http.get<Page<AdminWallet>>("/v1/admin/wallets", {
          ...o,
          query: { address: params.address, user_id: params.user_id, limit: params.limit, cursor: params.cursor },
        }),
      /** `DELETE /v1/admin/wallets/{id}` (support): unlink from its user. Audited. */
      unlink: (id: string, o?: CallOptions) => http.delete<void>(`/v1/admin/wallets/${seg(id)}`, o),
    },
  };
}

export type WalletsEndpoints = ReturnType<typeof walletsEndpoints>;
