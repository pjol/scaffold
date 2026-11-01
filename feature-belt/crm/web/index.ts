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
