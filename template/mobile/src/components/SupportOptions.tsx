import { errorMessage, useConfig } from "@scaffoldapp/shared";
import { ErrorState, ListGroup, ListRow, SkeletonRow, toast, useTheme } from "@scaffoldapp/ui-native";
import { router, type Href } from "expo-router";
import { Mail } from "lucide-react-native";
import { Linking } from "react-native";
import { supportLinks, type SupportLink } from "@/features/registry";
import { emailSupport } from "@/lib/support";

function openLink(link: SupportLink) {
  if (link.href.startsWith("/")) router.push(link.href as Href);
  else void Linking.openURL(link.href).catch(() => toast.error("Couldn't open that link. Try again."));
}

/**
 * The support email (from /v1/config) and the features' `supportLinks`. Used on Settings →
 * Support, the consent screen and the blocked-account screen.
 */
export function SupportOptions({ title = "Contact support" }: { title?: string }) {
  const theme = useTheme();
  const config = useConfig();
  const email = config.data?.support_email;
  const icon = (I: typeof Mail) => <I size={22} color={theme.colors.textMuted} strokeWidth={2} />;

  if (!email && supportLinks.length === 0 && !config.isPending) {
    return <ErrorState compact title="Couldn't load support details" info={config.error ? errorMessage(config.error) : undefined} onRetry={() => void config.refetch()} retrying={config.isRefetching} />;
  }
  return (
    <ListGroup title={title}>
      {email ? (
        <ListRow
          title={email}
          leading={icon(Mail)}
          accessibilityHint="Writes an email to support"
          onPress={() => void emailSupport(email).catch(() => toast.error(`No mail app found. Email ${email}.`))}
        />
      ) : config.isPending ? (
        <SkeletonRow />
      ) : null}
      {supportLinks.map((link) => (
        <ListRow key={link.id} title={link.title} leading={icon(link.icon)} onPress={() => openLink(link)} />
      ))}
    </ListGroup>
  );
}
