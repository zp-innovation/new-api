package workbuddy

// Token quota windows are per (account x model): the upstream never publishes
// the budget, it only reveals the reset clock once the window is spent. The
// published figures below are the seeds the console falls back to before an
// account has ever hit its cap, taken from the reference gateway's measured
// samples (179.7M~201.0M for deepseek-v4.1-flash, 193.0M for the -sg variant,
// 20.5M~22.1M for hy4-preview-f). Once a cap event exists the measured sample
// replaces the seed, so a seed is only ever a starting estimate.
const defaultTokenBudget = int64(200_000_000)

var seededTokenBudgets = map[string]int64{
	"deepseek-v4.1-flash":    defaultTokenBudget,
	"deepseek-v4.1-flash-sg": defaultTokenBudget,
	"hy4-preview-f":          20_000_000,
}

// SeededTokenBudget returns the published starting budget for a model, if one
// is known. Models outside the map have no budget estimate at all, and the
// console shows their absolute usage without a percentage.
func SeededTokenBudget(model string) (int64, bool) {
	budget, ok := seededTokenBudgets[model]
	return budget, ok
}
