"use client";

import { useMutation, useQueryClient, type InfiniteData, type QueryClient, type QueryKey, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { useEndpoints } from "../hooks/context";
import { useApiQuery, usePagedQuery, type InfiniteOverrides, type PagedQueryResult, type QueryOverrides } from "../hooks/internal";
import { listKeyParams } from "../hooks/keys";
import { registerRealtimeHandler, registerReconnectRefetch } from "../hooks/realtime-handlers";
import type { Page } from "../types";
import { walletsEndpoints } from "./api";
import type {
  AdminWallet,
  AdminWalletListParams,
  RegisterEmbeddedWalletRequest,
  UpdateWalletRequest,
  Wallet,
  WalletBalances,
  WalletFilter,
  WalletsConfig,
  WalletTransfer,
} from "./types";

/** Query keys. `all()` prefixes everything the user sees (reconnect refetch, sign-out). */
export const walletKeys = {
  all: () => ["wallets"] as const,
  config: () => ["wallets", "config"] as const,
  list: () => ["wallets", "list"] as const,
  balancesAll: () => ["wallets", "balances"] as const,
  balances: (filter: WalletFilter = {}) => ["wallets", "balances", listKeyParams(filter)] as const,
  activityAll: () => ["wallets", "activity"] as const,
  activity: (filter: WalletFilter = {}) => ["wallets", "activity", listKeyParams(filter)] as const,
  admin: {
    all: () => ["admin", "wallets"] as const,
    list: (params: Record<string, unknown> = {}) => ["admin", "wallets", "list", params] as const,
  },
} as const;

// ---- queries -------------------------------------------------------------------

/** `GET /v1/wallets/config` (works signed out; cached 5 min). No chains = not set up. */
export function useWalletsConfig(options?: QueryOverrides<WalletsConfig>): UseQueryResult<WalletsConfig, ApiError> {
  const w = useEndpoints(walletsEndpoints);
  return useApiQuery(walletKeys.config(), ({ signal }) => w.config({ signal }), { staleTime: 5 * 60_000, ...options });
}

/** The signed-in user's linked wallets, primary first. */
export function useWallets(options?: QueryOverrides<Wallet[]>): UseQueryResult<Wallet[], ApiError> {
  const w = useEndpoints(walletsEndpoints);
  return useApiQuery(walletKeys.list(), async ({ signal }) => (await w.list({ signal })).items, options);
}

/** Native and token balances (base units) for every wallet × chain, or one wallet / chain. */
export function useWalletBalances(filter: WalletFilter = {}, options?: QueryOverrides<WalletBalances>): UseQueryResult<WalletBalances, ApiError> {
  const w = useEndpoints(walletsEndpoints);
  return useApiQuery(walletKeys.balances(filter), ({ signal }) => w.balances(filter, { signal }), {
    staleTime: 15_000,
    placeholderData: (prev) => prev,
    ...options,
  });
}

/** Transfers touching the user's wallets, newest first; live through `wallets.transfer`. */
export function useWalletActivity(filter: WalletFilter = {}, options?: InfiniteOverrides<WalletTransfer>): PagedQueryResult<WalletTransfer> {
  const w = useEndpoints(walletsEndpoints);
  return usePagedQuery<WalletTransfer>(walletKeys.activity(filter), (cursor, signal) => w.activity({ ...filter, cursor, limit: 20 }, { signal }), options);
}

// ---- linking -------------------------------------------------------------------

export interface LinkWalletInput {
  /** The address the wallet reported (eth_requestAccounts). */
  address: string;
  /** The chain the wallet is on (eth_chainId); defaults to the server's default chain. */
  chainId?: number;
  label?: string;
  /**
   * Signs the challenge with personal_sign and resolves to the 0x signature. The
   * platform supplies it (web: the injected EIP-1193 provider; mobile: WalletConnect).
   */
  sign: (message: string) => Promise<string>;
}

/** Sign-In with Ethereum: challenge → wallet signs → server verifies and links. */
export function useLinkWallet(): UseMutationResult<Wallet, ApiError | Error, LinkWalletInput> {
  const w = useEndpoints(walletsEndpoints);
  const qc = useQueryClient();
  return useMutation<Wallet, ApiError | Error, LinkWalletInput>({
    mutationFn: async ({ address, chainId, label, sign }) => {
      const challenge = await w.challenge(chainId ? { address, chain_id: chainId } : { address });
      const signature = await sign(challenge.message);
      return w.link({ message: challenge.message, signature, ...(label ? { label } : {}) });
    },
    onSuccess: (wallet) => upsertWallet(qc, wallet),
    onSettled: () => invalidateDerived(qc),
  });
}

/** Register Privy embedded wallets from an identity token (503 `wallets_embedded_unconfigured` when off). */
export function useRegisterEmbeddedWallet(): UseMutationResult<Wallet[], ApiError, RegisterEmbeddedWalletRequest> {
  const w = useEndpoints(walletsEndpoints);
  const qc = useQueryClient();
  return useMutation<Wallet[], ApiError, RegisterEmbeddedWalletRequest>({
    mutationFn: async (body) => (await w.registerEmbedded(body)).items,
    onSuccess: (wallets) => wallets.forEach((wallet) => upsertWallet(qc, wallet)),
    onSettled: () => invalidateDerived(qc),
  });
}

// ---- manage --------------------------------------------------------------------

interface ListSnapshot {
  previous: Wallet[] | undefined;
}

function sortWallets(list: Wallet[]): Wallet[] {
  return [...list].sort((a, b) => Number(b.is_primary) - Number(a.is_primary) || a.created_at.localeCompare(b.created_at) || a.id.localeCompare(b.id));
}

function upsertWallet(qc: QueryClient, wallet: Wallet): void {
  qc.setQueryData<Wallet[]>(walletKeys.list(), (list) => {
    if (!list) return list;
    const others = list.filter((x) => x.id !== wallet.id).map((x) => (wallet.is_primary ? { ...x, is_primary: false } : x));
    return sortWallets([...others, wallet]);
  });
}

function invalidateDerived(qc: QueryClient): Promise<unknown> {
  return Promise.all([
    qc.invalidateQueries({ queryKey: walletKeys.list() }),
    qc.invalidateQueries({ queryKey: walletKeys.balancesAll() }),
    qc.invalidateQueries({ queryKey: walletKeys.activityAll() }),
  ]);
}

/** Rename or make primary; optimistic on the list, rolled back on failure. */
export function useUpdateWallet(): UseMutationResult<Wallet, ApiError, { id: string } & UpdateWalletRequest, ListSnapshot> {
  const w = useEndpoints(walletsEndpoints);
  const qc = useQueryClient();
  const key = walletKeys.list();
  return useMutation<Wallet, ApiError, { id: string } & UpdateWalletRequest, ListSnapshot>({
    mutationFn: ({ id, ...body }) => w.update(id, body),
    onMutate: async ({ id, label, is_primary }) => {
      await qc.cancelQueries({ queryKey: key });
      const previous = qc.getQueryData<Wallet[]>(key);
      if (previous) {
        qc.setQueryData<Wallet[]>(
          key,
          sortWallets(
            previous.map((x) => {
              if (x.id === id) return { ...x, ...(label !== undefined ? { label: label.trim() } : {}), ...(is_primary ? { is_primary: true } : {}) };
              return is_primary ? { ...x, is_primary: false } : x;
            }),
          ),
        );
      }
      return { previous };
    },
    onSuccess: (wallet) => upsertWallet(qc, wallet),
    onError: (_e, _v, ctx) => {
      if (ctx?.previous) qc.setQueryData(key, ctx.previous);
    },
  });
}

/** Unlink; optimistic (the oldest remaining wallet becomes primary), rolled back on failure. */
export function useUnlinkWallet(): UseMutationResult<void, ApiError, string, ListSnapshot> {
  const w = useEndpoints(walletsEndpoints);
  const qc = useQueryClient();
  const key = walletKeys.list();
  return useMutation<void, ApiError, string, ListSnapshot>({
    mutationFn: (id) => w.unlink(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: key });
      const previous = qc.getQueryData<Wallet[]>(key);
      if (previous) {
        const removed = previous.find((x) => x.id === id);
        let rest = previous.filter((x) => x.id !== id);
        if (removed?.is_primary && rest.length > 0) {
          const oldest = [...rest].sort((a, b) => a.created_at.localeCompare(b.created_at) || a.id.localeCompare(b.id))[0];
          rest = rest.map((x) => (x.id === oldest?.id ? { ...x, is_primary: true } : x));
        }
        qc.setQueryData<Wallet[]>(key, sortWallets(rest));
      }
      return { previous };
    },
    onError: (_e, _id, ctx) => {
      if (ctx?.previous) qc.setQueryData(key, ctx.previous);
    },
    onSettled: () => invalidateDerived(qc),
  });
}

