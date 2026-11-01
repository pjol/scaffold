#!/usr/bin/env node
/**
 * `pnpm check` (tokens part). Fails when:
 * - any palette misses a WCAG contrast target (7 body text / 4.5 text / 3 UI) in either mode,
 * - a line height, spacing or size value leaves the 4px grid,
 * - a theme.css name completes an existing Tailwind class (`--spacing-block` → `inline-block`),
 * - src/generated/* is stale (run `pnpm tokens`).
 *
 *   node scripts/check.mjs            audit + staleness
 *   node scripts/check.mjs --report   also print every contrast ratio as a Markdown table
 */
import fs from "node:fs";
import path from "node:path";
import { generatedFiles, loadTokens, pkgDir, srcDir } from "./load.mjs";
import { tailwindCollisions } from "./tailwind-names.mjs";

const report = process.argv.includes("--report");
const { tokens, cleanup } = loadTokens();
let failed = false;

try {
  for (const id of tokens.PALETTE_IDS) {
    const palette = tokens.getPalette(id);
    const rows = tokens.auditPalette(palette);
    const bad = rows.filter((r) => !r.pass);
    const margin = rows.reduce((m, r) => Math.min(m, r.ratio / r.min), Infinity);
    if (report) {
      console.log(`\n#### ${palette.name}\n\n| Mode | Pair | Ratio | Min | |\n|---|---|---|---|---|`);
      for (const r of rows) console.log(`| ${r.mode} | ${r.label} | ${tokens.formatRatio(r.ratio)} | ${r.min} | ${r.pass ? "pass" : "FAIL"} |`);
    }
    if (bad.length) {
      failed = true;
      console.error(`✗ ${id}: ${bad.length}/${rows.length} contrast checks failed`);
      for (const f of bad) console.error(`   ${f.mode.padEnd(5)} ${f.label}: ${tokens.formatRatio(f.ratio)} < ${f.min}  (${f.fg} on ${f.bg})`);
    } else {
      console.log(`✓ ${id}: ${rows.length} contrast checks pass (tightest margin ×${margin.toFixed(2)})`);
    }
    const rhythm = tokens.rhythmIssues(palette);
    if (rhythm.length) {
      failed = true;
      console.error(`✗ ${id}: ${rhythm.length} values off the 4px grid`);
      for (const issue of rhythm) console.error(`   ${issue}`);
    } else {
      console.log(`✓ ${id}: line heights, spacing and sizes on the 4px grid`);
    }
  }

  const names = await tailwindCollisions(fs.readFileSync(path.join(srcDir, "theme.css"), "utf8"), path.resolve(pkgDir, "../../web"));
  if (names.skipped) {
    console.warn(`! Tailwind name check skipped: ${names.skipped}`);
  } else if (names.collisions.length) {
    failed = true;
    console.error(`✗ ${names.collisions.length} theme names complete existing Tailwind classes; rename them`);
    for (const c of names.collisions) console.error(`   ${c}`);
  } else {
    console.log(`✓ theme names clash with no Tailwind class (${names.checked} checked)`);
  }

  for (const [file, content] of Object.entries(generatedFiles(tokens))) {
    const rel = path.relative(pkgDir, file);
    const current = fs.existsSync(file) ? fs.readFileSync(file, "utf8") : "";
    if (current !== content) {
      failed = true;
      console.error(`✗ ${rel} is stale: run \`pnpm tokens\``);
    } else {
      console.log(`✓ ${rel} is current`);
    }
  }
} finally {
  cleanup();
}

process.exit(failed ? 1 : 0);
