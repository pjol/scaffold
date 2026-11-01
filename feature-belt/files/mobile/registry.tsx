import { FolderClosed } from "lucide-react-native";
import type { HomeEntry } from "../registry";

/** Home → Files. */
export const filesHomeEntry: HomeEntry = { id: "files", title: "Files", icon: FolderClosed, href: "/files" };
