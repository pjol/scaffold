import { FileText } from "lucide-react-native";
import type { HomeEntry } from "../registry";

/** Home row: the org's pages and databases. */
export const docsHomeEntry: HomeEntry = { id: "docs", title: "Docs", icon: FileText, href: "/docs" };
