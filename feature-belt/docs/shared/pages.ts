"use client";

import { useMutation, useQueryClient, type UseMutationResult, type UseQueryResult } from "@tanstack/react-query";
import type { ApiError } from "../api/errors";
import type { CallOptions, HttpClient } from "../api/http";
import { seg } from "../api/query-string";
import { uploadFile, type UploadSource } from "../api/upload";
import { useApi, useEndpoints } from "../hooks/context";
import { useApiQuery, useDebouncedValue, usePagedQuery, type PagedQueryResult } from "../hooks/internal";
import type { Page } from "../types";
import { docsKeys } from "./hooks";
import type { DocsBlock, DocsCoverColor, DocsPage, DocsPageDetail, DocsPageSummary, DocsRefs, DocsRefUser, DocsSidebar } from "./types";

// Pages and the workspace around them: page chrome, templates, imports and
// exports, version diffs and the updates feed, following, analytics,
// verification, comment reactions, publish options, quick find, teamspaces.

// ---- page chrome -----------------------------------------------------------------

export type DocsPageFont = "default" | "serif" | "mono";

/** An icon from the app's set (lucide name, tinted with a palette role) or an image uploaded to the page. */
export type DocsCustomIcon = { type: "lucide"; name: string; color: DocsCoverColor } | { type: "image"; file_id: string };

/** Built-in covers: gradients between two palette roles, so they follow the theme and the palette. */
export const DOCS_COVER_GALLERY = {
  dawn: ["warning-soft", "danger-soft"],
  lagoon: ["info-soft", "success-soft"],
  dusk: ["primary", "accent"],
  meadow: ["success-soft", "warning-soft"],
  ember: ["danger", "warning"],
  glacier: ["info-soft", "primary-soft"],
  orchid: ["accent-soft", "primary-soft"],
  slate: ["border-strong", "sunken"],
  citrus: ["warning", "success"],
  tide: ["primary", "info"],
  aurora: ["success", "accent"],
  sand: ["sunken", "warning-soft"],
} as const satisfies Record<string, readonly [string, string]>;
export type DocsCoverGallery = keyof typeof DOCS_COVER_GALLERY;
export const DOCS_COVER_GALLERY_NAMES = Object.keys(DOCS_COVER_GALLERY) as DocsCoverGallery[];

/** Palette roles icons and colour covers can use. */
export const DOCS_ICON_COLORS: DocsCoverColor[] = ["neutral", "primary", "accent", "info", "success", "warning", "danger"];

/**
 * The icon set (lucide names). Web and mobile map each name to its component; keep the lists in step
 * (web `PageIcon.tsx`, mobile `PageIcon.tsx`).
 */
export const DOCS_ICON_SET = [
  "file-text", "book-open", "notebook-pen", "clipboard-list", "list-checks", "circle-check", "target", "flag", "rocket", "lightbulb",
  "star", "heart", "bookmark", "calendar", "clock", "alarm-clock", "map", "compass", "globe", "house",
  "building-2", "briefcase", "users", "user", "user-round-check", "megaphone", "message-square", "mail", "phone", "bell",
  "code", "terminal", "bug", "git-branch", "database", "server", "cpu", "shield", "lock", "key-round",
  "palette", "pen-tool", "image", "camera", "film", "music", "mic", "headphones", "layout-dashboard", "layers",
  "chart-bar", "chart-pie", "trending-up", "dollar-sign", "wallet", "shopping-cart", "package", "truck", "plane", "car",
  "graduation-cap", "school", "flask-conical", "atom", "leaf", "tree-pine", "sun", "moon", "cloud", "umbrella",
  "coffee", "utensils", "gift", "trophy", "medal", "sparkles", "zap", "flame", "anchor", "puzzle",
  "wrench", "settings", "folder", "archive", "inbox", "link", "search", "eye", "handshake", "scale",
] as const;
export type DocsIconName = (typeof DOCS_ICON_SET)[number];

