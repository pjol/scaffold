package crm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// Agent tools for 1.3: tickets, products and line items, quote drafts, email
// drafts, reports and the forecast. Every read goes through the same
// visible-record checks as REST (agent-capped through ctx); every write sets
// Target and an exact Preview. Agents never publish, unpublish or delete
// quotes, never send email (they write drafts the person sends) and never
// change owners or teams.

func (s *Service) depthTools() []app.Tool {
	orgSchema := `"org_id":{"type":"string","format":"uuid","description":"` + orgs.NounTitle() + `; optional when the user uses the CRM in one ` + orgs.Noun() + `."}`
	return []app.Tool{
		s.getTool(objTicket, "crm_get_ticket", "Get a CRM ticket the user can see: subject, description, status, priority, owner, response times, and its company, deal and contacts."),
		{
			Name:        "crm_list_tickets",
			Description: "List CRM tickets the user can see, newest first, with filters for status (open or closed), priority, owner (\"me\" or a person's id) and pipeline. Returns tickets, the number that match and a cursor.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + orgSchema + `,"status":{"type":"string","enum":["open","closed"]},
				"priority":{"type":"string","enum":["low","medium","high","urgent"]},"owner":{"type":"string"},"pipeline_id":{"type":"string","format":"uuid"},
				"limit":{"type":"integer","minimum":1,"maximum":100},"cursor":{"type":"string"}},"additionalProperties":false}`),
			ReadOnly: true,
			Handler:  s.toolListTickets,
		},
		{
			Name: "crm_create_ticket",
			Description: "Create a CRM ticket the user will own: subject, description, priority, optional status (a stage id of a ticket pipeline from crm_get_ticket or crm_list_tickets), " +
				"company, deal and contacts the user can see.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + orgSchema + `,"name":{"type":"string","maxLength":200},"description":{"type":"string","maxLength":5000},
				"priority":{"type":"string","enum":["low","medium","high","urgent"]},"stage_id":{"type":"string","format":"uuid"},
				"company_id":{"type":"string","format":"uuid"},"deal_id":{"type":"string","format":"uuid"},"contact_ids":{"type":"array","items":{"type":"string","format":"uuid"},"maxItems":20}},
				"required":["name"],"additionalProperties":false}`),
			Target: func(input json.RawMessage) (string, string) {
				var in struct {
					OrgID string `json:"org_id"`
				}
				_ = json.Unmarshal(input, &in)
				return KindWorkspace, in.OrgID
			},
			Preview: s.previewCreateTicket,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				orgID, fields, err := s.ticketToolInput(ctx, caller, input)
				if err != nil {
					return nil, err
				}
				return s.create(ctx, caller.UserID, orgID, objTicket, fields, false)
			},
		},
		{
			Name:        "crm_move_ticket_status",
			Description: "Move a CRM ticket the user can edit to another status (a stage id of its pipelines, from crm_get_ticket).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"ticket_id":{"type":"string","format":"uuid"},"stage_id":{"type":"string","format":"uuid"}},
				"required":["ticket_id","stage_id"],"additionalProperties":false}`),
			Target: func(input json.RawMessage) (string, string) {
				var in struct {
					TicketID string `json:"ticket_id"`
				}
				_ = json.Unmarshal(input, &in)
				return KindTicket, in.TicketID
			},
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				var in struct {
					TicketID uuid.UUID `json:"ticket_id"`
					StageID  uuid.UUID `json:"stage_id"`
				}
				if err := decodeInput(input, &in); err != nil {
					return app.ToolPreview{}, err
				}
				return s.previewStageMove(ctx, caller, objTicket, in.TicketID, in.StageID)
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					TicketID uuid.UUID `json:"ticket_id"`
					StageID  uuid.UUID `json:"stage_id"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				return s.update(ctx, caller.UserID, objTicket, in.TicketID, map[string]json.RawMessage{"stage": json.RawMessage(`"` + in.StageID.String() + `"`)})
			},
		},
		{
			Name:        "crm_list_products",
			Description: "List products in the CRM's product library (name, SKU, unit price in minor units, currency, billing, tax and default discount in basis points).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + orgSchema + `,"query":{"type":"string"}},"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					OrgID *uuid.UUID `json:"org_id"`
					Query string     `json:"query"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				orgID, err := s.toolOrg(ctx, caller.UserID, in.OrgID)
				if err != nil {
					return nil, err
				}
				rows, err := s.d.Pool.Query(ctx, `SELECT `+productCols+` FROM crm_products WHERE org_id = $1 AND archived_at IS NULL
					AND ($2 = '' OR name ILIKE $3 OR COALESCE(sku, '') ILIKE $3) ORDER BY lower(name), id LIMIT 100`, orgID, in.Query, likePattern(clip(in.Query, 200)))
				if err != nil {
					return nil, err
				}
				defer rows.Close()
				out := []*Product{}
				for rows.Next() {
					p, err := scanProduct(rows)
					if err != nil {
						return nil, err
					}
					out = append(out, p)
				}
				return out, rows.Err()
			},
		},
		{
			Name:        "crm_list_line_items",
			Description: "List a CRM deal's line items with each line's money and the totals (minor units of the deal's currency).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"deal_id":{"type":"string","format":"uuid"}},"required":["deal_id"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					DealID uuid.UUID `json:"deal_id"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				return s.lineItemsOf(ctx, caller.UserID, in.DealID)
			},
		},
		{
			Name: "crm_add_line_item",
			Description: "Add a line item to a CRM deal the user can edit: a product from crm_list_products (its price, tax and billing are copied) or a custom line with name and unit_price. " +
				"quantity has up to 3 decimals; unit_price is minor units; discount_bp and tax_bp are basis points (825 = 8.25 %).",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"deal_id":{"type":"string","format":"uuid"},"product_id":{"type":"string","format":"uuid"},
				"name":{"type":"string","maxLength":200},"unit_price":{"type":"integer","minimum":0},"quantity":{"type":"number","exclusiveMinimum":0},
				"discount_bp":{"type":"integer","minimum":0,"maximum":10000},"tax_bp":{"type":"integer","minimum":0,"maximum":10000}},"required":["deal_id"],"additionalProperties":false}`),
			Target: dealTarget,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				dealID, req, err := lineItemToolInput(input)
				if err != nil {
					return app.ToolPreview{}, err
				}
				return s.previewLineItem(ctx, caller, dealID, nil, req)
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				dealID, req, err := lineItemToolInput(input)
				if err != nil {
					return nil, err
				}
				return s.createLineItem(ctx, caller.UserID, dealID, req)
			},
		},
		{
			Name:        "crm_update_line_item",
			Description: "Change a line item of a CRM deal the user can edit: quantity, unit_price (minor units), discount_bp or tax_bp (basis points). deal_id is the line's deal.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"deal_id":{"type":"string","format":"uuid"},"line_item_id":{"type":"string","format":"uuid"},
				"unit_price":{"type":"integer","minimum":0},"quantity":{"type":"number","exclusiveMinimum":0},
				"discount_bp":{"type":"integer","minimum":0,"maximum":10000},"tax_bp":{"type":"integer","minimum":0,"maximum":10000}},
				"required":["deal_id","line_item_id"],"additionalProperties":false}`),
			Target: dealTarget,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				dealID, itemID, req, err := lineItemUpdateInput(input)
				if err != nil {
					return app.ToolPreview{}, err
				}
				if err := s.itemOnDeal(ctx, itemID, dealID); err != nil {
					return app.ToolPreview{}, err
				}
				return s.previewLineItem(ctx, caller, dealID, &itemID, req)
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				dealID, itemID, req, err := lineItemUpdateInput(input)
				if err != nil {
					return nil, err
				}
				if err := s.itemOnDeal(ctx, itemID, dealID); err != nil {
					return nil, err
				}
				return s.changeLineItem(ctx, caller.UserID, itemID, req)
			},
		},
		{
			Name:        "crm_list_quotes",
			Description: "List the quotes of a CRM deal the user can see: number, title, status (draft, published, accepted, declined), expiry and total.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"deal_id":{"type":"string","format":"uuid"}},"required":["deal_id"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					DealID uuid.UUID `json:"deal_id"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				return s.dealQuotes(ctx, caller.UserID, in.DealID)
			},
		},
		{
			Name:        "crm_get_quote",
			Description: "Get a CRM quote the user can see: lines, totals, status, buyer and expiry. Its public link is not given to agents.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"quote_id":{"type":"string","format":"uuid"}},"required":["quote_id"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					QuoteID uuid.UUID `json:"quote_id"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				return s.quoteDetail(ctx, caller.UserID, in.QuoteID)
			},
		},
		{
			Name: "crm_draft_quote",
			Description: "Draft a quote from a CRM deal the user can edit: it copies the deal's line items; title, expiry (date-time), buyer contact, comments and terms are optional. " +
				"The quote stays a draft: only a person can publish it to the buyer.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"deal_id":{"type":"string","format":"uuid"},"title":{"type":"string","maxLength":200},
				"expires_at":{"type":"string","format":"date-time"},"contact_id":{"type":"string","format":"uuid"},"comments":{"type":"string","maxLength":10000},
				"terms":{"type":"string","maxLength":10000}},"required":["deal_id"],"additionalProperties":false}`),
			Target: dealTarget,
			Preview: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
				dealID, req, err := quoteToolInput(input)
				if err != nil {
					return app.ToolPreview{}, err
				}
				return s.previewDraftQuote(ctx, caller, dealID, req)
			},
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				dealID, req, err := quoteToolInput(input)
				if err != nil {
					return nil, err
				}
				return s.draftQuote(ctx, caller.UserID, dealID, req)
			},
		},
		{
			Name: "crm_draft_mail",
			Description: "Write an email draft to a CRM contact the user can comment on, saved in the CRM for the user to review and send from their own mailbox " +
				"(agents can't send). to defaults to the contact's first address; deal_id or ticket_id file it there too.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"contact_id":{"type":"string","format":"uuid"},"subject":{"type":"string","maxLength":300},
				"body":{"type":"string","maxLength":20000},"to":{"type":"array","items":{"type":"string"},"maxItems":10},"cc":{"type":"array","items":{"type":"string"},"maxItems":10},
				"deal_id":{"type":"string","format":"uuid"},"ticket_id":{"type":"string","format":"uuid"}},"required":["contact_id","subject","body"],"additionalProperties":false}`),
			Target: func(input json.RawMessage) (string, string) {
				var in struct {
					ContactID string `json:"contact_id"`
				}
				_ = json.Unmarshal(input, &in)
				return KindContact, in.ContactID
			},
			Preview: s.previewDraftMail,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				contactID, req, err := draftToolInput(input)
				if err != nil {
					return nil, err
				}
				return s.saveDraft(ctx, caller.UserID, contactID, nil, req)
			},
		},
		{
			Name: "crm_run_report",
			Description: "Compute a CRM report over the records the user can see: object (company, contact, deal, ticket), measure (count, sum, avg of a number or amount property), " +
				"optional group_by property (with bucket day/week/month/quarter/year for dates) and list filters [{key, op, value}]. Money is minor units.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + orgSchema + `,"object":{"type":"string","enum":["company","contact","deal","ticket"]},
				"measure":{"type":"string","enum":["count","sum","avg"]},"property":{"type":"string"},"group_by":{"type":"string"},
				"bucket":{"type":"string","enum":["day","week","month","quarter","year"]},"filters":{"type":"array","items":{"type":"object"}},
				"currency":{"type":"string"},"tz":{"type":"string"}},"required":["object"],"additionalProperties":false}`),
			ReadOnly: true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					OrgID *uuid.UUID `json:"org_id"`
					TZ    string     `json:"tz"`
					ReportSpec
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				orgID, err := s.toolOrg(ctx, caller.UserID, in.OrgID)
				if err != nil {
					return nil, err
				}
				in.ReportSpec.Chart = "table"
				return s.runReport(ctx, caller.UserID, orgID, in.ReportSpec, in.TZ)
			},
		},
		{
			Name:        "crm_get_dashboard",
			Description: "Get a CRM dashboard the user can open, with every report computed over the records the user can see.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"dashboard_id":{"type":"string","format":"uuid"},"tz":{"type":"string"}},"required":["dashboard_id"],"additionalProperties":false}`),
			ReadOnly:    true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					DashboardID uuid.UUID `json:"dashboard_id"`
					TZ          string    `json:"tz"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				return s.dashboardWithResults(ctx, caller.UserID, in.DashboardID, in.TZ)
			},
		},
		{
			Name: "crm_forecast",
			Description: "The sales forecast over the deals the user can see: by close month and owner, amounts in pipeline, best case, commit and closed won, and the probability-weighted amount, " +
				"in one currency (minor units). from is a month like 2026-11; months 1–12.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{` + orgSchema + `,"from":{"type":"string"},"months":{"type":"integer","minimum":1,"maximum":12},
				"pipeline_id":{"type":"string","format":"uuid"},"currency":{"type":"string"}},"additionalProperties":false}`),
			ReadOnly: true,
			Handler: func(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
				var in struct {
					OrgID      *uuid.UUID `json:"org_id"`
					From       string     `json:"from"`
					Months     int        `json:"months"`
					PipelineID *uuid.UUID `json:"pipeline_id"`
					Currency   string     `json:"currency"`
				}
				if err := decodeInput(input, &in); err != nil {
					return nil, err
				}
				orgID, err := s.toolOrg(ctx, caller.UserID, in.OrgID)
				if err != nil {
					return nil, err
				}
				from := time.Now().UTC()
				from = time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
				if in.From != "" {
					t, ok := monthStart(in.From)
					if !ok {
						return nil, httpx.BadRequest("from is a month like 2026-11.")
					}
					from = t
				}
				if in.Months == 0 {
					in.Months = 3
				}
				return s.forecast(ctx, caller.UserID, orgID, from, in.Months, in.PipelineID, strings.ToUpper(in.Currency), nil)
			},
		},
	}
}

