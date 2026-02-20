package smart

import (
	"encoding/json"
	"testing"

	"github.com/darshan-rambhia/glint/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestEvaluateAttribute(t *testing.T) {
	tests := []struct {
		name       string
		attr       model.SMARTAttribute
		wantStatus int
		wantRate   *float64
	}{
		{
			name:       "critical attr ID 5, raw 0 -> passed",
			attr:       model.SMARTAttribute{ID: 5, Value: 100, Threshold: 10, RawValue: 0},
			wantStatus: model.StatusPassed,
			wantRate:   new(0.025),
		},
		{
			name:       "critical attr ID 5, raw 20 -> failed scrutiny (23.6%)",
			attr:       model.SMARTAttribute{ID: 5, Value: 100, Threshold: 10, RawValue: 20},
			wantStatus: model.StatusFailedScrutiny,
			wantRate:   new(0.236),
		},
		{
			name:       "critical attr ID 5, raw 100 -> failed scrutiny (50%)",
			attr:       model.SMARTAttribute{ID: 5, Value: 100, Threshold: 10, RawValue: 100},
			wantStatus: model.StatusFailedScrutiny,
			wantRate:   new(0.50),
		},
		{
			name:       "critical attr ID 197, raw 2 -> failed scrutiny (10%)",
			attr:       model.SMARTAttribute{ID: 197, Value: 100, Threshold: 0, RawValue: 2},
			wantStatus: model.StatusFailedScrutiny,
			wantRate:   new(0.10),
		},
		{
			name:       "critical attr ID 187, raw 5 -> passed (5% < 10%)",
			attr:       model.SMARTAttribute{ID: 187, Value: 100, Threshold: 0, RawValue: 5},
			wantStatus: model.StatusPassed,
			wantRate:   new(0.05),
		},
		{
			name:       "non-critical attr ID 1, raw 0 -> passed",
			attr:       model.SMARTAttribute{ID: 1, Value: 100, Threshold: 0, RawValue: 0},
			wantStatus: model.StatusPassed,
			wantRate:   new(0.02),
		},
		{
			name:       "non-critical attr ID 200, raw 200 -> warn scrutiny (15%)",
			attr:       model.SMARTAttribute{ID: 200, Value: 100, Threshold: 0, RawValue: 200},
			wantStatus: model.StatusWarnScrutiny,
			wantRate:   new(0.15),
		},
		{
			name:       "non-critical attr ID 194, raw 60 -> warn scrutiny (12%)",
			attr:       model.SMARTAttribute{ID: 194, Value: 100, Threshold: 0, RawValue: 60},
			wantStatus: model.StatusWarnScrutiny,
			wantRate:   new(0.12),
		},
		{
			name:       "no thresholds for unknown attr -> passed",
			attr:       model.SMARTAttribute{ID: 999, Value: 100, Threshold: 0, RawValue: 42},
			wantStatus: model.StatusPassed,
			wantRate:   nil,
		},
		{
			name:       "manufacturer threshold failure (value <= thresh)",
			attr:       model.SMARTAttribute{ID: 5, Value: 5, Threshold: 10, RawValue: 0},
			wantStatus: model.StatusFailedSmart,
			wantRate:   nil,
		},
		{
			name:       "empty attribute (all zeros) -> passed",
			attr:       model.SMARTAttribute{},
			wantStatus: model.StatusPassed,
			wantRate:   nil,
		},
		{
			name:       "critical attr ID 10, raw 1 -> failed scrutiny (15%)",
			attr:       model.SMARTAttribute{ID: 10, Value: 100, Threshold: 0, RawValue: 1},
			wantStatus: model.StatusFailedScrutiny,
			wantRate:   new(0.15),
		},
		{
			name:       "non-critical attr ID 199, raw 50 -> passed (3% < 10%)",
			attr:       model.SMARTAttribute{ID: 199, Value: 100, Threshold: 0, RawValue: 50},
			wantStatus: model.StatusPassed,
			wantRate:   new(0.03),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attr := tt.attr
			got := EvaluateAttribute(&attr, "ata")
			assert.Equal(t, tt.wantStatus, got, "status mismatch")
			assert.Equal(t, tt.wantStatus, attr.Status, "attr.Status mismatch")
			if tt.wantRate == nil {
				assert.Nil(t, attr.FailureRate)
			} else {
				assert.NotNil(t, attr.FailureRate)
				assert.InDelta(t, *tt.wantRate, *attr.FailureRate, 0.001)
			}
		})
	}
}

