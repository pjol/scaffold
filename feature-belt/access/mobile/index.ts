/**
 * Access UI for mobile stitching:
 *
 *   import { ShareSheet } from "@/features/access";
 *   <ShareSheet kind="docs.page" id={page.id} title={page.title} open={open} onClose={() => setOpen(false)} />
 *
 * Screens that use ResourceMenu (src/components/resource) get "Share…" automatically.
 */
export { ShareSheet, type ShareSheetProps } from "./ShareSheet";
export { PeoplePicker, type PeoplePickerProps } from "./PeoplePicker";
export { TeamsScreen } from "./TeamsScreen";
export { TeamScreen } from "./TeamScreen";
