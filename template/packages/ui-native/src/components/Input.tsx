import { withAlpha } from "@scaffoldapp/tokens";
import { forwardRef, useState, type ReactNode } from "react";
import { TextInput, View, type StyleProp, type TextInputProps, type ViewStyle } from "react-native";
import { renderIcon, type IconLike } from "../lib/icon";
import { useTheme } from "../theme/ThemeProvider";
import { useField } from "./Field";

export interface InputProps extends TextInputProps {
  leadingIcon?: IconLike;
  /** Right-side node (unit, IconButton). */
  trailing?: ReactNode;
  invalid?: boolean;
  containerStyle?: StyleProp<ViewStyle>;
}

/** Text input: 48 tall (grows with the text size), 16 pt text (no iOS zoom), focus ring, Field-aware. */
export const Input = forwardRef<TextInput, InputProps>(function Input({ leadingIcon, trailing, invalid, containerStyle, style, onFocus, onBlur, multiline, editable = true, ...rest }, ref) {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const field = useField();
  const [focused, setFocused] = useState(false);
  const bad = invalid ?? field?.invalid ?? false;
  const border = bad ? c.danger : focused ? c.ring : c.borderStrong;
  return (
    <View
      style={[
        {
          flexDirection: "row",
          alignItems: multiline ? "flex-start" : "center",
          minHeight: multiline ? s[24] : theme.tap.base,
          borderWidth: 1,
          borderColor: border,
          borderRadius: theme.radius.md,
          backgroundColor: c.surface,
          paddingHorizontal: s[3],
          gap: s[2],
          opacity: editable ? 1 : 0.6,
        },
        focused ? { boxShadow: `0 0 0 3px ${withAlpha(bad ? c.danger : c.ring, 0.25)}` } : null,
        containerStyle,
      ]}
    >
      {leadingIcon ? <View style={{ paddingTop: multiline ? s[3] : 0 }}>{renderIcon(leadingIcon, { size: 20, color: c.textMuted })}</View> : null}
      <TextInput
        ref={ref}
        editable={editable}
        multiline={multiline}
        placeholderTextColor={c.textSubtle}
        selectionColor={c.primary}
        cursorColor={c.primary}
        keyboardAppearance={theme.mode}
        accessibilityLabel={rest.accessibilityLabel ?? field?.label}
        accessibilityState={{ disabled: !editable }}
        onFocus={(e) => {
          setFocused(true);
          onFocus?.(e);
        }}
        onBlur={(e) => {
          setFocused(false);
          onBlur?.(e);
        }}
        style={[
          // lineHeight misplaces single-line text on iOS; only multiline gets it.
          multiline ? theme.type.body : { fontFamily: theme.type.body.fontFamily, fontSize: theme.type.body.fontSize, letterSpacing: theme.type.body.letterSpacing },
          { flex: 1, alignSelf: "stretch", color: c.text, paddingVertical: s[3], textAlignVertical: multiline ? "top" : "center" },
          style,
        ]}
        {...rest}
      />
      {trailing}
    </View>
  );
});

/** Multi-line input; grows with its content. */
export const TextArea = forwardRef<TextInput, Omit<InputProps, "multiline">>(function TextArea(props, ref) {
  return <Input ref={ref} multiline {...props} />;
});
