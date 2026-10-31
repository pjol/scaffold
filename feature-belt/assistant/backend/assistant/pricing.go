package assistant

import (
	"strconv"
	"strings"
)

// Estimated cost per turn, from list prices in USD per million tokens. It is
// an estimate for quotas and the admin report, never a bill: providers price
// long prompts, batch and regional processing differently. Unknown models
// record no cost. Add a row when you configure another model.
type price struct {
	input, output, cacheRead, cacheWrite float64
}

var prices = map[string]price{
	// Anthropic list prices (cache writes at the 5-minute rate, 1.25x input).
	"claude-opus-5-5":   {input: 4, output: 20, cacheRead: 0.20, cacheWrite: 5},
	"claude-sonnet-5-5": {input: 2, output: 10, cacheRead: 0.20, cacheWrite: 2.50},
	"claude-haiku-5-5":  {input: 0.10, output: 0.50, cacheRead: 0.01, cacheWrite: 0.125},
	"claude-fable-5-1":  {input: 10, output: 50, cacheRead: 0.25, cacheWrite: 12.50},
	"claude-opus-5":     {input: 5, output: 25, cacheRead: 0.50, cacheWrite: 6.25},
	"claude-sonnet-5":   {input: 2, output: 10, cacheRead: 0.20, cacheWrite: 2.50},
	// OpenAI (cached input assumed at a tenth of the input price).
	"gpt-6-sol":   {input: 2, output: 10, cacheRead: 0.20, cacheWrite: 2},
	"gpt-6.1-sol": {input: 2, output: 10, cacheRead: 0.20, cacheWrite: 2},
	"gpt-6-luna":  {input: 0.10, output: 0.50, cacheRead: 0.01, cacheWrite: 0.10},
	"gpt-6-astra": {input: 10, output: 50, cacheRead: 1, cacheWrite: 10},
	"mock":        {},
}

// estimateCost returns the USD estimate and whether the model is priced.
func estimateCost(model string, u Usage) (float64, bool) {
	p, ok := prices[strings.ToLower(model)]
	if !ok {
		return 0, false
	}
	return (float64(u.InputTokens)*p.input + float64(u.OutputTokens)*p.output +
		float64(u.CacheReadTokens)*p.cacheRead + float64(u.CacheWriteTokens)*p.cacheWrite) / 1e6, true
}

// formatCost renders a cost as a decimal string (money never travels as a
// float on the wire).
func formatCost(c float64) string { return strconv.FormatFloat(c, 'f', 6, 64) }
