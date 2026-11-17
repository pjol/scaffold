package engine

// SyncMarkets applies the catalog: new markets are listed, existing ones take
// the new parameters and status (resting orders keep their prices), and a
// manual price set or cleared by an admin takes effect.
func (e *Engine) SyncMarkets(specs []MarketSpec) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range specs {
		if s.Symbol == "" || !s.TickSize.IsPositive() {
			continue
		}
		m := e.markets[s.Symbol]
		if m == nil {
			m = e.addMarket(s)
			e.log.Info("exchange: market listed", "symbol", s.Symbol)
			continue
		}
		before := m.spec.Status
		e.applySpec(m, s)
		e.updateMark(m)
		if before != m.spec.Status {
			e.log.Info("exchange: market status", "symbol", s.Symbol, "status", m.spec.Status)
		}
	}
	e.sortSymbols()
}

// FeedKeys maps each price feed key to the symbols that read it.
func (e *Engine) FeedKeys() map[string][]string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := map[string][]string{}
	for _, sym := range e.symbols {
		k := e.markets[sym].spec.FeedKey()
		out[k] = append(out[k], sym)
	}
	return out
}
