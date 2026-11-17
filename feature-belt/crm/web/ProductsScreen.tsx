"use client";

import {
  CRM_BILLING_PERIOD_LABEL,
  CRM_BILLING_PERIODS,
  crmMoneyInput,
  crmParseMoney,
  crmParsePercent,
  crmPercent,
  errorMessage,
  useCrmDeleteProduct,
  useCrmProducts,
  useCrmSaveProduct,
  type CrmBilling,
  type CrmBillingPeriod,
  type CrmProduct,
} from "@scaffoldapp/shared";
import {
  Badge,
  Button,
  Card,
  ConfirmDialog,
  Dialog,
  EmptyState,
  ErrorState,
  Field,
  IconButton,
  Input,
  Menu,
  MenuItem,
  MenuSeparator,
  SegmentedControl,
  Select,
  Skeleton,
  SkeletonGroup,
  Switch,
  TextArea,
  toast,
} from "@scaffoldapp/ui-web";
import { Archive, ArchiveRestore, MoreHorizontal, Package, PenLine, Plus, Search, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { LoadMore } from "@/components/shell/LoadMore";
import { CrmFrame, useCrm } from "./CrmFrame";
import { priceText, termDuration } from "./QuoteDocument";

/** /crm/products: the product library deals and quotes add lines from. */
export function CrmProductsScreen() {
  const [editing, setEditing] = useState<CrmProduct | "new" | null>(null);
  return (
    <CrmFrame actions={<NewButton onClick={() => setEditing("new")} />}>
      <Products onEdit={setEditing} />
      {editing ? <ProductDialog product={editing === "new" ? null : editing} onClose={() => setEditing(null)} /> : null}
    </CrmFrame>
  );
}

function NewButton({ onClick }: { onClick: () => void }) {
  const { settings } = useCrm();
  if (!settings.can.manage_products) return null;
  return (
    <Button size="auto" leftIcon={<Plus />} onClick={onClick}>
      New product
    </Button>
  );
}

function Products({ onEdit }: { onEdit: (p: CrmProduct) => void }) {
  const { orgId, settings } = useCrm();
  const [q, setQ] = useState("");
  const [debounced, setDebounced] = useState("");
  const [archived, setArchived] = useState(false);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(q.trim()), 250);
    return () => clearTimeout(t);
  }, [q]);
  const products = useCrmProducts(orgId, debounced, archived);
  const save = useCrmSaveProduct(orgId);
  const del = useCrmDeleteProduct(orgId);
  const [deleting, setDeleting] = useState<CrmProduct | null>(null);
  const manage = !!settings.can.manage_products;
  const items = products.data?.items ?? [];
  return (
    <section className="flex flex-col gap-4" aria-label="Products">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <h2 className="type-title-sm text-fg">Products</h2>
        <Input aria-label="Search products" placeholder="Search name or SKU" value={q} onChange={(e) => setQ(e.target.value)} leadingIcon={<Search />} wrapperClassName="sm:ml-auto sm:w-64" />
        <Switch checked={archived} onCheckedChange={setArchived} label="Archived" />
      </div>
      {products.isPending ? (
        <SkeletonGroup label="Loading products" className="flex flex-col gap-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-16 w-full" />
          ))}
        </SkeletonGroup>
      ) : products.isError ? (
        <ErrorState title="Couldn't load products" info={products.error.message} onRetry={() => void products.refetch()} retrying={products.isRefetching} />
      ) : items.length === 0 ? (
        <Card padding="compact">
          <EmptyState
            icon={<Package />}
            title={debounced ? "No products match" : archived ? "No archived products" : "No products yet"}
            {...(manage ? {} : { info: "People who manage CRM products add them here; you can add them to deals." })}
          />
        </Card>
      ) : (
        <Card padding="none" className="overflow-hidden">
          <ul className="divide-y divide-border">
            {items.map((p) => (
              <li key={p.id} className="flex items-center gap-3 px-4 py-3">
                <Package className="size-5 shrink-0 text-fg-muted" aria-hidden />
                <div className="flex min-w-0 flex-1 flex-col">
                  <span className="flex flex-wrap items-center gap-2">
                    <span className="truncate type-label font-semibold text-fg">{p.name}</span>
                    {p.sku ? <Badge>{p.sku}</Badge> : null}
                    {p.archived_at ? <Badge tone="warning">Archived</Badge> : null}
                  </span>
                  <span className="truncate type-caption text-fg-muted">
                    {[p.billing === "recurring" && p.billing_period && p.term_periods ? `Term ${termDuration(p.billing_period, p.term_periods)}` : null, p.tax_bp ? `Tax ${crmPercent(p.tax_bp)}` : null, p.discount_bp ? `${crmPercent(p.discount_bp)} off` : null, p.description || null]
                      .filter(Boolean)
                      .join(" · ")}
                  </span>
                </div>
                <span className="shrink-0 type-label font-semibold tabular-nums text-fg">{priceText(p.unit_price, p.currency, p.billing, p.billing_period)}</span>
                {manage ? (
                  <Menu trigger={<IconButton label={`Actions for ${p.name}`} size="sm" icon={<MoreHorizontal />} />}>
                    <MenuItem icon={<PenLine />} onSelect={() => onEdit(p)}>
                      Edit
                    </MenuItem>
                    <MenuItem
                      icon={p.archived_at ? <ArchiveRestore /> : <Archive />}
                      onSelect={() =>
                        save.mutate(
                          { id: p.id, body: { archived: !p.archived_at } },
                          { onSuccess: () => toast.success(p.archived_at ? "Product restored" : "Product archived"), onError: (e) => toast.error(errorMessage(e, "Couldn't change it.")) },
                        )
                      }
                    >
                      {p.archived_at ? "Restore" : "Archive"}
                    </MenuItem>
                    <MenuSeparator />
                    <MenuItem icon={<Trash2 />} destructive onSelect={() => setDeleting(p)}>
                      Delete
                    </MenuItem>
                  </Menu>
                ) : null}
              </li>
            ))}
          </ul>
        </Card>
      )}
      <LoadMore hasNextPage={!!products.hasNextPage} isFetchingNextPage={products.isFetchingNextPage} fetchNextPage={() => products.fetchNextPage()} />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(o) => !o && setDeleting(null)}
        tone="danger"
        title={`Delete “${deleting?.name ?? ""}”?`}
        confirmLabel="Delete product"
        onConfirm={() =>
          deleting
            ? del.mutateAsync(deleting.id).then(
                () => toast.success("Product deleted"),
                (e: unknown) => Promise.reject(new Error(errorMessage(e, "Couldn't delete it."))),
              )
            : undefined
        }
      >
        Deals and quotes keep the lines they copied from it. Archive it instead to keep it out of pickers but in the library.
      </ConfirmDialog>
    </section>
  );
}

