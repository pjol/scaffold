import {
  errorMessage,
  normalizeSocialHandle,
  SOCIAL_HANDLE_PATTERN,
  useChangeSocialHandle,
  useSocialHandleCheck,
  useSocialMe,
  useUpdateSocialProfile,
  type SocialLink,
  type SocialMyProfile,
  type SocialVisibility,
} from "@scaffoldapp/shared";
import { Banner, Button, ErrorState, Field, IconButton, Input, ListGroup, Screen, SegmentedControl, Skeleton, Spinner, Text, TextArea, TitleSm, toast, useTheme } from "@scaffoldapp/ui-native";
import { router } from "expo-router";
import { AtSign, Check, ExternalLink, Plus, X } from "lucide-react-native";
import { useEffect, useState } from "react";
import { View } from "react-native";

const VISIBILITY: { value: SocialVisibility; label: string }[] = [
  { value: "public", label: "Everyone" },
  { value: "followers", label: "Followers" },
  { value: "private", label: "Only me" },
];

const day = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric", year: "numeric" });

function days(hours: number): string {
  const d = Math.round(hours / 24);
  return d === 1 ? "1 day" : `${d} days`;
}

function HandleSection({ me }: { me: SocialMyProfile }) {
  const theme = useTheme();
  const [value, setValue] = useState(me.user.handle);
  useEffect(() => setValue(me.user.handle), [me.user.handle]);
  const h = normalizeSocialHandle(value);
  const changed = h !== me.user.handle;
  const shape = SOCIAL_HANDLE_PATTERN.test(h);
  const check = useSocialHandleCheck(value, me.user.handle);
  const change = useChangeSocialHandle();
  const locked = me.handle_change_at ? new Date(me.handle_change_at) > new Date() : false;
  const settled = check.handle === h;

  let error: string | undefined;
  if (changed && h.length > 0 && !shape) error = "Use 3–20 letters, numbers or underscores, starting with a letter.";
  else if (changed && settled && check.data && !check.data.available) error = check.data.message;
  const ready = changed && shape && settled && check.data?.available === true && !locked;
  const info =
    `Old links to @${me.user.handle} keep working for ${days(me.limits.handle_redirect_hours)}.` +
    (me.limits.handle_cooldown_hours > 0 ? ` You can change your handle once every ${days(me.limits.handle_cooldown_hours)}.` : "");

  return (
    <View style={{ gap: theme.space[4] }}>
      <TitleSm>Handle</TitleSm>
      {locked && me.handle_change_at ? <Banner tone="info" title={`You can change your handle again on ${day.format(new Date(me.handle_change_at))}.`} /> : null}
      <Field label="Handle" info={info} error={error}>
        <Input
          value={value}
          onChangeText={setValue}
          leadingIcon={AtSign}
          maxLength={21}
          autoCapitalize="none"
          autoCorrect={false}
          editable={!locked}
          trailing={
            changed && shape && !error ? (
              check.isFetching || !settled ? (
                <Spinner size={16} />
              ) : check.data?.available ? (
                <Check size={20} color={theme.colors.successStrong} accessibilityLabel="Available" />
              ) : null
            ) : null
          }
        />
      </Field>
      <Button
        disabled={!ready}
        loading={change.isPending}
        onPress={() =>
          change.mutate(h, {
            onSuccess: (next) => toast.success(`You're @${next.user.handle} now`),
            onError: (err) => toast.error(errorMessage(err, "Couldn't change your handle. Try again.")),
          })
        }
      >
        Change handle
      </Button>
    </View>
  );
}

