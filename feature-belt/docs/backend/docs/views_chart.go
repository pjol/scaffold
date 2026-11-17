package docs

import (
	"math"
	"net/http"
	"sort"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
)

// Chart views are drawn from data worked out here, over every row the
// viewer can open that matches the view: one point per group of the X
// property (dates bucketed), measured as a count or an aggregation of a
// number property, optionally split into series by another property.

// ChartPoint is one bar, point or slice.
type ChartPoint struct {
	Key    string             `json:"key"`
	Label  string             `json:"label"`
	Color  string             `json:"color,omitempty"`
	Value  float64            `json:"value"`
	Series map[string]float64 `json:"series,omitempty"`
}

// ChartData is a chart view's data.
type ChartData struct {
	Points []ChartPoint `json:"points"`
	Series []GroupInfo  `json:"series"`
	Total  int          `json:"total"` // rows counted
}

// chart: GET /docs/databases/{id}/chart?view=&tz= (view on the database).
func (s *Service) chart(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	dbp, err := requirePage(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	if dbp.Kind != "database" {
		return errNotDatabase
	}
	views, err := s.viewsFor(ctx, id, r.URL.Query().Get("view"))
	if err != nil {
		return err
	}
	rq, err := rowsRequest(r, dbp, views)
	if err != nil {
		return err
	}
	if rq.view == nil || rq.view.Config.Chart == nil {
		return httpx.BadRequest("Choose a chart view.")
	}
	rq.me, rq.agent, rq.token, rq.noMeta = me, access.ViaAgent(ctx), "", true
	if rq.conj != "or" {
		rq.conj = "and"
	}
	out, err := s.chartData(r, rq)
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

func (s *Service) chartData(r *http.Request, rq rowQuery) (*ChartData, error) {
	cfg := *rq.view.Config.Chart
	all, c, err := s.matchingRows(r.Context(), rq)
	if err != nil {
		return nil, err
	}
	schema := rq.db.Schema
	out := &ChartData{Points: []ChartPoint{}, Series: []GroupInfo{}, Total: len(all)}
	if cfg.X == "" {
		return out, nil
	}
	xg := &GroupConfig{By: cfg.XBy, Step: 10}
	if p, ok := findProp(schema, cfg.X); ok {
		xg = cleanGroup(&GroupConfig{By: cfg.XBy}, p)
		if isDateish(p) && cfg.XBy != "" {
			xg.By = cfg.XBy
		}
	}
	groups := c.groupInfos(rq.db.ID, schema, all, cfg.X, xg)
	var sg *GroupConfig
	if cfg.Series != "" {
		if p, ok := findProp(schema, cfg.Series); ok {
			sg = cleanGroup(nil, p)
			out.Series = c.groupInfos(rq.db.ID, schema, all, cfg.Series, sg)
		}
	}
	yProp, hasY := findProp(schema, cfg.YProperty)
	measure := func(rows []*crowRef) float64 {
		if cfg.Y == "count" || !hasY {
			return float64(len(rows))
		}
		vals := make([]fValue, len(rows))
		for i, rr := range rows {
			vals[i] = c.Value(rq.db.ID, rr.r.ID, yProp)
		}
		v := aggregate(cfg.Y, vals)
		if v.Kind != fNumber || math.IsNaN(v.Num) {
			return 0
		}
		return v.Num
	}
	bucket := map[string][]*crowRef{}
	split := map[string]map[string][]*crowRef{}
	for _, r := range all {
		ref := &crowRef{r}
		var series []groupKey
		if sg != nil {
			series = c.groupKeys(rq.db.ID, schema, r, cfg.Series, sg)
		}
		for _, k := range c.groupKeys(rq.db.ID, schema, r, cfg.X, xg) {
			bucket[k.key] = append(bucket[k.key], ref)
			for _, sk := range series {
				if split[k.key] == nil {
					split[k.key] = map[string][]*crowRef{}
				}
				split[k.key][sk.key] = append(split[k.key][sk.key], ref)
			}
		}
	}
	for _, g := range groups {
		rows := bucket[g.Key]
		if g.Hidden || (cfg.HideEmpty && len(rows) == 0) || (g.Key == "" && len(rows) == 0) {
			continue
		}
		pt := ChartPoint{Key: g.Key, Label: g.Label, Color: g.Color, Value: measure(rows)}
		if sg != nil {
			pt.Series = map[string]float64{}
			for sk, list := range split[g.Key] {
				pt.Series[sk] = measure(list)
			}
		}
		out.Points = append(out.Points, pt)
	}
	switch cfg.Sort {
	case "x_desc":
		for i, j := 0, len(out.Points)-1; i < j; i, j = i+1, j-1 {
			out.Points[i], out.Points[j] = out.Points[j], out.Points[i]
		}
	case "y_asc":
		sort.SliceStable(out.Points, func(i, j int) bool { return out.Points[i].Value < out.Points[j].Value })
	case "y_desc":
		sort.SliceStable(out.Points, func(i, j int) bool { return out.Points[i].Value > out.Points[j].Value })
	}
	if cfg.Cumulative && cfg.Type == "line" {
		run := 0.0
		runs := map[string]float64{}
		for i := range out.Points {
			run += out.Points[i].Value
			out.Points[i].Value = run
			for k, v := range out.Points[i].Series {
				runs[k] += v
				out.Points[i].Series[k] = runs[k]
			}
		}
	}
	return out, nil
}

type crowRef struct{ r *crow }

// ---- formula check ---------------------------------------------------------------------

type formulaCheckReq struct {
	Expression string  `json:"expression"`
	PropertyID string  `json:"property_id"` // the formula's own id (it can't use itself)
	RowID      *string `json:"row_id"`      // preview against this row (else the first)
	TZ         string  `json:"tz"`
}

// FormulaCheck is a formula's type (or error) and a preview value.
type FormulaCheck struct {
	Type    string        `json:"type,omitempty"`
	Error   *FormulaError `json:"error,omitempty"`
	Preview any           `json:"preview"`
	Text    string        `json:"text"`
}

// checkFormula: POST /docs/databases/{id}/formula (view): compile an
// expression against the database and evaluate it on a row the caller can
// open, for the formula editor's live result.
func (s *Service) checkFormula(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var req formulaCheckReq
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if len(req.Expression) > maxFormula {
		return httpx.BadRequest("Keep the formula under 4,000 characters.")
	}
	dbp, err := requirePage(ctx, s.d.Pool, me, id, access.View)
	if err != nil {
		return err
	}
	if dbp.Kind != "database" {
		return errNotDatabase
	}
	v := memberViewer(ctx, me, req.TZ)
	c := s.newComputer(ctx, v)
	c.setSchema(id, dbp.Schema)
	f, err := compileFormula(req.Expression, id, req.PropertyID, c)
	out := FormulaCheck{}
	if err != nil {
		fe, ok := err.(*FormulaError)
		if !ok {
			fe = &FormulaError{Msg: err.Error()}
		}
		out.Error = fe
		httpx.OK(w, out)
		return nil
	}
	out.Type = f.Type.String()
	var row *Page
	if req.RowID != nil {
		if rid, err := uuid.Parse(*req.RowID); err == nil {
			if p, err := requirePage(ctx, s.d.Pool, me, rid, access.View); err == nil && p.IsRow && p.ParentID != nil && *p.ParentID == id {
				row = p
			}
		}
	}
	if row == nil {
		page, err := s.queryRows(ctx, rowQuery{db: dbp, limit: 1, me: me, agent: v.agent, tz: req.TZ, noMeta: true})
		if err == nil && len(page.Items) > 0 {
			row, _ = loadPage(ctx, s.d.Pool, page.Items[0].ID, false)
		}
	}
	if row != nil {
		c.addRow(pageRow(row))
		val := f.Eval(c, id, row.ID)
		out.Preview, out.Text = fValueJSON(val), fValueText(val, c)
	}
	httpx.OK(w, out)
	return nil
}
