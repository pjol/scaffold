package crm

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
)

// Connected records: the same JSON shapes as the app's own DTOs (Company,
// Contact, Deal, Activity, Board, Dashboard…) with the platform's string ids
// and a source, so the screens and agent tools render both with one code
// path. They are built per request from the person's own connection and
// never stored.

// withURL copies a source ref with the platform page of one item.
func withURL(r *app.SourceRef, u string) *app.SourceRef {
	c := *r
	c.URL = u
	return &c
}

// xref is a connected record's id in the app: "<connection id>~<platform id>".
// It is what routes (/crm/deals/<xref>) and DTO ids carry; the app's own
// records keep their UUIDs, so a route tells the two apart by parsing.
func xref(connID, platformID string) string { return connID + "~" + platformID }

// parseXRef splits an xref. Platform ids never contain "~"; connection ids
// are opaque but never contain it either.
func parseXRef(s string) (connID, platformID string, ok bool) {
	connID, platformID, ok = strings.Cut(s, "~")
	if !ok || connID == "" || platformID == "" || strings.ContainsAny(s, "/?#% ") {
		return "", "", false
	}
	return connID, platformID, true
}

// agentTargetID is the id agents see for a connected record, the Target of
// connected write tools: "<connection id>:<company|contact|deal>:<platform id>".
func agentTargetID(connID string, o objectType, platformID string) string {
	return connID + ":" + string(o) + ":" + platformID
}

// KindExternal is the agent Target kind of a connected record.
const KindExternal = "crm.external"

// XUserRef is a person on the platform (an owner): the platform's user id.
type XUserRef struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatar_url"`
	Email     string  `json:"email,omitempty"`
}

// XRecordRef names a connected record (its id is an xref).
type XRecordRef struct {
	Type objectType `json:"type"`
	ID   string     `json:"id"`
	Name string     `json:"name"`
}

// XStage is a pipeline stage on the platform (HubSpot stage id, Salesforce
// StageName).
type XStage struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Position    int    `json:"position"`
	Probability int    `json:"probability"`
	Outcome     string `json:"outcome"`
}

// XStageRef is a deal's stage.
type XStageRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Probability int    `json:"probability"`
	Outcome     string `json:"outcome"`
	Position    int    `json:"position"`
}

// XPipeline is a platform pipeline (Salesforce has one: its opportunity stages).
type XPipeline struct {
	ID       string    `json:"id"`
	OrgID    uuid.UUID `json:"org_id"`
	Name     string    `json:"name"`
	Position int       `json:"position"`
	Stages   []XStage  `json:"stages"`
}

func (p *XPipeline) stage(id string) (XStage, bool) {
	for _, st := range p.Stages {
		if st.ID == id {
			return st, true
		}
	}
	return XStage{}, false
}

func (st XStage) ref() XStageRef {
	return XStageRef{ID: st.ID, Name: st.Name, Probability: st.Probability, Outcome: st.Outcome, Position: st.Position}
}

