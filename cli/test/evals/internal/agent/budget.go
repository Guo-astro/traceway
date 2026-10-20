package agent

import (
	"fmt"
	"sync"
)

// Ledger tracks spending across every run of a dispatch (agents and judge)
// against the --max-cost-usd ceiling. It is checked before each run starts
// and after each model response.
type Ledger struct {
	mu      sync.Mutex
	Cap     float64
	spent   float64
	byModel map[string]float64
}

// NewLedger returns a ledger with a ceiling. Cap <= 0 means unlimited, which
// only the fake model uses.
func NewLedger(cap float64) *Ledger { return &Ledger{Cap: cap, byModel: map[string]float64{}} }

// Add records spending.
func (l *Ledger) Add(model string, usd float64) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.spent += usd
	l.byModel[model] += usd
}

// Spent returns the total.
func (l *Ledger) Spent() float64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.spent
}

// ByModel returns spending per model.
func (l *Ledger) ByModel() map[string]float64 {
	out := map[string]float64{}
	if l == nil {
		return out
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, v := range l.byModel {
		out[k] = v
	}
	return out
}

// Remaining returns what is left under the cap (+Inf semantics when no cap).
func (l *Ledger) Remaining() float64 {
	if l == nil || l.Cap <= 0 {
		return 1e18
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Cap - l.spent
}

// Exhausted reports whether the ceiling is reached.
func (l *Ledger) Exhausted() bool { return l.Remaining() <= 0 }

// String summarizes the ledger.
func (l *Ledger) String() string {
	if l == nil {
		return "no ledger"
	}
	return fmt.Sprintf("$%.4f of $%.2f", l.Spent(), l.Cap)
}
