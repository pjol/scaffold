import type { Upload, UploadPurpose, UploadRequest } from "../types";
import { ApiError, clientErrorMessage } from "./errors";
import type { CallOptions, HttpClient } from "./http";

/** The backend's default per-purpose cap (`storage.MaxUploadBytes`, 10 MiB). Purposes may differ. */
export const MAX_UPLOAD_BYTES = 10 * 1024 * 1024;

/** A React Native / Expo file reference (`expo-image-picker`, `expo-document-picker`). */
export interface NativeFileRef {
  uri: string;
  /** MIME type (`mimeType` from expo pickers). */
  type?: string | null | undefined;
  mimeType?: string | null | undefined;
  name?: string | null | undefined;
  /** Byte size if known (`fileSize` / `size` from pickers). */
  size?: number | null | undefined;
}

/** Anything uploadable: a Blob/File (web) or a `{ uri }` (React Native). */
export type UploadSource = Blob | NativeFileRef;

export interface UploadInput extends CallOptions {
  file: UploadSource;
  purpose: UploadPurpose;
  /** Overrides the detected MIME type. */
  contentType?: string | undefined;
  /** Reject larger files before presigning (the server enforces its own limit regardless). */
  maxBytes?: number | undefined;
}

export interface UploadResult {
  /** Send back to the owning endpoint, which checks the key belongs to the user. */
  key: string;
  /** Public URL for public purposes (avatars), else null. */
  public_url: string | null;
  content_type: string;
  size: number;
}

function isNativeRef(source: UploadSource): source is NativeFileRef {
  return typeof source === "object" && source !== null && typeof (source as NativeFileRef).uri === "string";
}

/** Extensions the backend accepts (`storage.extByType`). */
const EXT_TYPES: Record<string, string> = {
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  png: "image/png",
  heic: "image/heic",
  heif: "image/heic",
  webp: "image/webp",
  gif: "image/gif",
  mp4: "video/mp4",
  mov: "video/quicktime",
  m4a: "audio/mp4",
  mp3: "audio/mpeg",
  pdf: "application/pdf",
  txt: "text/plain",
  csv: "text/csv",
  zip: "application/zip",
  docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
};

/** Guess a MIME type from a file name or URI extension. */
export function guessContentType(nameOrUri: string | null | undefined): string | undefined {
  const m = /\.([a-z0-9]+)(?:[?#].*)?$/i.exec(nameOrUri ?? "");
  const ext = m?.[1];
  return ext ? EXT_TYPES[ext.toLowerCase()] : undefined;
}

function formatMb(bytes: number): string {
  return `${Math.round((bytes / (1024 * 1024)) * 10) / 10} MB`;
}

/**
 * Presign (`POST /v1/uploads`) then PUT the bytes straight to storage with the returned
 * headers (no auth header). Works with web `Blob`/`File` and React Native `{ uri }` refs
 * (read through `fetch(uri).blob()`, which RN supports for `file://`, `content://` and
 * `data:` URIs).
 */
export async function uploadFile(http: HttpClient, input: UploadInput): Promise<UploadResult> {
  const { file, purpose, signal } = input;
  let blob: Blob;
  let hintedType: string | undefined;
  if (isNativeRef(file)) {
    hintedType = file.mimeType ?? file.type ?? guessContentType(file.name ?? file.uri);
    try {
      const res = await http.fetch(file.uri, signal ? { signal } : undefined);
      blob = await res.blob();
    } catch (cause) {
      throw new ApiError({ status: 0, code: "network_error", message: "Couldn't read that file.", cause });
    }
  } else {
    blob = file;
    const name = (file as Blob & { name?: string }).name;
    hintedType = name ? guessContentType(name) : undefined;
  }

  const contentType = input.contentType ?? (blob.type || hintedType || "application/octet-stream");
  const size = blob.size;
  if (input.maxBytes !== undefined && size > input.maxBytes) {
    throw new ApiError({ status: 0, code: "invalid_request", message: `That file is too large. The limit is ${formatMb(input.maxBytes)}.` });
  }

  const body: UploadRequest = { purpose, content_type: contentType, size };
  const presign = await http.post<Upload>("/v1/uploads", body, { signal, timeoutMs: input.timeoutMs });

  const headers: Record<string, string> = { ...presign.headers };
  if (!Object.keys(headers).some((h) => h.toLowerCase() === "content-type")) headers["Content-Type"] = contentType;

  const controller = new AbortController();
  const onAbort = () => controller.abort();
  signal?.addEventListener?.("abort", onAbort);
  let timedOut = false;
  const timer = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, input.timeoutMs ?? 120_000);
  try {
    const res = await http.fetch(presign.upload_url, { method: "PUT", headers, body: blob, signal: controller.signal });
    if (!res.ok) {
      throw new ApiError({ status: res.status, code: res.status >= 500 ? "internal" : "invalid_request", message: "Upload failed. Try again." });
    }
  } catch (cause) {
    if (cause instanceof ApiError) throw cause;
    if (timedOut) throw new ApiError({ status: 0, code: "timeout", message: clientErrorMessage("timeout"), cause });
    if (signal?.aborted) throw new ApiError({ status: 0, code: "aborted", message: clientErrorMessage("aborted"), cause });
    throw new ApiError({ status: 0, code: "network_error", message: clientErrorMessage("network_error"), cause });
  } finally {
    clearTimeout(timer);
    signal?.removeEventListener?.("abort", onAbort);
  }
  return { key: presign.key, public_url: presign.public_url, content_type: contentType, size };
}
