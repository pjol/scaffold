import type { DocsPage, DocsProperty, DocsRefs, DocsRow, DocsRowsParams, DocsValue, DocsView, DocsViewConfig } from "@scaffoldapp/shared";
import type { RowsMeta } from "./model";

/** What every view body gets from DatabaseView. */
export interface ViewCtx {
  db: DocsPage;
  schema: DocsProperty[];
  view: DocsView;
  config: DocsViewConfig;
  rows: DocsRow[];
  refs: DocsRefs;
  meta: RowsMeta;
  /** The query the view's rows came from (sub-items reuse it with `parent`). */
  params: DocsRowsParams;
  token: string | null;
  /** The viewer may change this view's settings. */
  editable: boolean;
  /** The viewer may add rows. */
  canAdd: boolean;
  canEditRow: (r: DocsRow) => boolean;
  openRow: (id: string) => void;
  /** A tap on a value: checkbox toggles, buttons run, the rest opens the value sheet. */
  editValue: (r: DocsRow, p: DocsProperty) => void;
  setValues: (r: DocsRow, values: Record<string, DocsValue>) => void;
  pressButton: (r: DocsRow, p: DocsProperty) => void;
  /** "rowId:propId" of a running button. */
  pressing: string | null;
  /** Adds a row through this view (its default template applies unless `blank` or `template_id`), then opens it. */
  createRow: (values?: Record<string, DocsValue>, opts?: { parent_row?: string; template_id?: string; blank?: boolean }) => void;
  saveConfig: (next: DocsViewConfig) => void;
}