func TestFindBucket_BeyondAllRanges(t *testing.T) {
	// Use an attribute with buckets that have a gap (construct manually)
	thresh := AttrThreshold{
		ID:   999,
		Name: "Test",
		Buckets: []Bucket{
			{Low: 0, High: 10, AnnualFailureRate: 0.01},
			{Low: 20, High: 30, AnnualFailureRate: 0.05},
		},
	}
	// Value 15 falls between buckets — no match
	bucket := FindBucket(thresh, 15)
	assert.Nil(t, bucket)

	// Value 50 beyond all buckets
	bucket = FindBucket(thresh, 50)
	assert.Nil(t, bucket)
}

func TestEvaluateAttribute_CriticalNoBucket(t *testing.T) {
	// Critical attribute with a raw value that doesn't match any bucket -> warn
	// All critical attrs have buckets covering 0 to 1<<62-1, so we need a negative raw.
	// Since raw is int64, we can use a negative value to miss all buckets.
	attr := model.SMARTAttribute{
		ID:        5,
		Value:     100,
		Threshold: 0,
		RawValue:  -1,
	}
	status := EvaluateAttribute(&attr, "ata")
	assert.Equal(t, model.StatusWarnScrutiny, status)
}

func TestEvaluateAttribute_ThresholdNegativeOne(t *testing.T) {
	// Threshold of -1 means no manufacturer threshold (< 0 skips the SMART fail check,
	// since the condition is attr.Threshold > 0)
	attr := model.SMARTAttribute{
		ID:        5,
		Value:     100,
		Threshold: -1,
		RawValue:  0,
	}
	status := EvaluateAttribute(&attr, "ata")
	// Should proceed to Backblaze lookup, not trigger SMART failure
	assert.NotEqual(t, model.StatusFailedSmart, status)
	assert.Equal(t, model.StatusPassed, status)
}

func TestEvaluateDisk(t *testing.T) {
	disk := &model.Disk{
		Protocol: "ata",
		Attributes: []model.SMARTAttribute{
			{ID: 1, Value: 100, Threshold: 0, RawValue: 0},    // passed
			{ID: 5, Value: 100, Threshold: 10, RawValue: 0},   // passed
			{ID: 197, Value: 100, Threshold: 0, RawValue: 10}, // failed scrutiny
		},
	}

	status := EvaluateDisk(disk)
	assert.Equal(t, model.StatusFailedScrutiny, status)
	assert.Equal(t, model.StatusFailedScrutiny, disk.Status)
}

func TestEvaluateDisk_AllHealthy(t *testing.T) {
	disk := &model.Disk{
		Protocol: "ata",
		Attributes: []model.SMARTAttribute{
			{ID: 1, Value: 100, Threshold: 0, RawValue: 0},
			{ID: 5, Value: 100, Threshold: 10, RawValue: 0},
			{ID: 9, Value: 100, Threshold: 0, RawValue: 5000},
		},
	}

	status := EvaluateDisk(disk)
	assert.Equal(t, model.StatusPassed, status)
}

func TestEvaluateDisk_Empty(t *testing.T) {
	disk := &model.Disk{Protocol: "ata"}
	status := EvaluateDisk(disk)
	assert.Equal(t, model.StatusPassed, status)
}

func TestEvaluateDisk_MixedStatuses(t *testing.T) {
	disk := &model.Disk{
		Protocol: "ata",
		Attributes: []model.SMARTAttribute{
			{ID: 5, Value: 5, Threshold: 10, RawValue: 100},   // SMART fail
			{ID: 194, Value: 100, Threshold: 0, RawValue: 60}, // warn scrutiny
		},
	}

	status := EvaluateDisk(disk)
	assert.Equal(t, model.StatusFailedSmart|model.StatusWarnScrutiny, status)
}

func TestEvaluateAttribute_NonCriticalFailedScrutiny(t *testing.T) {
	// Temporarily add a non-critical threshold with high failure rate to cover
	// the rate >= 0.20 branch for non-critical attributes.
	thresholdTable[9999] = AttrThreshold{
		ID: 9999, Name: "Test Non-Critical",
		Buckets: []Bucket{
			{Low: 0, High: 100, AnnualFailureRate: 0.25},
		},
	}
	defer delete(thresholdTable, 9999)

	attr := model.SMARTAttribute{ID: 9999, Value: 100, Threshold: 0, RawValue: 50}
	status := EvaluateAttribute(&attr, "ata")
	assert.Equal(t, model.StatusFailedScrutiny, status)
	assert.NotNil(t, attr.FailureRate)
	assert.InDelta(t, 0.25, *attr.FailureRate, 0.001)
}

