/**
 * The core file-preview model shared by `FilePreview` in ui-web and ui-native: what kind a file
 * is, CSV, Markdown and JSON readers. Highlighting is a separate, lazily imported entry
 * (`@scaffoldapp/shared/preview/highlight`).
 */
export * from "./kind";
export * from "./csv";
export * from "./json";
export * from "./markdown";
