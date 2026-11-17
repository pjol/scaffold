/**
 * Mermaid diagrams (MIT), loaded on first use. securityLevel "strict" (no clicks, encoded labels), labels drawn as SVG
 * text (no HTML), and the result is shown as an <img> of an SVG data URL, so nothing in a diagram can run.
 */
type Mermaid = {
  initialize: (cfg: Record<string, unknown>) => void;
  render: (id: string, text: string) => Promise<{ svg: string }>;
};

let loading: Promise<Mermaid> | null = null;
let seq = 0;

function isDark(): boolean {
  const t = document.documentElement.dataset["theme"];
  if (t) return t === "dark";
  return window.matchMedia?.("(prefers-color-scheme: dark)").matches ?? false;
}

/** The diagram as an SVG data URL, or an error message. */
export async function renderMermaid(text: string): Promise<{ url: string; width?: number | undefined } | { error: string }> {
  loading ??= import("mermaid").then((m) => m.default as unknown as Mermaid);
  let mermaid: Mermaid;
  try {
    mermaid = await loading;
  } catch {
    loading = null;
    return { error: "Couldn't load the diagram renderer." };
  }
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: "strict",
    theme: isDark() ? "dark" : "neutral",
    htmlLabels: false,
    flowchart: { htmlLabels: false },
    fontFamily: "ui-sans-serif, system-ui, -apple-system, Segoe UI, sans-serif",
  });
  try {
    const { svg } = await mermaid.render(`docs-mermaid-${++seq}`, text);
    // Mermaid sizes the SVG to its container (width="100%"); as an image it needs its own size.
    const vb = /viewBox="[-\d.]+ [-\d.]+ ([\d.]+) ([\d.]+)"/.exec(svg);
    const sized = vb ? svg.replace(/<svg([^>]*?) width="100%"/, `<svg$1 width="${Math.ceil(Number(vb[1]))}" height="${Math.ceil(Number(vb[2]))}"`).replace(/ style="max-width:[^"]*"/, "") : svg;
    return { url: `data:image/svg+xml;charset=utf-8,${encodeURIComponent(sized)}`, width: vb ? Number(vb[1]) : undefined };
  } catch (e) {
    for (const id of [`docs-mermaid-${seq}`, `ddocs-mermaid-${seq}`]) document.getElementById(id)?.remove();
    return { error: e instanceof Error ? e.message.split("\n")[0] ?? "This diagram has an error." : "This diagram has an error." };
  }
}
