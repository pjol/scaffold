"use client";

import { ApiError, type HttpClient, type Upload } from "@scaffoldapp/shared";

/**
 * Uploads a file like the shared `uploadFile`, but PUTs with XMLHttpRequest so the composer can
 * show progress. Returns the storage key to send with the message.
 */
export async function uploadWithProgress(
  http: HttpClient,
  { file, purpose, contentType, onProgress, signal }: { file: File; purpose: string; contentType: string; onProgress: (fraction: number) => void; signal?: AbortSignal },
): Promise<{ key: string }> {
  const presign = await http.post<Upload>("/v1/uploads", { purpose, content_type: contentType, size: file.size }, { signal });
  const headers: Record<string, string> = { ...presign.headers };
  if (!Object.keys(headers).some((h) => h.toLowerCase() === "content-type")) headers["Content-Type"] = contentType;
  await new Promise<void>((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PUT", presign.upload_url);
    for (const [k, v] of Object.entries(headers)) xhr.setRequestHeader(k, v);
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && e.total > 0) onProgress(Math.min(1, e.loaded / e.total));
    };
    xhr.onload = () =>
      xhr.status >= 200 && xhr.status < 300
        ? resolve()
        : reject(new ApiError({ status: xhr.status, code: "upload_failed", message: "Couldn't upload that file. Try again." }));
    xhr.onerror = () => reject(new ApiError({ status: 0, code: "network_error", message: "Couldn't upload that file. Check your connection." }));
    xhr.onabort = () => reject(new ApiError({ status: 0, code: "aborted", message: "Upload cancelled." }));
    signal?.addEventListener("abort", () => xhr.abort());
    xhr.send(file);
  });
  onProgress(1);
  return { key: presign.key };
}