func BenchmarkEvaluateAttribute(b *testing.B) {
	attrs := []model.SMARTAttribute{
		{ID: 5, Value: 100, Threshold: 10, RawValue: 0},
		{ID: 197, Value: 100, Threshold: 0, RawValue: 0},
		{ID: 194, Value: 68, Threshold: 0, RawValue: 32},
		{ID: 187, Value: 100, Threshold: 0, RawValue: 0},
		{ID: 199, Value: 200, Threshold: 0, RawValue: 50},
	}
	b.ResetTimer()
	for b.Loop() {
		for i := range attrs {
			a := attrs[i]
			EvaluateAttribute(&a, "ata")
		}
	}
}

func FuzzParseATAAttributes(f *testing.F) {
	// String raw with all optional fields present.
	f.Add(`[{"id":5,"name":"Reallocated_Sector_Ct","value":100,"worst":100,"thresh":10,"raw":"0","flags":"0x0034"}]`)
	// Empty list.
	f.Add(`[]`)
	// float64 raw (JSON number) — minimal attribute.
	f.Add(`[{"id":1,"value":100,"worst":100,"thresh":0,"raw":0}]`)
	// String raw with trailing annotation — exercises extractLeadingInt("40 (Min/Max 25/55)").
	f.Add(`[{"id":194,"name":"Temperature_Celsius","value":68,"worst":55,"thresh":0,"raw":"40 (Min/Max 25/55)","flags":"-O---K","type":"old_age","when_failed":""}]`)
	// Float64 raw from JSON — exercises the float64 branch of parseRaw.
	f.Add(`[{"id":9,"name":"Power_On_Hours","value":97,"worst":97,"thresh":0,"raw":25000,"flags":"-O--CK","type":"old_age","when_failed":""}]`)
	// Null raw — exercises the nil branch of parseRaw.
	f.Add(`[{"id":1,"name":"Raw_Read_Error_Rate","value":100,"worst":100,"thresh":6,"raw":null}]`)
	// Missing id field — exercises the error return from toInt64.
	f.Add(`[{"name":"no_id","value":100,"raw":"0"}]`)
	// Two attributes with different raw types — exercises the full iteration path.
	f.Add(`[{"id":5,"name":"Reallocated_Sector_Ct","value":100,"worst":100,"thresh":10,"raw":"0"},{"id":197,"name":"Current_Pending_Sector","value":100,"worst":100,"thresh":0,"raw":2}]`)
	f.Fuzz(func(t *testing.T, s string) {
		var data []map[string]any
		if err := json.Unmarshal([]byte(s), &data); err != nil {
			return
		}
		attrs, err := ParseATAAttributes(data)
		if err != nil {
			return
		}
		// On success, every input entry must produce exactly one output attribute.
		if len(attrs) != len(data) {
			t.Fatalf("len(attrs)=%d != len(data)=%d", len(attrs), len(data))
		}
	})
}

func BenchmarkEvaluateDisk(b *testing.B) {
	disk := &model.Disk{
		Protocol: "ata",
		Attributes: []model.SMARTAttribute{
			{ID: 1, Value: 100, Threshold: 6, RawValue: 0},
			{ID: 5, Value: 100, Threshold: 10, RawValue: 0},
			{ID: 9, Value: 97, Threshold: 0, RawValue: 25000},
			{ID: 10, Value: 100, Threshold: 97, RawValue: 0},
			{ID: 187, Value: 100, Threshold: 0, RawValue: 0},
			{ID: 188, Value: 100, Threshold: 0, RawValue: 0},
			{ID: 194, Value: 68, Threshold: 0, RawValue: 32},
			{ID: 196, Value: 100, Threshold: 0, RawValue: 0},
			{ID: 197, Value: 100, Threshold: 0, RawValue: 0},
			{ID: 198, Value: 100, Threshold: 0, RawValue: 0},
			{ID: 199, Value: 200, Threshold: 0, RawValue: 0},
			{ID: 200, Value: 100, Threshold: 0, RawValue: 0},
		},
	}
	b.ResetTimer()
	for b.Loop() {
		EvaluateDisk(disk)
	}
}
