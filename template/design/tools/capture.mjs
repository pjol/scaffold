// Captures a design option's screens into <option>/shots and lints it for raw colours.
//
//   node design/tools/capture.mjs design/<option>             # every screen in the option
//   node design/tools/capture.mjs design/<option> home detail # only these screens
//   node design/tools/capture.mjs --one <file.html> <out.png> [--w 1440] [--h 900] [--theme dark] [--mobile] [--full] [--query palette=x]
//
// Screens are the option's *.html files except index.html (style guide) and logo.html (logo
// sheet), shot at desktop 1440×900 and phone 390×844, light and dark, reduced motion. A
// design/screens.json ({"desktopOnly": ["table"]}) limits some screens to desktop.
// Needs Playwright + Chromium once: pnpm add -Dw playwright && pnpm exec playwright install chromium
// (or set PLAYWRIGHT_MODULE to an existing install). Exits non-zero on page errors or raw colours.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

let chromium;
try {
  ({ chromium } = await import(process.env.PLAYWRIGHT_MODULE || "playwright"));
} catch {
  console.error("Playwright not found. Run: pnpm add -Dw playwright && pnpm exec playwright install chromium");
  process.exit(2);
}

const DOCS = ["index.html", "logo.html"];
const VIEW = {
  desktop: { width: 1440, height: 900, deviceScaleFactor: 1 },
  mobile: { width: 390, height: 844, deviceScaleFactor: 2, isMobile: true, hasTouch: true },
};

const LOCK = path.join(os.tmpdir(), "scaffold-design-capture.lock");
async function withLock(fn) {
  for (let i = 0; ; i++) {
    try { fs.mkdirSync(LOCK); break; } catch {
      try { if (Date.now() - fs.statSync(LOCK).mtimeMs > 180000) fs.rmSync(LOCK, { recursive: true, force: true }); } catch {}
      if (i % 20 === 0 && i) console.log("  waiting for another capture…");
      await new Promise((r) => setTimeout(r, 500));
    }
  }
  try { return await fn(); } finally { fs.rmSync(LOCK, { recursive: true, force: true }); }
}

async function shoot(browser, file, out, { view = "desktop", theme = "light", full = false, w, h, query = "" } = {}) {
  const v = { ...VIEW[view] };
  if (w) v.width = Number(w);
  if (h) v.height = Number(h);
  const ctx = await browser.newContext({
    viewport: { width: v.width, height: v.height }, deviceScaleFactor: v.deviceScaleFactor,
    isMobile: !!v.isMobile, hasTouch: !!v.hasTouch, colorScheme: theme, reducedMotion: "reduce",
  });
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(`pageerror: ${e.message}`));
  page.on("console", (m) => { if (m.type() === "error") errors.push(`console: ${m.text().slice(0, 240)}`); });
  page.on("requestfailed", (r) => errors.push(`requestfailed: ${r.url().slice(0, 160)}`));
  const url = pathToFileURL(path.resolve(file)).href + `?theme=${theme}&capture=1${query ? `&${query}` : ""}`;
  await page.goto(url, { waitUntil: "networkidle", timeout: 60000 });
  await page.evaluate(() => document.fonts && document.fonts.ready);
  await page.waitForTimeout(400);
  fs.mkdirSync(path.dirname(out), { recursive: true });
  await page.screenshot({ path: out, fullPage: full });
  await ctx.close();
  return errors;
}

// Raw colours outside tokens.css break "tokens only". The style guide and logo
// sheet print hex values on purpose; screens may not.
function lint(dir) {
  const files = fs.readdirSync(dir).filter((f) => /\.(html|css|js)$/.test(f) && !["tokens.css", ...DOCS].includes(f));
  const hits = [];
  for (const f of files) {
    fs.readFileSync(path.join(dir, f), "utf8").split("\n").forEach((line, i) => {
      const clean = line.replace(/&#\d+;|href="#[^"]*"|url\(#[^)]*\)/g, "");
      if (/#[0-9a-fA-F]{3,8}\b(?![^<]*>)/.test(clean) || /\b(rgba?|hsla?|oklch|oklab)\(\s*\d/.test(line))
        hits.push(`${f}:${i + 1}: ${line.trim().slice(0, 110)}`);
    });
  }
  return hits;
}

const args = process.argv.slice(2);
const opt = (n, d) => { const i = args.indexOf(`--${n}`); return i === -1 ? d : args[i + 1]; };
let failed = false;

await withLock(async () => {
  const browser = await chromium.launch({ headless: true });
  try {
    if (args[0] === "--one") {
      const errs = await shoot(browser, args[1], args[2], {
        view: args.includes("--mobile") ? "mobile" : "desktop", theme: opt("theme", "light"),
        full: args.includes("--full"), w: opt("w"), h: opt("h"), query: opt("query", ""),
      });
      errs.forEach((e) => console.error("  " + e));
      failed = errs.length > 0;
      console.log("saved", args[2]);
      return;
    }
    const dir = args[0];
    if (!dir || !fs.existsSync(dir)) { console.error("usage: capture.mjs design/<option> [screens…]"); failed = true; return; }
    let desktopOnly = [];
    try { desktopOnly = JSON.parse(fs.readFileSync(path.join(path.dirname(dir), "screens.json"), "utf8")).desktopOnly || []; } catch {}
    const only = args.slice(1).filter((a) => !a.startsWith("--"));
    const screens = fs.readdirSync(dir).filter((f) => f.endsWith(".html")).map((f) => f.slice(0, -5))
      .filter((s) => (only.length ? only.includes(s) : !DOCS.includes(`${s}.html`) || s === "logo"));
    for (const screen of screens) {
      const views = screen === "logo" || desktopOnly.includes(screen) ? ["desktop"] : ["desktop", "mobile"];
      for (const view of views) for (const theme of ["light", "dark"]) {
        const out = path.join(dir, "shots", `${screen}-${view}-${theme}.png`);
        const errs = await shoot(browser, path.join(dir, `${screen}.html`), out, { view, theme });
        console.log(`${errs.length ? "✗" : "✓"} ${path.relative(process.cwd(), out)}`);
        errs.forEach((e) => console.error("    " + e));
        if (errs.length) failed = true;
      }
    }
    const hits = lint(dir);
    if (hits.length) {
      console.error(`raw colours outside tokens.css (${hits.length}):`);
      hits.slice(0, 30).forEach((h) => console.error("  " + h));
      failed = true;
    }
  } finally {
    await browser.close();
  }
});
process.exit(failed ? 1 : 0);