// ---- tickets ----------------------------------------------------------------------------

func (s *Service) toolListTickets(ctx context.Context, caller *auth.Identity, input json.RawMessage) (any, error) {
	var in struct {
		OrgID      *uuid.UUID `json:"org_id"`
		Status     string     `json:"status"`
		Priority   string     `json:"priority"`
		Owner      string     `json:"owner"`
		PipelineID *uuid.UUID `json:"pipeline_id"`
		Limit      int        `json:"limit"`
		Cursor     string     `json:"cursor"`
	}
	if err := decodeInput(input, &in); err != nil {
		return nil, err
	}
	orgID, err := s.toolOrg(ctx, caller.UserID, in.OrgID)
	if err != nil {
		return nil, err
	}
	spec := listSpec{Cursor: in.Cursor, Limit: 50}
	if in.Limit > 0 {
		spec.Limit = min(in.Limit, 100)
	}
	switch in.Status {
	case "":
	case "open", "closed":
		status := in.Status
		spec.where = append(spec.where, func(b *sqlb) string { return "st.outcome = " + b.arg(status) })
	default:
		return nil, httpx.BadRequest("status is open or closed.")
	}
	if in.Priority != "" {
		if !hasOption(priorityOptions, in.Priority) {
			return nil, httpx.BadRequest("priority is low, medium, high or urgent.")
		}
		v, _ := json.Marshal([]string{in.Priority})
		spec.Filters = append(spec.Filters, Filter{Key: "priority", Op: "is_any", Value: v})
	}
	if in.Owner != "" {
		v, _ := json.Marshal([]string{in.Owner})
		spec.Filters = append(spec.Filters, Filter{Key: "owner", Op: "is_any", Value: v})
	}
	if in.PipelineID != nil {
		pid := *in.PipelineID
		spec.where = append(spec.where, func(b *sqlb) string { return "r.pipeline_id = " + b.arg(pid) })
	}
	props, err := s.loadProperties(ctx, s.d.Pool, orgID, objTicket)
	if err != nil {
		return nil, err
	}
	return s.queryRecords(ctx, s.d.Pool, caller.UserID, orgID, objTicket, props, spec, true)
}