function ProductDialog({ product, onClose }: { product: CrmProduct | null; onClose: () => void }) {
  const { orgId, settings } = useCrm();
  const save = useCrmSaveProduct(orgId);
  const [currency, setCurrency] = useState(product?.currency ?? settings.currency);
  const [name, setName] = useState(product?.name ?? "");
  const [sku, setSku] = useState(product?.sku ?? "");
  const [description, setDescription] = useState(product?.description ?? "");
  const [price, setPrice] = useState(product ? crmMoneyInput(product.unit_price, product.currency) : "");
  const [billing, setBilling] = useState<CrmBilling>(product?.billing ?? "one_time");
  const [period, setPeriod] = useState<CrmBillingPeriod>(product?.billing_period ?? "monthly");
  const [term, setTerm] = useState(product?.term_periods ? String(product.term_periods) : "");
  const [tax, setTax] = useState(product?.tax_bp ? String(product.tax_bp / 100) : "");
  const [discount, setDiscount] = useState(product?.discount_bp ? String(product.discount_bp / 100) : "");
  const [error, setError] = useState<string | null>(null);
  const submit = () => {
    if (!name.trim()) return setError("Give the product a name.");
    const cur = currency.trim().toUpperCase();
    if (!/^[A-Z]{3}$/.test(cur)) return setError("Currency is a 3-letter code like USD.");
    const unit = crmParseMoney(price, cur);
    if (unit === null) return setError("Enter the price as a number, like 1200.");
    const taxBp = crmParsePercent(tax);
    const discountBp = crmParsePercent(discount);
    if (taxBp === null || discountBp === null) return setError("Tax and discount are 0 to 100 %.");
    const t = term.trim() ? Number(term) : 0;
    if (billing === "recurring" && term.trim() && (!Number.isInteger(t) || t < 1 || t > 120)) return setError("A term is 1 to 120 billing periods.");
    setError(null);
    save.mutate(
      {
        ...(product ? { id: product.id } : {}),
        body: {
          name: name.trim(),
          sku: sku.trim(),
          description: description.trim(),
          unit_price: unit,
          currency: cur,
          billing,
          billing_period: billing === "recurring" ? period : "",
          term_periods: billing === "recurring" ? t : 0,
          tax_bp: taxBp,
          discount_bp: discountBp,
        },
      },
      {
        onSuccess: () => {
          toast.success(product ? "Product saved" : "Product added");
          onClose();
        },
        onError: (e) => setError(errorMessage(e, "Couldn't save the product.")),
      },
    );
  };
  return (
    <Dialog
      open
      onOpenChange={(o) => !o && onClose()}
      title={product ? `Edit ${product.name}` : "New product"}
      info="Deals and quotes copy a product's fields when it's added, so changing it here doesn't change them."
      size="lg"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button loading={save.isPending} onClick={submit}>
            {product ? "Save product" : "Add product"}
          </Button>
        </>
      }
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <div className="grid gap-4 sm:grid-cols-[2fr_1fr]">
          <Field label="Name" required error={error}>
            <Input value={name} maxLength={200} autoFocus onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="SKU">
            <Input value={sku} maxLength={64} onChange={(e) => setSku(e.target.value)} />
          </Field>
        </div>
        <Field label="Description">
          <TextArea value={description} maxLength={2000} onChange={(e) => setDescription(e.target.value)} />
        </Field>
        <div className="grid gap-4 sm:grid-cols-[2fr_1fr]">
          <Field label="Unit price">
            <Input value={price} inputMode="decimal" onChange={(e) => setPrice(e.target.value)} placeholder="1200" />
          </Field>
          <Field label="Currency">
            <Input value={currency} maxLength={3} autoCapitalize="characters" onChange={(e) => setCurrency(e.target.value.toUpperCase())} />
          </Field>
        </div>
        <Field label="Billing">
          <SegmentedControl
            value={billing}
            onValueChange={(v) => setBilling(v)}
            options={[
              { value: "one_time", label: "One time" },
              { value: "recurring", label: "Recurring" },
            ]}
          />
        </Field>
        {billing === "recurring" ? (
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Bills">
              <Select value={period} onValueChange={(v) => setPeriod(v as CrmBillingPeriod)} options={CRM_BILLING_PERIODS.map((p) => ({ value: p, label: CRM_BILLING_PERIOD_LABEL[p].every }))} />
            </Field>
            <Field label="Default term" info="Billing periods a new line runs for (12 monthly periods is a year). Empty: one year.">
              <Input value={term} inputMode="numeric" onChange={(e) => setTerm(e.target.value)} placeholder="12" />
            </Field>
          </div>
        ) : null}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Tax">
            <Input value={tax} inputMode="decimal" onChange={(e) => setTax(e.target.value)} placeholder="0" trailing={<span className="px-2 type-caption">%</span>} />
          </Field>
          <Field label="Default discount">
            <Input value={discount} inputMode="decimal" onChange={(e) => setDiscount(e.target.value)} placeholder="0" trailing={<span className="px-2 type-caption">%</span>} />
          </Field>
        </div>
        <button type="submit" hidden aria-hidden tabIndex={-1} />
      </form>
    </Dialog>
  );
}
