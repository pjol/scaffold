import type { AcceptConsentsRequest, AccountExport, ClientConfig, Me, UpdateMeRequest, Upload, UploadRequest } from "../../types";
import type { CallOptions, HttpClient } from "../http";
import { uploadFile, type UploadInput, type UploadResult } from "../upload";

export interface HealthStatus {
  status: string;
  error?: string;
}

/** Core account endpoints (`backend/internal/accounts`). */
export function coreEndpoints(http: HttpClient) {
  return {
    /** `GET /healthz`: liveness (no token). */
    health: (o?: CallOptions) => http.get<HealthStatus>("/healthz", { ...o, auth: false }),
    /** `GET /v1/config` (public): app name, legal versions, support email, minimum app versions, modules. */
    config: (o?: CallOptions) => http.get<ClientConfig>("/v1/config", { ...o, auth: false }),
    /** `GET /v1/me`. Never consent-gated, so the consent screen can render. */
    me: (o?: CallOptions) => http.get<Me>("/v1/me", o),
    /** `PATCH /v1/me`: display name, avatar (`avatar_url: ""` clears it). */
    updateMe: (body: UpdateMeRequest, o?: CallOptions) => http.patch<Me>("/v1/me", body, o),
    /** `POST /v1/me/consents`: accept the current Terms and Privacy versions. 409 when they moved. */
    acceptConsents: (body: AcceptConsentsRequest, o?: CallOptions) => http.post<Me>("/v1/me/consents", body, o),
    /** `GET /v1/me/export`: everything stored about the user, as JSON. */
    exportAccount: (o?: CallOptions) => http.get<AccountExport>("/v1/me/export", { timeoutMs: 60_000, ...o }),
    /** `DELETE /v1/me`: irreversible. Sign out afterwards. Rate limited to 3 per hour. */
    deleteAccount: (o?: CallOptions) => http.delete<void>("/v1/me", o),
    /** `POST /v1/uploads`: a presigned PUT. Prefer `upload`, which also sends the bytes. */
    presignUpload: (body: UploadRequest, o?: CallOptions) => http.post<Upload>("/v1/uploads", body, o),
    /** Presign + PUT a Blob/File (web) or `{ uri }` (native). */
    upload: (input: UploadInput): Promise<UploadResult> => uploadFile(http, input),
  };
}
