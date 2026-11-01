package crm

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/audit"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

const (
	maxImportBytes = 5 << 20
	maxImportRows  = 5000
	maxExportRows  = 10000
	importTimeout  = 15 * time.Minute
)

// ---- export -------------------------------------------------------------------------------

// export: GET /v1/crm/orgs/{orgID}/export?object=&columns=a,b&filters=&q=&sort=&dir=&tz=
// (crm.export, rate-limited, audited) → CSV of the visible rows, at most 10,000.
func (s *Service) export(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	q := r.URL.Query()
	o, ok := parseObject(q.Get("object"))
	if !ok {
		return httpx.BadRequest("object is company, contact or deal.")
	}
	spec, err := parseListSpec(q.Get)
	if err != nil {
		return err
	}
	props, err := s.loadProperties(ctx, s.d.Pool, orgID, o)
	if err != nil {
		return err
	}
	cols := []Property{}
	if c := strings.TrimSpace(q.Get("columns")); c != "" {
		for _, k := range strings.Split(c, ",") {
			p, ok := findProp(props, strings.TrimSpace(k))
			if !ok {
				return httpx.BadRequest("Unknown column “" + k + "”.")
			}
			cols = append(cols, p)
		}
	} else {
		cols = props
	}
	tz, err := location(spec.TZ)
	if err != nil {
		return err
	}
	loc, _ := time.LoadLocation(tz)
	var buf bytes.Buffer
	cw := csv.NewWriter(&buf)
	header := []string{"Record ID"}
	for _, p := range cols {
		header = append(header, p.Label)
	}
	_ = cw.Write(header)
	stages, err := s.loadPipelines(ctx, s.d.Pool, orgID)
	if err != nil {
		return err
	}
	pipelineNames := map[uuid.UUID]string{}
	for _, p := range stages {
		pipelineNames[p.ID] = p.Name
	}
	n, truncated := 0, false
	spec.Limit, spec.Cursor = 500, ""
	for {
		page, err := s.queryRecords(ctx, s.d.Pool, me(r), orgID, o, props, spec, false)
		if err != nil {
			return err
		}
		for _, rec := range page.Items {
			if n >= maxExportRows {
				truncated = true
				break
			}
			row := []string{rec.base().ID.String()}
			for _, p := range cols {
				row = append(row, csvSafe(exportValue(rec, p, loc, pipelineNames)))
			}
			_ = cw.Write(row)
			n++
		}
		if truncated || page.NextCursor == nil {
			break
		}
		spec.Cursor = *page.NextCursor
	}
	cw.Flush()
	keys := make([]string, len(cols))
	for i, p := range cols {
		keys[i] = p.Key
	}
	if err := audit.Log(ctx, s.d.Pool, me(r), "crm.export", "org", orgID.String(), map[string]any{
		"object": o, "rows": n, "truncated": truncated, "columns": keys, "filters": spec.Filters, "q": spec.Q,
	}); err != nil {
		return err
	}
	name := fmt.Sprintf("%s-%s.csv", o.path(), time.Now().In(loc).Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("X-Export-Rows", strconv.Itoa(n))
	if truncated {
		w.Header().Set("X-Export-Truncated", "true")
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
	return nil
}

// csvSafe stops spreadsheet apps from running a cell as a formula.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func exportValue(rec recordish, p Property, loc *time.Location, pipelines map[uuid.UUID]string) string {
	b := rec.base()
	ts := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.In(loc).Format("2006-01-02 15:04")
	}
	switch p.Key {
	case "created_at":
		return ts(&b.CreatedAt)
	case "updated_at":
		return ts(&b.UpdatedAt)
	case "last_activity_at":
		return ts(b.LastActivityAt)
	case "tags":
		names := []string{}
		for _, t := range b.Tags {
			names = append(names, t.Name)
		}
		return strings.Join(names, "; ")
	}
	if !p.Builtin {
		if p.Type == "user" {
			return b.PropsDisplay[p.Key]
		}
		if p.Type == "currency" {
			if v, ok := b.Props[p.Key].(float64); ok {
				return moneyDecimal(int64(v), "")
			}
		}
		return displayValue(p, b.Props[p.Key], nil)
	}
	switch v := rec.(type) {
	case *Contact:
		switch p.Key {
		case "name":
			return v.Name
		case "emails":
			return strings.Join(v.Emails, "; ")
		case "phones":
			return strings.Join(v.Phones, "; ")
		case "company":
			if v.Company != nil {
				return v.Company.Name
			}
			return ""
		}
	case *Deal:
		switch p.Key {
		case "amount":
			if v.Amount == nil {
				return ""
			}
			return moneyDecimal(*v.Amount, v.Currency)
		case "weighted_amount":
			if v.Weighted == nil {
				return ""
			}
			return moneyDecimal(*v.Weighted, v.Currency)
		case "probability":
			return strconv.Itoa(v.Stage.Probability) + "%"
		case "pipeline":
			return v.PipelineName
		case "stage_entered_at":
			return ts(&v.StageEnteredAt)
		case "closed_at":
			return ts(v.ClosedAt)
		}
	}
	_, text := currentValue(rec, p)
	if p.Type == "long_text" {
		if c, ok := rec.(*Company); ok {
			return c.Description
		}
	}
	return text
}

