#!/usr/bin/env node
// Writes the app icon, Android adaptive/monochrome layers and splash marks from the active
// palette (EXPO_PUBLIC_PALETTE, else the tokens default) into assets/images/. Plain Node:
// the mark (a frame with a cross brace) is drawn with signed distance fields and encoded as
// PNG with zlib. Commit the output. Replace the PNGs with designed ones once the design
// process picks a logo; app.config.ts reads only the file names.
//
//   pnpm --filter @scaffoldapp/mobile icons
import { writeFileSync, mkdirSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { deflateSync } from "node:zlib";

const require = createRequire(import.meta.url);
const palettes = require("@scaffoldapp/tokens/palettes.json");
const requested = (process.env.EXPO_PUBLIC_PALETTE || "").trim().toLowerCase();
const palette = palettes.palettes[requested] ?? palettes.palettes[palettes.default];
const out = join(dirname(fileURLToPath(import.meta.url)), "..", "assets", "images");
mkdirSync(out, { recursive: true });

const SIZE = 1024;

function hex(color) {
  const m = /^#?([0-9a-f]{6})$/i.exec(color);
  if (!m) throw new Error(`not a #RRGGBB colour: ${color}`);
  const n = parseInt(m[1], 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

// Signed distances (negative inside), in pixels.
function roundBox(px, py, half, radius) {
  const qx = Math.abs(px) - half + radius;
  const qy = Math.abs(py) - half + radius;
  return Math.hypot(Math.max(qx, 0), Math.max(qy, 0)) + Math.min(Math.max(qx, qy), 0) - radius;
}

function segment(px, py, ax, ay, bx, by, halfWidth) {
  const pax = px - ax;
  const pay = py - ay;
  const bax = bx - ax;
  const bay = by - ay;
  const h = Math.max(0, Math.min(1, (pax * bax + pay * bay) / (bax * bax + bay * bay)));
  return Math.hypot(pax - bax * h, pay - bay * h) - halfWidth;
}

/** Coverage (0–1) of the mark at pixel (x, y); `scale` is the frame's half-size as a share of the canvas. */
function mark(x, y, scale) {
  const px = x + 0.5 - SIZE / 2;
  const py = y + 0.5 - SIZE / 2;
  const half = SIZE * scale;
  const stroke = half * 0.3;
  const frame = Math.abs(roundBox(px, py, half - stroke / 2, half * 0.28)) - stroke / 2;
  const inner = half - stroke;
  const brace = segment(px, py, -inner, inner, inner, -inner, stroke * 0.42);
  const inside = roundBox(px, py, half - stroke / 2, half * 0.28) < 0;
  const d = Math.min(frame, inside ? brace : Infinity);
  return Math.max(0, Math.min(1, 0.5 - d));
}

const CRC = new Int32Array(256).map((_, n) => {
  let c = n;
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c;
});
function crc32(buf) {
  let c = -1;
  for (const b of buf) c = CRC[(c ^ b) & 255] ^ (c >>> 8);
  return (c ^ -1) >>> 0;
}
function chunk(type, data) {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type, "ascii"), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body));
  return Buffer.concat([len, body, crc]);
}

/** Encode a PNG; `pixel(x, y)` returns [r, g, b] (opaque) or [r, g, b, a]. */
function png(file, alpha, pixel) {
  const channels = alpha ? 4 : 3;
  const raw = Buffer.alloc((SIZE * channels + 1) * SIZE);
  let o = 0;
  for (let y = 0; y < SIZE; y++) {
    raw[o++] = 0; // filter: none
    for (let x = 0; x < SIZE; x++) {
      const p = pixel(x, y);
      for (let i = 0; i < channels; i++) raw[o++] = p[i] ?? 255;
    }
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(SIZE, 0);
  ihdr.writeUInt32BE(SIZE, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = alpha ? 6 : 2; // RGBA : RGB
  const bytes = Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk("IHDR", ihdr), chunk("IDAT", deflateSync(raw, { level: 9 })), chunk("IEND", Buffer.alloc(0))]);
  writeFileSync(join(out, file), bytes);
  console.log(`wrote assets/images/${file} (${bytes.length} bytes)`);
}

const mix = (a, b, t) => a.map((v, i) => Math.round(v + (b[i] - v) * t));
const bg = hex(palette.primary.light);
const fg = hex(palette.primaryFg.light);

// iOS/App Store icon: opaque (no alpha channel), full bleed; the OS applies the mask.
png("icon.png", false, (x, y) => mix(bg, fg, mark(x, y, 0.24)));
// Android adaptive foreground: the mark inside the 66/108 safe zone; the background colour is set in app.config.ts.
png("adaptive-icon.png", true, (x, y) => [...fg, Math.round(255 * mark(x, y, 0.17))]);
// Android 13+ themed icon: the system tints this silhouette.
png("monochrome-icon.png", true, (x, y) => [255, 255, 255, Math.round(255 * mark(x, y, 0.17))]);
// Splash marks on the palette's splash backgrounds (light and dark).
const splashLight = hex(palette.primary.light);
const splashDark = hex(palette.primary.dark);
png("splash-icon.png", true, (x, y) => [...splashLight, Math.round(255 * mark(x, y, 0.3))]);
png("splash-icon-dark.png", true, (x, y) => [...splashDark, Math.round(255 * mark(x, y, 0.3))]);
