import { IconButton, Text, TextArea, useTheme } from "@scaffoldapp/ui-native";
import { ArrowUp, Square } from "lucide-react-native";
import { useState } from "react";
import { View } from "react-native";

export interface ComposerProps {
  value: string;
  onChange: (value: string) => void;
  /** Sends the trimmed text; the owner clears `value` once the message is accepted. */
  onSend: (text: string) => Promise<boolean>;
  onStop: () => void;
  streaming: boolean;
  disabled?: boolean;
  maxChars: number;
}

/** Message box with Send, or Stop while a reply streams. */
export function Composer({ value: text, onChange: setText, onSend, onStop, streaming, disabled = false, maxChars }: ComposerProps) {
  const theme = useTheme();
  const [sending, setSending] = useState(false);
  const trimmed = text.trim();
  const over = text.length > maxChars;
  const canSend = !disabled && !streaming && !sending && trimmed.length > 0 && !over;

  const send = async () => {
    if (!canSend) return;
    setSending(true);
    try {
      await onSend(trimmed);
    } finally {
      setSending(false);
    }
  };

  return (
    <View style={{ gap: theme.space[1] }}>
      <View style={{ flexDirection: "row", alignItems: "flex-end", gap: theme.space[2] }}>
        <TextArea
          accessibilityLabel="Message"
          placeholder="Message the assistant"
          value={text}
          onChangeText={setText}
          editable={!disabled}
          containerStyle={{ flex: 1 }}
          style={{ maxHeight: theme.type.body.lineHeight * 6 + theme.space[6] }}
        />
        {streaming ? (
          <IconButton label="Stop reply" icon={Square} variant="secondary" size="lg" onPress={onStop} />
        ) : (
          <IconButton label="Send" icon={ArrowUp} variant="primary" size="lg" disabled={!canSend} loading={sending} onPress={() => void send()} />
        )}
      </View>
      {text.length > maxChars * 0.8 ? (
        <Text role="caption" tone={over ? "danger" : "muted"} tabular align="right" accessibilityLiveRegion="polite">
          {`${text.length}/${maxChars}`}
        </Text>
      ) : null}
    </View>
  );
}
