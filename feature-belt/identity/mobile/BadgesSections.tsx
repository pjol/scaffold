import {
  errorMessage,
  useAddIdentityDomain,
  useApplyForIdentityBadge,
  useCheckIdentityDomain,
  useIdentityBadges,
  useRemoveIdentityDomain,
  useResubmitIdentityBadgeApplication,
  useWithdrawIdentityBadgeApplication,
  type IdentityApplicationStatus,
  type IdentityBadgeApplication,
  type IdentityBadges,
  type IdentityDomain,
  type IdentityEvidenceKind,
} from "@scaffoldapp/shared";
import {
  Badge,
  Banner,
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  InfoTip,
  Input,
  ListGroup,
  ListRow,
  Sheet,
  Skeleton,
  Text,
  TextArea,
  TitleSm,
  ToggleChip,
  haptic,
  toast,
  useTheme,
  type BadgeTone,
} from "@scaffoldapp/ui-native";
import { router, type Href } from "expo-router";
import { Award, Check, Globe, Plus, Trash2, X } from "lucide-react-native";
import { useState, type ReactNode } from "react";
import { View } from "react-native";
import { IDENTITY_BADGE_MEANING, IdentityBadgeMark, identityBadgeName } from "./BadgeMark";
import { identityPaidBadgeLink } from "./seams";

const STATUS: Record<IdentityApplicationStatus, { label: string; tone: BadgeTone }> = {
  submitted: { label: "In review", tone: "info" },
  needs_info: { label: "Needs more", tone: "warning" },
  approved: { label: "Approved", tone: "success" },
  rejected: { label: "Not approved", tone: "danger" },
  withdrawn: { label: "Withdrawn", tone: "neutral" },
};
const KINDS: { value: IdentityEvidenceKind; label: string; placeholder: string }[] = [
  { value: "url", label: "Link", placeholder: "https://example.com/about" },
  { value: "domain", label: "Domain", placeholder: "example.com" },
  { value: "account", label: "Account", placeholder: "github:yourname" },
  { value: "text", label: "Note", placeholder: "Anything else a reviewer should know" },
];

