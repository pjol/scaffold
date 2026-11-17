"use client";

import { useMutation, useQueryClient, type InfiniteData, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import { exchangeKeys } from "../exchange/hooks";
import { useEndpoints } from "../hooks/context";
import { REALTIME_FALLBACK_POLL_MS, useApiQuery, useConnectionState, usePagedQuery, type PagedQueryResult } from "../hooks/internal";
import { registerReconnectRefetch, registerRealtimeHandler } from "../hooks/realtime-handlers";
import type { Page } from "../types";
import { copytradeEndpoints } from "./api";
import type {
  CopytradeAdminCopy,
  CopytradeAllocationBucket,
  CopytradeConfig,
  CopytradeCopier,
  CopytradeCopy,
  CopytradeEvent,
  CopytradeLeaderCard,
  CopytradeLeaderSettings,
  CopytradeLeaderSort,
  CopytradeLeaderStatus,
  CopytradeOverview,
  CopytradeSettingsPatch,
  CopytradeStartRequest,
  CopytradeStatus,
  CopytradeStopReason,
} from "./types";

/** Query keys. `copytradeKeys.all()` invalidates everything copy trading caches. */
export const copytradeKeys = {
  all: () => ["copytrade"] as const,
  config: () => ["copytrade", "config"] as const,
  leaders: (sort: CopytradeLeaderSort | "" = "") => ["copytrade", "leaders", sort] as const,
  leaderCard: (userId: string) => ["copytrade", "leader-card", userId] as const,
  myLeader: () => ["copytrade", "my-leader"] as const,
  copiers: () => ["copytrade", "copiers"] as const,
  copiesAll: () => ["copytrade", "copies"] as const,
  copies: (status: "open" | "stopped" | "" = "") => ["copytrade", "copies", status] as const,
  copy: (id: string) => ["copytrade", "copy", id] as const,
  events: (id: string) => ["copytrade", "events", id] as const,
  admin: {
    all: () => ["copytrade", "admin"] as const,
    overview: () => ["copytrade", "admin", "overview"] as const,
    copies: (status: CopytradeStatus | "" = "") => ["copytrade", "admin", "copies", status] as const,
  },
} as const;

type PagedCopies = InfiniteData<Page<CopytradeCopy>, string | undefined>;

// A copy frame (≤ 1/s per copy) refreshes its detail and its row in every list.
registerRealtimeHandler("copytrade.copy", (qc, e) => {
  qc.setQueryData(copytradeKeys.copy(e.data.id), e.data);
  qc.setQueriesData<PagedCopies>({ queryKey: copytradeKeys.copiesAll() }, (d) =>
    d ? { ...d, pages: d.pages.map((p) => ({ ...p, items: p.items.map((c) => (c.id === e.data.id ? e.data : c)) })) } : d,
  );
});

// New activity goes on top of the copy's event list.
registerRealtimeHandler("copytrade.event", (qc, e) => {
  qc.setQueryData<InfiniteData<Page<CopytradeEvent>, string | undefined>>(copytradeKeys.events(e.data.copy_id), (d) => {
    const first = d?.pages[0];
    if (!d || !first || first.items.some((x) => x.id === e.data.id)) return d;
    return { ...d, pages: [{ ...first, items: [e.data, ...first.items] }, ...d.pages.slice(1)] };
  });
  if (e.data.kind === "transfer" || e.data.kind === "start" || e.data.kind === "stop") {
    void qc.invalidateQueries({ queryKey: exchangeKeys.account() });
  }
});

registerReconnectRefetch(copytradeKeys.all());

/** Poll while the socket is down; realtime frames keep copies fresh otherwise. */
function useLivePoll(): number | false {
  return useConnectionState() === "open" ? false : REALTIME_FALLBACK_POLL_MS;
}

export function useCopytradeConfig(): UseQueryResult<CopytradeConfig, ApiError> {
  const api = useEndpoints(copytradeEndpoints);
  return useApiQuery(copytradeKeys.config(), ({ signal }) => api.config({ signal }), { staleTime: 5 * 60_000 });
}

/** The leader directory (refreshes every 30 s, like the server's cache). */
export function useCopytradeLeaders(sort?: CopytradeLeaderSort): UseQueryResult<{ items: CopytradeLeaderCard[] }, ApiError> {
  const api = useEndpoints(copytradeEndpoints);
  return useApiQuery(copytradeKeys.leaders(sort ?? ""), ({ signal }) => api.leaders(sort, { signal }), { refetchInterval: 30_000 });
}

export function useCopytradeLeader(userId: string | null | undefined): UseQueryResult<CopytradeLeaderCard, ApiError> {
  const api = useEndpoints(copytradeEndpoints);
  return useApiQuery(copytradeKeys.leaderCard(userId ?? ""), ({ signal }) => api.leader(userId ?? "", { signal }), undefined, !!userId);
}

/** The caller's own leading: settings, eligibility, copiers and assets. */
export function useMyCopytradeLeader(): UseQueryResult<CopytradeLeaderStatus, ApiError> {
  const api = useEndpoints(copytradeEndpoints);
  return useApiQuery(copytradeKeys.myLeader(), ({ signal }) => api.myLeader({ signal }));
}

export function useSetMyCopytradeLeader(): UseMutationResult<CopytradeLeaderStatus, ApiError, Partial<CopytradeLeaderSettings>> {
  const api = useEndpoints(copytradeEndpoints);
  const qc = useQueryClient();
  return useMutation<CopytradeLeaderStatus, ApiError, Partial<CopytradeLeaderSettings>>({
    mutationFn: (body) => api.setMyLeader(body),
    onSuccess: (st) => {
      qc.setQueryData(copytradeKeys.myLeader(), st);
      void qc.invalidateQueries({ queryKey: copytradeKeys.leaders() });
    },
  });
}

export function useCopytradeCopiers(): PagedQueryResult<CopytradeCopier> {
  const api = useEndpoints(copytradeEndpoints);
  return usePagedQuery<CopytradeCopier>(copytradeKeys.copiers(), (cursor, signal) => api.copiers({ cursor, limit: 50 }, { signal }));
}

/** The caller's copies, newest first. */
export function useCopytradeCopies(status: "open" | "stopped" | "" = ""): PagedQueryResult<CopytradeCopy> {
  const api = useEndpoints(copytradeEndpoints);
  const poll = useLivePoll();
  return usePagedQuery<CopytradeCopy>(
    copytradeKeys.copies(status),
    (cursor, signal) => api.copies({ cursor, limit: 20, status: status || undefined }, { signal }),
    { refetchInterval: poll },
  );
}

export function useCopytradeCopy(id: string | null | undefined): UseQueryResult<CopytradeCopy, ApiError> {
  const api = useEndpoints(copytradeEndpoints);
  const poll = useLivePoll();
  return useApiQuery(copytradeKeys.copy(id ?? ""), ({ signal }) => api.copy(id ?? "", { signal }), { refetchInterval: poll }, !!id);
}

export function useCopytradeEvents(id: string | null | undefined): PagedQueryResult<CopytradeEvent> {
  const api = useEndpoints(copytradeEndpoints);
  return usePagedQuery<CopytradeEvent>(copytradeKeys.events(id ?? ""), (cursor, signal) => api.events(id ?? "", { cursor, limit: 30 }, { signal }), undefined, !!id);
}

function useCopyMutation<V>(fn: (v: V) => Promise<CopytradeCopy>): UseMutationResult<CopytradeCopy, ApiError, V> {
  const qc = useQueryClient();
  return useMutation<CopytradeCopy, ApiError, V>({
    mutationFn: fn,
    onSuccess: (c) => {
      qc.setQueryData(copytradeKeys.copy(c.id), c);
      void qc.invalidateQueries({ queryKey: copytradeKeys.copiesAll() });
      void qc.invalidateQueries({ queryKey: copytradeKeys.events(c.id) });
      void qc.invalidateQueries({ queryKey: copytradeKeys.leaders() });
      void qc.invalidateQueries({ queryKey: exchangeKeys.account() });
    },
  });
}

/** Start copying a leader (moves the allocation from the main account). */
export function useStartCopytrade(): UseMutationResult<CopytradeCopy, ApiError, CopytradeStartRequest> {
  const api = useEndpoints(copytradeEndpoints);
  return useCopyMutation((body: CopytradeStartRequest) => api.start(body));
}

/** Change settings and/or pause ("paused") or resume ("active"). */
export function useUpdateCopytrade(id: string): UseMutationResult<CopytradeCopy, ApiError, { settings?: CopytradeSettingsPatch; status?: "active" | "paused" }> {
  const api = useEndpoints(copytradeEndpoints);
  return useCopyMutation((body: { settings?: CopytradeSettingsPatch; status?: "active" | "paused" }) => api.update(id, body));
}

/** Add (positive) or withdraw (negative) funds. */
export function useCopytradeFunds(id: string): UseMutationResult<CopytradeCopy, ApiError, string> {
  const api = useEndpoints(copytradeEndpoints);
  return useCopyMutation((amount: string) => api.funds(id, amount));
}

export function useStopCopytrade(id: string): UseMutationResult<CopytradeCopy, ApiError, { closePositions: boolean }> {
  const api = useEndpoints(copytradeEndpoints);
  return useCopyMutation(({ closePositions }: { closePositions: boolean }) => api.stop(id, closePositions));
}

export function useCloseCopytradePosition(id: string): UseMutationResult<unknown, ApiError, { symbol: string; sz?: string }> {
  const api = useEndpoints(copytradeEndpoints);
  const qc = useQueryClient();
  return useMutation<unknown, ApiError, { symbol: string; sz?: string }>({
    mutationFn: ({ symbol, sz }) => api.closePosition(id, symbol, sz),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: copytradeKeys.copy(id) });
      void qc.invalidateQueries({ queryKey: copytradeKeys.events(id) });
    },
  });
}