/** Word and character counts and reading time of a page's text blocks. */
export function docsWordCount(blocks: readonly DocsBlock[]): { words: number; characters: number; minutes: number } {
  let text = "";
  for (const b of blocks) {
    const spans = b.props.text;
    if (Array.isArray(spans)) text += ` ${spans.map((s) => s.text).join("")}`;
    if (b.type === "code" && typeof b.props.code === "string") text += ` ${b.props.code}`;
  }
  const words = (text.match(/[\p{L}\p{N}][\p{L}\p{N}'’-]*/gu) ?? []).length;
  return { words, characters: text.replace(/\s/g, "").length, minutes: Math.max(1, Math.round(words / 230)) };
}

/** The headings of a page (document order) for an outline. */
export function docsOutline(blocks: readonly DocsBlock[]): { id: string; level: number; text: string }[] {
  const out: { id: string; level: number; text: string }[] = [];
  for (const b of blocks) {
    const m = /^heading_(\d)$/.exec(b.type);
    if (!m) continue;
    const text = (b.props.text ?? []).map((s) => s.text).join("").trim();
    if (text) out.push({ id: b.id, level: Number(m[1]), text });
  }
  return out;
}

// ---- types -----------------------------------------------------------------------

export interface DocsTeamspace {
  team: { id: string; org_id: string; name: string; handle: string };
  pages: DocsPageSummary[];
}

export interface DocsReaction {
  emoji: string;
  count: number;
  mine: boolean;
}

export interface DocsPublishSettings {
  search_indexing: boolean;
  allow_duplicate: boolean;
  theme: "system" | "light" | "dark";
  show_outline: boolean;
  updated_at?: string;
}

export interface DocsTemplate {
  /** `builtin:<key>` or the template page's id. */
  id: string;
  source: "builtin" | "workspace";
  name: string;
  icon: string | null;
  custom_icon?: DocsCustomIcon | null;
  category: string;
  info?: string;
  kind: "page" | "database";
  cover?: DocsPage["cover"];
  created_by?: DocsRefUser | null;
  updated_at?: string;
  my_level?: string;
}

export interface DocsTemplates {
  builtin: DocsTemplate[];
  workspace: DocsTemplate[];
}

export interface DocsTemplatePreview {
  template: DocsTemplate;
  blocks: readonly DocsBlock[];
  refs: DocsRefs;
  views: string[];
  subpages: string[];
}

export interface DocsUseTemplateRequest {
  parent_id?: string | null;
  shared?: boolean;
  team_id?: string | null;
  title?: string;
}

export interface DocsImportError {
  path: string;
  message: string;
}

export interface DocsImportRun {
  id: string;
  org_id: string;
  parent_id: string | null;
  name: string;
  format: "markdown" | "text" | "html" | "docx" | "csv" | "zip";
  status: "running" | "done" | "failed";
  pages_total: number;
  pages_done: number;
  files_done: number;
  errors: DocsImportError[];
  message: string | null;
  root_page_id: string | null;
  created_at: string;
  finished_at: string | null;
}

export type DocsExportFormat = "markdown" | "zip" | "html" | "html_zip" | "docx";

export interface DocsExportResult {
  /** A one-hour download link; the file saves under `name`. */
  url: string;
  name: string;
  size: number;
  expires_at: string;
  /** What was left out (files too large to include). */
  notes: string[];
}

export interface DocsDiffEntry {
  block: DocsBlock;
  depth: number;
  change: "" | "added" | "removed" | "changed";
  before_text?: string;
}

export interface DocsDiffSummary {
  added: number;
  removed: number;
  changed: number;
  title_changed: boolean;
}

export interface DocsVersionDiff {
  against: "previous" | "current";
  from_title: string;
  to_title: string;
  from_at: string | null;
  to_at: string | null;
  entries: DocsDiffEntry[];
  summary: DocsDiffSummary;
  refs: DocsRefs;
}

export interface DocsUpdate {
  type: "edit" | "comment";
  at: string;
  people: DocsRefUser[];
  snapshot_id?: string;
  summary?: DocsDiffSummary;
  text: string;
  thread_id?: string;
  quote?: string | null;
}

export interface DocsAnalytics {
  days: { date: string; views: number; viewers: number }[];
  views: number;
  viewers: number;
  people: { user: DocsRefUser; views: number; last_viewed_at: string }[];
  /** Viewers not listed by name (they left the workspace). */
  others: number;
}

export interface DocsFindHit extends DocsPageSummary {
  parent_title: string | null;
  created_by: DocsRefUser | null;
  created_at: string;
}

export interface DocsFindParams {
  q?: string;
  kind?: "page" | "database";
  /** "me" or a person's id. */
  created_by?: string;
  edited?: "today" | "week" | "month" | "year";
  /** Only pages inside this page. */
  in?: string;
  sort?: "relevance" | "edited" | "created";
  limit?: number;
}

export interface DocsTrashParams {
  q?: string;
  by?: "me";
  kind?: "page" | "database";
  in?: string;
}

// ---- endpoints -------------------------------------------------------------------

/** `/v1/docs` endpoints for templates, imports, exports, history diffs, following, analytics and publishing options. */
export function docsPagesEndpoints(http: HttpClient) {
  const D = "/v1/docs";
  const org = (id: string) => `${D}/orgs/${seg(id)}`;
  const page = (id: string) => `${D}/pages/${seg(id)}`;
  return {
    /** A page read for a hover card (no visit or view is counted). */
    pagePreview: (pageId: string, o?: CallOptions) => http.get<DocsPageDetail>(page(pageId), { ...o, query: { preview: "1" } }),
    templates: (orgId: string, o?: CallOptions) => http.get<DocsTemplates>(`${org(orgId)}/templates`, o),
    templatePreview: (orgId: string, templateId: string, o?: CallOptions) => http.get<DocsTemplatePreview>(`${org(orgId)}/templates/${seg(templateId)}/preview`, o),
    useTemplate: (orgId: string, templateId: string, body: DocsUseTemplateRequest, o?: CallOptions) => http.post<DocsPage>(`${org(orgId)}/templates/${seg(templateId)}/use`, body, o),
    saveAsTemplate: (pageId: string, body: { shared?: boolean }, o?: CallOptions) => http.post<DocsPage>(`${page(pageId)}/template`, body, o),
    stopTemplate: (pageId: string, o?: CallOptions) => http.delete<DocsPage>(`${page(pageId)}/template`, o),

    startImport: (orgId: string, body: { key: string; name: string; parent_id?: string | null; shared?: boolean }, o?: CallOptions) =>
      http.post<DocsImportRun>(`${org(orgId)}/imports`, body, o),
    importRun: (id: string, o?: CallOptions) => http.get<DocsImportRun>(`${D}/imports/${seg(id)}`, o),
    imports: (orgId: string, o?: CallOptions) => http.get<{ items: DocsImportRun[] }>(`${org(orgId)}/imports`, o),
    exportPage: (pageId: string, format: DocsExportFormat, subpages: boolean, o?: CallOptions) =>
      http.get<DocsExportResult>(`${page(pageId)}/export/${seg(format)}`, { timeoutMs: 120_000, ...o, query: { subpages: subpages ? "true" : undefined } }),

    versionDiff: (snapshotId: string, against: "previous" | "current", o?: CallOptions) => http.get<DocsVersionDiff>(`${D}/history/${seg(snapshotId)}/diff`, { ...o, query: { against } }),
    updates: (pageId: string, cursor?: string, o?: CallOptions) => http.get<Page<DocsUpdate>>(`${page(pageId)}/updates`, { ...o, query: { cursor } }),

    follow: (pageId: string, o?: CallOptions) => http.put<{ following: boolean }>(`${page(pageId)}/follow`, undefined, o),
    unfollow: (pageId: string, o?: CallOptions) => http.delete<{ following: boolean }>(`${page(pageId)}/follow`, o),
    analytics: (pageId: string, days: number, o?: CallOptions) => http.get<DocsAnalytics>(`${page(pageId)}/analytics`, { ...o, query: { days } }),
    verify: (pageId: string, days: number, o?: CallOptions) => http.put<DocsPage>(`${page(pageId)}/verification`, { days }, o),
    unverify: (pageId: string, o?: CallOptions) => http.delete<DocsPage>(`${page(pageId)}/verification`, o),

    react: (commentId: string, emoji: string, o?: CallOptions) => http.put<void>(`${D}/comments/${seg(commentId)}/reactions`, { emoji }, o),
    unreact: (commentId: string, emoji: string, o?: CallOptions) => http.delete<void>(`${D}/comments/${seg(commentId)}/reactions`, { ...o, query: { emoji } }),

    publishSettings: (pageId: string, o?: CallOptions) => http.get<DocsPublishSettings>(`${page(pageId)}/publish`, o),
    updatePublishSettings: (pageId: string, body: Partial<DocsPublishSettings>, o?: CallOptions) => http.patch<DocsPublishSettings>(`${page(pageId)}/publish`, body, o),
    duplicatePublic: (token: string, body: { org_id: string; page_id?: string | null }, o?: CallOptions) => http.post<DocsPage>(`${D}/public/${seg(token)}/duplicate`, body, o),

    find: (orgId: string, params: DocsFindParams, o?: CallOptions) =>
      http.get<{ items: DocsFindHit[] }>(`${org(orgId)}/find`, { ...o, query: { ...params, q: params.q || undefined } }),
    trash: (orgId: string, params: DocsTrashParams & { cursor?: string }, o?: CallOptions) =>
      http.get<Page<DocsPageSummary>>(`${org(orgId)}/trash`, { ...o, query: { q: params.q || undefined, by: params.by, kind: params.kind, in: params.in, cursor: params.cursor } }),
    setTeamspaceHidden: (orgId: string, teamId: string, hidden: boolean, o?: CallOptions) =>
      hidden ? http.put<void>(`${org(orgId)}/teamspaces/${seg(teamId)}/hidden`, undefined, o) : http.delete<void>(`${org(orgId)}/teamspaces/${seg(teamId)}/hidden`, o),
  };
}

export type DocsPagesEndpoints = ReturnType<typeof docsPagesEndpoints>;

export const docsPagesKeys = {
  preview: (pageId: string) => ["docs", "preview", pageId] as const,
  templates: (orgId: string) => ["docs", "templates", orgId] as const,
  templatePreview: (orgId: string, id: string) => ["docs", "template-preview", orgId, id] as const,
  importRun: (id: string) => ["docs", "import", id] as const,
  imports: (orgId: string) => ["docs", "imports", orgId] as const,
  diff: (id: string, against: string) => ["docs", "diff", id, against] as const,
  updates: (pageId: string) => ["docs", "updates", pageId] as const,
  analytics: (pageId: string, days: number) => ["docs", "analytics", pageId, days] as const,
  publishSettings: (pageId: string) => ["docs", "publish-settings", pageId] as const,
  find: (orgId: string, params: DocsFindParams) => ["docs", "find", orgId, params] as const,
  trash: (orgId: string, params: DocsTrashParams) => ["docs", "trash", orgId, "filtered", params] as const,
};

// ---- hooks: previews -------------------------------------------------------------

/** A page for a hover card: read without counting a visit; cached a minute. */
export function useDocsPagePreview(pageId: string | null | undefined): UseQueryResult<DocsPageDetail, ApiError> {
  const d = useEndpoints(docsPagesEndpoints);
  return useApiQuery(docsPagesKeys.preview(pageId ?? ""), ({ signal }) => d.pagePreview(pageId!, { signal }), { staleTime: 60_000, retry: false }, !!pageId);
}

// ---- hooks: templates ------------------------------------------------------------

/** The built-in gallery and the workspace templates the caller can open. */
export function useDocsTemplates(orgId: string | null | undefined, enabled = true): UseQueryResult<DocsTemplates, ApiError> {
  const d = useEndpoints(docsPagesEndpoints);
  return useApiQuery(docsPagesKeys.templates(orgId ?? ""), ({ signal }) => d.templates(orgId!, { signal }), { staleTime: 60_000 }, !!orgId && enabled);
}

export function useDocsTemplatePreview(orgId: string | null | undefined, templateId: string | null | undefined): UseQueryResult<DocsTemplatePreview, ApiError> {
  const d = useEndpoints(docsPagesEndpoints);
  return useApiQuery(docsPagesKeys.templatePreview(orgId ?? "", templateId ?? ""), ({ signal }) => d.templatePreview(orgId!, templateId!, { signal }), { staleTime: 5 * 60_000 }, !!orgId && !!templateId);
}

function invalidatePages(qc: ReturnType<typeof useQueryClient>, orgId: string) {
  void qc.invalidateQueries({ queryKey: docsKeys.sidebar(orgId) });
  void qc.invalidateQueries({ queryKey: docsKeys.childrenAll() });
  void qc.invalidateQueries({ queryKey: docsKeys.recent(orgId) });
}

/** A new page from a template, placed like a new page. */
export function useDocsUseTemplate(orgId: string): UseMutationResult<DocsPage, ApiError, { templateId: string } & DocsUseTemplateRequest> {
  const d = useEndpoints(docsPagesEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsPage, ApiError, { templateId: string } & DocsUseTemplateRequest>({
    mutationFn: ({ templateId, ...body }) => d.useTemplate(orgId, templateId, body),
    onSuccess: () => invalidatePages(qc, orgId),
  });
}

/** Save a copy of the page as a workspace template (private, or for everyone). */
export function useDocsSaveAsTemplate(orgId: string): UseMutationResult<DocsPage, ApiError, { pageId: string; shared?: boolean }> {
  const d = useEndpoints(docsPagesEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsPage, ApiError, { pageId: string; shared?: boolean }>({
    mutationFn: ({ pageId, shared }) => d.saveAsTemplate(pageId, { shared: !!shared }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: docsPagesKeys.templates(orgId) }),
  });
}

/** A template becomes an ordinary page again. */
export function useDocsStopTemplate(orgId: string): UseMutationResult<DocsPage, ApiError, string> {
  const d = useEndpoints(docsPagesEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsPage, ApiError, string>({
    mutationFn: (pageId) => d.stopTemplate(pageId),
    onSuccess: (p) => {
      void qc.invalidateQueries({ queryKey: docsPagesKeys.templates(orgId) });
      void qc.invalidateQueries({ queryKey: docsKeys.page(p.id) });
      invalidatePages(qc, orgId);
    },
  });
}

// ---- hooks: import and export ----------------------------------------------------

/** Accepted import files (web `accept`). */
export const DOCS_IMPORT_ACCEPT = ".md,.markdown,.txt,.html,.htm,.docx,.csv,.tsv,.zip";

/** The type the upload is sent as: storage knows no .md or .html, so those go up as text/plain. */
export function docsImportContentType(name: string): string {
  const ext = /\.([a-z0-9]+)$/i.exec(name)?.[1]?.toLowerCase();
  if (ext === "zip") return "application/zip";
  if (ext === "docx") return "application/vnd.openxmlformats-officedocument.wordprocessingml.document";
  if (ext === "csv" || ext === "tsv") return "text/csv";
  return "text/plain";
}

/** Upload a file (purpose docs_import) and start importing it; resolves to the run. */
export function useDocsStartImport(orgId: string): UseMutationResult<DocsImportRun, ApiError, { file: UploadSource; name: string; parent_id?: string | null; shared?: boolean }> {
  const api = useApi();
  const d = useEndpoints(docsPagesEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsImportRun, ApiError, { file: UploadSource; name: string; parent_id?: string | null; shared?: boolean }>({
    mutationFn: async ({ file, name, parent_id, shared }) => {
      const up = await uploadFile(api.http, { file, purpose: "docs_import", contentType: docsImportContentType(name), timeoutMs: 600_000 });
      return d.startImport(orgId, { key: up.key, name, ...(parent_id ? { parent_id } : {}), ...(shared ? { shared } : {}) });
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: docsPagesKeys.imports(orgId) }),
  });
}

/** One import run, polled every second while it runs; the tree refreshes when it ends. */
export function useDocsImportRun(id: string | null | undefined, orgId?: string): UseQueryResult<DocsImportRun, ApiError> {
  const d = useEndpoints(docsPagesEndpoints);
  const qc = useQueryClient();
  return useApiQuery(
    docsPagesKeys.importRun(id ?? ""),
    async ({ signal }) => {
      const run = await d.importRun(id!, { signal });
      if (run.status !== "running" && orgId) invalidatePages(qc, orgId);
      return run;
    },
    { refetchInterval: (q) => (q.state.data?.status === "running" || !q.state.data ? 1000 : false), staleTime: 0 },
    !!id,
  );
}

export function useDocsImports(orgId: string | null | undefined, enabled = true): UseQueryResult<DocsImportRun[], ApiError> {
  const d = useEndpoints(docsPagesEndpoints);
  return useApiQuery(docsPagesKeys.imports(orgId ?? ""), async ({ signal }) => (await d.imports(orgId!, { signal })).items, { staleTime: 5_000 }, !!orgId && enabled);
}

/** Export a page; resolves to a one-hour download link. */
export function useDocsExport(): UseMutationResult<DocsExportResult, ApiError, { pageId: string; format: DocsExportFormat; subpages?: boolean }> {
  const d = useEndpoints(docsPagesEndpoints);
  return useMutation<DocsExportResult, ApiError, { pageId: string; format: DocsExportFormat; subpages?: boolean }>({
    mutationFn: ({ pageId, format, subpages }) => d.exportPage(pageId, format, !!subpages),
  });
}

// ---- hooks: history --------------------------------------------------------------

/** What a version changed since the one before it, or what changed since it (`current`). */
export function useDocsVersionDiff(snapshotId: string | null | undefined, against: "previous" | "current" = "previous"): UseQueryResult<DocsVersionDiff, ApiError> {
  const d = useEndpoints(docsPagesEndpoints);
  return useApiQuery(docsPagesKeys.diff(snapshotId ?? "", against), ({ signal }) => d.versionDiff(snapshotId!, against, { signal }), { staleTime: against === "current" ? 0 : 5 * 60_000 }, !!snapshotId);
}

/** The page's updates feed: saved versions with what changed, and new comment threads. */
export function useDocsUpdates(pageId: string | null | undefined, enabled = true): PagedQueryResult<DocsUpdate> {
  const d = useEndpoints(docsPagesEndpoints);
  return usePagedQuery(docsPagesKeys.updates(pageId ?? ""), (cursor, signal) => d.updates(pageId!, cursor, { signal }), undefined, !!pageId && enabled);
}

// ---- hooks: following, analytics, verification -----------------------------------

/** Follow or unfollow a page (optimistic on the page detail). */
export function useDocsFollow(pageId: string): UseMutationResult<{ following: boolean }, ApiError, boolean, { prev: DocsPageDetail | undefined }> {
  const d = useEndpoints(docsPagesEndpoints);
  const qc = useQueryClient();
  const key = docsKeys.page(pageId);
  return useMutation<{ following: boolean }, ApiError, boolean, { prev: DocsPageDetail | undefined }>({
    mutationFn: (on) => (on ? d.follow(pageId) : d.unfollow(pageId)),
    onMutate: async (on) => {
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<DocsPageDetail>(key);
      if (prev) qc.setQueryData<DocsPageDetail>(key, { ...prev, following: on });
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(key, ctx.prev);
    },
  });
}

/** Views and viewers per day (people who can edit the page). */
export function useDocsAnalytics(pageId: string | null | undefined, days = 28, enabled = true): UseQueryResult<DocsAnalytics, ApiError> {
  const d = useEndpoints(docsPagesEndpoints);
  return useApiQuery(docsPagesKeys.analytics(pageId ?? "", days), ({ signal }) => d.analytics(pageId!, days, { signal }), { staleTime: 60_000 }, !!pageId && enabled);
}

/** Verify a page for `days` (0: until changed), or remove the verification (null). */
export function useDocsVerify(pageId: string): UseMutationResult<DocsPage, ApiError, number | null> {
  const d = useEndpoints(docsPagesEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsPage, ApiError, number | null>({
    mutationFn: (days) => (days === null ? d.unverify(pageId) : d.verify(pageId, days)),
    onSuccess: (page) => {
      qc.setQueryData<DocsPageDetail>(docsKeys.page(pageId), (old) => (old ? { ...old, page: { ...page, my_level: old.page.my_level } } : old));
      void qc.invalidateQueries({ queryKey: docsKeys.page(pageId) });
    },
  });
}

// ---- hooks: comments -------------------------------------------------------------

/** Add or remove the caller's reaction on a comment. */
export function useDocsReact(pageId: string): UseMutationResult<void, ApiError, { commentId: string; emoji: string; on: boolean }> {
  const d = useEndpoints(docsPagesEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { commentId: string; emoji: string; on: boolean }>({
    mutationFn: ({ commentId, emoji, on }) => (on ? d.react(commentId, emoji) : d.unreact(commentId, emoji)),
    onSettled: () => void qc.invalidateQueries({ queryKey: docsKeys.commentsAll(pageId) }),
  });
}

/** Quick reactions offered first. */
export const DOCS_QUICK_REACTIONS = ["👍", "❤️", "🎉", "😄", "👀", "✅"] as const;

// ---- hooks: publishing -----------------------------------------------------------

export function useDocsPublishSettings(pageId: string | null | undefined, enabled = true): UseQueryResult<DocsPublishSettings, ApiError> {
  const d = useEndpoints(docsPagesEndpoints);
  return useApiQuery(docsPagesKeys.publishSettings(pageId ?? ""), ({ signal }) => d.publishSettings(pageId!, { signal }), { staleTime: 30_000 }, !!pageId && enabled);
}

export function useDocsUpdatePublishSettings(pageId: string): UseMutationResult<DocsPublishSettings, ApiError, Partial<DocsPublishSettings>> {
  const d = useEndpoints(docsPagesEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsPublishSettings, ApiError, Partial<DocsPublishSettings>>({
    mutationFn: (body) => d.updatePublishSettings(pageId, body),
    onSuccess: (s) => qc.setQueryData(docsPagesKeys.publishSettings(pageId), s),
  });
}

/** Copy a published page into one of the caller's workspaces (when the publisher allows it). */
export function useDocsDuplicatePublic(token: string): UseMutationResult<DocsPage, ApiError, { org_id: string; page_id?: string | null }> {
  const d = useEndpoints(docsPagesEndpoints);
  const qc = useQueryClient();
  return useMutation<DocsPage, ApiError, { org_id: string; page_id?: string | null }>({
    mutationFn: (body) => d.duplicatePublic(token, body),
    onSuccess: (p) => invalidatePages(qc, p.org_id),
  });
}

// ---- hooks: workspace ------------------------------------------------------------

/** Quick find with filters; without a query, recently edited pages matching them. */
export function useDocsFind(orgId: string | null | undefined, params: DocsFindParams, enabled = true): UseQueryResult<DocsFindHit[], ApiError> {
  const d = useEndpoints(docsPagesEndpoints);
  const q = useDebouncedValue((params.q ?? "").trim(), 200);
  const p = { ...params, q };
  return useApiQuery(docsPagesKeys.find(orgId ?? "", p), async ({ signal }) => (await d.find(orgId!, p, { signal })).items, { staleTime: 10_000, placeholderData: (prev) => prev }, !!orgId && enabled);
}

/** The trash with filters (who trashed it, kind, the page it was in). */
export function useDocsTrashFiltered(orgId: string | null | undefined, params: DocsTrashParams, enabled = true): PagedQueryResult<DocsPageSummary> {
  const d = useEndpoints(docsPagesEndpoints);
  const q = useDebouncedValue((params.q ?? "").trim(), 250);
  const p = { ...params, q };
  return usePagedQuery(docsPagesKeys.trash(orgId ?? "", p), (cursor, signal) => d.trash(orgId!, { ...p, ...(cursor ? { cursor } : {}) }, { signal }), undefined, !!orgId && enabled);
}

/** Hide a teamspace from the caller's sidebar, or show it again. */
export function useDocsSetTeamspaceHidden(orgId: string): UseMutationResult<void, ApiError, { teamId: string; hidden: boolean }, { prev: DocsSidebar | undefined }> {
  const d = useEndpoints(docsPagesEndpoints);
  const qc = useQueryClient();
  return useMutation<void, ApiError, { teamId: string; hidden: boolean }, { prev: DocsSidebar | undefined }>({
    mutationFn: ({ teamId, hidden }) => d.setTeamspaceHidden(orgId, teamId, hidden),
    onSettled: () => void qc.invalidateQueries({ queryKey: docsKeys.sidebar(orgId) }),
  });
}
