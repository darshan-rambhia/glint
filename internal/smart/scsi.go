package smart

import (
	"strconv"
	"strings"

	"github.com/darshan-rambhia/glint/internal/model"
)

// SCSI pseudo attribute IDs (high-range to avoid collision with ATA IDs 1-253).
const (
	SCSITemperature  = 300
	SCSIPowerOnHours = 301
)

// ParseSCSIText extracts metrics from smartctl -d scsi text output.
//
// SCSI (and SAT-translated SATA behind an HBA) output uses prose labels rather
// than a structured table.  We scan for the most useful lines:
//
//	"Current Drive Temperature:     32 C"
//	"Number of hours powered up = 12345.23"
//	"Accumulated power on time, hours:minutes 21867:04"
func ParseSCSIText(text string) []model.SMARTAttribute {
	var attrs []model.SMARTAttribute

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		lower := strings.ToLower(line)

		// Temperature: "Current Drive Temperature:     32 C"
		if strings.HasPrefix(lower, "current drive temperature:") {
			if t, ok := extractIntAfterColon(line); ok {
				attrs = append(attrs, model.SMARTAttribute{
					ID:        SCSITemperature,
					Name:      "Temperature",
					RawValue:  int64(t),
					RawString: strconv.Itoa(t),
				})
			}
			continue
		}

		// Power-on hours: "Number of hours powered up = 12345.23"
		if strings.HasPrefix(lower, "number of hours powered up") {
			if idx := strings.Index(line, "="); idx >= 0 {
				val := strings.TrimSpace(line[idx+1:])
				// May be a float like "12345.23" — truncate to whole hours.
				if dotIdx := strings.Index(val, "."); dotIdx >= 0 {
					val = val[:dotIdx]
				}
				if h, err := strconv.Atoi(strings.ReplaceAll(val, ",", "")); err == nil {
					attrs = append(attrs, model.SMARTAttribute{
						ID:        SCSIPowerOnHours,
						Name:      "Power On Hours",
						RawValue:  int64(h),
						RawString: strconv.Itoa(h),
					})
				}
			}
			continue
		}

		// Power-on hours (log-page format):
		// "Accumulated power on time, hours:minutes 21867:04"
		if strings.HasPrefix(lower, "accumulated power on time, hours:minutes") {
			parts := strings.Fields(line)
			// Last field should be "HH:MM"
			if len(parts) > 0 {
				hm := parts[len(parts)-1]
				if colonIdx := strings.Index(hm, ":"); colonIdx >= 0 {
					if h, err := strconv.Atoi(hm[:colonIdx]); err == nil {
						attrs = append(attrs, model.SMARTAttribute{
							ID:        SCSIPowerOnHours,
							Name:      "Power On Hours",
							RawValue:  int64(h),
							RawString: strconv.Itoa(h),
						})
					}
				}
			}
			continue
		}
	}

	return attrs
}

// extractIntAfterColon parses the first integer after the last colon in s.
// "Current Drive Temperature:     32 C" → 32, true
func extractIntAfterColon(s string) (int, bool) {
	idx := strings.LastIndex(s, ":")
	if idx < 0 {
		return 0, false
	}
	val := strings.TrimSpace(s[idx+1:])
	// Take leading numeric part only (may be followed by " C", " F", etc.)
	end := 0
	for end < len(val) && (val[end] >= '0' && val[end] <= '9') {
		end++
	}
	if end == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(val[:end])
	return n, err == nil
}
