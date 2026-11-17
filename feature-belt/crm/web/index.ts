/**
 * CRM screens other features and stitching import:
 *
 *   import { CrmRecordScreen, CrmListScreen, RecordPicker } from "@/features/crm";
 *
 * Route files under web/app/(app)/crm render these; record pages put their actions in
 * ResourceMenu with kind crm.<type> and id, so "Share…" and agent access appear there.
 */
export { CrmFrame, useCrm } from "./CrmFrame";
export { CrmDashboardScreen } from "./DashboardScreen";
export { CrmListScreen } from "./ListScreen";
export { CrmBoardScreen } from "./BoardScreen";
export { CrmRecordScreen } from "./RecordScreen";
export { CrmTasksScreen, TaskRow } from "./TasksScreen";
export { CrmImportScreen } from "./ImportScreen";
export { CrmSettingsScreen } from "./SettingsScreen";
export { CreateRecordDialog } from "./CreateRecordDialog";
export { RecordPicker, MemberPicker } from "./parts";
export { CrmTicketBoardScreen } from "./TicketBoardScreen";
export { CrmProductsScreen } from "./ProductsScreen";
export { CrmQuoteScreen } from "./QuoteScreen";
export { CrmPublicQuoteScreen } from "./PublicQuoteScreen";
export { CrmTemplatesScreen } from "./TemplatesScreen";
export { CrmFormsScreen } from "./FormsScreen";
export { CrmFormEditorScreen } from "./FormEditorScreen";
export { CrmPublicFormScreen } from "./PublicFormScreen";
export { CrmReportsScreen, CrmReportDashboardScreen } from "./ReportsScreen";
export { CrmForecastScreen } from "./ForecastScreen";
export { CrmWorkflowsScreen } from "./WorkflowsScreen";
export { CrmWorkflowEditorScreen } from "./WorkflowEditorScreen";
