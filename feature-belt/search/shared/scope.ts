/**
 * Org scope for search, wired by the app (search doesn't depend on orgs): a hook that
 * returns the org people are working in. With one registered, the palette, results page
 * and mobile screen search that org by default (`org_id`) and offer a toggle to search
 * every org. Register at module load, before anything renders:
 *
 *   registerSearchScope(() => {
 *     const { org } = useActiveOrg();
 *     return org ? { id: org.id, name: org.name, allLabel: `All ${orgsNoun("many")}` } : null;
 *   });
 */
export interface SearchScope {
  /** Sent as `org_id`. */
  id: string;
  /** The org's name, on the toggle. */
  name: string;
  /** The toggle that searches every org: "All workspaces". */
  allLabel: string;
}

let useScope: () => SearchScope | null = () => null;

/** Sets the hook that names the current org scope (called on every render of a search screen). */
export function registerSearchScope(hook: () => SearchScope | null): void {
  useScope = hook;
}

/** The org search is scoped to by default, or null when the app registered none. */
export function useSearchScope(): SearchScope | null {
  return useScope();
}