function dateText(iso: string | null): string {
  if (!iso) return "";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

function Section({ title, info, children }: { title: string; info?: string; children: ReactNode }) {
  const s = useTheme().space;
  return (
    <View style={{ gap: s[3] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <TitleSm style={{ flexShrink: 1 }}>{title}</TitleSm>
        {info ? <InfoTip content={info} /> : null}
      </View>
      {children}
    </View>
  );
}

type Draft = { kind: IdentityEvidenceKind; ref: string };

function ApplySheet({ st, open, onClose, resubmit }: { st: IdentityBadges; open: boolean; onClose: () => void; resubmit: IdentityBadgeApplication | null }) {
  const s = useTheme().space;
  const apply = useApplyForIdentityBadge();
  const again = useResubmitIdentityBadgeApplication();
  const taken = new Set(st.applications.filter((a) => (a.status === "submitted" || a.status === "needs_info") && a.id !== resubmit?.id).map((a) => a.category));
  const [category, setCategory] = useState(resubmit?.category ?? st.categories.find((c) => !taken.has(c.key))?.key ?? "");
  const [label, setLabel] = useState(resubmit?.display_label ?? "");
  const [note, setNote] = useState(resubmit?.note ?? "");
  const [evidence, setEvidence] = useState<Draft[]>(resubmit ? resubmit.evidence.map(({ kind, ref }) => ({ kind, ref })) : [{ kind: "url", ref: "" }]);
  const [error, setError] = useState<string | null>(null);
  const filled = evidence.filter((e) => e.ref.trim());
  const edit = (i: number, next: Partial<Draft>) => setEvidence(evidence.map((e, j) => (j === i ? { ...e, ...next } : e)));
  const submit = () => {
    setError(null);
    const body = { category, display_label: label.trim(), note: note.trim(), evidence: filled.map((e) => ({ kind: e.kind, ref: e.ref.trim() })) };
    const done = {
      onSuccess: () => {
        haptic.success();
        toast.success(resubmit ? "Sent again" : "Application sent");
        onClose();
      },
      onError: (e: unknown) => setError(errorMessage(e, "Couldn't send your application. Try again.")),
    };
    if (resubmit) again.mutate({ id: resubmit.id, body }, done);
    else apply.mutate(body, done);
  };
  return (
    <Sheet
      open={open}
      onClose={onClose}
      snapPoints={["90%"]}
      title={resubmit ? "Update your application" : "Apply for a badge"}
      info="Verified domains and linked accounts count as checked evidence. A reviewer opens links and reads notes."
      footer={
        <Button loading={apply.isPending || again.isPending} disabled={!category || filled.length === 0 || taken.has(category)} onPress={submit} fullWidth>
          {resubmit ? "Send again" : "Send application"}
        </Button>
      }
    >
      <View style={{ gap: s[5] }}>
        {resubmit?.reviewer_note ? <Banner tone="warning" title={resubmit.reviewer_note} /> : null}
        <Field label="Category">
          <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
            {st.categories.map((c) => (
              <ToggleChip key={c.key} size="sm" pressed={category === c.key} disabled={!!resubmit || taken.has(c.key)} onPressedChange={() => setCategory(c.key)}>
                {c.label}
              </ToggleChip>
            ))}
          </View>
        </Field>
        <Field label="Name to show" info="What the badge says: your business, organization or public name. Empty uses the category.">
          <Input value={label} maxLength={60} onChangeText={setLabel} placeholder={st.categories.find((c) => c.key === category)?.label} />
        </Field>
        <Field label={`Evidence (${filled.length}/10)`}>
          <View style={{ gap: s[4] }}>
            {evidence.map((ev, i) => (
              <View key={i} style={{ gap: s[2] }}>
                <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2], alignItems: "center" }}>
                  {KINDS.map((k) => (
                    <ToggleChip key={k.value} size="sm" pressed={ev.kind === k.value} onPressedChange={() => edit(i, { kind: k.value })}>
                      {k.label}
                    </ToggleChip>
                  ))}
                  {evidence.length > 1 ? <IconButton label="Remove" icon={Trash2} variant="ghost" onPress={() => setEvidence(evidence.filter((_, j) => j !== i))} /> : null}
                </View>
                <Input
                  accessibilityLabel="Evidence"
                  value={ev.ref}
                  maxLength={500}
                  autoCapitalize="none"
                  autoCorrect={ev.kind === "text"}
                  placeholder={KINDS.find((k) => k.value === ev.kind)?.placeholder}
                  onChangeText={(t) => edit(i, { ref: t })}
                />
              </View>
            ))}
            {evidence.length < 10 ? (
              <Button variant="secondary" size="md" leftIcon={Plus} onPress={() => setEvidence([...evidence, { kind: "url", ref: "" }])}>
                Add evidence
              </Button>
            ) : null}
          </View>
        </Field>
        <Field label="Note for reviewers">
          <TextArea value={note} maxLength={2000} onChangeText={setNote} />
        </Field>
        {error ? <Banner tone="danger" title={error} /> : null}
      </View>
    </Sheet>
  );
}

function DomainCard({ d }: { d: IdentityDomain }) {
  const theme = useTheme();
  const s = theme.space;
  const check = useCheckIdentityDomain();
  const remove = useRemoveIdentityDomain();
  const [confirm, setConfirm] = useState(false);
  return (
    <Card>
      <View style={{ gap: s[3] }}>
        <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
          <Globe size={20} color={theme.colors.textMuted} />
          <Text weight={600} style={{ flex: 1 }} numberOfLines={1}>
            {d.domain}
          </Text>
          {d.verified ? <Badge tone="success">Verified</Badge> : null}
          <IconButton label="Remove domain" icon={Trash2} variant="ghost" onPress={() => setConfirm(true)} />
        </View>
        {!d.verified ? (
          <>
            <Text role="label" tone="muted">
              Add a TXT record in your domain's DNS. Press and hold to copy.
            </Text>
            <Text role="label" selectable>
              Name: {d.txt_name}
            </Text>
            <Text role="label" selectable>
              Value: {d.txt_value}
            </Text>
            {d.last_error ? <Banner tone="warning" title={d.last_error} /> : null}
            <Button
              variant="secondary"
              size="md"
              loading={check.isPending}
              onPress={() =>
                check.mutate(d.domain, {
                  onSuccess: () => toast.success(`${d.domain} verified`),
                  onError: (e) => toast.error(errorMessage(e, "Couldn't check the domain. Try again.")),
                })
              }
            >
              Check
            </Button>
          </>
        ) : null}
      </View>
      <ConfirmDialog open={confirm} onClose={() => setConfirm(false)} title={`Remove ${d.domain}?`} confirmLabel="Remove" tone="danger" onConfirm={() => remove.mutateAsync(d.domain)}>
        <Text tone="muted">Applications you already sent keep it as evidence.</Text>
      </ConfirmDialog>
    </Card>
  );
}

