package reputation

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/db"
)

// Anti-gaming. The detectors are pure; the daily job feeds them from the
// seams the app plugs in (SetPairSource, SetVoteSource), and other modules
// raise their own flags with Flag. Any open flag puts the person
// under_review: score hidden, off the leaderboard, until staff clear or
// confirm it. A confirmed flag keeps them under review for 90 days.

// Integrity flag kinds raised by this package. Other modules pass their own
// (lowercase snake_case, e.g. "copy_front_run").
const (
	FlagWashPair = "wash_pair"
	FlagVoteRing = "vote_ring"
)

// Flag statuses.
const (
	FlagOpen      = "open"
	FlagCleared   = "cleared"
	FlagConfirmed = "confirmed"
)

// IntegrityFlag is a suspicion about one person, raised with Flag or by the
// detectors. Raising one that is already open for the same person, kind and
// counterparty does nothing.
type IntegrityFlag struct {
	UserID         uuid.UUID
	Kind           string
	CounterpartyID uuid.UUID // uuid.Nil: none
	Metrics        map[string]string
}

// Detector thresholds.
const (
	washMinTrades    = 10
	washShare        = 0.30 // the pair's volume ≥ 30% of either side's volume
	washMirrorTol    = 0.20 // one side's gain ≈ the other's loss, within 20%
	voteRingMinVotes = 20
	voteRingShare    = 0.50
	detectWindowDays = 30
)

// washMirrorMin is the smallest gain (in the app's units) the mirror test looks at.
var washMirrorMin = decimal.NewFromInt(10)

// PairStat is two people's dealings with each other over the window: how
// many trades they made with each other, the pair's volume and each side's
// gain on those trades (negative = a loss). A and B are distinct.
type PairStat struct {
	A, B         uuid.UUID
	Trades       int
	Volume       decimal.Decimal
	GainA, GainB decimal.Decimal
}

// PairSource lists every pair of people with dealings since a time (the
// app's trades with both sides). Plug one in with SetPairSource.
type PairSource func(ctx context.Context, q db.Querier, since time.Time) ([]PairStat, error)

// Vote is one upvote: who gave it and whose content got it.
type Vote struct{ VoterID, AuthorID uuid.UUID }

// VoteSource lists upvotes since a time. Plug one in with SetVoteSource.
type VoteSource func(ctx context.Context, q db.Querier, since time.Time) ([]Vote, error)

func mirrored(a, b decimal.Decimal) bool {
	gain, loss := a, b
	if gain.LessThan(loss) {
		gain, loss = loss, gain
	}
	if !gain.IsPositive() || !loss.IsNegative() || gain.LessThan(washMirrorMin) {
		return false
	}
	big := decimal.Max(gain, loss.Abs())
	return gain.Add(loss).Abs().LessThanOrEqual(big.Mul(decimal.NewFromFloat(washMirrorTol)))
}

func pct(part, whole decimal.Decimal) string {
	if !whole.IsPositive() {
		return "0.0"
	}
	return part.Div(whole).Mul(decimal.NewFromInt(100)).StringFixed(1)
}

// DetectWashPairs flags both sides of every pair with ≥ 10 trades whose
// volume is ≥ 30% of either side's total volume, or where one side's gain
// mirrors the other's loss within 20%. volume is each person's total volume
// over the same window.
func DetectWashPairs(pairs []PairStat, volume map[uuid.UUID]decimal.Decimal) []IntegrityFlag {
	var out []IntegrityFlag
	share := decimal.NewFromFloat(washShare)
	for _, p := range pairs {
		if p.Trades < washMinTrades || p.A == p.B || p.A == uuid.Nil || p.B == uuid.Nil {
			continue
		}
		va, vb := volume[p.A], volume[p.B]
		heavy := (va.IsPositive() && p.Volume.GreaterThanOrEqual(va.Mul(share))) ||
			(vb.IsPositive() && p.Volume.GreaterThanOrEqual(vb.Mul(share)))
		mirror := mirrored(p.GainA, p.GainB)
		if !heavy && !mirror {
			continue
		}
		for _, side := range [][2]uuid.UUID{{p.A, p.B}, {p.B, p.A}} {
			u, c := side[0], side[1]
			gu, gc := p.GainA, p.GainB
			if u == p.B {
				gu, gc = gc, gu
			}
			out = append(out, IntegrityFlag{UserID: u, Kind: FlagWashPair, CounterpartyID: c, Metrics: map[string]string{
				"trades_30d": strconv.Itoa(p.Trades), "pair_volume": p.Volume.StringFixed(2),
				"share_of_user_pct": pct(p.Volume, volume[u]), "share_of_counterparty_pct": pct(p.Volume, volume[c]),
				"gain_user": gu.StringFixed(2), "gain_counterparty": gc.StringFixed(2), "mirrored": strconv.FormatBool(mirror),
			}})
		}
	}
	return out
}

// DetectVoteRings flags each voter with ≥ 20 upvotes in the window of which
// ≥ 50% went to one author (the voter, with that author as counterparty).
// Self-votes are ignored.
func DetectVoteRings(votes []Vote) []IntegrityFlag {
	per := map[uuid.UUID]map[uuid.UUID]int{}
	for _, v := range votes {
		if v.VoterID == uuid.Nil || v.AuthorID == uuid.Nil || v.VoterID == v.AuthorID {
			continue
		}
		if per[v.VoterID] == nil {
			per[v.VoterID] = map[uuid.UUID]int{}
		}
		per[v.VoterID][v.AuthorID]++
	}
	voters := make([]uuid.UUID, 0, len(per))
	for v := range per {
		voters = append(voters, v)
	}
	sortIDs(voters)
	var out []IntegrityFlag
	for _, voter := range voters {
		authors := make([]uuid.UUID, 0, len(per[voter]))
		for a := range per[voter] {
			authors = append(authors, a)
		}
		sortIDs(authors)
		total, topN := 0, 0
		var top uuid.UUID
		for _, a := range authors {
			n := per[voter][a]
			total += n
			if n > topN {
				top, topN = a, n
			}
		}
		if total < voteRingMinVotes || float64(topN) < voteRingShare*float64(total) {
			continue
		}
		out = append(out, IntegrityFlag{UserID: voter, Kind: FlagVoteRing, CounterpartyID: top, Metrics: map[string]string{
			"upvotes_30d": strconv.Itoa(total), "to_counterparty": strconv.Itoa(topN),
			"share_pct": strconv.FormatFloat(100*float64(topN)/float64(total), 'f', 1, 64),
		}})
	}
	return out
}

func sortIDs(ids []uuid.UUID) {
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
}

// validKind: lowercase a–z, 0–9 and '_', 1–40 characters.
func validKind(k string) bool {
	if k == "" || len(k) > 40 {
		return false
	}
	for _, r := range k {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}