type ticketToolArgs struct {
	OrgID       *uuid.UUID  `json:"org_id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Priority    string      `json:"priority"`
	StageID     *uuid.UUID  `json:"stage_id"`
	CompanyID   *uuid.UUID  `json:"company_id"`
	DealID      *uuid.UUID  `json:"deal_id"`
	ContactIDs  []uuid.UUID `json:"contact_ids"`
}

// ticketToolInput turns the tool input into create fields.
func (s *Service) ticketToolInput(ctx context.Context, caller *auth.Identity, input json.RawMessage) (uuid.UUID, map[string]json.RawMessage, error) {
	var in ticketToolArgs
	if err := decodeInput(input, &in); err != nil {
		return uuid.Nil, nil, err
	}
	orgID, err := s.toolOrg(ctx, caller.UserID, in.OrgID)
	if err != nil {
		return uuid.Nil, nil, err
	}
	fields := map[string]json.RawMessage{}
	put := func(k string, v any) {
		b, _ := json.Marshal(v)
		fields[k] = b
	}
	put("name", in.Name)
	if in.Description != "" {
		put("description", in.Description)
	}
	if in.Priority != "" {
		put("priority", in.Priority)
	}
	if in.StageID != nil {
		put("stage", in.StageID.String())
	}
	if in.CompanyID != nil {
		put("company_id", in.CompanyID.String())
	}
	if in.DealID != nil {
		put("deal_id", in.DealID.String())
	}
	if len(in.ContactIDs) > 0 {
		put("contact_ids", in.ContactIDs)
	}
	return orgID, fields, nil
}

// previewCreateTicket validates exactly as the write does and lists every field it sets.
func (s *Service) previewCreateTicket(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
	var in ticketToolArgs
	if err := decodeInput(input, &in); err != nil {
		return app.ToolPreview{}, err
	}
	orgID, fields, err := s.ticketToolInput(ctx, caller, input)
	if err != nil {
		return app.ToolPreview{}, err
	}
	props, err := s.loadProperties(ctx, s.d.Pool, orgID, objTicket)
	if err != nil {
		return app.ToolPreview{}, err
	}
	check := map[string]json.RawMessage{}
	for k, v := range fields {
		if k != "contact_ids" && k != "company_id" && k != "deal_id" {
			check[k] = v
		}
	}
	if in.CompanyID != nil {
		check["company"] = fields["company_id"]
	}
	if in.DealID != nil {
		check["deal"] = fields["deal_id"]
	}
	var p pending
	if err := s.applyFields(ctx, s.d.Pool, orgID, caller.UserID, objTicket, props, nil, check, &p); err != nil {
		return app.ToolPreview{}, asHTTP(err)
	}
	lines := []string{}
	for _, c := range p.changes {
		lines = append(lines, c.Label+": "+c.After)
	}
	if p.stage == nil {
		st, err := s.defaultStageOf(ctx, s.d.Pool, orgID, objTicket, nil)
		if err != nil {
			return app.ToolPreview{}, err
		}
		lines = append(lines, "Status: "+st.Name)
	}
	if len(in.ContactIDs) > 0 {
		refs, err := s.visibleRefs(ctx, s.d.Pool, caller.UserID, objContact, in.ContactIDs)
		if err != nil {
			return app.ToolPreview{}, err
		}
		names := []string{}
		for _, id := range in.ContactIDs {
			ref, ok := refs[id]
			if !ok {
				return app.ToolPreview{}, httpx.BadRequest("Choose contacts you can see.")
			}
			names = append(names, ref.Name)
		}
		lines = append(lines, "Contacts: "+strings.Join(names, ", "))
	}
	lines = append(lines, "Owner: you")
	return app.ToolPreview{Summary: "Create the ticket “" + clip(in.Name, 80) + "”", After: strings.Join(lines, "\n"), Link: "/crm/tickets"}, nil
}

// previewStageMove names the record, its current stage and the one it moves to.
func (s *Service) previewStageMove(ctx context.Context, caller *auth.Identity, o objectType, id, stageID uuid.UUID) (app.ToolPreview, error) {
	orgID, _, err := s.requireRecord(ctx, s.d.Pool, caller.UserID, o, id, access.View)
	if err != nil {
		return app.ToolPreview{}, err
	}
	cur, err := s.getOne(ctx, s.d.Pool, caller.UserID, o, id)
	if err != nil {
		return app.ToolPreview{}, err
	}
	st, err := loadStage(ctx, s.d.Pool, orgID, stageID)
	if err != nil {
		return app.ToolPreview{}, asHTTP(err)
	}
	if st.Object != o {
		return app.ToolPreview{}, httpx.BadRequest("Choose a stage from a " + o.label() + " pipeline.")
	}
	from := ""
	if t, ok := cur.(*Ticket); ok {
		from = t.Stage.Name
	}
	return app.ToolPreview{Summary: fmt.Sprintf("Move “%s” from %s to %s", cur.base().Title, from, st.Name), Before: from, After: st.Name, Link: o.link(id)}, nil
}

// ---- line items --------------------------------------------------------------------------

func dealTarget(input json.RawMessage) (string, string) {
	var in struct {
		DealID string `json:"deal_id"`
	}
	_ = json.Unmarshal(input, &in)
	return KindDeal, in.DealID
}

func lineItemToolInput(input json.RawMessage) (uuid.UUID, lineItemReq, error) {
	var in struct {
		DealID     uuid.UUID       `json:"deal_id"`
		ProductID  *uuid.UUID      `json:"product_id"`
		Name       *string         `json:"name"`
		UnitPrice  *int64          `json:"unit_price"`
		Quantity   json.RawMessage `json:"quantity"`
		DiscountBP *int            `json:"discount_bp"`
		TaxBP      *int            `json:"tax_bp"`
	}
	if err := decodeInput(input, &in); err != nil {
		return uuid.Nil, lineItemReq{}, err
	}
	if in.ProductID == nil && (in.Name == nil || in.UnitPrice == nil) {
		return uuid.Nil, lineItemReq{}, httpx.BadRequest("Give a product_id, or a name and a unit_price.")
	}
	return in.DealID, lineItemReq{ProductID: in.ProductID, Name: in.Name, UnitPrice: in.UnitPrice, Quantity: in.Quantity, DiscountBP: in.DiscountBP, TaxBP: in.TaxBP}, nil
}

func lineItemUpdateInput(input json.RawMessage) (uuid.UUID, uuid.UUID, lineItemReq, error) {
	var in struct {
		DealID     uuid.UUID       `json:"deal_id"`
		LineItemID uuid.UUID       `json:"line_item_id"`
		UnitPrice  *int64          `json:"unit_price"`
		Quantity   json.RawMessage `json:"quantity"`
		DiscountBP *int            `json:"discount_bp"`
		TaxBP      *int            `json:"tax_bp"`
	}
	if err := decodeInput(input, &in); err != nil {
		return uuid.Nil, uuid.Nil, lineItemReq{}, err
	}
	return in.DealID, in.LineItemID, lineItemReq{UnitPrice: in.UnitPrice, Quantity: in.Quantity, DiscountBP: in.DiscountBP, TaxBP: in.TaxBP}, nil
}

func (s *Service) itemOnDeal(ctx context.Context, itemID, dealID uuid.UUID) error {
	d, err := s.itemDeal(ctx, s.d.Pool, itemID)
	if err != nil {
		return err
	}
	if d != dealID {
		return httpx.NotFound("Line item not found on that deal.")
	}
	return nil
}

// previewLineItem computes the line exactly as the write would (and the
// deal's new amount when it follows its line items).
func (s *Service) previewLineItem(ctx context.Context, caller *auth.Identity, dealID uuid.UUID, itemID *uuid.UUID, req lineItemReq) (app.ToolPreview, error) {
	orgID, _, err := s.requireRecord(ctx, s.d.Pool, caller.UserID, objDeal, dealID, access.View)
	if err != nil {
		return app.ToolPreview{}, err
	}
	var name, currency string
	var follow bool
	var amount *int64
	if err := s.d.Pool.QueryRow(ctx, `SELECT name, currency, amount_from_items, amount FROM crm_deals WHERE id = $1`, dealID).Scan(&name, &currency, &follow, &amount); err != nil {
		return app.ToolPreview{}, err
	}
	items, totals, err := dealLineItems(ctx, s.d.Pool, dealID)
	if err != nil {
		return app.ToolPreview{}, err
	}
	li := &LineItem{}
	var before *LineItem
	if itemID != nil {
		for _, x := range items {
			if x.ID == *itemID {
				cp := *x
				before, li = x, &cp
			}
		}
		if before == nil {
			return app.ToolPreview{}, httpx.NotFound("Line item not found.")
		}
	}
	if err := s.applyLineItem(ctx, s.d.Pool, orgID, currency, req, li, itemID == nil); err != nil {
		return app.ToolPreview{}, err
	}
	lt, _ := li.input().totals()
	describe := func(x *LineItem, t LineTotals) string {
		parts := []string{fmt.Sprintf("%s × %s at %s", formatQuantity(x.qtyMilli), x.Name, formatMoney(x.UnitPrice, currency))}
		if x.DiscountBP > 0 {
			parts = append(parts, "discount "+formatPercent(x.DiscountBP))
		} else if x.DiscountAmount > 0 {
			parts = append(parts, "discount "+formatMoney(x.DiscountAmount, currency))
		}
		if x.TaxBP > 0 {
			parts = append(parts, "tax "+formatPercent(x.TaxBP))
		}
		if x.Billing == "recurring" {
			parts = append(parts, fmt.Sprintf("billed %s for %d periods", deref(x.BillingPeriod), x.TermPeriods))
		}
		return strings.Join(parts, ", ") + " = " + formatMoney(t.Total, currency)
	}
	after := describe(li, lt)
	pv := app.ToolPreview{After: after, Link: objDeal.link(dealID)}
	newValue := totals.ContractValue + lt.ContractValue
	if before != nil {
		pv.Before = describe(before, before.LineTotals)
		newValue -= before.ContractValue
		pv.Summary = "Change the line “" + before.Name + "” on “" + name + "”"
	} else {
		pv.Summary = "Add “" + li.Name + "” to “" + name + "”"
	}
	if follow {
		old := ""
		if amount != nil {
			old = formatMoney(*amount, currency)
		}
		pv.After += "\nDeal amount (follows line items): " + old + " → " + formatMoney(newValue, currency)
	}
	return pv, nil
}

// ---- quotes and drafts ---------------------------------------------------------------------

func quoteToolInput(input json.RawMessage) (uuid.UUID, quoteReq, error) {
	var in struct {
		DealID    uuid.UUID  `json:"deal_id"`
		Title     *string    `json:"title"`
		ExpiresAt *time.Time `json:"expires_at"`
		ContactID *uuid.UUID `json:"contact_id"`
		Comments  *string    `json:"comments"`
		Terms     *string    `json:"terms"`
	}
	if err := decodeInput(input, &in); err != nil {
		return uuid.Nil, quoteReq{}, err
	}
	req := quoteReq{Title: in.Title, ExpiresAt: in.ExpiresAt, Comments: in.Comments, Terms: in.Terms}
	if in.ContactID != nil {
		raw := json.RawMessage(`"` + in.ContactID.String() + `"`)
		req.ContactID = &raw
	}
	return in.DealID, req, nil
}

func (s *Service) previewDraftQuote(ctx context.Context, caller *auth.Identity, dealID uuid.UUID, req quoteReq) (app.ToolPreview, error) {
	orgID, _, err := s.requireRecord(ctx, s.d.Pool, caller.UserID, objDeal, dealID, access.View)
	if err != nil {
		return app.ToolPreview{}, err
	}
	var name, currency string
	if err := s.d.Pool.QueryRow(ctx, `SELECT name, currency FROM crm_deals WHERE id = $1`, dealID).Scan(&name, &currency); err != nil {
		return app.ToolPreview{}, err
	}
	items, totals, err := dealLineItems(ctx, s.d.Pool, dealID)
	if err != nil {
		return app.ToolPreview{}, err
	}
	title := name
	if req.Title != nil && strings.TrimSpace(*req.Title) != "" {
		title = strings.TrimSpace(*req.Title)
	}
	expires := time.Now().Add(defaultQuoteTTL)
	if req.ExpiresAt != nil {
		expires = *req.ExpiresAt
	}
	if err := validQuoteText(title, deref(req.Terms), deref(req.Comments), expires); err != nil {
		return app.ToolPreview{}, err
	}
	lines := []string{"Title: " + title, "Expires: " + expires.UTC().Format("2 Jan 2006 15:04 UTC"),
		fmt.Sprintf("%s from the deal, total %s", plural(len(items), "line", "lines"), formatMoney(totals.Total, currency))}
	if req.ContactID != nil {
		id, err := s.quoteContact(ctx, s.d.Pool, caller.UserID, orgID, *req.ContactID)
		if err != nil {
			return app.ToolPreview{}, err
		}
		if id != nil {
			t, _ := recordTitle(ctx, s.d.Pool, objContact, *id)
			lines = append(lines, "For: "+t)
		}
	}
	if c := strings.TrimSpace(deref(req.Comments)); c != "" {
		lines = append(lines, "Comments: "+c)
	}
	if t := strings.TrimSpace(deref(req.Terms)); t != "" {
		lines = append(lines, "Terms: "+t)
	}
	lines = append(lines, "It stays a draft until a person publishes it.")
	return app.ToolPreview{Summary: "Draft a quote for “" + name + "”", After: strings.Join(lines, "\n"), Link: objDeal.link(dealID)}, nil
}

func draftToolInput(input json.RawMessage) (uuid.UUID, draftReq, error) {
	var in struct {
		ContactID uuid.UUID  `json:"contact_id"`
		Subject   string     `json:"subject"`
		Body      string     `json:"body"`
		To        []string   `json:"to"`
		CC        []string   `json:"cc"`
		DealID    *uuid.UUID `json:"deal_id"`
		TicketID  *uuid.UUID `json:"ticket_id"`
	}
	if err := decodeInput(input, &in); err != nil {
		return uuid.Nil, draftReq{}, err
	}
	return in.ContactID, draftReq{To: in.To, CC: in.CC, Subject: &in.Subject, Body: &in.Body, DealID: in.DealID, TicketID: in.TicketID}, nil
}

func (s *Service) previewDraftMail(ctx context.Context, caller *auth.Identity, input json.RawMessage) (app.ToolPreview, error) {
	contactID, req, err := draftToolInput(input)
	if err != nil {
		return app.ToolPreview{}, err
	}
	if _, _, err := s.requireRecord(ctx, s.d.Pool, caller.UserID, objContact, contactID, access.View); err != nil {
		return app.ToolPreview{}, err
	}
	rec, err := s.getOne(ctx, s.d.Pool, caller.UserID, objContact, contactID)
	if err != nil {
		return app.ToolPreview{}, err
	}
	to, err := validAddresses(req.To, "To")
	if err != nil {
		return app.ToolPreview{}, httpx.BadRequest(err.Error() + ".")
	}
	cc, err := validAddresses(req.CC, "Cc")
	if err != nil {
		return app.ToolPreview{}, httpx.BadRequest(err.Error() + ".")
	}
	if len(to) == 0 {
		if e := contactEmails(rec.(*Contact)); len(e) > 0 {
			to = e[:1]
		} else {
			return app.ToolPreview{}, httpx.BadRequest("This contact has no email address yet.")
		}
	}
	head := "To: " + strings.Join(to, ", ")
	if len(cc) > 0 {
		head += "\nCc: " + strings.Join(cc, ", ")
	}
	head += "\nSubject: " + deref(req.Subject)
	return app.ToolPreview{Summary: "Save an email draft to " + rec.base().Title + " for you to review and send",
		After: head + "\n\n" + deref(req.Body), Link: objContact.link(contactID)}, nil
}