function Domains({ domains }: { domains: IdentityDomain[] }) {
  const s = useTheme().space;
  const add = useAddIdentityDomain();
  const [domain, setDomain] = useState("");
  return (
    <Section title="Domains" info="Prove you run a website: add the TXT record in your domain's DNS settings, then check it. Public email and hosting domains don't count.">
      <View style={{ flexDirection: "row", gap: s[2], alignItems: "center" }}>
        <Input
          accessibilityLabel="Domain"
          containerStyle={{ flex: 1 }}
          placeholder="example.com"
          autoCapitalize="none"
          autoCorrect={false}
          keyboardType="url"
          value={domain}
          onChangeText={setDomain}
        />
        <Button
          variant="secondary"
          size="md"
          loading={add.isPending}
          disabled={domain.trim().length < 4}
          onPress={() =>
            add.mutate(domain.trim(), {
              onSuccess: () => setDomain(""),
              onError: (e) => toast.error(errorMessage(e, "Couldn't add the domain. Try again.")),
            })
          }
        >
          Add
        </Button>
      </View>
      {domains.map((d) => (
        <DomainCard key={d.domain} d={d} />
      ))}
    </Section>
  );
}

function ApplicationItem({ a, st, onResubmit }: { a: IdentityBadgeApplication; st: IdentityBadges; onResubmit: (a: IdentityBadgeApplication) => void }) {
  const s = useTheme().space;
  const withdraw = useWithdrawIdentityBadgeApplication();
  const [confirm, setConfirm] = useState(false);
  const cat = st.categories.find((c) => c.key === a.category)?.label ?? a.category;
  const open = a.status === "submitted" || a.status === "needs_info";
  return (
    <View style={{ gap: s[2], paddingVertical: s[3] }}>
      <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
        <Text weight={500} style={{ flex: 1 }} numberOfLines={2}>
          {a.display_label && a.display_label !== cat ? `${cat} · ${a.display_label}` : cat}
        </Text>
        <Badge tone={STATUS[a.status].tone}>{STATUS[a.status].label}</Badge>
      </View>
      <Text role="caption" tone="muted">
        Sent {dateText(a.submitted_at)} · {a.evidence.filter((e) => e.verified).length} of {a.evidence.length} evidence checked
      </Text>
      {a.reviewer_note && a.status !== "submitted" && a.status !== "withdrawn" ? (
        <Banner tone={a.status === "rejected" ? "danger" : a.status === "approved" ? "success" : "warning"} title={a.reviewer_note} />
      ) : null}
      {open ? (
        <View style={{ flexDirection: "row", flexWrap: "wrap", gap: s[2] }}>
          {a.status === "needs_info" ? (
            <Button size="md" onPress={() => onResubmit(a)}>
              Update and send
            </Button>
          ) : null}
          <Button size="md" variant="ghost" onPress={() => setConfirm(true)}>
            Withdraw
          </Button>
        </View>
      ) : null}
      <ConfirmDialog
        open={confirm}
        onClose={() => setConfirm(false)}
        title="Withdraw this application?"
        confirmLabel="Withdraw"
        tone="danger"
        onConfirm={() => withdraw.mutateAsync(a.id)}
        errorText={(e) => errorMessage(e, "Couldn't withdraw. Try again.")}
      >
        <Text tone="muted">You can apply again later.</Text>
      </ConfirmDialog>
    </View>
  );
}

