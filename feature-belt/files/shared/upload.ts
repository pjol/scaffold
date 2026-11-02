import { ApiError, clientErrorMessage } from "../api/errors";
import type { HttpClient } from "../api/http";
import { filesEndpoints } from "./api";
import type { FilesConnectedItem, FilesItem } from "./types";

/** Sends the bytes to the presigned URL. Web passes an XHR one for progress; the default uses fetch. */
export type FilesPutFn = (
  url: string,
  headers: Record<string, string>,
  body: Blob,
  opts: { signal?: AbortSignal | undefined; onProgress?: ((sent: number, total: number) => void) | undefined },
) => Promise<void>;

export interface FilesUploadInput {
  orgId: string;
  /** The folder (null or undefined: the top of your My drive). */
  parentId?: string | null;
  /** Upload a new version of this file instead of a new file. */
  itemId?: string;
  name: string;
  body: Blob;
  /** Overrides the detected type (the server checks it against the bytes). */
  contentType?: string;
  /** `fail` answers 409 name_taken instead of numbering the name. */
  onConflict?: "rename" | "fail";
  signal?: AbortSignal;
  onProgress?: (sent: number, total: number) => void;
  put?: FilesPutFn;
}

const EXT_TYPES: Record<string, string> = {
  md: "text/markdown",
  markdown: "text/markdown",
  csv: "text/csv",
  txt: "text/plain",
  json: "application/json",
  pdf: "application/pdf",
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  gif: "image/gif",
  webp: "image/webp",
  heic: "image/heic",
  svg: "image/svg+xml",
  mp4: "video/mp4",
  mov: "video/quicktime",
  mp3: "audio/mpeg",
  m4a: "audio/mp4",
  wav: "audio/wav",
  zip: "application/zip",
  docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
};

/** A MIME type from a file name ("" when unknown; the server fills it in from the bytes). */
export function filesGuessType(name: string): string {
  const m = /\.([a-z0-9]+)$/i.exec(name);
  return (m?.[1] && EXT_TYPES[m[1].toLowerCase()]) || "";
}

const fetchPut =
  (http: HttpClient): FilesPutFn =>
  async (url, headers, body, { signal }) => {
    let res: Response;
    try {
      res = await http.fetch(url, { method: "PUT", headers, body, ...(signal ? { signal } : {}) });
    } catch (cause) {
      if (signal?.aborted) throw new ApiError({ status: 0, code: "aborted", message: clientErrorMessage("aborted"), cause });
      throw new ApiError({ status: 0, code: "network_error", message: clientErrorMessage("network_error"), cause });
    }
    if (!res.ok) throw new ApiError({ status: res.status, code: res.status >= 500 ? "internal" : "invalid_request", message: "Upload failed. Try again." });
  };

/**
 * Uploads one file into the drive: presign (`POST …/uploads`), PUT the bytes straight to
 * storage, then complete (the server checks the size and that the bytes match the type).
 * Empty files are created without an upload. Aborting cancels the server-side upload.
 */
export async function filesUpload(http: HttpClient, input: FilesUploadInput): Promise<FilesItem> {
  const api = filesEndpoints(http);
  const contentType = input.contentType || input.body.type || filesGuessType(input.name) || "application/octet-stream";
  const signal = input.signal;
  if (input.body.size === 0) {
    if (input.itemId) throw new ApiError({ status: 0, code: "invalid_request", message: "That file is empty. Choose another." });
    return api.createFile(input.orgId, { parent_id: input.parentId ?? null, name: input.name, text: "", content_type: contentType, on_conflict: input.onConflict ?? "rename" }, { signal });
  }
  const ticket = await api.startUpload(
    input.orgId,
    { parent_id: input.parentId ?? null, ...(input.itemId ? { item_id: input.itemId } : {}), name: input.name, content_type: contentType, size: input.body.size },
    { signal },
  );
  try {
    const put = input.put ?? fetchPut(http);
    await put(ticket.upload_url, ticket.headers, input.body, { signal, onProgress: input.onProgress });
    return await api.completeUpload(ticket.upload_id, input.onConflict ?? "rename", { signal, timeoutMs: 60_000 });
  } catch (error) {
    if (signal?.aborted) void api.cancelUpload(ticket.upload_id).catch(() => undefined);
    throw error;
  }
}

