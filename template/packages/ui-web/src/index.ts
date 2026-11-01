/**
 * @scaffoldapp/ui-web: web components (Tailwind v4 + motion + Radix), themed entirely
 * from `@scaffoldapp/tokens`. Consumed as source; client components carry their own
 * "use client".
 *
 * Setup (once per app, done in web/):
 *   app/globals.css   @import "tailwindcss"; @import "@scaffoldapp/tokens/theme.css"; @source "../../packages/ui-web/src";
 *   app/layout.tsx    <head><ThemeScript /></head> … <UIProvider>{children}<Toaster /></UIProvider>
 *
 * House rules baked into the API: tokens only, no subtitle/description props (`info`
 * renders an InfoTip), skeleton/empty/error states, motion that respects reduced motion.
 */

// theme
export { ThemeScript } from "./theme/ThemeScript";
export { themeInitScript, THEME_STORAGE_KEY, PALETTE_STORAGE_KEY } from "./theme/script";
export { useColorMode, usePalette, useThemeColors, type ColorModeState, type PaletteState, type ThemePreference } from "./theme/store";
export { UIProvider, type UIProviderProps } from "./theme/UIProvider";

// motion
export * from "./motion";

// utilities
export { cn } from "./lib/cn";
export { Slot, composeRefs, type SlotProps } from "./lib/slot";
export { formatCents, formatInteger, type CentsDisplay } from "./lib/format";
export { useMediaQuery, useHoverCapable, useIsDesktop, useControllableState, useMounted, useLatest, useIsomorphicLayoutEffect } from "./lib/hooks";

// actions and surfaces
export { Button, type ButtonProps, type ButtonVariant, type ButtonSize } from "./components/Button";
export { IconButton, type IconButtonProps, type IconButtonVariant, type IconButtonSize } from "./components/IconButton";
export { Card, type CardProps, type CardVariant } from "./components/Card";
export { ListRow, Divider, type ListRowProps } from "./components/ListRow";
export { Badge, type BadgeProps, type BadgeTone, type BadgeVariant } from "./components/Badge";
export { Chip, ToggleChip, type ChipProps, type ToggleChipProps } from "./components/Chip";
export { Avatar, type AvatarProps, type AvatarSize, type AvatarImageProps } from "./components/Avatar";
export { Banner, type BannerProps, type BannerTone } from "./components/Banner";
export { Menu, MenuItem, MenuSeparator, MenuLabel, MenuSub, MenuChoice, type MenuProps, type MenuItemProps, type MenuSubProps, type MenuChoiceProps } from "./components/Menu";

// help and text
export { InfoTip, type InfoTipProps } from "./components/InfoTip";
export { Tooltip, type TooltipProps } from "./components/Tooltip";
export { Hero, Display, Headline, Title, TitleSm, BodyLg, Body, Label, Caption, type TextProps } from "./components/Text";
export { Spinner, type SpinnerProps } from "./components/Spinner";

// forms
export { Field, useField, useFieldControlProps, type FieldProps } from "./components/Field";
export { Input, TextArea, controlClasses, controlHeight, type InputProps, type TextAreaProps } from "./components/Input";
export { Select, type SelectProps, type SelectOption } from "./components/Select";
export { Switch, type SwitchProps } from "./components/Switch";
export { Checkbox, type CheckboxProps } from "./components/Checkbox";
export { SegmentedControl, type SegmentedControlProps, type SegmentOption } from "./components/SegmentedControl";
export { Slider, type SliderProps } from "./components/Slider";
export { Stepper, type StepperProps } from "./components/Stepper";

// overlays
export { Dialog, DialogClose, ConfirmDialog, type DialogProps, type ConfirmDialogProps } from "./components/Dialog";
export { Sheet, type SheetProps } from "./components/Sheet";
export { Tabs, TabsList, Tab, TabPanel, type TabsProps, type TabProps } from "./components/Tabs";
export { Toaster, toast, type ToasterProps } from "./components/Toaster";

// states
export { Skeleton, SkeletonText, SkeletonRow, SkeletonGroup, type SkeletonProps } from "./components/Skeleton";
export { EmptyState, ErrorState, type EmptyStateProps, type ErrorStateProps } from "./components/EmptyState";

// numbers and moments
export { AnimatedNumber, type AnimatedNumberProps } from "./components/AnimatedNumber";
export { Money, type MoneyProps } from "./components/Money";
export { Celebrate, type CelebrateProps } from "./components/Celebrate";
export { RatingStars, type RatingStarsProps } from "./components/RatingStars";

// photos
export { PhotoUploader, type PhotoUploaderProps, type PhotoUploadResult, type UploadedPhoto } from "./components/PhotoUploader";
export { PhotoGrid, type PhotoGridProps } from "./components/PhotoGrid";
export { PhotoLightbox, formatPhotoTime, type PhotoLightboxProps, type GalleryPhoto } from "./components/PhotoLightbox";

// legal
export { LegalDocument, formatEffectiveDate, type LegalDocumentProps, type LegalDocumentData, type LegalSection, type LegalBlock } from "./components/LegalDocument";
export { ConsentForm, type ConsentFormProps, type ConsentFormValue } from "./components/ConsentForm";

// shell
export { AppShell, type AppShellProps } from "./shell/AppShell";
export { TopBar, type TopBarProps } from "./shell/TopBar";
export { SideNav, type SideNavProps } from "./shell/SideNav";
export { BottomNav, type BottomNavProps } from "./shell/BottomNav";
export { isNavItemActive, badgeText, type ShellNavItem, type LinkComponent } from "./shell/nav";