// ---- import --------------------------------------------------------------------------------

// ImportTarget is a field a CSV column can map to.
type ImportTarget struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

func importTargets(o objectType, props []Property) []ImportTarget {
	out := []ImportTarget{}
	if o == objContact {
		out = append(out, ImportTarget{Key: "full_name", Label: "Full name", Type: "text"})
	}
	for _, p := range props {
		if !p.Importable {
			continue
		}
		out = append(out, ImportTarget{Key: p.Key, Label: p.Label, Type: p.Type, Required: p.Required})
	}
	if o == objDeal {
		out = append(out, ImportTarget{Key: "contact_email", Label: "Contact email", Type: "email"})
	}
	return out
}

// aliases maps common CSV headings to targets.
var aliases = map[string]string{
	"email": "emails", "email address": "emails", "e-mail": "emails", "work email": "emails", "emails": "emails",
	"phone": "phones", "phone number": "phones", "mobile": "phones", "mobile phone": "phones", "phones": "phones",
	"first name": "first_name", "firstname": "first_name", "given name": "first_name",
	"last name": "last_name", "lastname": "last_name", "surname": "last_name", "family name": "last_name",
	"full name": "full_name", "contact name": "full_name",
	"title": "job_title", "job title": "job_title", "position": "job_title", "role": "job_title",
	"company": "company", "company name": "company", "organization": "company", "account": "company", "account name": "company",
	"website": "domain", "website url": "domain", "domain": "domain", "company domain": "domain", "url": "domain",
	"industry": "industry", "employees": "employees", "number of employees": "employees", "company size": "employees",
	"owner": "owner", "contact owner": "owner", "company owner": "owner", "deal owner": "owner", "owner email": "owner",
	"lifecycle stage": "lifecycle_stage", "lifecycle": "lifecycle_stage", "city": "city", "country": "country", "country/region": "country",
	"deal name": "name", "deal": "name", "amount": "amount", "deal amount": "amount", "value": "amount", "currency": "currency",
	"stage": "stage", "deal stage": "stage", "pipeline": "pipeline", "close date": "close_date", "expected close date": "close_date",
	"tags": "tags", "labels": "tags", "team": "team", "description": "description", "notes": "description",
	"contact email": "contact_email", "associated contact": "contact_email",
}

func suggestMapping(o objectType, headers []string, targets []ImportTarget) []*string {
	out := make([]*string, len(headers))
	used := map[string]bool{}
	for i, h := range headers {
		hl := strings.ToLower(strings.TrimSpace(h))
		var pick string
		for _, t := range targets {
			if hl == strings.ToLower(t.Label) || hl == t.Key {
				pick = t.Key
				break
			}
		}
		if pick == "" {
			if a, ok := aliases[hl]; ok {
				pick = a
			}
		}
		if pick == "name" && o == objContact {
			pick = "full_name"
		}
		if pick == "" && hl == "name" {
			pick = "name"
		}
		if pick != "" && !used[pick] && slices.ContainsFunc(targets, func(t ImportTarget) bool { return t.Key == pick }) {
			used[pick] = true
			p := pick
			out[i] = &p
		}
	}
	return out
}

type importReq struct {
	ObjectType      string     `json:"object_type"`
	Filename        string     `json:"filename"`
	CSV             string     `json:"csv"`
	Mapping         []*string  `json:"mapping"` // per column: target key or null to skip
	Dedupe          string     `json:"dedupe"`  // update | skip | create
	PipelineID      *uuid.UUID `json:"pipeline_id"`
	CreateCompanies bool       `json:"create_companies"`
}

func decodeImport(r *http.Request) (*importReq, objectType, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxImportBytes+64<<10)
	var req importReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, "", httpx.NewError(http.StatusRequestEntityTooLarge, "too_large", "That file is over 5 MB. Split it into smaller files.")
		}
		return nil, "", httpx.BadRequest("Invalid JSON: " + err.Error())
	}
	o, ok := parseObject(req.ObjectType)
	if !ok {
		return nil, "", httpx.BadRequest("object_type is company, contact or deal.")
	}
	if req.Dedupe == "" {
		req.Dedupe = "update"
	}
	if !slices.Contains([]string{"update", "skip", "create"}, req.Dedupe) {
		return nil, "", httpx.BadRequest("dedupe is update, skip or create.")
	}
	req.Filename = clip(req.Filename, 200)
	return &req, o, nil
}

// parseCSV reads a header row and data rows (BOM, ; or , separated).
func parseCSV(text string) ([]string, [][]string, error) {
	text = strings.TrimPrefix(text, "\ufeff")
	if !utf8.ValidString(text) {
		return nil, nil, httpx.BadRequest("Save the file as UTF-8 CSV and try again.")
	}
	first, _, _ := strings.Cut(text, "\n")
	cr := csv.NewReader(strings.NewReader(text))
	if strings.Count(first, ";") > strings.Count(first, ",") {
		cr.Comma = ';'
	}
	cr.FieldsPerRecord, cr.LazyQuotes, cr.TrimLeadingSpace = -1, true, true
	header, err := cr.Read()
	if err == io.EOF {
		return nil, nil, httpx.BadRequest("The file is empty.")
	}
	if err != nil {
		return nil, nil, httpx.BadRequest("That isn't a CSV file we can read: " + err.Error())
	}
	if len(header) > 200 {
		return nil, nil, httpx.BadRequest("The file has more than 200 columns.")
	}
	rows := [][]string{}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, httpx.BadRequest("Line " + strconv.Itoa(len(rows)+2) + " can't be read: " + err.Error())
		}
		empty := true
		for _, c := range rec {
			if strings.TrimSpace(c) != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}
		rows = append(rows, rec)
		if len(rows) > maxImportRows {
			return nil, nil, httpx.BadRequest(fmt.Sprintf("The file has more than %d rows. Split it into smaller files.", maxImportRows))
		}
	}
	return header, rows, nil
}

