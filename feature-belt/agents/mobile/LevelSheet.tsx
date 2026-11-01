import { AGENTS_LEVEL_INFO, AGENTS_LEVEL_LABEL, AGENTS_LEVELS, type AgentsLevel } from "@scaffoldapp/shared";
import { ListGroup, ListRow, Sheet, useTheme } from "@scaffoldapp/ui-native";
import { Check } from "lucide-react-native";
import { View } from "react-native";

export interface LevelSheetProps {
  open: boolean;
  onClose: () => void;
  title: string;
  /** The current setting; null = Default. */
  value: AgentsLevel | null;
  /** Show "Default (Read)" first, with what it inherits. Omit for the default level itself. */
  inherited?: { level: AgentsLevel; source: string } | undefined;
  onChoose: (level: AgentsLevel | null) => void;
  /** A destructive last row ("Remove setting"). */
  remove?: { label: string; onPress: () => void } | undefined;
  /** A first row that opens the item. */
  open_?: { label: string; onPress: () => void } | undefined;
}

/** Off · Read · Suggest · Edit (and Default) as a sheet, each with its explanation in an InfoTip. */
export function LevelSheet({ open, onClose, title, value, inherited, onChoose, remove, open_ }: LevelSheetProps) {
  const theme = useTheme();
  const check = (on: boolean) => (on ? <Check size={20} color={theme.colors.primary} accessibilityLabel="Current" /> : undefined);
  const pick = (l: AgentsLevel | null) => {
    onChoose(l);
    onClose();
  };
  return (
    <Sheet open={open} onClose={onClose} title={title} snapPoints={["60%", "85%"]}>
      <View style={{ gap: theme.space[4] }}>
        {open_ ? (
          <ListGroup>
            <ListRow title={open_.label} onPress={open_.onPress} />
          </ListGroup>
        ) : null}
        <ListGroup>
          {inherited ? (
            <ListRow
              title={`Default (${AGENTS_LEVEL_LABEL[inherited.level]})`}
              info={`Follows ${inherited.source}.`}
              trailing={check(value === null)}
              chevron={false}
              onPress={() => pick(null)}
            />
          ) : null}
          {AGENTS_LEVELS.map((l) => (
            <ListRow key={l} title={AGENTS_LEVEL_LABEL[l]} info={AGENTS_LEVEL_INFO[l]} trailing={check(value === l)} chevron={false} onPress={() => pick(l)} />
          ))}
        </ListGroup>
        {remove ? (
          <ListGroup>
            <ListRow
              title={remove.label}
              destructive
              chevron={false}
              onPress={() => {
                remove.onPress();
                onClose();
              }}
            />
          </ListGroup>
        ) : null}
      </View>
    </Sheet>
  );
}
