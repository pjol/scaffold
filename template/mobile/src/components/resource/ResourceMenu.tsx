import { Check, MoreVertical } from "lucide-react-native";
import { Fragment, useState } from "react";
import { View } from "react-native";
import { IconButton, ListGroup, ListRow, Sheet, Text, useTheme } from "@scaffoldapp/ui-native";
import { resourceMenuProviders, type ResourceMenuAction, type ResourceMenuContext } from "@/features/registry";

export interface ResourceMenuProps extends ResourceMenuContext {
  /** The screen's own actions (Rename, Move, Delete…), shown first; destructive ones go last. */
  actions?: ResourceMenuAction[];
  /** Accessible name for the button. @default "More actions" */
  label?: string;
}

/**
 * The one ⋮ button every resource and feature screen uses for secondary actions: a sheet
 * with the screen's own actions, then what installed features add for any resource through
 * `resourceMenuProviders` (Share…, AI agent access…). An action with `choices` opens a
 * second sheet. Pass `kind` and `id` for one resource; only `feature` for a whole section.
 */
export function ResourceMenu({ actions = [], label = "More actions", ...ctx }: ResourceMenuProps) {
  const theme = useTheme();
  const [open, setOpen] = useState(false);
  const [picking, setPicking] = useState<ResourceMenuAction | null>(null);
  const [pending, setPending] = useState<(() => void) | null>(null);
  // The provider list is static, so calling each hook in order keeps hook order stable.
  const provided = resourceMenuProviders.map((p) => ({ id: p.id, ...p.useActions(ctx) }));
  const groups = [actions.filter((a) => !a.destructive), provided.flatMap((p) => p.actions), actions.filter((a) => a.destructive)].filter(
    (g) => g.length > 0,
  );

  // Run an action after the sheet has closed, so a dialog or screen it opens isn't stacked on it.
  const choose = (a: ResourceMenuAction) => {
    if (a.choices) {
      setPending(() => () => setPicking(a));
    } else if (a.onSelect) {
      setPending(() => a.onSelect ?? null);
    }
    setOpen(false);
  };

  return (
    <>
      {groups.length > 0 ? <IconButton label={label} icon={MoreVertical} onPress={() => setOpen(true)} /> : null}
      <Sheet
        open={open}
        onClose={() => setOpen(false)}
        onDismissed={() => {
          const run = pending;
          setPending(null);
          run?.();
        }}
        {...(ctx.title ? { title: ctx.title } : {})}
        snapPoints={["50%", "85%"]}
      >
        <View style={{ gap: theme.space[4] }}>
          {groups.map((group, gi) => (
            <ListGroup key={gi}>
              {group.map((a) => (
                <ListRow
                  key={a.id}
                  title={a.label}
                  leading={<a.icon size={20} color={a.destructive ? theme.colors.dangerStrong : theme.colors.text} />}
                  {...(a.hint ? { trailing: <Text tone="muted">{a.hint}</Text> } : {})}
                  destructive={a.destructive}
                  disabled={a.disabled}
                  chevron={Boolean(a.choices)}
                  onPress={() => choose(a)}
                />
              ))}
            </ListGroup>
          ))}
        </View>
      </Sheet>
      <Sheet open={picking !== null} onClose={() => setPicking(null)} {...(picking ? { title: picking.label } : {})}>
        <ListGroup>
          {(picking?.choices ?? []).map((c) => (
            <ListRow
              key={c.id}
              title={c.label}
              {...(c.hint ? { info: c.hint } : {})}
              trailing={c.checked ? <Check size={20} color={theme.colors.primary} accessibilityLabel="Current" /> : undefined}
              chevron={false}
              disabled={c.disabled}
              onPress={() => {
                c.onSelect();
                setPicking(null);
              }}
            />
          ))}
        </ListGroup>
      </Sheet>
      {provided.map((p) => (p.element ? <Fragment key={p.id}>{p.element}</Fragment> : null))}
    </>
  );
}
