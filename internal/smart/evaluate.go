package smart

import (
	"github.com/darshan-rambhia/glint/internal/model"
)

// EvaluateAttribute assesses a single SMART attribute and sets its Status and FailureRate.
// It returns the resulting status bitfield value.
func EvaluateAttribute(attr *model.SMARTAttribute, protocol string) int {
	// Step 1: Check manufacturer threshold (raw value >= threshold means SMART failure).
	// Threshold of 0 means "always passing" per ATA spec, so skip it.
	if attr.Threshold > 0 && attr.Value > 0 && attr.Value <= attr.Threshold {
		attr.Status = model.StatusFailedSmart
		return model.StatusFailedSmart
	}

	// Step 2: Look up Backblaze-derived thresholds.
	thresh, ok := LookupThreshold(attr.ID)
	if !ok {
		// No statistical data for this attribute.
		attr.Status = model.StatusPassed
		return model.StatusPassed
	}

	// Step 3: Find which bucket the raw value falls into.
	bucket := FindBucket(thresh, attr.RawValue)
	critical := IsCritical(attr.ID)

	if bucket != nil {
		rate := bucket.AnnualFailureRate
		attr.FailureRate = &rate

		// Step 4: Apply Scrutiny-style rules.
		if critical {
			if rate >= 0.10 {
				attr.Status = model.StatusFailedScrutiny
				return model.StatusFailedScrutiny
			}
		} else {
			if rate >= 0.20 {
				attr.Status = model.StatusFailedScrutiny
				return model.StatusFailedScrutiny
			}
			if rate >= 0.10 {
				attr.Status = model.StatusWarnScrutiny
				return model.StatusWarnScrutiny
			}
		}
	} else if critical {
		// Critical attribute with no matching bucket -> warn.
		attr.Status = model.StatusWarnScrutiny
		return model.StatusWarnScrutiny
	}

	attr.Status = model.StatusPassed
	return model.StatusPassed
}

// EvaluateDisk evaluates all SMART attributes on a disk and sets the disk's
// aggregate Status as the bitwise OR of all attribute statuses.
// Returns the aggregate status.
func EvaluateDisk(disk *model.Disk) int {
	status := model.StatusPassed
	for i := range disk.Attributes {
		s := EvaluateAttribute(&disk.Attributes[i], disk.Protocol)
		status |= s
	}
	disk.Status = status
	return status
}
