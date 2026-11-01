/** Small formatting helpers the repository screens share on web and mobile. */

/** The first 7 characters of a commit id. */
export function reposShortSha(sha: string): string {
  return sha.slice(0, 7);
}

/** "12 KB", "3.4 MB". */
export function reposFormatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1).replace(/\.0$/, "")} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(1).replace(/\.0$/, "")} GB`;
}

const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** "5 minutes ago", "yesterday", "3 days ago"; the date after 30 days ("Oct 3, 2026"). */
export function reposRelativeTime(iso: string, now = Date.now()): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return iso;
  const s = Math.round((now - t) / 1000);
  if (s < 45) return "just now";
  const unit = (n: number, one: string) => `${n} ${one}${n === 1 ? "" : "s"} ago`;
  if (s < 3600) return unit(Math.max(1, Math.round(s / 60)), "minute");
  if (s < 86_400) return unit(Math.round(s / 3600), "hour");
  const days = Math.round(s / 86_400);
  if (days === 1) return "yesterday";
  if (days < 30) return unit(days, "day");
  const d = new Date(t);
  return `${months[d.getMonth()]} ${d.getDate()}, ${d.getFullYear()}`;
}

/** "1 commit", "3 commits". */
export function reposCount(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`;
}

/** The folder part of a path ("" at the root). */
export function reposDirname(path: string): string {
  const i = path.lastIndexOf("/");
  return i < 0 ? "" : path.slice(0, i);
}

/** The last part of a path. */
export function reposBasename(path: string): string {
  const i = path.lastIndexOf("/");
  return i < 0 ? path : path.slice(i + 1);
}

/** The language id the API would give a path (for new files in the editor). */
export function reposLanguageOf(path: string): string {
  const base = reposBasename(path).toLowerCase();
  if (base === "dockerfile") return "dockerfile";
  if (base === "makefile") return "makefile";
  const ext = base.includes(".") ? base.slice(base.lastIndexOf(".") + 1) : "";
  return languageByExt[ext] ?? "plaintext";
}

const languageByExt: Record<string, string> = {
  go: "go", js: "javascript", mjs: "javascript", cjs: "javascript", jsx: "javascript", ts: "typescript", tsx: "typescript",
  py: "python", rb: "ruby", rs: "rust", java: "java", kt: "kotlin", swift: "swift", c: "c", h: "c", cpp: "cpp", cc: "cpp",
  cs: "csharp", php: "php", sh: "bash", bash: "bash", json: "json", yml: "yaml", yaml: "yaml", toml: "ini", ini: "ini",
  md: "markdown", html: "xml", xml: "xml", svg: "xml", css: "css", scss: "scss", sql: "sql", diff: "diff",
};

/** The steps to resolve a pull request's conflicts locally, as shell commands. */
export function reposResolveSteps(cloneUrl: string, base: string, head: string): string {
  return [`git clone ${cloneUrl}`, `cd ${cloneUrl.split("/").pop()?.replace(/\.git$/, "") ?? "repo"}`, `git checkout ${head}`, `git merge origin/${base}`, "# fix the conflicts, then", "git commit -a", `git push origin ${head}`].join("\n");
}
