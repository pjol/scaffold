"use client";

import { errorMessage, useReposLink, type ApiError } from "@scaffoldapp/shared";
import { Button, Card, EmptyState, ErrorState, ProviderIcon, Skeleton, SkeletonGroup } from "@scaffoldapp/ui-web";
import { ExternalLink, Link2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect } from "react";

const PLATFORM: Record<string, string> = { github: "GitHub", gitlab: "GitLab" };

/**
 * A drive link to a connected repository. Whoever opens it is checked live with their own
 * GitHub or GitLab connection: with access they land in the repository; without, they're told
 * how to connect (or that their account can't see it).
 */
export function RepoLinkScreen({ linkId }: { linkId: string }) {
  const router = useRouter();
  const link = useReposLink(linkId);
  const target = link.data?.repo_id;
  useEffect(() => {
    if (target) router.replace(`/repos/${target}`);
  }, [target, router]);
  return (
    <div className="mx-auto w-full max-w-content px-gutter py-section">
      {link.isPending || target ? (
        <SkeletonGroup label="Opening the repository" className="flex flex-col gap-4">
          <Skeleton className="h-10 w-64" />
          <Skeleton className="h-40 w-full" />
        </SkeletonGroup>
      ) : link.isError ? (
        <Card>
          <ErrorState
            title={(link.error as ApiError).status === 404 ? "Link not found" : "Couldn't open this link"}
            info={errorMessage(link.error, "It may have been deleted or not shared with you.")}
            onRetry={() => void link.refetch()}
            retrying={link.isFetching}
          />
        </Card>
      ) : (
        <Card>
          <EmptyState
            icon={link.data.provider ? <ProviderIcon provider={link.data.provider} size={24} /> : <Link2 />}
            title={
              link.data.reason === "no_access"
                ? `Your ${PLATFORM[link.data.provider] ?? ""} account can't open ${link.data.full_name}`
                : link.data.reason === "unavailable"
                  ? "Connected repositories aren't available here"
                  : `Connect ${PLATFORM[link.data.provider] ?? "the platform"} to open ${link.data.full_name}`
            }
            info={
              link.data.reason === "no_access"
                ? `Ask someone on ${PLATFORM[link.data.provider] ?? "the platform"} to give your account access, then open the link again.`
                : "Links open with your own account, so you see exactly what it can see."
            }
            action={
              <div className="flex flex-wrap justify-center gap-2">
                {link.data.reason === "no_connection" && link.data.fix_url ? (
                  <Button asChild>
                    <Link href={link.data.fix_url}>Connect {PLATFORM[link.data.provider] ?? ""}</Link>
                  </Button>
                ) : null}
                {link.data.web_url ? (
                  <Button asChild variant="outline" rightIcon={<ExternalLink />}>
                    <a href={link.data.web_url} target="_blank" rel="noopener noreferrer">
                      Open in {PLATFORM[link.data.provider] ?? "the platform"}
                    </a>
                  </Button>
                ) : null}
              </div>
            }
          />
        </Card>
      )}
    </div>
  );
}