// inspectImport: POST /v1/crm/orgs/{orgID}/imports/inspect {object_type, csv}
// → headers, the first 5 rows, row count, targets and a suggested mapping.
func (s *Service) inspectImport(w http.ResponseWriter, r *http.Request) error {
	req, o, err := decodeImport(r)
	if err != nil {
		return err
	}
	header, rows, err := parseCSV(req.CSV)
	if err != nil {
		return err
	}
	props, err := s.loadProperties(r.Context(), s.d.Pool, orgParam(r), o)
	if err != nil {
		return err
	}
	targets := importTargets(o, props)
	sample := rows
	if len(sample) > 5 {
		sample = sample[:5]
	}
	httpx.OK(w, map[string]any{"headers": header, "sample": sample, "row_count": len(rows), "targets": targets,
		"mapping": suggestMapping(o, header, targets)})
	return nil
}

// RowResult is what an import does (or would do) with one row.
type RowResult struct {
	Row      int        `json:"row"`    // line in the file (the header is line 1)
	Action   string     `json:"action"` // create | update | skip | error
	Message  string     `json:"message,omitempty"`
	Title    string     `json:"title,omitempty"`
	RecordID *uuid.UUID `json:"record_id,omitempty"`
}

// ImportSummary counts row outcomes.
type ImportSummary struct {
	Total        int         `json:"total"`
	Create       int         `json:"create"`
	Update       int         `json:"update"`
	Skip         int         `json:"skip"`
	Error        int         `json:"error"`
	Rows         []RowResult `json:"rows"`
	NewTags      []string    `json:"new_tags"`
	NewCompanies int         `json:"new_companies"`
}

// previewImport: POST /v1/crm/orgs/{orgID}/imports/preview — validates every
// row and reports what would happen, without writing.
func (s *Service) previewImport(w http.ResponseWriter, r *http.Request) error {
	req, o, err := decodeImport(r)
	if err != nil {
		return err
	}
	header, rows, err := parseCSV(req.CSV)
	if err != nil {
		return err
	}
	if err := checkMapping(req.Mapping, header); err != nil {
		return err
	}
	im, err := s.newImporter(r.Context(), me(r), orgParam(r), o, req)
	if err != nil {
		return err
	}
	sum := &ImportSummary{Rows: []RowResult{}, NewTags: []string{}}
	for i, row := range rows {
		res := im.row(r.Context(), i+2, row, true)
		sum.add(res, 500)
	}
	sum.NewTags, sum.NewCompanies = im.newTagNames(), len(im.newCompanies)
	httpx.OK(w, sum)
	return nil
}

func (sum *ImportSummary) add(res RowResult, keep int) {
	sum.Total++
	switch res.Action {
	case "create":
		sum.Create++
	case "update":
		sum.Update++
	case "skip":
		sum.Skip++
	default:
		sum.Error++
	}
	if len(sum.Rows) < keep {
		sum.Rows = append(sum.Rows, res)
	}
}

func checkMapping(mapping []*string, header []string) error {
	if len(mapping) != len(header) {
		return httpx.BadRequest("Map every column (or skip it).")
	}
	seen := map[string]bool{}
	for _, m := range mapping {
		if m == nil {
			continue
		}
		if seen[*m] && *m != "emails" && *m != "phones" && *m != "tags" {
			return httpx.BadRequest("Two columns map to the same field. Map each field once.")
		}
		seen[*m] = true
	}
	return nil
}

// Import is a run, for the history list and polling.
type Import struct {
	ID         uuid.UUID   `json:"id"`
	ObjectType objectType  `json:"object_type"`
	Filename   string      `json:"filename"`
	Status     string      `json:"status"`
	Dedupe     string      `json:"dedupe"`
	Total      int         `json:"total_rows"`
	Processed  int         `json:"processed"`
	Created    int         `json:"created_count"`
	Updated    int         `json:"updated_count"`
	Skipped    int         `json:"skipped_count"`
	Errors     int         `json:"error_count"`
	RowErrors  []RowResult `json:"errors"`
	CreatedBy  *UserRef    `json:"created_by"`
	CreatedAt  time.Time   `json:"created_at"`
	FinishedAt *time.Time  `json:"finished_at"`
}

