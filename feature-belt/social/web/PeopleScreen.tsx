"use client";

import { useSocialPeople, useSocialSuggestions } from "@scaffoldapp/shared";
import { Button, EmptyState, ErrorState } from "@scaffoldapp/ui-web";
import { Rss, SearchX, Users } from "lucide-react";
import Link from "next/link";
import { SearchInput } from "@/components/admin/SearchInput";
import { PageHeader, Section } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { PeopleSkeleton, PersonRow } from "./parts";

function Suggestions() {
  const s = useSocialSuggestions();
  return (
    <Section title="Suggested for you" info="People followed by people you follow, then the most followed.">
      {s.isPending ? (
        <PeopleSkeleton label="Loading suggestions" />
      ) : s.isError ? (
        <ErrorState compact title="Couldn't load suggestions" info={s.error.message} onRetry={() => void s.refetch()} retrying={s.isFetching} />
      ) : s.data.items.length === 0 ? (
        <EmptyState compact icon={<Users />} title="Nobody to suggest yet" />
      ) : (
        <ul aria-label="Suggested people">
          {s.data.items.map((p) => (
            <PersonRow key={p.user_id} person={p} info={p.mutuals ? `Followed by ${p.mutuals} ${p.mutuals === 1 ? "person" : "people"} you follow` : undefined} />
          ))}
        </ul>
      )}
    </Section>
  );
}

function Results({ q, onClear }: { q: string; onClear: () => void }) {
  const people = useSocialPeople(q);
  const short = q.replace(/^@/, "").trim().length < 2;
  return (
    <Section title="Results">
      {short ? (
        <EmptyState compact icon={<SearchX />} title="Type 2 or more characters" />
      ) : people.isPending || (people.isFetching && people.query !== q.trim()) ? (
        <PeopleSkeleton label="Searching" rows={3} />
      ) : people.isError ? (
        <ErrorState compact title="Couldn't search people" info={people.error.message} onRetry={() => void people.refetch()} retrying={people.isFetching} />
      ) : people.data.items.length === 0 ? (
        <EmptyState
          compact
          icon={<SearchX />}
          title={`Nobody matches “${q.trim()}”`}
          action={
            <Button variant="secondary" size="auto" onClick={onClear}>
              Clear search
            </Button>
          }
        />
      ) : (
        <ul aria-label="People found">
          {people.data.items.map((p) => (
            <PersonRow key={p.user_id} person={p} />
          ))}
        </ul>
      )}
    </Section>
  );
}

/** /people: search by name or @handle (`?q=`), and suggestions while the box is empty. */
export function PeopleScreen() {
  const [q, setQ] = useUrlState("q", "");
  return (
    <div className="mx-auto flex w-full max-w-content flex-col px-gutter py-section">
      <div className="mx-auto flex w-full max-w-prose flex-col gap-6">
        <PageHeader
          title="People"
          action={
            <Button asChild variant="secondary" size="sm" leftIcon={<Rss />}>
              <Link href="/following">Following</Link>
            </Button>
          }
        />
        <SearchInput value={q} onSearch={setQ} placeholder="Search by name or @handle" label="Search people" />
        <div className="flex flex-col gap-section">{q ? <Results q={q} onClear={() => setQ(null)} /> : <Suggestions />}</div>
      </div>
    </div>
  );
}
