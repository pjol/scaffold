"use client";

import { IconButton } from "@scaffoldapp/ui-web";
import { useEffect, useState } from "react";
import { SearchIcon } from "./icons";
import { isMac } from "./SearchPalette";
import { setSearchPaletteOpen } from "./store";

/**
 * Top-bar search button (registry `headerActions`); the tooltip names the shortcut. From
 * 640 px up: phones get the Home tile and the /search page, keeping the top bar to the
 * account controls at 375 px.
 */
export function SearchButton() {
  const [shortcut, setShortcut] = useState("Ctrl+K");
  useEffect(() => setShortcut(isMac() ? "⌘K" : "Ctrl+K"), []);
  return (
    <span className="hidden sm:contents">
      <IconButton label={`Search (${shortcut})`} tooltip variant="ghost" icon={<SearchIcon />} onClick={() => setSearchPaletteOpen(true)} />
    </span>
  );
}