export function useCopytradeAdminOverview(): UseQueryResult<CopytradeOverview, ApiError> {
  const api = useEndpoints(copytradeEndpoints);
  return useApiQuery(copytradeKeys.admin.overview(), ({ signal }) => api.admin.overview({ signal }), { refetchInterval: 15_000 });
}

export function useCopytradeAdminCopies(status: CopytradeStatus | "" = ""): PagedQueryResult<CopytradeAdminCopy> {
  const api = useEndpoints(copytradeEndpoints);
  return usePagedQuery<CopytradeAdminCopy>(copytradeKeys.admin.copies(status), (cursor, signal) =>
    api.admin.copies({ cursor, limit: 50, status: status || undefined }, { signal }),
  );
}

/** Staff: stop a copy (positions closed, funds returned; audited). */
export function useStopCopytradeAsStaff(): UseMutationResult<void, ApiError, string> {
  const api = useEndpoints(copytradeEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: (id) => api.admin.stop(id),
    onSuccess: () => void qc.invalidateQueries({ queryKey: copytradeKeys.admin.all() }),
  });
}

// ---- display helpers ----

/** "12.5" → "+12.50%"; null → "—". */
export function formatCopytradePct(pct: string | null | undefined, signed = true): string {
  if (pct === null || pct === undefined || pct === "") return "—";
  const n = Number(pct);
  if (!Number.isFinite(n)) return "—";
  const s = Math.abs(n).toFixed(2);
  return `${signed ? (n > 0 ? "+" : n < 0 ? "−" : "") : n < 0 ? "−" : ""}${s}%`;
}

const bucketLabels: Record<CopytradeAllocationBucket, string> = { lt_100: "Under 100", "100_1k": "100–1,000", "1k_10k": "1,000–10,000", gt_10k: "Over 10,000" };

/** An allocation bucket, with the quote currency: "100–1,000 USD". */
export function copytradeAllocationLabel(bucket: CopytradeAllocationBucket, currency: string): string {
  return `${bucketLabels[bucket]} ${currency}`;
}

const stopLabels: Record<Exclude<CopytradeStopReason, "">, string> = {
  user: "You stopped it",
  stop_loss: "Stop loss",
  drawdown: "Drawdown limit",
  leader_disabled: "The trader stopped offering copies",
  leader_left: "The trader left",
  staff: "Stopped by the team",
  blocked: "Blocked",
  account_closed: "Account closed",
};

/** Why a copy stopped, for people. */
export function copytradeStopReasonLabel(reason: CopytradeStopReason): string {
  return reason ? stopLabels[reason] : "";
}

/** Status for people: "Copying", "Paused", "Stopping", "Stopped". */
export function copytradeStatusLabel(status: CopytradeStatus): string {
  return { active: "Copying", paused: "Paused", stopping: "Stopping", stopped: "Stopped" }[status];
}
