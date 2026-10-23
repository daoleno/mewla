package lifecycle

import "time"

// Protocol timing constants. Fixed so lifecycle decisions are deterministic.
const (
	LeaseGrace    = 10 * time.Minute
	LostGrace     = 30 * time.Minute
	EventClaimTTL = 2 * time.Minute
)
