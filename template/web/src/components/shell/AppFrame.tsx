"use client";

import { useAuthSession, useConfig, useMe, type Me } from "@scaffoldapp/shared";
import { AppShell, Banner, BottomNav, SideNav, TopBar, type ShellNavItem } from "@scaffoldapp/ui-web";
import { House, Settings, ShieldCheck } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, type ReactNode } from "react";
import { signOutTarget } from "@/components/auth/useSignOut";
import { SupportLinks } from "@/components/account/SupportLinks";
import { ConsentScreen } from "@/components/legal/ConsentScreen";
import { useNavBadges } from "@/features/nav";
import { consentRoutes, featureNav, headerActions, type NavItem } from "@/features/registry";
import { signInHref } from "@/lib/redirect";
import { AccountBlocked } from "./AccountBlocked";
import { AccountMenu } from "./AccountMenu";
import { AppMark } from "./AppMark";
import { FrameSkeleton } from "./FrameSkeleton";

/** Most tabs the phone bar shows (design.md); further feature sections move to the account menu. */
const MAX_TABS = 5;

function toShellItem(item: NavItem, badge?: number): ShellNavItem {
  const Icon = item.icon;
  return { href: item.href, label: item.label, icon: <Icon aria-hidden />, ...(item.match ? { match: item.match } : {}), ...(badge ? { badge } : {}) };
}

/** Registry `consentRoutes` as whole-path regexes: these open while consent is pending. */
const CONSENT_ROUTES = consentRoutes.map((p) => new RegExp(`^${p}$`));

const HOME: NavItem = { href: "/", label: "Home", icon: House, match: "exact" };
const SETTINGS: NavItem = { href: "/settings", label: "Settings", icon: Settings };
const ADMIN: NavItem = { href: "/admin", label: "Admin", icon: ShieldCheck };

function Shell({ me, children }: { me: Me; children: ReactNode }) {
  const pathname = usePathname() ?? "/";
  const config = useConfig();
  const badges = useNavBadges(featureNav);
  const features = featureNav.map((item, i) => toShellItem(item, badges[i]));
  const staff = me.admin_role !== null;

  const tabRoom = MAX_TABS - 2; // Home and Settings always show
  const tabFeatures = features.length > tabRoom ? features.slice(0, tabRoom) : features;
  const overflow = featureNav.slice(tabFeatures.length);

  return (
    <AppShell
      sideNav={
        <SideNav
          items={[toShellItem(HOME), ...features]}
          secondary={[toShellItem(SETTINGS), ...(staff ? [toShellItem(ADMIN)] : [])]}
          activeHref={pathname}
          linkAs={Link}
          header={<AppMark />}
        />
      }
      topBar={
        <TopBar
          leading={<AppMark className="lg:hidden" />}
          actions={
            <>
              {headerActions.map(({ id, Component }) => (
                <Component key={id} />
              ))}
              <AccountMenu me={me} overflow={overflow} />
            </>
          }
        />
      }
      bottomNav={<BottomNav items={[toShellItem(HOME), ...tabFeatures, toShellItem(SETTINGS)]} activeHref={pathname} linkAs={Link} />}
      notice={
        me.status === "suspended" ? (
          <div className="mx-auto w-full max-w-content px-gutter pt-4">
            <Banner tone="warning">
              Your account is suspended: you can view, but not change, anything.
              {config.data?.support_email ? (
                <>
                  {" "}
                  <a className="underline underline-offset-2" href={`mailto:${config.data.support_email}`}>
                    Contact support
                  </a>
                </>
              ) : null}
              <SupportLinks variant="inline" />
            </Banner>
          </div>
        ) : null
      }
    >
      {children}
    </AppShell>
  );
}

/**
 * The signed-in app's gate and frame, in order: session (signed out → /sign-in with a
 * return path) → `me` (blocked account screens) → consent (`me.consent.required` →
 * ConsentScreen, except on registry `consentRoutes`) → the shell with core nav +
 * `featureNav` + `headerActions`, and a banner while suspended.
 */
export function AppFrame({ children }: { children: ReactNode }) {
  const auth = useAuthSession();
  const pathname = usePathname() ?? "/";
  const router = useRouter();
  const signedIn = auth.isLoaded && auth.isSignedIn;
  const me = useMe({ enabled: signedIn });

  useEffect(() => {
    if (!auth.isLoaded || auth.isSignedIn) return;
    router.replace(signOutTarget() ?? signInHref(`${pathname}${window.location.search}`));
  }, [auth.isLoaded, auth.isSignedIn, pathname, router]);

  if (!signedIn || me.isPending) return <FrameSkeleton />;
  // A failed background refetch keeps the last good `me`; only a first load that fails blocks.
  if (me.isError && !me.data) return <AccountBlocked error={me.error} onRetry={() => void me.refetch()} retrying={me.isFetching} />;
  if (!me.data) return <FrameSkeleton />;
  if (me.data.consent.required && !CONSENT_ROUTES.some((re) => re.test(pathname))) return <ConsentScreen />;
  return <Shell me={me.data}>{children}</Shell>;
}
