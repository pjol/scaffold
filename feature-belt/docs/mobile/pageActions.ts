import { ApiError, errorMessage, type DocsExportResult } from "@scaffoldapp/shared";
import { toast } from "@scaffoldapp/ui-native";
import * as DocumentPicker from "expo-document-picker";
import { File, Paths } from "expo-file-system";
import * as Sharing from "expo-sharing";

const MIME: Record<string, string> = {
  md: "text/markdown",
  zip: "application/zip",
  html: "text/html",
  docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
};

/** Downloads a finished export to the cache and opens the share sheet (save to Files, AirDrop, print to PDF). */
export async function docsShareExport(r: DocsExportResult): Promise<void> {
  try {
    const safe = r.name.replace(/[\\/:*?"<>|]+/g, "_") || "page";
    const dest = new File(Paths.cache, safe);
    if (dest.exists) dest.delete();
    const file = await File.downloadFileAsync(r.url, dest);
    if (!(await Sharing.isAvailableAsync())) {
      toast.error("Sharing isn't available on this device.");
      return;
    }
    const ext = /\.([a-z]+)$/i.exec(safe)?.[1]?.toLowerCase() ?? "";
    await Sharing.shareAsync(file.uri, { mimeType: MIME[ext] ?? "application/octet-stream", dialogTitle: r.name });
  } catch (e) {
    toast.error(e instanceof ApiError ? errorMessage(e, "Couldn't export the page.") : "Couldn't save the export. Check your connection and try again.");
  }
}

export interface DocsPickedFile {
  uri: string;
  name: string;
  mimeType: string | null;
}

/** One file from the document picker to import (Markdown, text, HTML, Word, CSV or a zip). */
export async function docsPickImportFile(): Promise<DocsPickedFile | null> {
  const res = await DocumentPicker.getDocumentAsync({
    multiple: false,
    copyToCacheDirectory: true,
    type: ["text/*", "application/zip", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "text/csv", "*/*"],
  });
  if (res.canceled || !res.assets[0]) return null;
  const a = res.assets[0];
  return { uri: a.uri, name: a.name, mimeType: a.mimeType ?? null };
}
