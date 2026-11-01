import { Inter_400Regular } from "@expo-google-fonts/inter/400Regular";
import { Inter_500Medium } from "@expo-google-fonts/inter/500Medium";
import { Inter_600SemiBold } from "@expo-google-fonts/inter/600SemiBold";
import { Inter_700Bold } from "@expo-google-fonts/inter/700Bold";
import { useFonts } from "expo-font";

/**
 * Every face the app loads, keyed by the names palettes use in `FontFamilyToken.native`
 * (`theme.font(role, weight)` returns these keys). A palette that adds a family adds its
 * `@expo-google-fonts/<family>/<weight>` imports here; unknown names fall back to the
 * system font.
 */
export const FONT_FACES: Record<string, number> = {
  Inter_400Regular,
  Inter_500Medium,
  Inter_600SemiBold,
  Inter_700Bold,
};

/**
 * Load {@link FONT_FACES}. Keep the splash screen up until it settles:
 *
 * ```tsx
 * const [loaded, error] = useAppFonts();
 * useEffect(() => { if (loaded || error) void SplashScreen.hideAsync(); }, [loaded, error]);
 * ```
 */
export function useAppFonts(): [loaded: boolean, error: Error | null] {
  return useFonts(FONT_FACES);
}
