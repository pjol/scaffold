/**
 * Connected repositories (GitHub, GitLab) share every screen with the app's own; these
 * helpers tell them apart where it matters.
 */
import type { ReposRepo, ReposSupports } from "./types";

/** What the app's own repositories support (they send no `supports`). */
const APP_SUPPORTS: ReposSupports = {
  settings: true,
  credentials: true,
  upload: true,
  blame: true,
  resolve_threads: true,
  request_changes: true,
  reviewers: true,
  realtime: true,
  checks: false,
  code_search_refs: true,
  tree_commits: true,
  import: false,
};

/** Whether a repository's host offers something (`reposSupports(repo, "blame")`). */
export function reposSupports(repo: Pick<ReposRepo, "supports">, key: keyof ReposSupports): boolean {
  return (repo.supports ?? APP_SUPPORTS)[key];
}

/** True for a connected repository's id (`github~…`, `gitlab~…`). */
export function reposIsConnectedId(id: string): boolean {
  return /^(github|gitlab)~/.test(id);
}

/** "GitHub" or "GitLab" for a connected repository, else null. */
export function reposPlatformName(repo: Pick<ReposRepo, "source">): string | null {
  const s = repo.source;
  if (!s || s.type !== "connected") return null;
  return s.provider === "gitlab" ? "GitLab" : s.provider === "github" ? "GitHub" : s.provider;
}

/** "Pull request" on GitHub and here; "Merge request" on GitLab. */
export function reposPullNoun(repo: Pick<ReposRepo, "source">, plural = false): string {
  const gl = repo.source?.type === "connected" && repo.source.provider === "gitlab";
  return gl ? (plural ? "Merge requests" : "Merge request") : plural ? "Pull requests" : "Pull request";
}

/** Where people manage access to a connected repository on its platform. */
export function reposPlatformAccessUrl(repo: Pick<ReposRepo, "source" | "web_url">): string | null {
  if (!repo.web_url || repo.source?.type !== "connected") return null;
  return repo.source.provider === "gitlab" ? `${repo.web_url}/-/project_members` : `${repo.web_url}/settings/access`;
}
