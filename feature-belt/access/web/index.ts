/**
 * Access UI other features and stitching import:
 *
 *   import { ShareDialog, ShareButton, PeoplePicker } from "@/features/access";
 *   <ShareDialog kind="docs.page" id={page.id} title={page.title} open={open} onOpenChange={setOpen} />
 *
 * Screens that use ResourceMenu (src/components/resource) get "Share…" automatically when
 * they pass kind and id.
 */
export { ShareButton, ShareDialog, type ShareDialogProps } from "./ShareDialog";
export { PeoplePicker, SubjectAvatar, type PeoplePickerProps } from "./PeoplePicker";
export { LEVEL_INFO, LEVEL_LABEL } from "./labels";
