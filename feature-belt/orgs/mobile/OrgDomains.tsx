import {
  errorMessage,
  hasErrorCode,
  orgsNoun,
  orgsNounTitle,
  useAddOrgDomain,
  useOrgDomains,
  useRemoveOrgDomain,
  useUpdateOrgDomain,
  useVerifyOrgDomain,
  type Org,
  type OrgDomain,
  type OrgDomainPolicy,
} from "@scaffoldapp/shared";
import { Badge, Button, ConfirmDialog, EmptyState, ErrorState, Field, haptic, Input, Label, ListGroup, ListRow, SegmentedControl, Sheet, SkeletonRow, Text, toast, useTheme } from "@scaffoldapp/ui-native";
import { AtSign, Plus, RefreshCw, Share2, Trash2 } from "lucide-react-native";
import { useState } from "react";
import { Share, View } from "react-native";
import { DOMAIN_POLICY_LABEL, DOMAIN_STATUS_LABEL, DOMAIN_STATUS_TONE } from "./labels";

function Record({ label, value }: { label: string; value: string }) {
  const theme = useTheme();
  return (
    <View style={{ gap: theme.space[1] }}>
      <Label tone="muted">{label}</Label>
      <Text selectable>{value}</Text>
    </View>
  );
}

function DomainSheet({
  org,
  domain,
  onClose,
  onDismissed,
  onRemove,
}: {
  org: Org;
  domain: OrgDomain | null;
  onClose: () => void;
  onDismissed: () => void;
  onRemove: (d: OrgDomain) => void;
}) {
  const theme = useTheme();
  const s = theme.space;
  const c = theme.colors;
  const update = useUpdateOrgDomain(org.id);
  const verify = useVerifyOrgDomain(org.id);
  const d = domain;
  // An email proves one mailbox, not the domain: joining people on opening the app needs the DNS record.
  const dnsForAutoJoin = d !== null && d.status === "verified" && d.verify_method === "email" && d.policy === "auto_join";
  const showDns = d !== null && (d.status === "pending" || dnsForAutoJoin);
  const check = () => {
    if (!d) return;
    verify.mutate(d.id, {
      onSuccess: (v) => {
        haptic.success();
        toast.success(`${v.domain} verified`);
      },
      onError: (e) => (hasErrorCode(e, "verification_failed") ? haptic.error() : toast.error(errorMessage(e, "Couldn't check the domain. Try again."))),
    });
  };
  return (
    <Sheet open={d !== null} onClose={onClose} onDismissed={onDismissed} title={d?.domain ?? ""} snapPoints={showDns ? ["85%"] : [380]}>
      {d ? (
        <View style={{ gap: s[6] }}>
          <View style={{ flexDirection: "row", alignItems: "center", gap: s[2] }}>
            <Badge tone={DOMAIN_STATUS_TONE[d.status]}>{DOMAIN_STATUS_LABEL[d.status]}</Badge>
          </View>
          <Field label={`People at ${d.domain}`} info="Join automatically: no approval needed, and once the DNS record is verified they become members as soon as they open the app. Ask to join: an admin approves each request in Members.">
            <SegmentedControl<OrgDomainPolicy>
              accessibilityLabel={`People at ${d.domain}`}
              value={d.policy}
              onValueChange={(policy) => update.mutate({ domainId: d.id, policy }, { onError: (e) => toast.error(errorMessage(e, "Couldn't change that. Try again.")) })}
              options={[
                { value: "auto_join", label: DOMAIN_POLICY_LABEL.auto_join },
                { value: "request", label: DOMAIN_POLICY_LABEL.request },
              ]}
            />
          </Field>
          {showDns ? (
            <View style={{ gap: s[4] }}>
              <Text tone="muted">
                {dnsForAutoJoin
                  ? `Verified with an email address. Until this TXT record is verified, people join from "${orgsNounTitle("many")} you can join" rather than on opening the app.`
                  : "Add this TXT record at your DNS provider, then check."}
              </Text>
              <Record label="Name" value={d.txt_name} />
              <Record label="Value" value={d.txt_value} />
              {d.last_check_error ? (
                <Text accessibilityRole="alert" style={{ color: c.warningStrong }}>
                  {d.last_check_error}
                </Text>
              ) : null}
              <View style={{ gap: s[2] }}>
                <Button leftIcon={RefreshCw} loading={verify.isPending} onPress={check}>
                  Check now
                </Button>
                <Button variant="secondary" leftIcon={Share2} onPress={() => void Share.share({ message: `TXT record\nName: ${d.txt_name}\nValue: ${d.txt_value}` })}>
                  Share record
                </Button>
              </View>
            </View>
          ) : d.status === "taken" ? (
            <Text tone="muted">Another {orgsNoun()} verified this domain with its DNS record. Remove it, or contact support if the domain is yours.</Text>
          ) : null}
          <View style={{ marginHorizontal: -s[4] }}>
            <ListRow title="Remove domain" destructive chevron={false} leading={<Trash2 size={22} color={c.dangerStrong} strokeWidth={2} />} onPress={() => onRemove(d)} />
          </View>
        </View>
      ) : null}
    </Sheet>
  );
}

