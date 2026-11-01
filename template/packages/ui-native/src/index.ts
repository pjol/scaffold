/**
 * @scaffoldapp/ui-native: React Native components on the tokens in `@scaffoldapp/tokens`
 * (Reanimated 4, Gesture Handler, react-native-svg, lucide, expo-haptics/image/linear-gradient).
 *
 * Root setup (mobile/app/_layout.tsx does this):
 *
 *   <GestureHandlerRootView style={{ flex: 1 }}>
 *     <SafeAreaProvider>
 *       <ThemeProvider mode={appearance}>
 *         …app…
 *         <ToastHost />
 *       </ThemeProvider>
 *     </SafeAreaProvider>
 *   </GestureHandlerRootView>
 *
 * Keep the splash up until `useAppFonts()` settles. No component has a subtitle or
 * description prop: context goes in `info` (an InfoTip).
 */

// theme
export { ThemeProvider, useTheme, useLayout, type ThemeProviderProps, type ColorModePreference, type NativeTheme } from "./theme/ThemeProvider";
export { useAppFonts, FONT_FACES } from "./theme/fonts";

// helpers
export { haptic } from "./lib/haptics";
export { renderIcon, type IconLike } from "./lib/icon";
export { usePressScale } from "./lib/press";
export { enterUp, popIn, fadeIn, fadeOut, layoutSpring } from "./lib/motion";
export { formatCents, formatInteger, type CentsDisplay } from "./lib/format";
export { useControllableState } from "./lib/useControllable";

// text
export { Text, Hero, Display, Headline, Title, TitleSm, BodyLg, Body, Label, Caption, type TextProps, type TextTone } from "./components/Text";

// actions and surfaces
export { Button, buttonColors, type ButtonProps, type ButtonVariant, type ButtonSize } from "./components/Button";
export { IconButton, type IconButtonProps, type IconButtonVariant } from "./components/IconButton";
export { Card, type CardProps, type CardVariant } from "./components/Card";
export { Badge, type BadgeProps, type BadgeTone } from "./components/Badge";
export { Chip, ToggleChip, type ChipProps, type ToggleChipProps } from "./components/Chip";
export { InfoTip, type InfoTipProps } from "./components/InfoTip";
export { Spinner, type SpinnerProps } from "./components/Spinner";
export { Avatar, AvatarGroup, type AvatarProps, type AvatarGroupProps, type AvatarSize } from "./components/Avatar";
export { ListRow, ListGroup, Divider, type ListRowProps, type ListGroupProps } from "./components/ListRow";

// shell
export { Screen, type ScreenProps, type ScreenEdge } from "./components/Screen";
export { TabBar, type TabBarProps } from "./components/TabBar";

// forms
export { Field, useField, type FieldProps } from "./components/Field";
export { Input, TextArea, type InputProps } from "./components/Input";
export { Switch, type SwitchProps } from "./components/Switch";
export { SegmentedControl, type SegmentedControlProps, type SegmentOption } from "./components/SegmentedControl";
export { Stepper, type StepperProps } from "./components/Stepper";
export { Checkbox, type CheckboxProps } from "./components/Checkbox";

// overlays
export { Sheet, type SheetProps, type SnapPoint } from "./components/Sheet";
export { Dialog, ConfirmDialog, type DialogProps, type ConfirmDialogProps } from "./components/Dialog";
export { ToastHost, toast, Banner, type ToastOptions, type ToastType, type BannerProps } from "./components/Toast";

// states
export { Skeleton, SkeletonText, SkeletonRow, type SkeletonProps } from "./components/Skeleton";
export { EmptyState, ErrorState, type EmptyStateProps, type ErrorStateProps } from "./components/EmptyState";

// numbers and moments
export { AnimatedNumber, type AnimatedNumberProps } from "./components/AnimatedNumber";
export { Money, type MoneyProps } from "./components/Money";
export { Celebrate, type CelebrateProps } from "./components/Celebrate";

// photos
export { PhotoUploader, pickPhotos, type PhotoUploaderProps, type PhotoUploadResult, type UploadedPhoto, type PickedPhoto } from "./components/PhotoUploader";
export { PhotoGrid, type PhotoGridProps } from "./components/PhotoGrid";
export { PhotoLightbox, type PhotoLightboxProps, type GalleryPhoto } from "./components/PhotoLightbox";

// legal
export { LegalDocument, formatEffectiveDate, type LegalDocumentProps, type LegalDocumentData, type LegalSection, type LegalBlock } from "./components/LegalDocument";
export { ConsentForm, type ConsentFormProps, type ConsentFormValue } from "./components/ConsentForm";
