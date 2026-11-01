#!/usr/bin/env node
/**
 * Guards the "runs in browsers AND React Native (Hermes)" rule: library code under src/
 * must not touch DOM globals, Node builtins, React DOM, or APIs Hermes lacks. The
 * tsconfig has no Node types, so tsc already catches most Node globals; this catches
 * the rest. Exit 1 on any finding.
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const srcDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "src");

const NODE_BUILTINS = "fs|path|os|crypto|child_process|http|https|http2|net|tls|dns|zlib|stream|buffer|util|events|url|querystring|worker_threads|process|readline|vm|assert|perf_hooks";

const FORBIDDEN = [
  [/\bwindow\./, "window (DOM-only)"],
  [/\bdocument\./, "document (DOM-only; take it as a parameter)"],
  [/\b(localStorage|sessionStorage)\b/, "web storage (take a storage adapter as a parameter)"],
  [/\bnavigator\./, "navigator"],
  [/from\s+["']node:|require\(\s*["']node:|import\(\s*["']node:/, "Node builtins"],
  [new RegExp(`from\\s+["'](${NODE_BUILTINS})(/[^"']*)?["']|require\\(\\s*["'](${NODE_BUILTINS})["']`), "Node builtins"],
  [/\bprocess\.|\bBuffer\./, "Node globals (pass values in from the app)"],
  [/from\s+["']react-dom/, "React DOM"],
  [/\bstructuredClone\s*\(/, "structuredClone (not in Hermes)"],
  [/Intl\.(RelativeTimeFormat|ListFormat|PluralRules|Segmenter)/, "Intl APIs Hermes lacks"],
  [/new\s+URL(SearchParams)?\s*\(/, "URL/URLSearchParams (incomplete in React Native)"],
  [/\bTextDecoder\b/, "TextDecoder (not in Hermes)"],
  [/AbortSignal\.(timeout|any)\s*\(/, "AbortSignal.timeout/any (not in Hermes)"],
  [/\bcrypto\.randomUUID\s*\(/, "crypto.randomUUID without fallback (use generateId)"],
  [/\.findLast(Index)?\s*\(/, "Array#findLast (ES2023)"],
  [/Object\.hasOwn\s*\(/, "Object.hasOwn (use Object.prototype.hasOwnProperty.call)"],
];

function stripComments(source) {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:"'`])\/\/.*$/gm, "$1");
}

function walk(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) return e.name === "__tests__" || e.name === "test" ? [] : walk(p);
    return /\.tsx?$/.test(e.name) && !/\.test\.tsx?$/.test(e.name) ? [p] : [];
  });
}

const files = walk(srcDir);
const violations = [];
for (const file of files) {
  const code = stripComments(fs.readFileSync(file, "utf8"));
  const lines = code.split("\n");
  for (const [pattern, why] of FORBIDDEN) {
    const line = lines.findIndex((l) => pattern.test(l));
    if (line >= 0) violations.push(`${path.relative(srcDir, file)}:${line + 1}: ${why}`);
  }
}

if (files.length < 20) {
  console.error(`✗ portability: only ${files.length} files found under src/; is the path right?`);
  process.exit(1);
}
if (violations.length) {
  console.error(`✗ portability: ${violations.length} problem(s) in library code`);
  for (const v of violations) console.error(`   src/${v}`);
  process.exit(1);
}
console.log(`✓ portability: ${files.length} files are web + Hermes safe`);
