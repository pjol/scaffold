import { useAuthSession, useMe } from "@scaffoldapp/shared";
import { Avatar, Badge, Banner, Caption, ConfirmDialog, ListGroup, ListRow, Screen, Text, Title, useTheme } from "@scaffoldapp/ui-native";
import { router, useScrollToTop } from "expo-router";
import { FileText, FlaskConical, LifeBuoy, LogOut, Palette, ShieldCheck, UserCog } from "lucide-react-native";
import { useRef, useState } from "react";
import { View, type ScrollView } from "react-native";
import { settingsSections, type SettingsGroup, type SettingsItem, type SettingsSection } from "@/features/registry";
import { usePreferences } from "@/lib/preferences";
import { useSignOut } from "@/lib/session";
import { appVersion, buildNumber } from "@/lib/version";

const MODE_LABEL = { system: "System", light: "Light", dark: "Dark" } as const;

/** Settings headings in order (same ids and labels as web). "You" also holds Profile, Appearance and Account. */
const GROUPS: readonly { id: SettingsGroup; title: string }[] = [
  { id: "you", title: "You" },
  { id: "workspace", title: "Workspace" },
  { id: "agents", title: "AI agents" },
  { id: "developer", title: "Developer" },
];

/** Whether any registry section names a group: then Settings renders headed groups. */
const GROUPED = settingsSections.some((section) => section.group);

function ItemRow({ item }: { item: SettingsItem }) {
  const theme = useTheme();
  const badge = item.useBadge?.();
  const Icon = item.icon;
  return (
    <ListRow
      title={item.title}
      {...(item.info ? { info: item.info } : {})}
      leading={<Icon size={22} color={theme.colors.textMuted} strokeWidth={2} />}
      trailing={badge !== undefined && badge !== 0 ? <Badge size="sm" tone="danger" variant="solid">{String(badge)}</Badge> : undefined}
      onPress={() => router.push(item.href)}
    />
  );
}

/** The rows of every section in `group`, in registry order. */
function itemsIn(sections: readonly SettingsSection[], group: SettingsGroup): SettingsItem[] {
  return sections.filter((section) => section.group === group).flatMap((section) => section.items);
}

/**
 * Settings: Profile · Appearance · feature sections · Account · Legal · Support · Sign out.
 * Once registry sections use groups: You (Profile, Appearance, its sections, Account) ·
 * Workspace · AI agents · Developer · sections without a group (untitled ones under
 * "Other") · Legal · Support · Sign out. No admin on mobile.
 */
export default function SettingsScreen() {
  const theme = useTheme();
  const c = theme.colors;
  const s = theme.space;
  const ref = useRef<ScrollView>(null);
  useScrollToTop(ref);
  const session = useAuthSession();
  const me = useMe();
  const prefs = usePreferences();
  const signOut = useSignOut();
  const [confirm, setConfirm] = useState(false);
  const isStaff = me.data?.admin_role === "admin" || me.data?.admin_role === "support";
  const icon = (I: typeof Palette, danger = false) => <I size={22} color={danger ? c.dangerStrong : c.textMuted} strokeWidth={2} />;
  const legal = (doc: "terms" | "privacy") => router.push({ pathname: "/legal/[doc]", params: { doc } });
  const sections = settingsSections.map((section) => ({ ...section, items: section.items.filter((i) => !i.staffOnly || isStaff) })).filter((section) => section.items.length > 0);
  const name = me.data?.display_name || me.data?.email || "Profile";
  const build = buildNumber();
  const ungrouped = sections.filter((section) => !section.group);
  const other = ungrouped.filter((section) => !section.title).flatMap((section) => section.items);
  const profileRow = <ListRow key="profile" title={name} leading={<Avatar name={name} src={me.data?.avatar_url ?? null} size="md" />} accessibilityHint="Opens your profile" onPress={() => router.push("/settings/profile")} />;
  const appearanceRow = (
    <ListRow key="appearance" title="Appearance" leading={icon(Palette)} trailing={<Text tone="muted">{MODE_LABEL[prefs.appearance]}</Text>} onPress={() => router.push("/settings/appearance")} />
  );
  const accountRow = <ListRow key="account" title="Account" leading={icon(UserCog)} info="Download your data or delete your account." onPress={() => router.push("/settings/account")} />;

  return (
    <Screen ref={ref} edges={["top"]} gap={s[6]}>
      <View style={{ gap: s[3] }}>
        <Title>Settings</Title>
        {session.mode === "dev" ? (
          <Badge tone="warning" icon={FlaskConical}>
            {`Dev sign-in · ${session.email ?? ""}`}
          </Badge>
        ) : null}
        {isStaff ? <Banner tone="info" title="Admin tools are in the web app." /> : null}
      </View>

      {GROUPED ? (
        <>
          <ListGroup title="You">
            {profileRow}
            {appearanceRow}
            {itemsIn(sections, "you").map((item) => (
              <ItemRow key={item.id} item={item} />
            ))}
            {accountRow}
          </ListGroup>
          {GROUPS.slice(1).map(({ id, title }) => {
            const items = itemsIn(sections, id);
            return items.length > 0 ? (
              <ListGroup key={id} title={title}>
                {items.map((item) => (
                  <ItemRow key={item.id} item={item} />
                ))}
              </ListGroup>
            ) : null;
          })}
          {ungrouped
            .filter((section) => section.title)
            .map((section) => (
              <ListGroup key={section.id} {...(section.title ? { title: section.title } : {})}>
                {section.items.map((item) => (
                  <ItemRow key={item.id} item={item} />
                ))}
              </ListGroup>
            ))}
          {other.length > 0 ? (
            <ListGroup title="Other">
              {other.map((item) => (
                <ItemRow key={item.id} item={item} />
              ))}
            </ListGroup>
          ) : null}
        </>
      ) : (
        <>
          <ListGroup>
            {profileRow}
            {appearanceRow}
          </ListGroup>
          {sections.map((section) => (
            <ListGroup key={section.id} {...(section.title ? { title: section.title } : {})}>
              {section.items.map((item) => (
                <ItemRow key={item.id} item={item} />
              ))}
            </ListGroup>
          ))}
          <ListGroup>{accountRow}</ListGroup>
        </>
      )}

      <ListGroup title="Legal">
        <ListRow title="Terms of Service" leading={icon(FileText)} onPress={() => legal("terms")} />
        <ListRow title="Privacy Policy" leading={icon(ShieldCheck)} onPress={() => legal("privacy")} />
      </ListGroup>

      <ListGroup>
        <ListRow title="Support" leading={icon(LifeBuoy)} onPress={() => router.push("/support")} />
      </ListGroup>

      <ListGroup>
        <ListRow title="Sign out" destructive leading={icon(LogOut, true)} chevron={false} onPress={() => setConfirm(true)} />
      </ListGroup>

      <Caption align="center">{build ? `Version ${appVersion()} (${build})` : `Version ${appVersion()}`}</Caption>

      <ConfirmDialog open={confirm} onClose={() => setConfirm(false)} title="Sign out?" tone="danger" confirmLabel="Sign out" onConfirm={signOut} />
    </Screen>
  );
}