// XRecord is what every connected row carries (Record's JSON).
type XRecord struct {
	ID             string            `json:"id"`
	OrgID          uuid.UUID         `json:"org_id"`
	Type           objectType        `json:"type"`
	Title          string            `json:"title"`
	Owner          *XUserRef         `json:"owner"`
	Team           *TeamRef          `json:"team"`
	Tags           []Tag             `json:"tags"`
	Props          map[string]any    `json:"props"`
	PropsDisplay   map[string]string `json:"props_display"`
	CreatedBy      *string           `json:"created_by"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	LastActivityAt *time.Time        `json:"last_activity_at"`
	// MyLevel is edit when the connection may write (the platform still
	// refuses what the account can't change, and that error is shown), else view.
	MyLevel access.Level   `json:"my_level"`
	Source  *app.SourceRef `json:"source"`

	platformID string
}

func (r *XRecord) xbase() *XRecord { return r }

// xrecordish is implemented by *XCompany, *XContact and *XDeal.
type xrecordish interface{ xbase() *XRecord }

// XCompany is a HubSpot company or a Salesforce Account.
type XCompany struct {
	XRecord
	Name        string  `json:"name"`
	Domain      *string `json:"domain"`
	Industry    string  `json:"industry"`
	Employees   *int    `json:"employees"`
	Phone       string  `json:"phone"`
	City        string  `json:"city"`
	Country     string  `json:"country"`
	Description string  `json:"description"`
}

// XContact is a HubSpot or Salesforce contact.
type XContact struct {
	XRecord
	FirstName      string      `json:"first_name"`
	LastName       string      `json:"last_name"`
	Name           string      `json:"name"`
	Emails         []string    `json:"emails"`
	Phones         []string    `json:"phones"`
	JobTitle       string      `json:"job_title"`
	LifecycleStage string      `json:"lifecycle_stage"`
	City           string      `json:"city"`
	Country        string      `json:"country"`
	Company        *XRecordRef `json:"company"`
}

// XDeal is a HubSpot deal or a Salesforce Opportunity.
type XDeal struct {
	XRecord
	Name           string      `json:"name"`
	Amount         *int64      `json:"amount"`
	Currency       string      `json:"currency"`
	Weighted       *int64      `json:"weighted_amount"`
	PipelineID     string      `json:"pipeline_id"`
	PipelineName   string      `json:"pipeline_name"`
	Stage          XStageRef   `json:"stage"`
	CloseDate      *string     `json:"close_date"`
	Company        *XRecordRef `json:"company"`
	StageEnteredAt time.Time   `json:"stage_entered_at"`
	ClosedAt       *time.Time  `json:"closed_at"`
}

// XAssocCompany, XAssocContact and XAssocDeal are linked records on the platform.
type XAssocCompany struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Domain    *string `json:"domain"`
	IsPrimary bool    `json:"is_primary"`
}

type XAssocContact struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Email    *string `json:"email"`
	Phone    *string `json:"phone"`
	JobTitle string  `json:"job_title"`
}

type XAssocDeal struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Amount       *int64  `json:"amount"`
	Currency     string  `json:"currency"`
	StageName    string  `json:"stage_name"`
	StageOutcome string  `json:"stage_outcome"`
	CloseDate    *string `json:"close_date"`
}

// XAssociations are the records linked on the platform that the account can see.
type XAssociations struct {
	Companies []XAssocCompany `json:"companies"`
	Contacts  []XAssocContact `json:"contacts"`
	Deals     []XAssocDeal    `json:"deals"`
}

// XRecordDetail is GET /v1/crm/{type}/{xref}.
type XRecordDetail struct {
	Record       xrecordish    `json:"record"`
	Associations XAssociations `json:"associations"`
	CanDelete    bool          `json:"can_delete"`
}

// XActivity is a platform engagement or activity (Activity's JSON).
type XActivity struct {
	ID              string         `json:"id"`
	OrgID           uuid.UUID      `json:"org_id"`
	Type            string         `json:"type"`
	Records         []XRecordRef   `json:"records"`
	Subject         string         `json:"subject"`
	Body            string         `json:"body"`
	Mentions        []Mention      `json:"mentions"`
	OccurredAt      time.Time      `json:"occurred_at"`
	DurationMinutes *int           `json:"duration_minutes"`
	Outcome         *string        `json:"outcome"`
	Direction       *string        `json:"direction"`
	DueAt           *time.Time     `json:"due_at"`
	Assignee        *XUserRef      `json:"assignee"`
	ReminderAt      *time.Time     `json:"reminder_at"`
	DoneAt          *time.Time     `json:"done_at"`
	Priority        string         `json:"priority"`
	CreatedBy       *XUserRef      `json:"created_by"`
	Agent           *string        `json:"agent"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	CanEdit         bool           `json:"can_edit"`
	Source          *app.SourceRef `json:"source"`
}

// XTimelineItem is one entry of a connected record's timeline (activities only).
type XTimelineItem struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"` // activity
	At       time.Time  `json:"at"`
	Activity *XActivity `json:"activity,omitempty"`
}