// runImport: POST /v1/crm/orgs/{orgID}/imports (crm.import, 10/h) — starts
// the import in the background and answers 202 with the run to poll.
func (s *Service) runImport(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	req, o, err := decodeImport(r)
	if err != nil {
		return err
	}
	header, rows, err := parseCSV(req.CSV)
	if err != nil {
		return err
	}
	if err := checkMapping(req.Mapping, header); err != nil {
		return err
	}
	im, err := s.newImporter(ctx, me(r), orgID, o, req)
	if err != nil {
		return err
	}
	var running int
	if err := s.d.Pool.QueryRow(ctx, `SELECT count(*) FROM crm_imports WHERE org_id = $1 AND created_by = $2 AND status = 'running'`, orgID, me(r)).Scan(&running); err != nil {
		return err
	}
	if running > 0 {
		return httpx.Conflict("Your previous import is still running. Wait for it to finish.")
	}
	var id uuid.UUID
	if err := s.d.Pool.QueryRow(ctx, `INSERT INTO crm_imports (org_id, object_type, filename, dedupe, total_rows, created_by)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, orgID, string(o), req.Filename, req.Dedupe, len(rows), me(r)).Scan(&id); err != nil {
		return err
	}
	if err := audit.Log(ctx, s.d.Pool, me(r), "crm.import", "crm.import", id.String(), map[string]any{
		"org_id": orgID, "object": o, "rows": len(rows), "filename": req.Filename, "dedupe": req.Dedupe,
	}); err != nil {
		return err
	}
	bg.Go(ctx, "crm.import", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, importTimeout)
		defer cancel()
		s.processImport(ctx, id, im, rows)
		return nil
	})
	imp, err := s.loadImport(ctx, orgID, id)
	if err != nil {
		return err
	}
	httpx.JSON(w, http.StatusAccepted, imp)
	return nil
}

func (s *Service) processImport(ctx context.Context, id uuid.UUID, im *importer, rows [][]string) {
	sum := &ImportSummary{Rows: []RowResult{}}
	errorsKept := []RowResult{}
	flush := func(status string) {
		b, _ := json.Marshal(errorsKept)
		finished := "NULL"
		if status != "running" {
			finished = "now()"
		}
		if _, err := s.d.Pool.Exec(context.WithoutCancel(ctx), `UPDATE crm_imports SET status = $2, processed = $3, created_count = $4, updated_count = $5,
			skipped_count = $6, error_count = $7, errors = $8, finished_at = `+finished+` WHERE id = $1`,
			id, status, sum.Total, sum.Create, sum.Update, sum.Skip, sum.Error, b); err != nil {
			slog.ErrorContext(ctx, "crm: import progress", "import_id", id, "err", err)
		}
	}
	defer func() {
		if p := recover(); p != nil {
			slog.ErrorContext(ctx, "crm: import panicked", "import_id", id, "panic", p)
			flush("failed")
		}
	}()
	for i, row := range rows {
		if ctx.Err() != nil {
			errorsKept = append(errorsKept, RowResult{Row: i + 2, Action: "error", Message: "The import ran out of time here. Import the remaining rows again."})
			flush("failed")
			return
		}
		res := im.row(ctx, i+2, row, false)
		sum.add(res, 0)
		if res.Action == "error" && len(errorsKept) < 200 {
			errorsKept = append(errorsKept, res)
		}
		if (i+1)%100 == 0 {
			flush("running")
		}
	}
	flush("done")
	s.boardChanged(ctx, im.pipelineIDs()...)
}

func (s *Service) loadImport(ctx context.Context, orgID, id uuid.UUID) (*Import, error) {
	var imp Import
	var ot string
	var errs []byte
	var uid *uuid.UUID
	var uname, avatar *string
	err := s.d.Pool.QueryRow(ctx, `SELECT i.id, i.object_type, i.filename, i.status, i.dedupe, i.total_rows, i.processed, i.created_count,
		i.updated_count, i.skipped_count, i.error_count, i.errors, i.created_at, i.finished_at, u.id, u.display_name, u.avatar_url
		FROM crm_imports i LEFT JOIN users u ON u.id = i.created_by AND u.status <> 'deleted' WHERE i.id = $1 AND i.org_id = $2`, id, orgID).
		Scan(&imp.ID, &ot, &imp.Filename, &imp.Status, &imp.Dedupe, &imp.Total, &imp.Processed, &imp.Created, &imp.Updated, &imp.Skipped,
			&imp.Errors, &errs, &imp.CreatedAt, &imp.FinishedAt, &uid, &uname, &avatar)
	if db.IsNoRows(err) {
		return nil, httpx.NotFound("Import not found.")
	}
	if err != nil {
		return nil, err
	}
	imp.ObjectType = objectType(ot)
	imp.RowErrors = []RowResult{}
	_ = json.Unmarshal(errs, &imp.RowErrors)
	if uid != nil && uname != nil {
		imp.CreatedBy = &UserRef{ID: *uid, Name: *uname, AvatarURL: avatar}
	}
	return &imp, nil
}

// listImports: GET /v1/crm/orgs/{orgID}/imports (crm.import): the 50 latest runs.
func (s *Service) listImports(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	orgID := orgParam(r)
	rows, err := s.d.Pool.Query(ctx, `SELECT id FROM crm_imports WHERE org_id = $1 ORDER BY created_at DESC, id DESC LIMIT 50`, orgID)
	if err != nil {
		return err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []*Import{}
	for _, id := range ids {
		imp, err := s.loadImport(ctx, orgID, id)
		if err != nil {
			return err
		}
		out = append(out, imp)
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

// getImport: GET /v1/crm/orgs/{orgID}/imports/{importID} — poll a run.
func (s *Service) getImport(w http.ResponseWriter, r *http.Request) error {
	id, err := idParam(r, "importID")
	if err != nil {
		return err
	}
	imp, err := s.loadImport(r.Context(), orgParam(r), id)
	if err != nil {
		return err
	}
	httpx.OK(w, imp)
	return nil
}

// failStaleImports marks runs a restart interrupted as failed.
func (s *Service) failStaleImports(ctx context.Context) error {
	_, err := s.d.Pool.Exec(ctx, `UPDATE crm_imports SET status = 'failed', finished_at = now()
		WHERE status = 'running' AND created_at < now() - $1::interval`, (importTimeout + time.Minute).String())
	return err
}

// ---- row conversion ------------------------------------------------------------------

type importer struct {
	s            *Service
	userID       uuid.UUID
	orgID        uuid.UUID
	o            objectType
	req          *importReq
	props        []Property
	pipelines    []Pipeline
	pipeline     *Pipeline
	currency     string
	members      map[string]uuid.UUID // lower email and lower name → id
	teams        map[string]uuid.UUID // lower name and handle → id
	tags         map[string]uuid.UUID // lower name → id
	newTags      map[string]bool
	seen         map[string]int // dedupe keys in this file → first row
	newCompanies map[string]bool
	touched      map[uuid.UUID]bool
}

func (s *Service) newImporter(ctx context.Context, userID, orgID uuid.UUID, o objectType, req *importReq) (*importer, error) {
	im := &importer{s: s, userID: userID, orgID: orgID, o: o, req: req, members: map[string]uuid.UUID{}, teams: map[string]uuid.UUID{},
		tags: map[string]uuid.UUID{}, newTags: map[string]bool{}, seen: map[string]int{}, newCompanies: map[string]bool{}, touched: map[uuid.UUID]bool{}}
	var err error
	if im.props, err = s.loadProperties(ctx, s.d.Pool, orgID, o); err != nil {
		return nil, err
	}
	targets := importTargets(o, im.props)
	for _, m := range req.Mapping {
		if m != nil && !slices.ContainsFunc(targets, func(t ImportTarget) bool { return t.Key == *m }) {
			return nil, httpx.BadRequest("“" + *m + "” isn't a field you can import into.")
		}
	}
	if err := s.d.Pool.QueryRow(ctx, `SELECT currency FROM crm_settings WHERE org_id = $1`, orgID).Scan(&im.currency); err != nil {
		return nil, err
	}
	if im.pipelines, err = s.loadPipelines(ctx, s.d.Pool, orgID); err != nil {
		return nil, err
	}
	if o == objDeal {
		if len(im.pipelines) == 0 {
			return nil, httpx.BadRequest("Add a pipeline in CRM settings first.")
		}
		im.pipeline = &im.pipelines[0]
		if req.PipelineID != nil {
			im.pipeline = nil
			for i := range im.pipelines {
				if im.pipelines[i].ID == *req.PipelineID {
					im.pipeline = &im.pipelines[i]
				}
			}
			if im.pipeline == nil {
				return nil, httpx.BadRequest("Choose a pipeline from this " + orgs.Noun() + ".")
			}
		}
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT u.id, lower(u.email), lower(u.display_name) FROM org_members m JOIN users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND u.status <> 'deleted'`, orgID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		var email, name string
		if err := rows.Scan(&id, &email, &name); err != nil {
			rows.Close()
			return nil, err
		}
		im.members[email], im.members[name] = id, id
	}
	rows.Close()
	rows, err = s.d.Pool.Query(ctx, `SELECT id, lower(name), handle FROM access_teams WHERE org_id = $1`, orgID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		var name, handle string
		if err := rows.Scan(&id, &name, &handle); err != nil {
			rows.Close()
			return nil, err
		}
		im.teams[name], im.teams[handle], im.teams["@"+handle] = id, id, id
	}
	rows.Close()
	rows, err = s.d.Pool.Query(ctx, `SELECT id, lower(name) FROM crm_tags WHERE org_id = $1`, orgID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		im.tags[name] = id
	}
	rows.Close()
	return im, nil
}

