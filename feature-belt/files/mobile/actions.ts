import { ApiError, errorMessage, filesEndpoints, filesUpload, type FilesItem, type HttpClient } from "@scaffoldapp/shared";
import { toast } from "@scaffoldapp/ui-native";
import * as DocumentPicker from "expo-document-picker";
import { File, Paths } from "expo-file-system";
import * as Sharing from "expo-sharing";
import * as WebBrowser from "expo-web-browser";
import { pickPhotos } from "@scaffoldapp/ui-native";

/** A file picked on the device, ready to upload. */
export interface FilesLocalFile {
  uri: string;
  name: string;
  mimeType: string | null;
  size: number | null;
}

/** Photos from the system picker (no library permission needed), up to 20 at a time. */
export async function filesPickPhotos(): Promise<FilesLocalFile[]> {
  const picked = await pickPhotos("library", 20, 1);
  return picked.map((p, i) => ({ uri: p.uri, name: p.name ?? `Photo ${new Date().toISOString().slice(0, 10)} ${i + 1}.jpg`, mimeType: p.mimeType ?? null, size: p.size ?? null }));
}

/** Any files from the document picker (Files, iCloud Drive, Google Drive…). */
export async function filesPickDocuments(): Promise<FilesLocalFile[]> {
  const res = await DocumentPicker.getDocumentAsync({ multiple: true, copyToCacheDirectory: true });
  if (res.canceled) return [];
  return res.assets.map((a) => ({ uri: a.uri, name: a.name, mimeType: a.mimeType ?? null, size: a.size ?? null }));
}

/** Uploads picked files one by one; returns how many landed and the first error. */
export async function filesUploadLocal(http: HttpClient, orgId: string, parentId: string | null, files: FilesLocalFile[], onEach?: (done: number) => void): Promise<{ ok: number; error: string | null }> {
  let ok = 0;
  let error: string | null = null;
  for (const f of files) {
    try {
      const blob = await (await http.fetch(f.uri)).blob();
      await filesUpload(http, { orgId, parentId, name: f.name, body: blob, ...(f.mimeType ? { contentType: f.mimeType } : {}) });
      ok++;
      onEach?.(ok);
    } catch (e) {
      error ??= errorMessage(e, "Upload failed. Try again.");
    }
  }
  return { ok, error };
}

/** Opens a preview link in the in-app browser (PDF, video, audio and documents render there). */
export async function filesOpenInBrowser(http: HttpClient, item: FilesItem): Promise<void> {
  try {
    const u = await filesEndpoints(http).url(item.id, { disposition: "inline" });
    await WebBrowser.openBrowserAsync(u.url);
  } catch (e) {
    toast.error(errorMessage(e, "Couldn't open the file."));
  }
}

/** Downloads a file (or an old version) to the cache and opens the share sheet: save to Files, open in another app. */
export async function filesSaveOrShare(http: HttpClient, item: FilesItem, version?: string): Promise<void> {
  try {
    const u = await filesEndpoints(http).url(item.id, { disposition: "attachment", ...(version ? { version } : {}) });
    const safe = item.name.replace(/[\\/:*?"<>|]+/g, "_") || "file";
    const dest = new File(Paths.cache, safe);
    if (dest.exists) dest.delete();
    const file = await File.downloadFileAsync(u.url, dest);
    if (!(await Sharing.isAvailableAsync())) {
      toast.error("Sharing isn't available on this device.");
      return;
    }
    await Sharing.shareAsync(file.uri, { mimeType: item.mime_type || "application/octet-stream", dialogTitle: item.name });
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e, "Couldn't download the file.") : "Couldn't download the file. Check your connection and try again.");
  }
}