// XTimelinePage is GET /v1/crm/{type}/{xref}/timeline.
type XTimelinePage struct {
	Items      []XTimelineItem `json:"items"`
	NextCursor *string         `json:"next_cursor"`
	Upcoming   []*XActivity    `json:"upcoming"`
}

// XBoardColumn is one platform stage with the account's deals and their totals.
type XBoardColumn struct {
	Stage      XStage       `json:"stage"`
	Count      int          `json:"count"`
	Totals     []MoneyTotal `json:"totals"`
	Deals      []*XDeal     `json:"deals"`
	NextCursor *string      `json:"next_cursor"`
}

// XBoard is GET /v1/crm/orgs/{orgID}/board?source=<connection id>.
type XBoard struct {
	Pipeline XPipeline      `json:"pipeline"`
	Columns  []XBoardColumn `json:"columns"`
	Open     []MoneyTotal   `json:"open_totals"`
	// Partial: the pipeline has more deals than totals are summed over
	// (counts stay exact). Set only for platforms without aggregate queries.
	Partial bool           `json:"partial,omitempty"`
	Source  *app.SourceRef `json:"source"`
}

// XStageSummary is one open stage's deals.
type XStageSummary struct {
	StageID string       `json:"stage_id"`
	Name    string       `json:"name"`
	Count   int          `json:"count"`
	Totals  []MoneyTotal `json:"totals"`
}

// XOwnerActivity counts what an owner logged this week.
type XOwnerActivity struct {
	User     XUserRef `json:"user"`
	Notes    int      `json:"notes"`
	Calls    int      `json:"calls"`
	Emails   int      `json:"emails"`
	Meetings int      `json:"meetings"`
	Tasks    int      `json:"tasks_done"`
	Total    int      `json:"total"`
}

// XDashboard is GET /v1/crm/orgs/{orgID}/dashboard?source=<connection id>.
type XDashboard struct {
	Pipeline   XPipeline        `json:"pipeline"`
	Currency   string           `json:"currency"`
	Open       []XStageSummary  `json:"open_by_stage"`
	OpenTotals []MoneyTotal     `json:"open_totals"`
	Won        ClosedSummary    `json:"won_this_month"`
	Lost       ClosedSummary    `json:"lost_this_month"`
	Activity   []XOwnerActivity `json:"activity_this_week"`
	Tasks      TaskSummary      `json:"my_tasks"`
	Source     *app.SourceRef   `json:"source"`
}

// XSearchHit is a connected record found by the platform's search.
type XSearchHit struct {
	Type   objectType     `json:"type"`
	ID     string         `json:"id"`
	OrgID  uuid.UUID      `json:"org_id"`
	Title  string         `json:"title"`
	Detail string         `json:"detail"`
	Score  float64        `json:"score"`
	At     time.Time      `json:"updated_at"`
	Source *app.SourceRef `json:"source"`
}

// xPage is one page of a connected list. Total is the platform's count of
// matches for the account (first page).
type xPage struct {
	Items      []xrecordish `json:"items"`
	NextCursor *string      `json:"next_cursor"`
	Total      *int         `json:"total"`
}

// xActivityPage is a page of connected tasks.
type xActivityPage struct {
	Items      []*XActivity `json:"items"`
	NextCursor *string      `json:"next_cursor"`
}

// moneyMinor converts a platform decimal amount to minor units of cur.
func moneyMinor(v float64, cur string) int64 {
	f := 1.0
	for range exponent(cur) {
		f *= 10
	}
	x := v * f
	if x < 0 {
		return int64(x - 0.5)
	}
	return int64(x + 0.5)
}

// moneyDecimalString is minor units as the decimal string platforms take.
func moneyDecimalString(minor int64, cur string) string { return moneyDecimal(minor, cur) }

func addTotal(list []MoneyTotal, cur string, amount, weighted int64) []MoneyTotal {
	for i := range list {
		if list[i].Currency == cur {
			list[i].Amount += amount
			list[i].Weighted += weighted
			return list
		}
	}
	return append(list, MoneyTotal{Currency: cur, Amount: amount, Weighted: weighted})
}