function PaidBadge({ st }: { st: IdentityBadges }) {
  const theme = useTheme();
  const paid = st.paid;
  const label = st.kinds.find((k) => k.kind === "paid")?.label ?? "Supporter";
  const href = identityPaidBadgeLink(paid.entitlement);
  if (!href && !paid.active && !paid.held) return null;
  return (
    <Section title={`${label} badge`} info={IDENTITY_BADGE_MEANING.paid}>
      {paid.held ? (
        <Banner tone="warning" title={`Staff removed your ${label.toLowerCase()} badge. Contact support to ask about it.`} />
      ) : paid.active ? (
        <ListGroup>
          <ListRow leading={<IdentityBadgeMark kind="paid" />} title="Active" info={paid.expires_at ? `Renews or ends ${dateText(paid.expires_at)}.` : undefined} />
        </ListGroup>
      ) : (
        <>
          <ListGroup title="To get it">
            {paid.requirements.map((r) => (
              <ListRow
                key={r.key}
                leading={
                  <View accessible accessibilityLabel={r.met ? "Met" : "Not met"}>
                    {r.met ? <Check size={20} color={theme.colors.successStrong} /> : <X size={20} color={theme.colors.dangerStrong} />}
                  </View>
                }
                title={r.label}
                info={r.detail}
              />
            ))}
          </ListGroup>
          {href ? (
            <Button disabled={!paid.eligible} onPress={() => router.push(href as Href)}>
              {`Get the ${label.toLowerCase()} badge`}
            </Button>
          ) : null}
        </>
      )}
    </Section>
  );
}

/** Settings → Verification, below ID verification: badges, the paid badge, applications and domain proofs. */
export function IdentityBadgesSections() {
  const theme = useTheme();
  const s = theme.space;
  const q = useIdentityBadges();
  const [sheet, setSheet] = useState<{ open: boolean; resubmit: IdentityBadgeApplication | null; n: number }>({ open: false, resubmit: null, n: 0 });
  if (q.isPending) {
    return (
      <View accessibilityLabel="Loading badges" accessibilityRole="progressbar" style={{ gap: s[3] }}>
        <Skeleton height={24} width="40%" />
        <Skeleton height={56} />
      </View>
    );
  }
  if (q.isError) return <ErrorState title="Couldn't load your badges" info={errorMessage(q.error)} onRetry={() => void q.refetch()} retrying={q.isRefetching} />;
  const st = q.data;
  const openSheet = (resubmit: IdentityBadgeApplication | null) => setSheet((v) => ({ open: true, resubmit, n: v.n + 1 }));
  return (
    <View style={{ gap: s[8] }}>
      <Section title="Badges" info="The highest one shows next to your name: official, then organization, notable, supporter.">
        {st.badges.length === 0 ? (
          <EmptyState compact icon={Award} title="No badges yet" />
        ) : (
          <ListGroup>
            {st.badges.map((b) => (
              <ListRow
                key={b.kind}
                leading={<IdentityBadgeMark kind={b.kind} />}
                title={identityBadgeName(b)}
                info={[IDENTITY_BADGE_MEANING[b.kind], `Since ${dateText(b.since)}.`, b.expires_at ? `${b.kind === "paid" ? "Renews or ends" : "Reviewed again"} ${dateText(b.expires_at)}.` : ""]
                  .filter(Boolean)
                  .join(" ")}
              />
            ))}
          </ListGroup>
        )}
      </Section>
      <PaidBadge st={st} />
      {st.categories.length > 0 ? (
        <Section title="Apply for a badge" info={`For ${st.categories.map((c) => c.label.toLowerCase()).join(", ")}. Staff review it; badges are reviewed again after a while.`}>
          <Button variant="secondary" onPress={() => openSheet(null)}>
            Apply
          </Button>
        </Section>
      ) : null}
      {st.applications.length > 0 ? (
        <Section title="Applications">
          <ListGroup>
            {st.applications.map((a) => (
              <ApplicationItem key={a.id} a={a} st={st} onResubmit={openSheet} />
            ))}
          </ListGroup>
        </Section>
      ) : null}
      <Domains domains={st.domains} />
      <ApplySheet key={sheet.n} st={st} open={sheet.open} onClose={() => setSheet((v) => ({ ...v, open: false }))} resubmit={sheet.resubmit} />
    </View>
  );
}
