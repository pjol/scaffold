# @scaffoldapp/ui-native

React Native components for the Expo app, built on `createNativeTheme` from `@scaffoldapp/tokens/native`. Consumed as TypeScript source (Metro transpiles it); peers are the app's Expo SDK 57 packages (Reanimated 4, Gesture Handler, safe-area-context, react-native-svg, lucide, expo-haptics/image/image-picker/linear-gradient/font).

## Root setup

`mobile/app/_layout.tsx` already does this: `GestureHandlerRootView` › `SafeAreaProvider` › `ThemeProvider paletteId mode` › app › `ToastHost`, with the splash held until `useAppFonts()` settles. Components read the theme with `useTheme()` (`colors`, `space`, `size`, `tap`, `type`, `radius`, `elevation`, `motion`, `font()`) and window spacing with `useLayout()` (`gutter`, `section`, `card`, `maxContent`).

## Components

| Area | Exports |
|---|---|
| Text | `Text role tone weight tabular`, `Hero` `Display` `Headline` `Title` `TitleSm` `BodyLg` `Body` `Label` `Caption` |
| Actions | `Button` (primary/secondary/outline/ghost/soft/danger; `md` 40 → 48 hit, `lg` 48 default, `xl` 56), `IconButton` (`label` required), `Chip`, `ToggleChip` |
| Surfaces | `Card`, `ListRow`, `ListGroup`, `Divider`, `Avatar`, `AvatarGroup`, `Badge` |
| Help | `InfoTip` (ⓘ, 48 hit area, haptic tick, anchored bubble, announced to screen readers) |
| Shell | `Screen` (insets on content padding, keyboard avoidance, sticky `footer`, tablet width cap), `TabBar` (expo-router tabs) |
| Forms | `Field` (label · ⓘ · control · error), `Input`, `TextArea`, `Switch`, `SegmentedControl`, `Stepper`, `Checkbox` |
| Overlays | `Sheet` (snap points, drag to dismiss, `onDismissed`), `Dialog`, `ConfirmDialog` (async, `confirmDisabled` for type-to-confirm), `toast`, `ToastHost`, `Banner` |
| States | `Skeleton`, `SkeletonText`, `SkeletonRow`, `EmptyState`, `ErrorState`, `Spinner` |
| Numbers | `AnimatedNumber`, `Money`, `Celebrate` |
| Photos | `pickPhotos` (system picker; camera asked at use), `PhotoUploader`, `PhotoGrid`, `PhotoLightbox` |
| Legal | `LegalDocument`, `ConsentForm` (take `@scaffoldapp/shared/legal` documents) |
| Motion | `enterUp`, `popIn`, `fadeIn`, `fadeOut`, `layoutSpring`, `usePressScale`, `haptic` |

## Rules the components keep (keep them in new ones)

- No `description`/`subtitle` props: context goes in `info`. No outer margins: parents own gaps.
- Tokens only: colours from `theme.colors`, spacing from `theme.space` (approved steps), text from type roles. No raw hex.
- Hit areas ≥ 48 (`hitSlop` grows smaller visuals); text never sits in a fixed-height box (`minHeight` + padding); font scaling stays on (`maxFontSizeMultiplier` 1.5 only on cramped chrome: tab labels, badges).
- Springs from `theme.motion.spring` (mass explicit). Reanimated skips animations under reduced motion; loops (spinner, shimmer) check `useReducedMotion()` and stop.
- Shadows (`theme.elevation`) only on floating layers (sheets, dialogs, toasts, popovers).

## Fonts

`FONT_FACES` in `src/theme/fonts.ts` maps the face names palettes use (`Inter_600SemiBold`) to `@expo-google-fonts` modules. A palette with a new family adds its imports there.