/** Domains segment (admins): claim email domains, verify them, choose who may join and how. */
export function OrgDomains({ org }: { org: Org }) {
  const theme = useTheme();
  const s = theme.space;
  const domains = useOrgDomains(org.id);
  const add = useAddOrgDomain(org.id);
  const remove = useRemoveOrgDomain(org.id);
  const [domain, setDomain] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [openId, setOpenId] = useState<string | null>(null);
  const [queued, setQueued] = useState<OrgDomain | null>(null);
  const [removing, setRemoving] = useState<OrgDomain | null>(null);
  const rows = domains.data?.items ?? [];
  const open = rows.find((d) => d.id === openId) ?? null;

  const submit = () => {
    const value = domain.trim().toLowerCase();
    if (!value.includes(".")) return setError("Enter a domain like example.com.");
    setError(null);
    add.mutate(
      { domain: value },
      {
        onSuccess: (d) => {
          haptic.success();
          setDomain("");
          toast.success(d.status === "verified" ? `${d.domain} verified with your email` : `${d.domain} added`);
          if (d.status !== "verified") setOpenId(d.id);
        },
        onError: (e) => setError(errorMessage(e, "Couldn't add the domain. Try again.")),
      },
    );
  };

  return (
    <View style={{ gap: s[8] }}>
      <View style={{ gap: s[4] }}>
        <Field
          label="Add an email domain"
          info={`People whose verified email is on a verified domain find ${org.name} in Settings and join it, or ask to, without an invite. It's verified at once when your own email is on it.`}
          error={error}
        >
          <Input value={domain} onChangeText={setDomain} placeholder="example.com" keyboardType="url" autoCapitalize="none" autoCorrect={false} returnKeyType="done" onSubmitEditing={submit} />
        </Field>
        <Button leftIcon={Plus} loading={add.isPending} onPress={submit}>
          Add domain
        </Button>
      </View>

      <ListGroup title="Email domains">
        {domains.isPending ? (
          <View accessibilityLabel="Loading" accessibilityRole="progressbar">
            <SkeletonRow />
          </View>
        ) : domains.isError ? (
          <ErrorState compact title="Couldn't load domains" info={errorMessage(domains.error)} onRetry={() => void domains.refetch()} retrying={domains.isRefetching} />
        ) : rows.length === 0 ? (
          <EmptyState compact icon={AtSign} title="No domains yet" />
        ) : (
          rows.map((d) => (
            <ListRow key={d.id} title={d.domain} trailing={<Badge tone={DOMAIN_STATUS_TONE[d.status]}>{DOMAIN_STATUS_LABEL[d.status]}</Badge>} onPress={() => setOpenId(d.id)} />
          ))
        )}
      </ListGroup>

      <DomainSheet
        org={org}
        domain={open}
        onClose={() => setOpenId(null)}
        onDismissed={() => {
          // Open the confirmation once the sheet's modal has gone.
          if (queued) {
            setRemoving(queued);
            setQueued(null);
          }
        }}
        onRemove={(d) => {
          setQueued(d);
          setOpenId(null);
        }}
      />
      <ConfirmDialog
        open={removing !== null}
        onClose={() => setRemoving(null)}
        title={removing ? `Remove ${removing.domain}?` : ""}
        tone="danger"
        confirmLabel="Remove"
        errorText={(e) => errorMessage(e, "Couldn't remove the domain. Try again.")}
        onConfirm={async () => {
          if (!removing) return;
          await remove.mutateAsync(removing.id);
          haptic.success();
          toast.success(`${removing.domain} removed`);
        }}
      >
        <Text tone="muted">People with an email on it can no longer join on their own. Current members stay.</Text>
      </ConfirmDialog>
    </View>
  );
}
