package reputation

import (
	"time"

	"github.com/shopspring/decimal"
)

// The rollup: snapshots of a person's cumulative counters become one Day per
// UTC day. Flows, activity, volume and incidents are the differences between
// the day's open and close snapshots, so they need no event history. Pure,
// so it is tested without a database.

// Snapshot is one reputation_snapshots row: a person's counters at a moment.
type Snapshot struct {
	At        time.Time
	Value     decimal.Decimal
	NetFlows  decimal.Decimal // cumulative: money put in minus taken out
	Activity  int64           // cumulative
	Volume    decimal.Decimal // cumulative
	Incidents int64           // cumulative
	Exposed   bool            // open exposure at that moment
}

func nonNeg(d decimal.Decimal) decimal.Decimal {
	if d.IsNegative() {
		return decimal.Zero
	}
	return d
}

func nonNegInt(n int64) int {
	if n < 0 {
		return 0
	}
	return int(n)
}

// RollupDays turns snapshots (ascending; include the last one before from
// when it exists) into a Day for every UTC day in [from, to] that has a
// snapshot.
//
//   - Close is the day's last snapshot; the open is the last snapshot before
//     the day (yesterday's close), or the day's first snapshot when there is
//     none (counting starts at the first snapshot).
//   - Flows, activity, volume and incidents are close minus open; a counter
//     that went down (the source recounted) counts as 0.
//   - Ret = (close − open − flows) / max(open, floor).
//   - Active: activity that day, or exposure in any of its snapshots.
func RollupDays(snaps []Snapshot, from, to time.Time, floor decimal.Decimal) []Day {
	var out []Day
	from, to = dayStart(from), dayStart(to)
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		next := d.AddDate(0, 0, 1)
		openIdx, closeIdx, firstIdx := -1, -1, -1
		exposed := false
		for i, s := range snaps {
			switch {
			case s.At.Before(d):
				openIdx = i
			case s.At.Before(next):
				if firstIdx < 0 {
					firstIdx = i
				}
				closeIdx = i
				exposed = exposed || s.Exposed
			}
		}
		if closeIdx < 0 {
			continue
		}
		if openIdx < 0 {
			openIdx = firstIdx
		}
		open, cl := snaps[openIdx], snaps[closeIdx]
		flows := cl.NetFlows.Sub(open.NetFlows)
		activity := nonNegInt(cl.Activity - open.Activity)
		out = append(out, Day{Day: d, Open: open.Value, Close: cl.Value, Flows: flows,
			Ret:    DayReturn(open.Value, cl.Value, flows, floor),
			Active: activity > 0 || exposed, Activity: activity, Volume: nonNeg(cl.Volume.Sub(open.Volume)),
			Incidents: nonNegInt(cl.Incidents - open.Incidents), Source: SourceSnapshots})
	}
	return out
}
