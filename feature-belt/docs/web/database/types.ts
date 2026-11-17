import type { DocsComputedValue, DocsGroupInfo, DocsPage, DocsProperty, DocsRefs, DocsRow, DocsValue, DocsView, DocsViewConfig } from "@scaffoldapp/shared";

/** What the first page of a view's rows carries besides rows. */
export interface RowsMeta {
  total?: number | undefined;
  groups?: DocsGroupInfo[] | undefined;
  sub_groups?: DocsGroupInfo[] | undefined;
  calculations?: Record<string, DocsComputedValue> | undefined;
}

/** Sections of the view settings sheet. */
export type ViewSettingsSection = "layout" | "properties" | "group" | "sub_group" | "sub_items" | "chart" | "form" | "timeline";

/** What every view body gets from DatabaseView (and ConnectedDatabaseView). Fields after `external` are optional. */
export interface ViewProps {
  db: DocsPage;
  schema: DocsProperty[];
  view: DocsView;
  config: DocsViewConfig;
  rows: DocsRow[];
  refs: DocsRefs;
  loading: boolean;
  hasMore: boolean;
  loadMore: () => void;
  editable: boolean;
  orgId: string;
  publicView: boolean;
  onOpenRow: (id: string) => void;
  onSetValue: (row: DocsRow, propId: string, value: DocsValue) => void;
  onSetTitle: (row: DocsRow, title: string) => void;
  onCreateRow: (values?: Record<string, DocsValue>, title?: string) => void;
  onMoveRow: (row: DocsRow, change: { values?: Record<string, DocsValue>; position?: string }) => void;
  onSchemaChange: (props: DocsProperty[]) => void;
  onConfigChange: (cfg: DocsViewConfig) => void;
  onTrashRow: (row: DocsRow) => void;
  onDuplicateRow: (row: DocsRow) => void;
  /** Properties can be added, renamed and removed (default: editable). Off for connected databases. */
  schemaEditable?: boolean;
  /** A connected database: rows open on the platform and carry its agent scope (no trash or duplicate here). */
  external?: { platform: string; connectionId: string };
  /** The first page's total, groups, sub-groups and footer calculations. */
  meta?: RowsMeta;
  /** A public link's token (files and related rows open through it). */
  token?: string | null;
  /** View settings save for everyone (else they stay with this viewer). */
  configEditable?: boolean;
  onPressButton?: (row: DocsRow, propId: string) => void;
  onCreateSubRow?: (parent: DocsRow) => void;
  /** Opens the property settings (a new property when `isNew`). */
  onEditProperty?: (p: DocsProperty, isNew?: boolean) => void;
  /** Adds a filter on the property and opens the filters. */
  onAddFilter?: (p: DocsProperty) => void;
  onOpenSettings?: (section?: ViewSettingsSection) => void;
  /** A row just added here: its title opens for typing. */
  autoEditRow?: string | null;
  /** Asks before deleting a property. */
  onDeleteProperty?: (p: DocsProperty) => void;
}