func (im *importer) newTagNames() []string {
	out := []string{}
	for t := range im.newTags {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

func (im *importer) pipelineIDs() []uuid.UUID {
	out := []uuid.UUID{}
	for _, p := range im.pipelines {
		out = append(out, p.ID)
	}
	return out
}

var listSplit = regexp.MustCompile(`\s*[;,]\s*`)

var dateLayouts = []string{"2006-01-02", "2006/01/02", "01/02/2006", "1/2/2006", "01/02/06", "1/2/06", "02.01.2006", "2.1.2006",
	"Jan 2, 2006", "January 2, 2006", "2 Jan 2006", "2 January 2006", "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05"}

func parseImportDate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}

func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// row converts and applies (or, with dry, only checks) one CSV row.
func (im *importer) row(ctx context.Context, line int, cells []string, dry bool) RowResult {
	res := RowResult{Row: line}
	fail := func(msg string) RowResult {
		res.Action, res.Message = "error", msg
		return res
	}
	in := map[string]json.RawMessage{}
	var emails, phones, tagNames []string
	var companyCell, contactEmail, pipelineCell, stageCell string
	currency := im.currency
	cell := func(i int) string {
		if i < len(cells) {
			return strings.TrimSpace(cells[i])
		}
		return ""
	}
	// Currency first, so amounts parse in it.
	for i, m := range im.req.Mapping {
		if m != nil && *m == "currency" && cell(i) != "" {
			currency = strings.ToUpper(cell(i))
		}
	}
	for i, m := range im.req.Mapping {
		if m == nil {
			continue
		}
		v := cell(i)
		if v == "" {
			continue
		}
		key := *m
		switch key {
		case "full_name":
			first, last := v, ""
			if j := strings.LastIndex(v, " "); j > 0 {
				first, last = strings.TrimSpace(v[:j]), strings.TrimSpace(v[j+1:])
			}
			in["first_name"], in["last_name"] = jsonString(first), jsonString(last)
			continue
		case "emails":
			emails = append(emails, listSplit.Split(v, -1)...)
			continue
		case "phones":
			phones = append(phones, listSplit.Split(v, -1)...)
			continue
		case "tags":
			tagNames = append(tagNames, listSplit.Split(v, -1)...)
			continue
		case "company":
			companyCell = v
			continue
		case "contact_email":
			contactEmail = v
			continue
		case "pipeline":
			pipelineCell = v
			continue
		case "stage":
			stageCell = v
			continue
		}
		p, ok := findProp(im.props, key)
		if !ok {
			continue
		}
		raw, err := im.convert(p, v, currency)
		if err != nil {
			return fail(err.Error())
		}
		in[key] = raw
	}
	if len(emails) > 0 {
		b, _ := json.Marshal(emails)
		in["emails"] = b
	}
	if len(phones) > 0 {
		b, _ := json.Marshal(phones)
		in["phones"] = b
	}
	if len(tagNames) > 0 {
		ids := []string{}
		for _, t := range tagNames {
			if t = strings.TrimSpace(t); t == "" {
				continue
			}
			if len([]rune(t)) > 40 {
				return fail("Tag “" + clip(t, 40) + "” is longer than 40 characters.")
			}
			id, ok := im.tags[strings.ToLower(t)]
			if !ok {
				if dry {
					im.newTags[t] = true
					continue
				}
				var err error
				if id, err = im.createTag(ctx, t); err != nil {
					return fail("Couldn't add the tag “" + t + "”.")
				}
			}
			ids = append(ids, id.String())
		}
		if len(ids) > 0 || !dry {
			b, _ := json.Marshal(ids)
			in["tags"] = b
		}
	}
	if im.o == objDeal {
		pl := im.pipeline
		if pipelineCell != "" {
			pl = nil
			for i := range im.pipelines {
				if strings.EqualFold(im.pipelines[i].Name, pipelineCell) {
					pl = &im.pipelines[i]
				}
			}
			if pl == nil {
				return fail("No pipeline called “" + clip(pipelineCell, 60) + "”.")
			}
		}
		var stage *Stage
		if stageCell != "" {
			for i := range pl.Stages {
				if strings.EqualFold(pl.Stages[i].Name, stageCell) {
					stage = &pl.Stages[i]
				}
			}
			if stage == nil {
				return fail("“" + clip(stageCell, 60) + "” isn't a stage of " + pl.Name + ".")
			}
		} else {
			for i := range pl.Stages {
				if pl.Stages[i].Outcome == "open" {
					stage = &pl.Stages[i]
					break
				}
			}
		}
		if stage != nil {
			in["stage"] = jsonString(stage.ID.String())
		}
		if _, ok := in["currency"]; !ok {
			in["currency"] = jsonString(currency)
		}
	}
	// Associations.
	if companyCell != "" && (im.o == objContact || im.o == objDeal) {
		id, err := im.findCompany(ctx, companyCell, dry)
		if err != nil {
			return fail(err.Error())
		}
		if id != nil {
			if im.o == objDeal {
				in["company"] = jsonString(id.String())
			} else {
				in["company_id"] = jsonString(id.String())
			}
		}
	}
	if contactEmail != "" && im.o == objDeal {
		e, err := cleanEmail(contactEmail)
		if err != nil {
			return fail("“" + clip(contactEmail, 60) + "” isn't an email address.")
		}
		dups, err := im.s.findDuplicates(ctx, im.s.d.Pool, im.userID, im.orgID, objContact, map[string]any{"emails": []string{e}}, uuid.Nil)
		if err != nil {
			return fail("Couldn't look up the contact.")
		}
		if len(dups) == 0 {
			return fail("No contact you can see has the email " + e + ".")
		}
		in["contact_ids"] = json.RawMessage(`["` + dups[0].ID.String() + `"]`)
	}
	return im.apply(ctx, res, in, dry)
}

// apply dedupes and creates or updates the record.
func (im *importer) apply(ctx context.Context, res RowResult, in map[string]json.RawMessage, dry bool) RowResult {
	fail := func(err error) RowResult {
		res.Action = "error"
		var he *httpx.Error
		if errors.As(err, &he) {
			res.Message = he.Message
		} else if ve, ok := err.(valueError); ok {
			res.Message = ve.msg
		} else {
			slog.WarnContext(ctx, "crm: import row", "err", err)
			res.Message = "Couldn't save this row."
		}
		return res
	}
	// Validate the fields as a create would.
	var p pending
	check := map[string]json.RawMessage{}
	for k, v := range in {
		if k != "company_id" && k != "contact_ids" {
			check[k] = v
		}
	}
	if err := im.s.applyFields(ctx, im.s.d.Pool, im.orgID, im.userID, im.o, im.props, nil, check, &p); err != nil {
		return fail(err)
	}
	res.Title = importTitle(im.o, p.cols)
	if im.o == objContact {
		first, _ := p.cols["first_name"].(string)
		last, _ := p.cols["last_name"].(string)
		em, _ := p.cols["emails"].([]string)
		if first == "" && last == "" && len(em) == 0 {
			return fail(verr("Add a name or an email address."))
		}
	} else if n, _ := p.cols["name"].(string); n == "" {
		return fail(verr("The name is empty."))
	}
	// Duplicates: earlier rows of this file, then records the importer can see.
	keys := dedupeKeys(im.o, p.cols)
	for _, k := range keys {
		if first, ok := im.seen[k]; ok && im.req.Dedupe != "create" {
			if dry {
				res.Action = "update"
				if im.req.Dedupe == "skip" {
					res.Action = "skip"
				}
				res.Message = fmt.Sprintf("Same %s as line %d.", dedupeLabel(im.o), first)
				return res
			}
		}
	}
	for _, k := range keys {
		if _, ok := im.seen[k]; !ok {
			im.seen[k] = res.Row
		}
	}
	var existing *Duplicate
	if im.req.Dedupe != "create" {
		dups, err := im.s.findDuplicates(ctx, im.s.d.Pool, im.userID, im.orgID, im.o, p.cols, uuid.Nil)
		if err != nil {
			return fail(err)
		}
		if len(dups) > 0 {
			existing = &dups[0]
		}
	}
	if existing != nil {
		res.RecordID = &existing.ID
		if im.req.Dedupe == "skip" {
			res.Action, res.Message = "skip", "Already exists: "+existing.Name+"."
			return res
		}
		lvl, err := access.LevelOf(ctx, im.s.d.Pool, im.userID, im.o.kind(), existing.ID)
		if err != nil {
			return fail(err)
		}
		if lvl < access.Edit {
			res.Action, res.Message = "skip", "Matches "+existing.Name+", which you can't edit."
			return res
		}
		res.Action, res.Message = "update", "Updates "+existing.Name+"."
		if dry {
			return res
		}
		upd := map[string]json.RawMessage{}
		for k, v := range in {
			if k == "company_id" || k == "contact_ids" {
				continue
			}
			upd[k] = v
		}
		if im.o == objContact {
			// Keep existing emails and add new ones.
			var cur []string
			_ = im.s.d.Pool.QueryRow(ctx, `SELECT emails FROM crm_contacts WHERE id = $1`, existing.ID).Scan(&cur)
			newEmails, _ := p.cols["emails"].([]string)
			for _, e := range newEmails {
				if !slices.Contains(cur, e) {
					cur = append(cur, e)
				}
			}
			b, _ := json.Marshal(cur)
			upd["emails"] = b
		}
		if _, err := im.s.updateWith(ctx, im.userID, im.o, existing.ID, upd, true); err != nil {
			return fail(err)
		}
		if err := im.associateExisting(ctx, existing.ID, in); err != nil {
			return fail(err)
		}
		im.touched[existing.ID] = true
		return res
	}
	res.Action = "create"
	if dry {
		return res
	}
	in["allow_duplicate"] = json.RawMessage("true")
	rec, err := im.s.create(ctx, im.userID, im.orgID, im.o, in, true)
	if err != nil {
		return fail(err)
	}
	id := rec.base().ID
	res.RecordID = &id
	im.touched[id] = true
	return res
}

func (im *importer) associateExisting(ctx context.Context, id uuid.UUID, in map[string]json.RawMessage) error {
	var ids []string
	if raw, ok := in["company_id"]; ok && im.o == objContact {
		var c string
		_ = json.Unmarshal(raw, &c)
		ids = append(ids, c)
	}
	if raw, ok := in["contact_ids"]; ok && im.o == objDeal {
		_ = json.Unmarshal(raw, &ids)
	}
	if len(ids) == 0 {
		return nil
	}
	return db.WithTx(ctx, im.s.d.Pool, func(tx pgx.Tx) error {
		for _, s := range ids {
			other, err := uuid.Parse(s)
			if err != nil {
				continue
			}
			if im.o == objContact {
				if err := im.s.linkContactCompany(ctx, tx, im.userID, im.orgID, id, other, false); err != nil {
					return err
				}
			} else if err := im.s.linkDealContact(ctx, tx, im.userID, im.orgID, id, other); err != nil {
				return err
			}
		}
		return nil
	})
}

func dedupeKeys(o objectType, cols map[string]any) []string {
	switch o {
	case objContact:
		emails, _ := cols["emails"].([]string)
		out := []string{}
		for _, e := range emails {
			out = append(out, "e:"+e)
		}
		return out
	case objCompany:
		if d, _ := cols["domain"].(string); d != "" {
			return []string{"d:" + d}
		}
	}
	return nil
}

func dedupeLabel(o objectType) string {
	if o == objContact {
		return "email"
	}
	return "domain"
}

func importTitle(o objectType, cols map[string]any) string {
	if o == objContact {
		first, _ := cols["first_name"].(string)
		last, _ := cols["last_name"].(string)
		emails, _ := cols["emails"].([]string)
		return contactName(first, last, emails)
	}
	n, _ := cols["name"].(string)
	return n
}

func (im *importer) createTag(ctx context.Context, name string) (uuid.UUID, error) {
	var id uuid.UUID
	err := im.s.d.Pool.QueryRow(ctx, `INSERT INTO crm_tags (org_id, name, created_by) VALUES ($1, $2, $3)
		ON CONFLICT (org_id, lower(name)) DO UPDATE SET updated_at = crm_tags.updated_at RETURNING id`, im.orgID, name, im.userID).Scan(&id)
	if err == nil {
		im.tags[strings.ToLower(name)] = id
	}
	return id, err
}

// findCompany matches a company cell (domain, website or name) to a company
// the importer can see, creating it when asked.
func (im *importer) findCompany(ctx context.Context, cell string, dry bool) (*uuid.UUID, error) {
	b := newSQL(ctx, im.userID)
	domain, derr := cleanDomain(cell)
	var cond string
	if derr == nil && strings.Contains(cell, ".") && !strings.Contains(cell, " ") {
		cond = "r.domain = " + b.arg(domain)
	} else {
		cond = "lower(r.name) = lower(" + b.arg(cell) + ")"
		domain = ""
	}
	var id uuid.UUID
	err := im.s.d.Pool.QueryRow(ctx, `SELECT r.id FROM crm_companies r WHERE r.org_id = `+b.arg(im.orgID)+` AND `+cond+`
		AND access_level($1, 'crm.company', r.id, $2) >= 1 ORDER BY r.created_at, r.id LIMIT 1`, b.args...).Scan(&id)
	if err == nil {
		return &id, nil
	}
	if !db.IsNoRows(err) {
		return nil, err
	}
	if !im.req.CreateCompanies {
		return nil, verr("No company you can see matches “%s”.", clip(cell, 60))
	}
	key := strings.ToLower(cell)
	if dry {
		im.newCompanies[key] = true
		return nil, nil
	}
	in := map[string]json.RawMessage{"allow_duplicate": json.RawMessage("true")}
	if domain != "" {
		name := domain
		if i := strings.Index(name, "."); i > 0 {
			name = strings.ToUpper(name[:1]) + name[1:i]
		}
		in["name"], in["domain"] = jsonString(name), jsonString(domain)
	} else {
		in["name"] = jsonString(cell)
	}
	rec, err := im.s.create(ctx, im.userID, im.orgID, objCompany, in, true)
	if err != nil {
		return nil, err
	}
	id = rec.base().ID
	im.newCompanies[key] = true
	return &id, nil
}

// convert turns a cell into the JSON a field expects.
func (im *importer) convert(p Property, v, currency string) (json.RawMessage, error) {
	switch p.Type {
	case "number":
		f, err := strconv.ParseFloat(strings.ReplaceAll(v, ",", ""), 64)
		if err != nil {
			return nil, verr("%s: “%s” isn't a number.", p.Label, clip(v, 40))
		}
		if p.Key == "employees" {
			return json.RawMessage(strconv.Itoa(int(f))), nil
		}
		return json.RawMessage(strconv.FormatFloat(f, 'f', -1, 64)), nil
	case "currency":
		n, err := parseMoney(v, currency)
		if err != nil {
			return nil, verr("%s: “%s” isn't an amount.", p.Label, clip(v, 40))
		}
		return json.RawMessage(strconv.FormatInt(n, 10)), nil
	case "currency_code":
		return jsonString(strings.ToUpper(v)), nil
	case "date":
		d, ok := parseImportDate(v)
		if !ok {
			return nil, verr("%s: “%s” isn't a date (use 2026-11-30).", p.Label, clip(v, 40))
		}
		return jsonString(d), nil
	case "checkbox":
		switch strings.ToLower(v) {
		case "yes", "y", "true", "1", "x", "✓":
			return json.RawMessage("true"), nil
		case "no", "n", "false", "0":
			return json.RawMessage("false"), nil
		}
		return nil, verr("%s: use yes or no.", p.Label)
	case "select":
		for _, o := range p.Options {
			if strings.EqualFold(o.Value, v) || strings.EqualFold(o.Label, v) {
				return jsonString(o.Value), nil
			}
		}
		return nil, verr("%s: “%s” isn't one of its options.", p.Label, clip(v, 40))
	case "multi_select":
		out := []string{}
		for _, part := range listSplit.Split(v, -1) {
			found := false
			for _, o := range p.Options {
				if strings.EqualFold(o.Value, part) || strings.EqualFold(o.Label, part) {
					out, found = append(out, o.Value), true
				}
			}
			if !found && part != "" {
				return nil, verr("%s: “%s” isn't one of its options.", p.Label, clip(part, 40))
			}
		}
		b, _ := json.Marshal(out)
		return b, nil
	case "user":
		id, ok := im.members[strings.ToLower(v)]
		if !ok {
			return nil, verr("%s: no member called or with the email “%s”.", p.Label, clip(v, 60))
		}
		return jsonString(id.String()), nil
	case "team":
		id, ok := im.teams[strings.ToLower(v)]
		if !ok {
			return nil, verr("No team called “%s”.", clip(v, 60))
		}
		return jsonString(id.String()), nil
	}
	return jsonString(v), nil
}
