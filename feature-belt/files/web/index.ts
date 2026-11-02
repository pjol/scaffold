/**
 * Files UI other features and stitching import:
 *
 *   import { FilesPreview, FilesMarkdown, filesItemIcon } from "@/features/files";
 *
 * Screens live behind the routes in web/app/(app)/files and web/app/(public)/f.
 */
export { filesAdminNav, filesHome, filesNav } from "./registry";
export { FilesPreview, type FilesPreviewSource } from "./Preview";
export { FilesMarkdown } from "./Markdown";
export { filesItemIcon, filesIconTone, filesTypeIcon } from "./icons";
export { FilesLayout, StorageMeter } from "./FilesLayout";
export { FilesConnectedAccountScreen, FilesConnectedItemScreen } from "./ConnectedScreens";
