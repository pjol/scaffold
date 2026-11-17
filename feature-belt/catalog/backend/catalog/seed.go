package catalog

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// devdata/elements.ndjson is 50 chemical elements in the source format:
// symbols, Latin names and other spellings as aliases (Fe, Ferrum; Aluminum;
// Wolfram), the element category, atomic number, standard atomic weight,
// group, period, block and state at 20 °C, and approximate reference prices
// for five metals (development data, not quotes). Point CATALOG_SOURCE_FILE at
// it to try syncs in development.
//
//go:embed devdata/elements.ndjson
var devItems []byte

type seedSource struct{}

func (seedSource) Name() string { return "seed" }

func (seedSource) Fetch(ctx context.Context, emit func(Item) error) error {
	return DecodeItems(bytes.NewReader(devItems), emit, skipTo(ctx))
}

// SeedDev fills an empty catalog with the development elements through a
// sync run (so Admin → Catalog shows one). A catalog with items is left alone.
func (s *Service) SeedDev(ctx context.Context, _ []uuid.UUID) error {
	var any bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM catalog_items)`).Scan(&any); err != nil || any {
		return err
	}
	run, err := s.startRun(ctx, s.d.Pool, TriggerSeed, "seed", false)
	if errors.Is(err, errSyncRunning) {
		return nil
	}
	if err != nil {
		return err
	}
	run, err = s.execute(ctx, run, seedSource{}, false)
	if err != nil {
		return err
	}
	if run.Status != RunOK {
		return fmt.Errorf("catalog seed: %s", run.Error)
	}
	return nil
}
