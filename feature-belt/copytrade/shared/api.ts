import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import type { ExchangeFill, ExchangeOrder } from "../exchange/types";
import type { Page } from "../types";
import type {
  CopytradeAdminCopy,
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
} from "./types";

type PageParams = { limit?: number; cursor?: string };

/** Copy trading endpoints (`/v1/copytrade`, `/v1/admin/copytrade`). */
export function copytradeEndpoints(http: HttpClient) {
  const C = "/v1/copytrade";
  const copy = (id: string) => `${C}/copies/${seg(id)}`;
  return {
    config: (o?: CallOptions) => http.get<CopytradeConfig>(`${C}/config`, { ...o, auth: false }),
    leaders: (sort?: CopytradeLeaderSort, o?: CallOptions) => http.get<{ items: CopytradeLeaderCard[] }>(`${C}/leaders`, { ...o, query: { sort } }),
    leader: (userId: string, o?: CallOptions) => http.get<CopytradeLeaderCard>(`${C}/leaders/${seg(userId)}`, o),
    myLeader: (o?: CallOptions) => http.get<CopytradeLeaderStatus>(`${C}/leader`, o),
    setMyLeader: (body: Partial<CopytradeLeaderSettings>, o?: CallOptions) => http.put<CopytradeLeaderStatus>(`${C}/leader`, body, o),
    copiers: (params: PageParams = {}, o?: CallOptions) => http.get<Page<CopytradeCopier>>(`${C}/copiers`, { ...o, query: params }),
    /** Newest first; `status` "open" (not stopped) or "stopped". */
    copies: (params: PageParams & { status?: "open" | "stopped" } = {}, o?: CallOptions) =>
      http.get<Page<CopytradeCopy>>(`${C}/copies`, { ...o, query: params }),
    start: (body: CopytradeStartRequest, o?: CallOptions) => http.post<CopytradeCopy>(`${C}/copies`, body, o),
    copy: (id: string, o?: CallOptions) => http.get<CopytradeCopy>(copy(id), o),
    update: (id: string, body: { settings?: CopytradeSettingsPatch; status?: Extract<CopytradeStatus, "active" | "paused"> }, o?: CallOptions) =>
      http.patch<CopytradeCopy>(copy(id), body, o),
    /** Positive adds from the main account; negative withdraws free funds to it. */
    funds: (id: string, amount: string, o?: CallOptions) => http.post<CopytradeCopy>(`${copy(id)}/funds`, { amount }, o),
    stop: (id: string, closePositions = true, o?: CallOptions) => http.post<CopytradeCopy>(`${copy(id)}/stop`, { close_positions: closePositions }, o),
    closePosition: (id: string, symbol: string, sz?: string, o?: CallOptions) =>
      http.post<{ order: ExchangeOrder; fills: ExchangeFill[] }>(`${copy(id)}/positions/${seg(symbol)}/close`, sz ? { sz } : {}, o),
    events: (id: string, params: PageParams = {}, o?: CallOptions) => http.get<Page<CopytradeEvent>>(`${copy(id)}/events`, { ...o, query: params }),
    admin: {
      overview: (o?: CallOptions) => http.get<CopytradeOverview>("/v1/admin/copytrade/overview", o),
      copies: (params: PageParams & { status?: CopytradeStatus } = {}, o?: CallOptions) =>
        http.get<Page<CopytradeAdminCopy>>("/v1/admin/copytrade/copies", { ...o, query: params }),
      stop: (id: string, o?: CallOptions) => http.post<void>(`/v1/admin/copytrade/copies/${seg(id)}/stop`, undefined, o),
    },
  };
}

export type CopytradeEndpoints = ReturnType<typeof copytradeEndpoints>;
