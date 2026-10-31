#!/usr/bin/env node
// dev-up's env generator. Node >= 18, no dependencies.
//
//   node scripts/dev-up/env.mjs write [--set K=V]... [--force K=V]... [--add K=V]...
//       Writes backend/.env, web/.env.local and mobile/.env (for each surface
//       directory that exists) and prints which providers are live.
//   node scripts/dev-up/env.mjs shell [--set|--force|--add K=V]...
//       Prints the computed map as KEY=value lines for dev-up's shell: project
//       identity, DEFAULT_PORT_BASE, the `dev`-tier settings and every runtime
//       value. Secret values are never printed: has_<KEY>=yes|no instead.
//
// Where a value comes from, lowest to highest:
//   1. scaffold.json -> env: the entry's `dev` value, else its `default`
//   2. --set K=V        computed by dev-up (URLs, CORS); .env.dev may override
//   3. .env.dev X       fills NEXT_PUBLIC_X / EXPO_PUBLIC_X when those exist
//   4. .env.dev K       overrides every tier that declares K (blank = not set)
//   5. --add K=V        comma-list union (ADMIN_EMAILS always keeps the dev admin)
//   6. --force K=V      owned by dev-up or a flag (ports, DATABASE_URL, APP_ENV, --s3,
//                       --redis, --lan, --public); a .env.dev value is ignored, with a note
//   7. --hook-env FILE  per-tier values from feature hooks (hook_env <tier> KEY VALUE):
//                       lines "tier<TAB>KEY<TAB>value"; a key the tier doesn't declare is
//                       still written (with a warning to declare it in scaffold.json)
// Values starting with ".data/" become absolute paths under the project root.
//
// .env.dev is parsed here, never `source`d by a shell, so its secrets reach
// only the runtime file of the tier that declares them.
//
// Exit 1 (nothing written) when validation fails: Clerk keys half-set, from
// different instances, or live keys; a required-always variable empty.

import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");
const SPEC_FILE = path.join(ROOT, "scaffold.json");
const DEV_FILE = path.join(ROOT, ".env.dev");
const RUN_DIR = path.join(ROOT, ".data", "run");
const BACKUP_DIR = path.join(ROOT, ".data", "env-backup");
const TARGETS = [
  { tier: "backend", dir: "backend", file: "backend/.env" },
  { tier: "web", dir: "web", file: "web/.env.local" },
  { tier: "mobile", dir: "mobile", file: "mobile/.env" },
];
const CLIENT_PREFIXES = ["NEXT_PUBLIC_", "EXPO_PUBLIC_"];
const CORE_GROUPS = new Set(["core", "database", "auth", "storage", "email", "observability", "dev", "hooks"]);

const tty = process.stdout.isTTY && !process.env.NO_COLOR;
const paint = (code, s) => (tty ? `\x1b[${code}m${s}\x1b[0m` : s);
const errors = [];
const warnings = [];
const err = (m) => errors.includes(m) || errors.push(m);
const warn = (m) => warnings.includes(m) || warnings.push(m);

// ---- inputs -----------------------------------------------------------------

function parseArgs(argv) {
  const out = { cmd: argv[0], set: new Map(), force: new Map(), add: new Map(), hook: new Map() };
  for (let i = 1; i < argv.length; i++) {
    const flag = argv[i];
    if (flag === "--hook-env" && i + 1 < argv.length) {
      readHookEnv(argv[++i], out.hook);
      continue;
    }
    const kind = { "--set": "set", "--force": "force", "--add": "add" }[flag];
    if (!kind || i + 1 >= argv.length) usage(`unknown or incomplete argument: ${flag}`);
    const pair = argv[++i];
    const eq = pair.indexOf("=");
    if (eq < 1) usage(`${flag} expects KEY=VALUE`);
    out[kind].set(pair.slice(0, eq), pair.slice(eq + 1));
  }
  return out;
}

// hook_env values: Map<tier, Map<key, value>>.
function readHookEnv(file, into) {
  let text = "";
  try {
    text = fs.readFileSync(file, "utf8");
  } catch (e) {
    usage(`cannot read --hook-env ${file}: ${e.message}`);
  }
  for (const line of text.split("\n")) {
    if (!line) continue;
    const [tier, key, ...rest] = line.split("\t");
    if (!["backend", "web", "mobile"].includes(tier) || !/^[A-Za-z_][A-Za-z0-9_]*$/.test(key || "")) continue;
    if (!into.has(tier)) into.set(tier, new Map());
    into.get(tier).set(key, rest.join("\t"));
  }
}

