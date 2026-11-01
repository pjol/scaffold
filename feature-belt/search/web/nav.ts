import type { HeaderAction, HomeTile, SessionMount } from "../registry";
import { SearchIcon } from "./icons";
import { SearchButton } from "./SearchButton";
import { SearchPalette } from "./SearchPalette";

/** ⌘K / Ctrl+K command palette, available on every signed-in screen. */
export const searchPaletteMount: SessionMount = SearchPalette;

/** Top-bar button that opens the palette. */
export const searchHeaderAction: HeaderAction = { id: "search", Component: SearchButton };

/** Home tile: the full results page (the way in on phones, where there is no ⌘K). */
export const searchHomeTile: HomeTile = { href: "/search", label: "Search", icon: SearchIcon };
