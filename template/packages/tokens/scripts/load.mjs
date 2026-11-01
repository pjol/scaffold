/**
 * Load this package's TypeScript source in plain Node: transpile to CommonJS in a temp
 * dir with the workspace `typescript`, then require it. No build step, no test runner.
 */
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const here = path.dirname(fileURLToPath(import.meta.url));
export const pkgDir = path.resolve(here, "..");
export const srcDir = path.join(pkgDir, "src");

function walk(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) return walk(p);
    return e.name.endsWith(".ts") && !e.name.endsWith(".test.ts") ? [p] : [];
  });
}

/** @returns {{ tokens: any, cleanup: () => void }} */
export function loadTokens() {
  const out = fs.mkdtempSync(path.join(os.tmpdir(), "app-tokens-"));
  for (const file of walk(srcDir)) {
    const rel = path.relative(srcDir, file).replace(/\.ts$/, ".js");
    const code = ts.transpileModule(fs.readFileSync(file, "utf8"), {
      compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true },
      fileName: file,
    }).outputText;
    fs.mkdirSync(path.dirname(path.join(out, rel)), { recursive: true });
    fs.writeFileSync(path.join(out, rel), code);
  }
  const require = createRequire(import.meta.url);
  const tokens = require(path.join(out, "index.js"));
  return { tokens, cleanup: () => fs.rmSync(out, { recursive: true, force: true }) };
}

/** The generated files and their expected contents. */
export function generatedFiles(tokens) {
  return {
    [path.join(srcDir, "generated", "tokens.css")]: tokens.tokensStylesheet(),
    [path.join(srcDir, "generated", "palettes.json")]: `${JSON.stringify(tokens.paletteManifest(), null, 2)}\n`,
  };
}
