"use client";

import { docsTitle, errorMessage, useDocsExport, type DocsBlock, type DocsExportFormat, type DocsPageDetail } from "@scaffoldapp/shared";
import { Button, Dialog, Switch, cn, toast } from "@scaffoldapp/ui-web";
import { Download, FileCode2, FileText, FileType2, Printer, FolderArchive } from "lucide-react";
import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { BlockView } from "./BlockView";
import { CoverArt } from "./Dialogs";
import { PageIcon } from "./PageIcon";

const FORMATS: { id: DocsExportFormat | "pdf"; label: string; info: string; icon: typeof FileText; subpages: boolean }[] = [
  { id: "pdf", label: "PDF", info: "Through your browser's print dialog: choose Save as PDF.", icon: Printer, subpages: false },
  { id: "markdown", label: "Markdown", info: "One .md file.", icon: FileText, subpages: false },
  { id: "zip", label: "Markdown & CSV (.zip)", info: "Pages as .md, databases as .csv, with images and files, in folders like the page tree.", icon: FolderArchive, subpages: true },
  { id: "html", label: "HTML", info: "One self-contained page with its images.", icon: FileCode2, subpages: true },
  { id: "html_zip", label: "HTML (.zip)", info: "A page per file, linked to each other, with images and files.", icon: FolderArchive, subpages: true },
  { id: "docx", label: "Word (.docx)", info: "Headings, lists, tables and images; subpages follow on new pages.", icon: FileType2, subpages: true },
];

/** Prints the page alone (no app chrome), in the light theme, then cleans up. */
function PrintPortal({ detail, blocks, onDone }: { detail: DocsPageDetail; blocks: readonly DocsBlock[]; onDone: () => void }) {
  useEffect(() => {
    const html = document.documentElement;
    const prev = html.getAttribute("data-theme");
    const style = document.createElement("style");
    style.textContent = "@media print { body > *:not(#docs-print) { display: none !important; } #docs-print { display: block !important; } @page { margin: 18mm; } }";
    document.head.appendChild(style);
    html.setAttribute("data-theme", "light");
    const done = () => {
      if (prev === null) html.removeAttribute("data-theme");
      else html.setAttribute("data-theme", prev);
      style.remove();
      onDone();
    };
    window.addEventListener("afterprint", done, { once: true });
    const t = setTimeout(() => window.print(), 300);
    return () => {
      clearTimeout(t);
      window.removeEventListener("afterprint", done);
    };
  }, [onDone]);
  const page = detail.page;
  return createPortal(
    <div id="docs-print" className="hidden bg-canvas text-fg">
      {page.cover ? <CoverArt cover={page.cover} className="mb-6 h-32" /> : null}
      <div className="flex flex-col gap-4">
        <PageIcon icon={page.icon} custom={page.custom_icon} kind={page.kind} size="lg" fallback={false} />
        <h1 className="type-display text-fg">{docsTitle(page.title)}</h1>
        <BlockView blocks={blocks} refs={detail.refs} />
      </div>
    </div>,
    document.body,
  );
}

/** Export a page as PDF (print), Markdown, a Markdown + CSV zip, HTML, an HTML zip or Word, optionally with subpages. */
export function ExportDialog({ open, onOpenChange, detail, blocks }: { open: boolean; onOpenChange: (open: boolean) => void; detail: DocsPageDetail; blocks: readonly DocsBlock[] }) {
  const [format, setFormat] = useState<DocsExportFormat | "pdf">("pdf");
  const [subpages, setSubpages] = useState(true);
  const [printing, setPrinting] = useState(false);
  const exporter = useDocsExport();
  const chosen = FORMATS.find((f) => f.id === format)!;
  const go = () => {
    if (format === "pdf") {
      onOpenChange(false);
      setPrinting(true);
      return;
    }
    exporter.mutate(
      { pageId: detail.page.id, format, subpages: chosen.subpages && subpages },
      {
        onSuccess: (r) => {
          const a = document.createElement("a");
          a.href = r.url;
          a.download = r.name;
          a.rel = "noopener";
          document.body.appendChild(a);
          a.click();
          a.remove();
          onOpenChange(false);
          if (r.notes.length) toast.message(`Exported. ${r.notes.length} ${r.notes.length === 1 ? "file was" : "files were"} too large to include.`);
          else toast.success("Exported.");
        },
        onError: (e) => toast.error(errorMessage(e, "Couldn't export the page.")),
      },
    );
  };
  return (
    <>
      <Dialog
        open={open}
        onOpenChange={onOpenChange}
        title="Export"
        info="Exports hold only what you can open: subpages you can't see are left out."
        footer={
          <Button leftIcon={format === "pdf" ? <Printer /> : <Download />} loading={exporter.isPending} onClick={go}>
            {format === "pdf" ? "Print or save as PDF" : "Export"}
          </Button>
        }
      >
        <div className="flex flex-col gap-3">
          <div role="radiogroup" aria-label="Format" className="flex flex-col gap-1">
            {FORMATS.map((f) => {
              const Icon = f.icon;
              return (
                <button
                  key={f.id}
                  type="button"
                  role="radio"
                  aria-checked={format === f.id}
                  onClick={() => setFormat(f.id)}
                  className={cn(
                    "flex items-start gap-3 rounded-md border px-3 py-2 text-left focus-visible:outline-2 focus-visible:outline-ring",
                    format === f.id ? "border-primary bg-primary-soft" : "border-border hover:bg-sunken",
                  )}
                >
                  <Icon aria-hidden className="mt-0.5 size-4 shrink-0 text-fg-muted" />
                  <span className="flex flex-col">
                    <span className="type-label font-semibold text-fg">{f.label}</span>
                    <span className="type-caption text-fg-muted">{f.info}</span>
                  </span>
                </button>
              );
            })}
          </div>
          {chosen.subpages && detail.page.kind === "page" ? <Switch checked={subpages} onCheckedChange={setSubpages} label="Include subpages" /> : null}
        </div>
      </Dialog>
      {printing ? <PrintPortal detail={detail} blocks={blocks} onDone={() => setPrinting(false)} /> : null}
    </>
  );
}