/** Sends one chunk to a platform's upload session. Web passes an XHR one for progress; the default uses fetch. */
export type FilesChunkPutFn = (
  url: string,
  headers: Record<string, string>,
  body: Blob,
  opts: { signal?: AbortSignal | undefined; onProgress?: ((sent: number, total: number) => void) | undefined },
) => Promise<{ status: number; text: string }>;

export interface FilesConnectedUploadInput {
  connectionId: string;
  orgId?: string | null;
  /** The platform folder (omit: the top of My Drive / OneDrive). */
  parentRef?: string;
  /** Upload a new version of this file instead. */
  itemRef?: string;
  name: string;
  body: Blob;
  contentType?: string;
  signal?: AbortSignal;
  onProgress?: (sent: number, total: number) => void;
  put?: FilesChunkPutFn;
}

const fetchChunk =
  (http: HttpClient): FilesChunkPutFn =>
  async (url, headers, body, { signal }) => {
    let res: Response;
    try {
      res = await http.fetch(url, { method: "PUT", headers, body, ...(signal ? { signal } : {}) });
    } catch (cause) {
      if (signal?.aborted) throw new ApiError({ status: 0, code: "aborted", message: clientErrorMessage("aborted"), cause });
      throw new ApiError({ status: 0, code: "network_error", message: clientErrorMessage("network_error"), cause });
    }
    return { status: res.status, text: await res.text() };
  };

/**
 * Uploads one file to a connected drive: the server opens a resumable session on the platform
 * (`POST …/connected/{id}/uploads`), the bytes go straight there in chunks with Content-Range
 * (Drive answers 308, Graph 202 until the last one), then the new item is read back. Large files
 * resume chunk by chunk; nothing passes through the app's storage.
 */
export async function filesConnectedUpload(http: HttpClient, input: FilesConnectedUploadInput): Promise<FilesConnectedItem> {
  const api = filesEndpoints(http);
  const signal = input.signal;
  const contentType = input.contentType || input.body.type || filesGuessType(input.name) || "application/octet-stream";
  const ticket = await api.connectedStartUpload(
    input.connectionId,
    { ...(input.parentRef ? { parent_ref: input.parentRef } : {}), ...(input.itemRef ? { item_ref: input.itemRef } : {}), name: input.name, content_type: contentType, size: input.body.size },
    input.orgId,
    { signal },
  );
  if (ticket.item) return ticket.item;
  const put = input.put ?? fetchChunk(http);
  const total = input.body.size;
  const chunk = Math.max(ticket.chunk_size || total, 256 * 1024);
  let last = { status: 0, text: "" };
  for (let start = 0; start < total; start += chunk) {
    const end = Math.min(start + chunk, total);
    const headers = { ...ticket.headers, "Content-Range": `bytes ${start}-${end - 1}/${total}` };
    last = await put(ticket.upload_url, headers, input.body.slice(start, end), {
      signal,
      onProgress: input.onProgress ? (sent) => input.onProgress?.(start + sent, total) : undefined,
    });
    const more = end < total;
    if (more && last.status !== 308 && last.status !== 202 && !(last.status >= 200 && last.status < 300)) {
      throw new ApiError({ status: last.status, code: last.status === 404 ? "upload_expired" : "upload_failed", message: "The upload stopped. Try again." });
    }
  }
  if (last.status < 200 || last.status >= 300) throw new ApiError({ status: last.status, code: "upload_failed", message: "The upload didn't finish. Try again." });
  let id = "";
  try {
    id = (JSON.parse(last.text) as { id?: string }).id ?? "";
  } catch {
    /* handled below */
  }
  if (!id) throw new ApiError({ status: 0, code: "upload_failed", message: "The upload finished, but the platform didn't say where the file went. Refresh to see it." });
  const detail = await api.connectedItem(input.connectionId, ticket.ref_prefix + id, input.orgId, { signal });
  return detail.item;
}
