package smart

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/darshan-rambhia/glint/internal/model"
)

// NVMe pseudo attribute IDs, mapped from NVMe SMART field names.
const (
	NVMeCriticalWarning      = 1
	NVMeTemperature          = 2
	NVMeAvailableSpare       = 3
	NVMeAvailableSpareThresh = 4
	NVMePercentageUsed       = 5
	NVMeDataUnitsRead        = 6
	NVMeDataUnitsWritten     = 7
	NVMePowerOnHours         = 8
	NVMeMediaErrors          = 9
	NVMeNumErrLogEntries     = 10
)

// nvmeFieldMap maps lowercase smartctl field labels to pseudo attribute IDs and names.
var nvmeFieldMap = map[string]struct {
	ID   int
	Name string
}{
	"critical warning":                {NVMeCriticalWarning, "Critical Warning"},
	"temperature":                     {NVMeTemperature, "Temperature"},
	"available spare":                 {NVMeAvailableSpare, "Available Spare"},
	"available spare threshold":       {NVMeAvailableSpareThresh, "Available Spare Threshold"},
	"percentage used":                 {NVMePercentageUsed, "Percentage Used"},
	"data units read":                 {NVMeDataUnitsRead, "Data Units Read"},
	"data units written":              {NVMeDataUnitsWritten, "Data Units Written"},
	"power on hours":                  {NVMePowerOnHours, "Power On Hours"},
	"media and data integrity errors": {NVMeMediaErrors, "Media Errors"},
	"error information log entries":   {NVMeNumErrLogEntries, "Error Log Entries"},
}

// ParseNVMeText parses NVMe SMART data from smartctl text output.
// It extracts key-value pairs from lines like "Temperature:                        35 Celsius"
// and maps them to pseudo SMART attributes.
func ParseNVMeText(text string) ([]model.SMARTAttribute, error) {
	attrs := make([]model.SMARTAttribute, 0, len(nvmeFieldMap))

	lines := strings.SplitSeq(text, "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Split on first colon.
		before, after, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}

		key := strings.TrimSpace(before)
		valStr := strings.TrimSpace(after)

		keyLower := strings.ToLower(key)
		field, ok := nvmeFieldMap[keyLower]
		if !ok {
			continue
		}

		rawVal, rawStr := parseNVMeValue(valStr)
		attrs = append(attrs, model.SMARTAttribute{
			ID:        field.ID,
			Name:      field.Name,
			RawValue:  rawVal,
			RawString: rawStr,
		})
	}

	if len(attrs) == 0 {
		return nil, fmt.Errorf("no NVMe SMART attributes found in text")
	}

	return attrs, nil
}

// parseNVMeValue extracts the numeric value from a smartctl value string.
// Handles formats like "35 Celsius", "100%", "0x0000", "1,234,567", plain numbers.
func parseNVMeValue(s string) (int64, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ""
	}

	// Handle hex values.
	if after, ok := strings.CutPrefix(s, "0x"); ok {
		val, err := strconv.ParseInt(after, 16, 64)
		if err != nil {
			return 0, s
		}
		return val, s
	}

	// Strip commas for large numbers, then take leading numeric part.
	cleaned := strings.ReplaceAll(s, ",", "")

	// Extract leading numeric portion.
	end := 0
	for end < len(cleaned) && (cleaned[end] >= '0' && cleaned[end] <= '9') {
		end++
	}
	if end == 0 {
		return 0, s
	}

	val, err := strconv.ParseInt(cleaned[:end], 10, 64)
	if err != nil {
		return 0, s
	}

	return val, s
}
