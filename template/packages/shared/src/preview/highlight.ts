/**
 * Syntax highlighting for `FilePreview` on web and native: highlight.js (BSD-3) tokenises and
 * this turns its markup into plain token lines each platform renders with its own text
 * components (no HTML injection). Import it lazily (`import("@scaffoldapp/shared/preview/highlight")`)
 * so the highlighter only loads when a code file opens.
 */
import hljs from "highlight.js/lib/core";
import bash from "highlight.js/lib/languages/bash";
import c from "highlight.js/lib/languages/c";
import cpp from "highlight.js/lib/languages/cpp";
import csharp from "highlight.js/lib/languages/csharp";
import css from "highlight.js/lib/languages/css";
import diff from "highlight.js/lib/languages/diff";
import dockerfile from "highlight.js/lib/languages/dockerfile";
import go from "highlight.js/lib/languages/go";
import ini from "highlight.js/lib/languages/ini";
import java from "highlight.js/lib/languages/java";
import javascript from "highlight.js/lib/languages/javascript";
import json from "highlight.js/lib/languages/json";
import kotlin from "highlight.js/lib/languages/kotlin";
import makefile from "highlight.js/lib/languages/makefile";
import markdown from "highlight.js/lib/languages/markdown";
import php from "highlight.js/lib/languages/php";
import python from "highlight.js/lib/languages/python";
import ruby from "highlight.js/lib/languages/ruby";
import rust from "highlight.js/lib/languages/rust";
import scss from "highlight.js/lib/languages/scss";
import sql from "highlight.js/lib/languages/sql";
import swift from "highlight.js/lib/languages/swift";
import typescript from "highlight.js/lib/languages/typescript";
import xml from "highlight.js/lib/languages/xml";
import yaml from "highlight.js/lib/languages/yaml";

const languages: Record<string, Parameters<typeof hljs.registerLanguage>[1]> = {
  bash, c, cpp, csharp, css, diff, dockerfile, go, ini, java, javascript, json, kotlin, makefile, markdown,
  php, python, ruby, rust, scss, sql, swift, typescript, xml, yaml,
};
for (const [name, lang] of Object.entries(languages)) if (!hljs.getLanguage(name)) hljs.registerLanguage(name, lang);

/** One run of text and its colour role. */
export interface FilePreviewToken {
  text: string;
  role: FilePreviewTokenRole;
}

/** Colour roles; each platform maps them onto semantic colour tokens so a restyle recolours code. */
export type FilePreviewTokenRole = "keyword" | "string" | "number" | "comment" | "function" | "type" | "attr" | "meta" | "deletion" | "addition" | "plain";

/** Above these, files render as plain lines (highlighting would stall the screen). */
const MAX_CHARS = 400_000;
const MAX_LINES = 10_000;

const ENTITIES: Record<string, string> = { "&amp;": "&", "&lt;": "<", "&gt;": ">", "&quot;": '"', "&#x27;": "'", "&#39;": "'" };
const decode = (s: string) => s.replace(/&(?:amp|lt|gt|quot|#x27|#39);/g, (m) => ENTITIES[m] ?? m);

function role(scope: string | undefined): FilePreviewTokenRole {
  switch ((scope ?? "").split(".")[0]) {
    case "keyword":
    case "selector-tag":
    case "literal":
    case "built_in":
      return "keyword";
    case "string":
    case "regexp":
    case "char":
    case "symbol":
    case "template-tag":
      return "string";
    case "number":
      return "number";
    case "comment":
    case "quote":
    case "doctag":
      return "comment";
    case "title":
    case "function":
    case "section":
      return "function";
    case "type":
    case "class":
    case "name":
    case "selector-class":
    case "selector-id":
      return "type";
    case "attr":
    case "attribute":
    case "variable":
    case "property":
    case "params":
    case "template-variable":
      return "attr";
    case "meta":
    case "tag":
    case "bullet":
      return "meta";
    case "deletion":
      return "deletion";
    case "addition":
      return "addition";
  }
  return "plain";
}

/** Plain lines (no highlighting). */
export function filePreviewPlainLines(code: string): FilePreviewToken[][] {
  return code
    .replace(/\r\n?/g, "\n")
    .replace(/\n$/, "")
    .split("\n")
    .map((l) => (l ? [{ text: l, role: "plain" as const }] : []));
}

/** `code` as lines of tokens; unknown languages, huge files and anything the highlighter rejects come back plain. */
export function filePreviewHighlight(code: string, language: string): FilePreviewToken[][] {
  const src = code.replace(/\r\n?/g, "\n");
  if (language === "plaintext" || !hljs.getLanguage(language) || src.length > MAX_CHARS) return filePreviewPlainLines(src);
  let html: string;
  try {
    html = hljs.highlight(src, { language, ignoreIllegals: true }).value;
  } catch {
    return filePreviewPlainLines(src);
  }
  const lines: FilePreviewToken[][] = [[]];
  const stack: string[] = [];
  const re = /<span class="([^"]*)">|<\/span>|([^<]+)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(html))) {
    if (m[1] !== undefined) stack.push(m[1].replace(/hljs-/g, "").split(" ")[0] ?? "");
    else if (m[0] === "</span>") stack.pop();
    else if (m[2] !== undefined) {
      const r = role(stack[stack.length - 1]);
      decode(m[2])
        .split("\n")
        .forEach((part, i) => {
          if (i > 0) lines.push([]);
          if (part) lines[lines.length - 1]!.push({ text: part, role: r });
        });
    }
    if (lines.length > MAX_LINES) return filePreviewPlainLines(src);
  }
  if (lines.length > 1 && lines[lines.length - 1]!.length === 0 && src.endsWith("\n")) lines.pop();
  return lines;
}