// ---- admin ---------------------------------------------------------------------

/** `GET /v1/admin/wallets` (support): by address (full or 4+ hex prefix) and/or user. Audited server-side. */
export function useAdminWallets(params: AdminWalletListParams = {}, options?: InfiniteOverrides<AdminWallet>): PagedQueryResult<AdminWallet> {
  const w = useEndpoints(walletsEndpoints);
  return usePagedQuery<AdminWallet>(walletKeys.admin.list(listKeyParams(params)), (cursor, signal) => w.admin.list({ ...params, cursor }, { signal }), {
    placeholderData: (prev) => prev,
    ...options,
  });
}

/** `DELETE /v1/admin/wallets/{id}`: unlink a wallet from its user. Audited. */
export function useAdminUnlinkWallet(): UseMutationResult<void, ApiError, string> {
  const w = useEndpoints(walletsEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => w.admin.unlink(id),
    onSettled: () =>
      Promise.all([qc.invalidateQueries({ queryKey: walletKeys.admin.all() }), qc.invalidateQueries({ queryKey: ["admin", "audit"] })]),
  });
}

// ---- realtime ------------------------------------------------------------------

type ActivityData = InfiniteData<Page<WalletTransfer>, string | undefined>;

/** Put a live transfer at the top of each cached activity list it belongs to. */
export function applyTransferEvent(qc: QueryClient, transfer: WalletTransfer): void {
  for (const [key, data] of qc.getQueriesData<ActivityData>({ queryKey: walletKeys.activityAll() })) {
    if (!data) continue;
    const filter = ((key as QueryKey)[2] ?? {}) as WalletFilter;
    if (filter.chain_id !== undefined && filter.chain_id !== transfer.chain_id) continue;
    const first = data.pages[0];
    const top = first?.items[0];
    const known = data.pages.some((p) => p.items.some((t) => t.id === transfer.id));
    if (filter.wallet_id !== undefined || !first || known || (top && transfer.occurred_at < top.occurred_at)) {
      // Can't place it locally (wallet filter, backfilled older transfer): refetch.
      if (!known) void qc.invalidateQueries({ queryKey: key, exact: true });
      continue;
    }
    qc.setQueryData<ActivityData>(key, { ...data, pages: [{ ...first, items: [transfer, ...first.items] }, ...data.pages.slice(1)] });
  }
}

registerRealtimeHandler("wallets.transfer", (qc, e) => {
  applyTransferEvent(qc, e.data);
  void qc.invalidateQueries({ queryKey: walletKeys.balancesAll() });
});

registerRealtimeHandler("wallets.changed", (qc) => {
  void invalidateDerived(qc);
});

registerReconnectRefetch(walletKeys.all());
