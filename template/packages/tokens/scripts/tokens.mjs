#!/usr/bin/env node
/**
 * `pnpm tokens`: regenerate src/generated/tokens.css and src/generated/palettes.json
 * from the TypeScript palettes. Commit the output.
 */
import fs from "node:fs";
import path from "node:path";
import { generatedFiles, loadTokens, pkgDir } from "./load.mjs";

const { tokens, cleanup } = loadTokens();
try {
  for (const [file, content] of Object.entries(generatedFiles(tokens))) {
    fs.mkdirSync(path.dirname(file), { recursive: true });
    const current = fs.existsSync(file) ? fs.readFileSync(file, "utf8") : null;
    if (current === content) {
      console.log("unchanged", path.relative(pkgDir, file));
    } else {
      fs.writeFileSync(file, content);
      console.log("wrote", path.relative(pkgDir, file));
    }
  }
} finally {
  cleanup();
}
