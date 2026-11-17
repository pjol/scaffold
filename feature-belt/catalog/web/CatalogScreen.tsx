"use client";

import { catalogQueryReady, useCatalogFind, useCatalogItems, useCatalogSummary, useMe, type CatalogCategory } from "@scaffoldapp/shared";
import { Button, Chip, EmptyState, ErrorState, Select } from "@scaffoldapp/ui-web";
import { Library, SearchX, Settings2 } from "lucide-react";
import Link from "next/link";
import { SearchInput } from "@/components/admin/SearchInput";
import { LoadMore } from "@/components/shell/LoadMore";
import { Screen } from "@/components/shell/PageHeader";
import { useUrlState } from "@/lib/url-state";
import { CatalogItemRows, CatalogRowsSkeleton } from "./components";
import { catalogPaths } from "./nav";

const CHIPS = 12;

/** Category chips (the largest few, then a picker for the rest); the choice lives in the URL. */
function CategoryFilter({ categories, value, onChange }: { categories: CatalogCategory[]; value: string; onChange: (v: string) => void }) {
  if (categories.length < 2) return null;
  const top = categories.slice(0, CHIPS);
  const rest = categories.slice(CHIPS);
  const picked = rest.find((c) => c.name === value);
  return (
    <div className="-mx-gutter flex items-center gap-2 overflow-x-auto px-gutter pb-1" role="group" aria-label="Category">
      <Chip size="sm" selected={value === ""} aria-pressed={value === ""} onClick={() => onChange("")}>
        All
      </Chip>
      {[...top, ...(picked ? [picked] : [])].map((c) => (
        <Chip key={c.name} size="sm" count={c.count} selected={value === c.name} aria-pressed={value === c.name} onClick={() => onChange(value === c.name ? "" : c.name)}>
          {c.name}
        </Chip>
      ))}
      {rest.length > 0 ? (
        <Select
          aria-label="More categories"
          placeholder="More…"
          options={rest.map((c) => ({ value: c.name, label: `${c.name} (${c.count})` }))}
          value={picked ? picked.name : ""}
          onValueChange={onChange}
          className="w-40 shrink-0"
        />
      ) : null}
    </div>
  );
}

/** /catalog: search by name or alias (on the device when the catalog is small enough), or browse by category. */
export function CatalogScreen() {
  const me = useMe();
  const [q, setQ] = useUrlState("q", "");
  const [category, setCategory] = useUrlState("category", "");
  const summary = useCatalogSummary();
  const searching = catalogQueryReady(q);
  const find = useCatalogFind({ q, category: category || undefined, limit: 50 });
  const browse = useCatalogItems({ category: category || undefined, limit: 50 }, { enabled: !searching });
  const staff = !!me.data?.admin_role;

  let body;
  if (searching) {
    if (find.isError) {
      body = <ErrorState title="Couldn't search the catalog" info={find.error?.message} onRetry={find.retry} retrying={find.isFetching} />;
    } else if (!find.items) {
      body = <CatalogRowsSkeleton label="Searching" />;
    } else if (find.items.length === 0) {
      body = (
        <EmptyState
          icon={<SearchX />}
          title="No matching items"
          action={
            <Button variant="secondary" size="auto" onClick={() => setQ(null)}>
              Clear search
            </Button>
          }
        />
      );
    } else {
      body = <CatalogItemRows items={find.items} />;
    }
  } else if (browse.isPending) {
    body = <CatalogRowsSkeleton label="Loading the catalog" />;
  } else if (browse.isError) {
    body = <ErrorState title="Couldn't load the catalog" info={browse.error.message} onRetry={() => void browse.refetch()} retrying={browse.isFetching} />;
  } else if (browse.data.items.length === 0) {
    body = (
      <EmptyState
        icon={<Library />}
        title={category ? "No items in this category" : "No items yet"}
        action={
          category ? (
            <Button variant="secondary" size="auto" onClick={() => setCategory(null)}>
              Show all
            </Button>
          ) : staff ? (
            <Button asChild variant="secondary" size="auto">
              <Link href={catalogPaths.admin}>Open sync</Link>
            </Button>
          ) : undefined
        }
      />
    );
  } else {
    body = (
      <>
        <CatalogItemRows items={browse.data.items} />
        <LoadMore hasNextPage={browse.hasNextPage} isFetchingNextPage={browse.isFetchingNextPage} fetchNextPage={browse.fetchNextPage} />
      </>
    );
  }

  return (
    <div className="mx-auto w-full max-w-content px-gutter py-section">
      <Screen
        title="Catalog"
        info="Items come from the catalog's source and are refreshed every night. Search matches names and other names an item goes by."
        action={
          staff ? (
            <Button asChild variant="secondary" size="sm" leftIcon={<Settings2 />}>
              <Link href={catalogPaths.admin}>Manage</Link>
            </Button>
          ) : undefined
        }
      >
        <div className="flex flex-col gap-4">
          <SearchInput value={q} onSearch={(v) => setQ(v || null)} placeholder="Name or other name" label="Search the catalog" className="sm:max-w-md" />
          <CategoryFilter categories={summary.data?.categories ?? []} value={category} onChange={(v) => setCategory(v || null)} />
          {body}
        </div>
      </Screen>
    </div>
  );
}
