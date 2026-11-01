/**
 * Theme names that complete an existing Tailwind class. A theme variable `--<ns>-<name>`
 * makes `<root>-<name>` valid for every utility reading that namespace; when that class
 * already exists, Tailwind emits both rules and the token quietly restyles it
 * (`--spacing-block` gave every `inline-block` `inline-size: 1.5rem`; `--spacing-md`
 * hijacks `max-w-md`).
 *
 * Uses the web app's Tailwind: its class list finds the roots each namespace feeds, then
 * each `<root>-<name>` that compiles under Tailwind's default theme is compiled again
 * with this package's names added. Any difference is a collision. Overriding one of
 * Tailwind's own keys (`--radius-md`) is intended and not checked.
 */
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";

/** Tailwind v4 theme namespaces that feed utilities, longest first for prefix matching. */
const NAMESPACES = [
  "inset-shadow", "drop-shadow", "text-shadow", "font-weight", "perspective", "container",
  "tracking", "leading", "spacing", "animate", "radius", "shadow", "aspect", "color", "text",
  "font", "blur", "ease",
];

const PROBE = "zzprobe";

/** `--name` declarations at the top level of every `@theme` block (wildcard resets skipped). */
export function themeNames(css) {
  const names = [];
  for (const m of css.matchAll(/@theme\b[^{]*\{/g)) {
    let depth = 1;
    let i = m.index + m[0].length;
    let line = "";
    for (; i < css.length && depth > 0; i++) {
      const ch = css[i];
      if (ch === "{") depth++;
      else if (ch === "}") depth--;
      else if (ch === ";" || ch === "\n") {
        const d = depth === 1 && /^\s*(--[a-z0-9-]+)\s*:/i.exec(line);
        if (d && !d[1].endsWith("-*")) names.push(d[1]);
        line = "";
        continue;
      }
      line += ch;
    }
  }
  return names;
}

/** `--spacing-gap` → { ns: "spacing", key: "gap" }; null for sub-properties and other variables. */
function split(variable) {
  for (const ns of NAMESPACES) {
    const prefix = `--${ns}-`;
    if (variable.startsWith(prefix)) {
      const key = variable.slice(prefix.length);
      return key && !key.includes("--") ? { ns, key } : null;
    }
  }
  return null;
}

/**
 * @param {string} themeCss  this package's theme.css
 * @param {string} webDir    the web app whose `tailwindcss` to use
 * @returns {Promise<{ skipped?: string, checked: number, collisions: string[] }>}
 */
export async function tailwindCollisions(themeCss, webDir) {
  let require, tw;
  try {
    require = createRequire(path.join(webDir, "package.json"));
    tw = require("tailwindcss");
  } catch {
    return { skipped: "tailwindcss isn't installed in web/ (run pnpm install)", checked: 0, collisions: [] };
  }
  if (typeof tw.__unstable__loadDesignSystem !== "function") {
    return { skipped: "this Tailwind has no design-system API; update scripts/tailwind-names.mjs", checked: 0, collisions: [] };
  }
  const loadStylesheet = async (id, base) => {
    const file = require.resolve(id);
    return { path: file, base, content: fs.readFileSync(file, "utf8") };
  };
  const load = (theme) =>
    tw.__unstable__loadDesignSystem(`@import "tailwindcss/theme.css";\n@import "tailwindcss/utilities.css";\n@theme {\n${theme}\n}\n`, {
      base: webDir,
      loadStylesheet,
    });

  const defaults = new Set(themeNames(fs.readFileSync(require.resolve("tailwindcss/theme.css"), "utf8")));
  const ours = themeNames(themeCss)
    .filter((v) => !defaults.has(v))
    .map((v) => ({ variable: v, ...split(v) }))
    .filter((t) => t.ns);
  const value = (ns) => (ns === "color" ? "#000" : "1px");
  const namespaces = [...new Set(ours.map((t) => t.ns))];

  // Roots per namespace: the classes a probe name produces.
  const probe = await load(namespaces.map((ns) => `--${ns}-${PROBE}-${ns}: ${value(ns)};`).join("\n"));
  const roots = Object.fromEntries(namespaces.map((ns) => [ns, []]));
  for (const [name] of probe.getClassList()) {
    for (const ns of namespaces) {
      const suffix = `-${PROBE}-${ns}`;
      if (name.endsWith(suffix)) roots[ns].push(name.slice(0, -suffix.length));
    }
  }

  const before = await load("");
  const after = await load(ours.map((t) => `${t.variable}: ${value(t.ns)};`).join("\n"));
  const collisions = [];
  let checked = 0;
  for (const t of ours) {
    const classes = roots[t.ns].map((root) => `${root}-${t.key}`);
    checked += classes.length;
    const was = before.candidatesToCss(classes);
    const now = after.candidatesToCss(classes);
    classes.forEach((cls, i) => {
      if (was[i] !== null && was[i] !== now[i]) collisions.push(`${t.variable} changes Tailwind's \`${cls}\``);
    });
  }
  return { checked, collisions };
}
