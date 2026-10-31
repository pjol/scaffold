import { PALETTE_IDS } from "@scaffoldapp/tokens";

/** localStorage key for the colour-mode choice (`light` | `dark`; absent = system). */
export const THEME_STORAGE_KEY = "scaffoldapp.theme";
/** localStorage key for the chosen palette id (absent = the build's palette). */
export const PALETTE_STORAGE_KEY = "scaffoldapp.palette";

/**
 * The pre-paint script body: applies the stored palette and colour mode to `<html>`
 * (`data-palette`, `data-theme`) before first paint, so there is no flash. Unknown
 * palette ids are ignored. Inline it in the root layout's `<head>` (see `ThemeScript`).
 */
export function themeInitScript(): string {
  const ids = JSON.stringify(PALETTE_IDS);
  const pk = JSON.stringify(PALETTE_STORAGE_KEY);
  const tk = JSON.stringify(THEME_STORAGE_KEY);
  return `(function(){try{var d=document.documentElement,s=window.localStorage,p=s.getItem(${pk}),t=s.getItem(${tk});if(p&&${ids}.indexOf(p)>-1)d.setAttribute("data-palette",p);if(t==="light"||t==="dark")d.setAttribute("data-theme",t)}catch(e){}})();`;
}