function usage(msg) {
  if (msg) process.stderr.write(`env.mjs: ${msg}\n`);
  process.stderr.write("usage: node scripts/dev-up/env.mjs write|shell [--set K=V] [--force K=V] [--add K=V] [--hook-env FILE]\n");
  process.exit(2);
}

// Minimal dotenv: KEY=value, `export ` prefix, '#' comments, single quotes
// (literal), double quotes (\n \" \\ escapes, may span lines).
function parseDotenv(text, label) {
  const vars = new Map();
  const lines = text.split(/\r?\n/);
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();
    if (!line || line.startsWith("#")) continue;
    const m = /^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/.exec(line);
    if (!m) {
      warn(`${label} line ${i + 1}: not KEY=value (ignored)`);
      continue;
    }
    const key = m[1];
    let raw = m[2];
    let value;
    if (raw.startsWith('"')) {
      let body = raw.slice(1);
      let end = closingQuote(body);
      while (end < 0 && i + 1 < lines.length) {
        body += "\n" + lines[++i];
        end = closingQuote(body);
      }
      if (end < 0) {
        warn(`${label}: ${key} has an unterminated double quote (ignored)`);
        continue;
      }
      value = body.slice(0, end).replace(/\\(n|"|\\|\$)/g, (_, c) => (c === "n" ? "\n" : c));
    } else if (raw.startsWith("'")) {
      const end = raw.indexOf("'", 1);
      if (end < 0) {
        warn(`${label}: ${key} has an unterminated single quote (ignored)`);
        continue;
      }
      value = raw.slice(1, end);
    } else {
      value = raw.replace(/\s+#.*$/, "").trim();
    }
    vars.set(key, value);
  }
  return vars;
}

function closingQuote(s) {
  for (let i = 0; i < s.length; i++) {
    if (s[i] === "\\") i++;
    else if (s[i] === '"') return i;
  }
  return -1;
}

function crc32(str) {
  let crc = 0xffffffff;
  for (const byte of Buffer.from(str)) {
    let c = (crc ^ byte) & 0xff;
    for (let k = 0; k < 8; k++) c = c & 1 ? (c >>> 1) ^ 0xedb88320 : c >>> 1;
    crc = (crc >>> 8) ^ c;
  }
  return (crc ^ 0xffffffff) >>> 0;
}

const nonEmpty = (v) => typeof v === "string" && v.trim() !== "";
const sha256 = (s) => crypto.createHash("sha256").update(s).digest("hex");

// ---- resolution -------------------------------------------------------------

function resolveAll(spec, devVars, args) {
  const entries = [...(spec.env || [])];
  const declared = new Set(entries.map((e) => e.name));
  // A hook may set a key its tier doesn't declare: write it anyway, flagged.
  for (const [tier, kv] of args.hook) {
    for (const key of kv.keys()) {
      if (entries.some((e) => e.name === key && e.tier === tier)) continue;
      warn(`hook_env ${tier} ${key}: not declared for ${tier} in scaffold.json (written anyway; declare it)`);
      entries.push({ name: key, tier, group: "hooks", secret: true, doc: "set by a dev-up hook" });
    }
  }
  const notedForce = new Set();
  const values = []; // { tier, entry, value }

  for (const entry of entries) {
    const n = entry.name;
    let v;
    if (entry.dev !== undefined && entry.dev !== null) v = String(entry.dev);
    else if (entry.default !== undefined && entry.default !== null) v = String(entry.default);
    if (args.set.has(n)) v = args.set.get(n);
    for (const p of CLIENT_PREFIXES) {
      if (n.startsWith(p) && nonEmpty(devVars.get(n.slice(p.length)))) v = devVars.get(n.slice(p.length));
    }
    if (nonEmpty(devVars.get(n))) v = devVars.get(n);
    if (args.add.has(n)) v = unionList(v, args.add.get(n));
    if (args.force.has(n)) {
      const f = args.force.get(n);
      if (nonEmpty(devVars.get(n)) && devVars.get(n) !== f && !notedForce.has(n)) {
        warn(`${n} in .env.dev is ignored: dev-up sets it (port block or a flag)`);
        notedForce.add(n);
      }
      v = f;
    }
    const hooked = args.hook.get(entry.tier);
    if (hooked && hooked.has(n)) v = hooked.get(n);
    if (typeof v === "string" && v.startsWith(".data/")) v = path.join(ROOT, v);
    values.push({ tier: entry.tier, entry, value: v });
  }

  // .env.dev keys nobody reads.
  for (const k of devVars.keys()) {
    if (declared.has(k)) continue;
    if (CLIENT_PREFIXES.some((p) => declared.has(p + k))) continue;
    warn(`${k} in .env.dev is not declared in scaffold.json, so it is written nowhere (add it to scaffold.json -> env)`);
  }
  return values;
}

function unionList(a, b) {
  const seen = new Set();
  const out = [];
  for (const item of `${a || ""},${b || ""}`.split(",")) {
    const t = item.trim();
    if (t && !seen.has(t.toLowerCase())) {
      seen.add(t.toLowerCase());
      out.push(t);
    }
  }
  return out.join(",");
}

function tierMap(values, tier) {
  const m = new Map();
  for (const r of values) if (r.tier === tier && !m.has(r.entry.name)) m.set(r.entry.name, r.value);
  return m;
}

// ---- validation -------------------------------------------------------------

async function validate(values, writing) {
  const be = tierMap(values, "backend");
  const web = tierMap(values, "web");
  const mob = tierMap(values, "mobile");

  for (const r of values) {
    if (!writing.has(r.tier)) continue;
    if (r.entry.required === "always" && !nonEmpty(r.value)) {
      err(`${r.entry.name} (${r.tier}) is required: set it in .env.dev`);
    }
  }

  const sk = be.get("CLERK_SECRET_KEY") || web.get("CLERK_SECRET_KEY") || "";
  const pks = [
    ["web", web.get("NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY")],
    ["mobile", mob.get("EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY")],
  ].filter(([, v]) => nonEmpty(v));
  const pk = pks.length ? pks[0][1] : "";
  if (pks.length === 2 && pks[0][1] !== pks[1][1]) {
    err("web and mobile Clerk publishable keys differ: they must come from the same Clerk instance (set CLERK_PUBLISHABLE_KEY once in .env.dev)");
  }
  for (const [, v] of pks) if (v.startsWith("pk_live_")) err("a pk_live_ Clerk key refuses to run in development: use the development instance's pk_test_ key");
  if (sk.startsWith("sk_live_")) err("an sk_live_ Clerk key refuses to run in development: use the development instance's sk_test_ key");
  if (nonEmpty(pk) !== nonEmpty(sk)) {
    err("Clerk keys are half set: set both CLERK_PUBLISHABLE_KEY and CLERK_SECRET_KEY in .env.dev, or neither (dev sign-in)");
  }
  let clerk = null;
  if (nonEmpty(pk) && nonEmpty(sk) && !errors.length) clerk = await clerkInstance(pk, sk);

  if (be.get("AUTH_DEV_BYPASS") === "false" && !nonEmpty(sk)) {
    err("AUTH_DEV_BYPASS=false needs CLERK_SECRET_KEY (or leave AUTH_DEV_BYPASS unset)");
  }
  if (be.get("AUTH_DEV_BYPASS") === "false") {
    for (const [tier, m, k] of [["web", web, "NEXT_PUBLIC_AUTH_DEV"], ["mobile", mob, "EXPO_PUBLIC_AUTH_DEV"]]) {
      if (writing.has(tier) && m.get(k) === "true") warn(`AUTH_DEV_BYPASS=false but ${k}=true: the ${tier} dev picker's tokens will be refused (set AUTH_DEV=false in .env.dev)`);
    }
  }
  return clerk;
}

// The publishable key encodes the instance's frontend API host; the secret key
// does not. Both instances' JWKS must share a key id. Best effort: offline is a
// warning, a mismatch is an error. A success is cached by key fingerprint.
async function clerkInstance(pk, sk) {
  let host = "";
  try {
    host = Buffer.from(pk.replace(/^pk_(test|live)_/, ""), "base64").toString("utf8").replace(/\$$/, "");
  } catch {
    host = "";
  }
  if (!/^[a-z0-9.-]+\.[a-z]{2,}$/i.test(host)) {
    err("CLERK_PUBLISHABLE_KEY does not look like a Clerk publishable key");
    return null;
  }
  const cache = path.join(RUN_DIR, "clerk-instance.ok");
  const fp = sha256(`${pk}\n${sk}`);
  try {
    if (fs.readFileSync(cache, "utf8").trim() === fp) return { host, verified: true };
  } catch {}
  if (typeof fetch !== "function") {
    warn("could not check that the Clerk keys share an instance (this Node has no fetch)");
    return { host, verified: false };
  }
  try {
    const signal = AbortSignal.timeout(5000);
    const [backend, frontend] = await Promise.all([
      fetch("https://api.clerk.com/v1/jwks", { headers: { Authorization: `Bearer ${sk}` }, signal }),
      fetch(`https://${host}/.well-known/jwks.json`, { signal }),
    ]);
    if (backend.status === 401 || backend.status === 403) {
      err("Clerk rejected CLERK_SECRET_KEY (HTTP " + backend.status + "): copy it again from the dashboard");
      return null;
    }
    if (!backend.ok || !frontend.ok) {
      warn(`could not check that the Clerk keys share an instance (HTTP ${backend.status}/${frontend.status})`);
      return { host, verified: false };
    }
    const a = new Set(((await backend.json()).keys || []).map((k) => k.kid));
    const b = ((await frontend.json()).keys || []).map((k) => k.kid);
    if (!b.some((k) => a.has(k))) {
      err(`CLERK_SECRET_KEY and CLERK_PUBLISHABLE_KEY belong to different Clerk instances (publishable key is for ${host})`);
      return null;
    }
    fs.mkdirSync(RUN_DIR, { recursive: true });
    fs.writeFileSync(cache, fp + "\n");
    return { host, verified: true };
  } catch {
    warn("could not reach Clerk to check that the keys share an instance (offline?)");
    return { host, verified: false };
  }
}

// ---- output -----------------------------------------------------------------

function formatValue(v) {
  if (/^[A-Za-z0-9_./:@,+=%~?&-]*$/.test(v)) return v;
  if (!v.includes("'") && !v.includes("\n")) return `'${v}'`;
  return `"${v.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\$/g, "\\$").replace(/\n/g, "\\n")}"`;
}

function render(values, tier) {
  // Grouped by `group` (in order of first appearance), declaration order within.
  const groups = new Map();
  const seen = new Set();
  for (const r of values) {
    if (r.tier !== tier || seen.has(r.entry.name)) continue;
    seen.add(r.entry.name);
    const g = r.entry.group || "other";
    if (!groups.has(g)) groups.set(g, []);
    groups.get(g).push(r);
  }
  const lines = [];
  for (const [g, rows] of groups) {
    lines.push("", `# --- ${g} ---`);
    for (const r of rows) lines.push(nonEmpty(r.value) ? `${r.entry.name}=${formatValue(r.value)}` : `# ${r.entry.name}=`);
  }
  const body = lines.join("\n").replace(/^\n/, "") + "\n";
  const header =
    "# Generated by scripts/dev-up/env.mjs from scaffold.json + .env.dev. Do not edit:\n" +
    "# put overrides in .env.dev at the project root and re-run scripts/dev-up/dev-up.sh.\n" +
    `# dev-up-hash: ${sha256(body)}\n\n`;
  return header + body;
}

// A file is "ours and untouched" when its recorded hash matches its body.
function handEdited(text) {
  const m = /^# dev-up-hash: ([0-9a-f]{64})\n\n/m.exec(text);
  if (!m) return true;
  return sha256(text.slice(m.index + m[0].length)) !== m[1];
}

function providers(values, clerk) {
  const be = tierMap(values, "backend");
  const web = tierMap(values, "web");
  const mob = tierMap(values, "mobile");
  const on = (k, m = be) => nonEmpty(m.get(k));
  const rel = (p) => (p && p.startsWith(ROOT + path.sep) ? path.relative(ROOT, p) : p);
  const rows = [];
  const pk = web.get("NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY") || mob.get("EXPO_PUBLIC_CLERK_PUBLISHABLE_KEY");
  if (nonEmpty(pk) && on("CLERK_SECRET_KEY")) {
    rows.push(["auth", "live", `Clerk ${clerk?.host ? `(${clerk.host}${clerk.verified ? "" : ", instance not verified"})` : ""}` + (be.get("AUTH_DEV_BYPASS") === "true" ? " + dev sign-in" : "")]);
  } else {
    rows.push(["auth", "dev", "dev sign-in only (no Clerk keys)"]);
  }
  rows.push(on("S3_BUCKET") ? ["storage", "live", `S3 bucket ${be.get("S3_BUCKET")}${on("S3_ENDPOINT") ? ` at ${be.get("S3_ENDPOINT")}` : ""}`] : ["storage", "dev", `local disk (${rel(be.get("LOCAL_UPLOAD_DIR")) || "uploads"})`]);
  rows.push(on("RESEND_API_KEY") ? ["mail", "live", "Resend (real mail is sent)"] : ["mail", "dev", on("MAIL_SINK_DIR") ? `mail sink (${rel(be.get("MAIL_SINK_DIR"))})` : "logged"]);
  rows.push(on("REDIS_URL") ? ["realtime", "live", "Redis"] : ["realtime", "dev", "in-memory"]);

  // Feature groups: live when every secret the group declares is set.
  const groups = new Map();
  for (const r of values) {
    const g = r.entry.group;
    if (!g || CORE_GROUPS.has(g) || !r.entry.secret || r.tier === "dev") continue;
    if (!groups.has(g)) groups.set(g, { set: [], unset: [] });
    groups.get(g)[nonEmpty(r.value) ? "set" : "unset"].push(r.entry.name);
  }
  for (const [g, s] of groups) {
    rows.push(s.unset.length ? [g, "dev", `fallback (${s.unset.join(", ")} unset)`] : [g, "live", `${s.set.join(", ")} set`]);
  }
  return rows;
}

// ---- main -------------------------------------------------------------------

const args = parseArgs(process.argv.slice(2));
if (!["write", "shell"].includes(args.cmd)) usage();

let spec;
try {
  spec = JSON.parse(fs.readFileSync(SPEC_FILE, "utf8"));
} catch (e) {
  process.stderr.write(`env.mjs: cannot read scaffold.json: ${e.message}\n`);
  process.exit(1);
}
const devVars = fs.existsSync(DEV_FILE) ? parseDotenv(fs.readFileSync(DEV_FILE, "utf8"), ".env.dev") : new Map();
const values = resolveAll(spec, devVars, args);
const slug = spec.slug || "app";

if (args.cmd === "shell") {
  const out = [];
  const emitted = new Set();
  const emit = (k, v) => {
    if (emitted.has(k)) return;
    emitted.add(k);
    out.push(`${k}=${String(v ?? "").replace(/\n/g, "\\n")}`);
  };
  emit("SLUG", slug);
  emit("APP_NAME", spec.name || slug);
  emit("BUNDLE_ID", spec.bundle_id || "");
  emit("DEFAULT_PORT_BASE", 20000 + (crc32(slug) % 999) * 10);
  for (const r of values) {
    const n = r.entry.name;
    if (r.entry.secret) emit(`has_${n}`, nonEmpty(r.value) ? "yes" : "no");
    else emit(n, r.value);
  }
  for (const w of warnings) process.stderr.write(paint("33", `  ! ${w}`) + "\n");
  process.stdout.write(out.join("\n") + "\n");
  process.exit(0);
}

// write
const writing = new Set(TARGETS.filter((t) => fs.existsSync(path.join(ROOT, t.dir))).map((t) => t.tier));
const clerk = await validate(values, writing);
for (const w of warnings) process.stderr.write(paint("33", `  ! ${w}`) + "\n");
if (errors.length) {
  for (const e of errors) process.stderr.write(paint("31", `  x ${e}`) + "\n");
  process.stderr.write(paint("31", "  env not written: fix .env.dev and re-run") + "\n");
  process.exit(1);
}

for (const t of TARGETS) {
  if (!writing.has(t.tier)) continue;
  const file = path.join(ROOT, t.file);
  const next = render(values, t.tier);
  if (fs.existsSync(file)) {
    const current = fs.readFileSync(file, "utf8");
    if (current !== next && handEdited(current)) {
      fs.mkdirSync(BACKUP_DIR, { recursive: true });
      const backup = path.join(BACKUP_DIR, t.file.replace(/\//g, "") + ".dev-up-backup");
      fs.writeFileSync(backup, current, { mode: 0o600 });
      console.log(paint("33", `  ${t.file} was edited by hand: kept a copy in ${path.relative(ROOT, backup)}`));
    }
  }
  fs.writeFileSync(file, next, { mode: 0o600 });
  console.log(`  wrote ${t.file}`);
}

const rows = providers(values, clerk);
const lines = rows.map(([name, state, what]) => `  ${name.padEnd(9)} ${state === "live" ? "live" : "dev "}  ${what}`);
fs.mkdirSync(RUN_DIR, { recursive: true });
fs.writeFileSync(path.join(RUN_DIR, "providers.txt"), lines.join("\n") + "\n");
for (const l of lines) console.log(l.includes(" live ") ? paint("32", l) : l);
