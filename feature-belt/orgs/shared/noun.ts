/**
 * What the product calls an organization in copy: "organization" by default, or "team",
 * "club", "workspace"… (orgs stitching step 1). API names, routes and types keep `org`.
 * Call `configureOrgsNoun` once at module load in the app's stitching, before anything
 * renders; every orgs screen and the features built on orgs read it when they render.
 * Portable (web, mobile and server components).
 */
export interface OrgsNounConfig {
  /** Singular, lower case: "workspace". */
  one: string;
  /** Plural, lower case: "workspaces". */
  many: string;
  /** Singular for titles and sentence starts. Default: `one` with a capital first letter. */
  One?: string;
  /** Plural for titles and sentence starts. Default: `many` with a capital first letter. */
  Many?: string;
  /** Indefinite article for `one`. Default: "an" before a, e, i, o, else "a". */
  article?: "a" | "an";
}

const capital = (s: string) => s.charAt(0).toUpperCase() + s.slice(1);

let noun = { one: "organization", many: "organizations", One: "Organization", Many: "Organizations", article: "an" };

/** Names organizations in every orgs-based screen: `configureOrgsNoun({ one: "workspace", many: "workspaces" })`. */
export function configureOrgsNoun(next: OrgsNounConfig): void {
  const one = next.one.trim().toLowerCase();
  const many = next.many.trim().toLowerCase();
  if (!one || !many) throw new Error("configureOrgsNoun needs `one` and `many`");
  noun = {
    one,
    many,
    One: next.One?.trim() || capital(one),
    Many: next.Many?.trim() || capital(many),
    article: next.article ?? (/^[aeio]/.test(one) ? "an" : "a"),
  };
}

/** Lower case: `orgsNoun()` → "workspace", `orgsNoun("many")` → "workspaces". */
export function orgsNoun(count: "one" | "many" = "one"): string {
  return count === "many" ? noun.many : noun.one;
}

/** Capitalised, for titles and sentence starts: "Workspace", `orgsNounTitle("many")` → "Workspaces". */
export function orgsNounTitle(count: "one" | "many" = "one"): string {
  return count === "many" ? noun.Many : noun.One;
}

/** With its article, lower case: "a workspace", "an organization". */
export function orgsNounA(): string {
  return `${noun.article} ${noun.one}`;
}
