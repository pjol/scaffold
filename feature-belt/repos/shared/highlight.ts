/**
 * Syntax highlighting for the code viewer on web and mobile: highlight.js (BSD-3)
 * tokenises, and this turns its markup into plain token lines that each platform
 * renders with its own text components (no HTML injection anywhere).
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

const registered: Record<string, Parameters<typeof hljs.registerLanguage>[1]> = {
  bash, c, cpp, csharp, css, diff, dockerfile, go, ini, java, javascript, json, kotlin, makefile, markdown,
  php, python, ruby, rust, scss, sql, swift, typescript, xml, yaml,
};
for (const [name, lang] of Object.entries(registered)) hljs.registerLanguage(name, lang);

/** One run of text with the highlighter's scope ("keyword", "string", "title.function"), or none. */
export interface ReposToken {
  text: string;
  scope?: string;
}

/** Colour role for a scope, for renderers: map each to a token colour. */
export type ReposTokenRole = "keyword" | "string" | "number" | "comment" | "function" | "type" | "attr" | "meta" | "deletion" | "addition" | "plain";

/** Above these, files render as plain text (highlighting would stall the page). */
const MAX_HIGHLIGHT_CHARS = 300_000;
const MAX_HIGHLIGHT_LINES = 8_000;

const entities: Record<string, string> = { "&amp;": "&", "&lt;": "<", "&gt;": ">", "&quot;": '"', "&#x27;": "'", "&#39;": "'" };

function decode(s: string): string {
  return s.replace(/&(?:amp|lt|gt|quot|#x27|#39);/g, (m) => entities[m] ?? m);
}

/** True when `language` (from the API's `language`) has a highlighter. */
export function reposCanHighlight(language: string): boolean {
  return language !== "plaintext" && !!hljs.getLanguage(language);
}

/**
 * Split `code` into lines of tokens. Unknown languages, huge files and anything the
 * highlighter rejects come back as plain lines.
 */
export function reposHighlightLines(code: string, language: string): ReposToken[][] {
  const plain = () => code.replace(/\n$/, "").split("\n").map((l) => (l ? [{ text: l }] : []));
  if (!reposCanHighlight(language) || code.length > MAX_HIGHLIGHT_CHARS) return plain();
  let html: string;
  try {
    html = hljs.highlight(code, { language, ignoreIllegals: true }).value;
  } catch {
    return plain();
  }
  const lines: ReposToken[][] = [[]];
  const stack: string[] = [];
  const re = /<span class="([^"]*)">|<\/span>|([^<]+)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(html))) {
    if (m[1] !== undefined) {
      stack.push(m[1].replace(/hljs-/g, "").split(" ")[0] ?? "");
    } else if (m[0] === "</span>") {
      stack.pop();
    } else if (m[2] !== undefined) {
      const scope = stack[stack.length - 1];
      const parts = decode(m[2]).split("\n");
      parts.forEach((part, i) => {
        if (i > 0) lines.push([]);
        if (part) lines[lines.length - 1]!.push(scope ? { text: part, scope } : { text: part });
      });
    }
    if (lines.length > MAX_HIGHLIGHT_LINES) return plain();
  }
  if (lines.length > 1 && lines[lines.length - 1]!.length === 0 && code.endsWith("\n")) lines.pop();
  return lines;
}

/** The colour role for a highlighter scope. */
export function reposTokenRole(scope: string | undefined): ReposTokenRole {
  if (!scope) return "plain";
  const s = scope.split(".")[0];
  switch (s) {
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
    case "link":
      return "meta";
    case "deletion":
      return "deletion";
    case "addition":
      return "addition";
    default:
      return "plain";
  }
}
