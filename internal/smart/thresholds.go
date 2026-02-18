package smart

// Bucket defines a failure rate range for a given raw value interval.
// Low is inclusive, High is exclusive (except for the last bucket where High is MaxInt).
type Bucket struct {
	Low               int64
	High              int64
	AnnualFailureRate float64
}

// AttrThreshold holds the Backblaze-derived failure buckets for one SMART attribute.
type AttrThreshold struct {
	ID      int
	Name    string
	Buckets []Bucket
}

// thresholdTable maps ATA attribute IDs to their failure-rate buckets.
// Data derived from Backblaze hard drive statistics reports.
var thresholdTable = map[int]AttrThreshold{
	// --- Critical attributes ---

	5: {
		ID: 5, Name: "Reallocated Sectors Count",
		Buckets: []Bucket{
			{Low: 0, High: 0, AnnualFailureRate: 0.025},
			{Low: 1, High: 4, AnnualFailureRate: 0.027},
			{Low: 4, High: 16, AnnualFailureRate: 0.075},
			{Low: 16, High: 70, AnnualFailureRate: 0.236},
			{Low: 70, High: 1<<62 - 1, AnnualFailureRate: 0.50},
		},
	},
	10: {
		ID: 10, Name: "Spin Retry Count",
		Buckets: []Bucket{
			{Low: 0, High: 0, AnnualFailureRate: 0.025},
			{Low: 1, High: 3, AnnualFailureRate: 0.15},
			{Low: 3, High: 1<<62 - 1, AnnualFailureRate: 0.35},
		},
	},
	187: {
		ID: 187, Name: "Reported Uncorrectable Errors",
		Buckets: []Bucket{
			{Low: 0, High: 0, AnnualFailureRate: 0.015},
			{Low: 1, High: 10, AnnualFailureRate: 0.05},
			{Low: 10, High: 50, AnnualFailureRate: 0.15},
			{Low: 50, High: 1<<62 - 1, AnnualFailureRate: 0.40},
		},
	},
	188: {
		ID: 188, Name: "Command Timeout",
		Buckets: []Bucket{
			{Low: 0, High: 0, AnnualFailureRate: 0.015},
			{Low: 1, High: 100, AnnualFailureRate: 0.03},
			{Low: 100, High: 1000, AnnualFailureRate: 0.08},
			{Low: 1000, High: 1<<62 - 1, AnnualFailureRate: 0.20},
		},
	},
	196: {
		ID: 196, Name: "Reallocate Event Count",
		Buckets: []Bucket{
			{Low: 0, High: 0, AnnualFailureRate: 0.025},
			{Low: 1, High: 5, AnnualFailureRate: 0.05},
			{Low: 5, High: 1<<62 - 1, AnnualFailureRate: 0.25},
		},
	},
	197: {
		ID: 197, Name: "Current Pending Sector Count",
		Buckets: []Bucket{
			{Low: 0, High: 0, AnnualFailureRate: 0.025},
			{Low: 1, High: 5, AnnualFailureRate: 0.10},
			{Low: 5, High: 1<<62 - 1, AnnualFailureRate: 0.35},
		},
	},
	198: {
		ID: 198, Name: "Offline Uncorrectable Sector Count",
		Buckets: []Bucket{
			{Low: 0, High: 0, AnnualFailureRate: 0.025},
			{Low: 1, High: 5, AnnualFailureRate: 0.10},
			{Low: 5, High: 1<<62 - 1, AnnualFailureRate: 0.35},
		},
	},

	// --- Important (non-critical) attributes ---

	1: {
		ID: 1, Name: "Read Error Rate",
		Buckets: []Bucket{
			{Low: 0, High: 0, AnnualFailureRate: 0.02},
			{Low: 1, High: 1000, AnnualFailureRate: 0.03},
			{Low: 1000, High: 100000, AnnualFailureRate: 0.08},
			{Low: 100000, High: 1<<62 - 1, AnnualFailureRate: 0.15},
		},
	},
	9: {
		ID: 9, Name: "Power-On Hours",
		Buckets: []Bucket{
			{Low: 0, High: 10000, AnnualFailureRate: 0.02},
			{Low: 10000, High: 20000, AnnualFailureRate: 0.025},
			{Low: 20000, High: 40000, AnnualFailureRate: 0.03},
			{Low: 40000, High: 1<<62 - 1, AnnualFailureRate: 0.06},
		},
	},
	194: {
		ID: 194, Name: "Temperature",
		Buckets: []Bucket{
			{Low: 0, High: 35, AnnualFailureRate: 0.02},
			{Low: 35, High: 45, AnnualFailureRate: 0.025},
			{Low: 45, High: 55, AnnualFailureRate: 0.05},
			{Low: 55, High: 1<<62 - 1, AnnualFailureRate: 0.12},
		},
	},
	199: {
		ID: 199, Name: "UDMA CRC Error Count",
		Buckets: []Bucket{
			{Low: 0, High: 0, AnnualFailureRate: 0.025},
			{Low: 1, High: 100, AnnualFailureRate: 0.03},
			{Low: 100, High: 1<<62 - 1, AnnualFailureRate: 0.10},
		},
	},
	200: {
		ID: 200, Name: "Multi-Zone Error Rate",
		Buckets: []Bucket{
			{Low: 0, High: 0, AnnualFailureRate: 0.02},
			{Low: 1, High: 100, AnnualFailureRate: 0.05},
			{Low: 100, High: 1<<62 - 1, AnnualFailureRate: 0.15},
		},
	},
}

// criticalAttributes is the set of SMART attribute IDs considered critical
// for disk health assessment.
var criticalAttributes = map[int]bool{
	5:   true,
	10:  true,
	187: true,
	188: true,
	196: true,
	197: true,
	198: true,
}

// IsCritical reports whether the given SMART attribute ID is considered critical.
func IsCritical(id int) bool {
	return criticalAttributes[id]
}

// LookupThreshold returns the threshold entry for the given attribute ID, if one exists.
func LookupThreshold(id int) (AttrThreshold, bool) {
	t, ok := thresholdTable[id]
	return t, ok
}

// FindBucket returns the matching bucket for a raw value, or nil if none match.
func FindBucket(t AttrThreshold, raw int64) *Bucket {
	for i := range t.Buckets {
		b := &t.Buckets[i]
		if raw >= b.Low && raw <= b.High {
			return b
		}
	}
	return nil
}
