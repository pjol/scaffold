/**
 * TeX rendering with KaTeX (MIT), loaded on first use so pages without equations never download it. `trust` stays
 * off: \href, \url, \includegraphics and HTML extensions are refused, so an equation can't add links or markup.
 */
type Katex = { render: (tex: string, el: HTMLElement, opts: Record<string, unknown>) => void; renderToString: (tex: string, opts: Record<string, unknown>) => string };

let loading: Promise<Katex> | null = null;

export function loadKatex(): Promise<Katex> {
  loading ??= import("./katex-load").then((m) => m.default as unknown as Katex);
  return loading;
}

/** Renders TeX into el (display: a block equation). Shows the source with an error tone when it doesn't parse. */
export function renderTeX(el: HTMLElement, tex: string, display: boolean): void {
  void loadKatex().then(
    (k) => {
      try {
        k.render(tex || "\\phantom{x}", el, { displayMode: display, throwOnError: true, trust: false, strict: "ignore", output: "htmlAndMathml", maxSize: 50, maxExpand: 500 });
        el.removeAttribute("data-error");
      } catch (e) {
        el.textContent = tex;
        el.dataset["error"] = e instanceof Error ? e.message.replace(/^KaTeX parse error: /, "") : "Can't read this equation.";
      }
    },
    () => {
      el.textContent = tex;
    },
  );
}
