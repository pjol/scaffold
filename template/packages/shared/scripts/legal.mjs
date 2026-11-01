#!/usr/bin/env node
/**
 * Legal documents.
 *
 *   node scripts/legal.mjs export [--out <dir>]   write terms.md + privacy.md (default: <repo>/docs/legal)
 *   node scripts/legal.mjs check                  fail when TERMS_VERSION / PRIVACY_VERSION differ from
 *                                                 backend/internal/legal/legal.go, or docs/legal/*.md is stale
 *
 * The TypeScript in src/legal is transpiled to CommonJS in a temp dir with the workspace
 * `typescript`, then required, so this runs on plain Node.
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const pkgDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repoDir = path.resolve(pkgDir, "..", "..");
const legalSrc = path.join(pkgDir, "src", "legal");
const goFile = path.join(repoDir, "backend", "internal", "legal", "legal.go");
const FILES = { terms: "terms.md", privacy: "privacy.md" };

function loadLegal() {
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "app-legal-"));
  try {
    for (const name of fs.readdirSync(legalSrc)) {
      if (!name.endsWith(".ts")) continue;
      const file = path.join(legalSrc, name);
      const code = ts.transpileModule(fs.readFileSync(file, "utf8"), {
        compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
        fileName: file,
      }).outputText;
      fs.writeFileSync(path.join(out, name.replace(/\.ts$/, ".js")), code);
    }
    return createRequire(import.meta.url)(path.join(out, "index.js"));
  } finally {
    fs.rmSync(out, { recursive: true, force: true });
  }
}

const [command = "check", ...rest] = process.argv.slice(2);
const legal = loadLegal();

if (command === "export") {
  const flag = rest.indexOf("--out");
  const outDir = flag >= 0 && rest[flag + 1] ? path.resolve(rest[flag + 1]) : path.join(repoDir, "docs", "legal");
  fs.mkdirSync(outDir, { recursive: true });
  for (const [kind, file] of Object.entries(FILES)) {
    const doc = legal.LEGAL_DOCUMENTS[kind];
    fs.writeFileSync(path.join(outDir, file), legal.legalDocumentToMarkdown(doc));
    console.log(`wrote ${path.relative(process.cwd(), path.join(outDir, file))} (${doc.title} ${doc.version}, ${doc.sections.length} sections)`);
  }
  process.exit(0);
}

if (command !== "check") {
  console.error(`unknown command "${command}" (use export or check)`);
  process.exit(2);
}

let failed = false;
const fail = (msg) => {
  failed = true;
  console.error(`✗ ${msg}`);
};

if (!fs.existsSync(goFile)) {
  console.log(`- legal: ${path.relative(repoDir, goFile)} not found; version check skipped`);
} else {
  const go = fs.readFileSync(goFile, "utf8");
  const goVersion = (name) => new RegExp(`${name}\\s*=\\s*"([^"]+)"`).exec(go)?.[1];
  for (const [name, ts_] of [
    ["TermsVersion", legal.TERMS_VERSION],
    ["PrivacyVersion", legal.PRIVACY_VERSION],
  ]) {
    const want = goVersion(name);
    if (!want) fail(`legal: ${name} not found in ${path.relative(repoDir, goFile)}`);
    else if (want !== ts_) fail(`legal: ${name} is "${want}" in Go but "${ts_}" in src/legal/versions.ts; bump both together`);
  }
  if (!failed) console.log(`✓ legal: versions match the backend (terms ${legal.TERMS_VERSION}, privacy ${legal.PRIVACY_VERSION})`);
}

for (const [kind, file] of Object.entries(FILES)) {
  const doc = legal.LEGAL_DOCUMENTS[kind];
  const expected = kind === "terms" ? legal.TERMS_VERSION : legal.PRIVACY_VERSION;
  if (doc.version !== expected) fail(`legal: ${kind} document version ${doc.version} differs from ${expected}`);
  const md = path.join(repoDir, "docs", "legal", file);
  if (fs.existsSync(md) && fs.readFileSync(md, "utf8") !== legal.legalDocumentToMarkdown(doc)) {
    fail(`legal: docs/legal/${file} is stale; run \`pnpm --filter @scaffoldapp/shared legal:export\``);
  }
  const open = legal.unfilledPlaceholders(doc);
  if (open.length) console.log(`- legal: ${kind} still has placeholders to fill before launch: ${open.join(", ")}`);
}

process.exit(failed ? 1 : 0);