function AboutSection({ me }: { me: SocialMyProfile }) {
  const theme = useTheme();
  const s = theme.space;
  const [bio, setBio] = useState(me.bio);
  const [links, setLinks] = useState<SocialLink[]>(me.links);
  useEffect(() => {
    setBio(me.bio);
    setLinks(me.links);
  }, [me.bio, me.links]);
  const update = useUpdateSocialProfile();
  const max = me.limits.bio_max_chars;
  const clean = links.map((l) => ({ url: l.url.trim(), label: l.label.trim() })).filter((l) => l.url);
  const dirty = bio.trim() !== me.bio || JSON.stringify(clean) !== JSON.stringify(me.links);
  const over = [...bio].length > max;
  const setLink = (i: number, patch: Partial<SocialLink>) => setLinks((ls) => ls.map((l, j) => (j === i ? { ...l, ...patch } : l)));

  return (
    <View style={{ gap: s[4] }}>
      <TitleSm>About</TitleSm>
      <Field
        label="Bio"
        error={over ? `Keep it to ${max} characters.` : undefined}
        labelEnd={
          <Text role="caption" tone={over ? "danger" : "muted"}>
            {`${[...bio].length}/${max}`}
          </Text>
        }
      >
        <TextArea value={bio} onChangeText={setBio} />
      </Field>
      <View style={{ gap: s[3] }}>
        <Text role="label">Links</Text>
        {links.map((l, i) => (
          <View key={i} style={{ gap: s[2], paddingBottom: s[2] }}>
            <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
              <Input
                accessibilityLabel={`Link ${i + 1} label`}
                placeholder="Label"
                value={l.label}
                maxLength={40}
                onChangeText={(t) => setLink(i, { label: t })}
                containerStyle={{ flex: 1 }}
              />
              <IconButton label={`Remove link ${i + 1}`} icon={X} variant="ghost" onPress={() => setLinks((ls) => ls.filter((_, j) => j !== i))} />
            </View>
            <Input
              accessibilityLabel={`Link ${i + 1} address`}
              placeholder="https://"
              value={l.url}
              keyboardType="url"
              autoCapitalize="none"
              autoCorrect={false}
              onChangeText={(t) => setLink(i, { url: t })}
            />
          </View>
        ))}
        {links.length < me.limits.max_links ? (
          <Button variant="ghost" size="md" leftIcon={Plus} style={{ alignSelf: "flex-start" }} onPress={() => setLinks((ls) => [...ls, { url: "", label: "" }])}>
            Add link
          </Button>
        ) : null}
      </View>
      <Button
        disabled={!dirty || over}
        loading={update.isPending && update.variables?.bio !== undefined}
        onPress={() =>
          update.mutate(
            { bio: bio.trim(), links: clean },
            { onSuccess: () => toast.success("Saved"), onError: (err) => toast.error(errorMessage(err, "Couldn't save. Try again.")) },
          )
        }
      >
        Save
      </Button>
    </View>
  );
}

function PrivacySection({ me }: { me: SocialMyProfile }) {
  const theme = useTheme();
  const update = useUpdateSocialProfile();
  return (
    <ListGroup title="Who sees what" info="Your name, photo, handle, bio, links and follower counts are visible to everyone signed in.">
      <View style={{ gap: theme.space[4] }}>
        {me.privacy.map((row) => (
          <Field key={row.key} label={row.label}>
            <SegmentedControl<SocialVisibility>
              accessibilityLabel={row.label}
              options={VISIBILITY}
              value={row.visibility}
              onValueChange={(v) =>
                v !== row.visibility &&
                update.mutate(
                  { privacy: { [row.key]: v } },
                  { onSuccess: () => toast.success("Saved"), onError: (err) => toast.error(errorMessage(err, "Couldn't save. Try again.")) },
                )
              }
            />
          </Field>
        ))}
      </View>
    </ListGroup>
  );
}

/** Settings → Public profile: handle, bio, links, and who sees each part of the profile. */
export function ProfileSettingsScreen() {
  const theme = useTheme();
  const me = useSocialMe();
  return (
    <Screen edges={["bottom"]} keyboard>
      {me.isPending ? (
        <View accessibilityLabel="Loading your profile" accessibilityRole="progressbar" style={{ gap: theme.space[4] }}>
          <Skeleton height={theme.tap.base} />
          <Skeleton height={theme.tap.xl * 2} />
          <Skeleton height={theme.tap.base} />
        </View>
      ) : me.isError ? (
        <ErrorState title="Couldn't load your profile" info={errorMessage(me.error)} onRetry={() => void me.refetch()} retrying={me.isFetching} />
      ) : (
        <>
          <HandleSection me={me.data} />
          <AboutSection me={me.data} />
          <PrivacySection me={me.data} />
          <Button variant="ghost" rightIcon={ExternalLink} onPress={() => router.push(`/u/${me.data.user.handle}`)}>
            View profile
          </Button>
        </>
      )}
    </Screen>
  );
}
