package smart

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/darshan-rambhia/glint/internal/model"
)

// ParseATAAttributes parses ATA SMART attributes from the PVE API response format.
// Each entry is expected to have fields: id, name, value, worst, thresh, raw, flags, type, when_failed.
func ParseATAAttributes(data []map[string]interface{}) ([]model.SMARTAttribute, error) {
	attrs := make([]model.SMARTAttribute, 0, len(data))

	for i, entry := range data {
		attr, err := parseOneATA(entry)
		if err != nil {
			return nil, fmt.Errorf("attribute %d: %w", i, err)
		}
		attrs = append(attrs, attr)
	}

	return attrs, nil
}

func parseOneATA(entry map[string]interface{}) (model.SMARTAttribute, error) {
	var attr model.SMARTAttribute

	id, err := toInt64(entry["id"])
	if err != nil {
		return attr, fmt.Errorf("parsing id: %w", err)
	}
	attr.ID = int(id)

	if name, ok := entry["name"].(string); ok {
		attr.Name = name
	}

	if v, err := toInt64(entry["value"]); err == nil {
		attr.Value = v
	}
	if v, err := toInt64(entry["worst"]); err == nil {
		attr.Worst = v
	}
	if v, err := toInt64(entry["thresh"]); err == nil {
		attr.Threshold = v
	}

	rawStr, rawVal, err := parseRaw(entry["raw"])
	if err != nil {
		return attr, fmt.Errorf("parsing raw for attr %d: %w", attr.ID, err)
	}
	attr.RawString = rawStr
	attr.RawValue = rawVal

	return attr, nil
}

// parseRaw handles the raw field which can be a string (possibly with extra info)
// or a numeric value. Returns the original string representation and the parsed numeric value.
func parseRaw(v interface{}) (string, int64, error) {
	switch r := v.(type) {
	case string:
		return r, extractLeadingInt(r), nil
	case float64:
		return strconv.FormatInt(int64(r), 10), int64(r), nil
	case int64:
		return strconv.FormatInt(r, 10), r, nil
	case int:
		return strconv.Itoa(r), int64(r), nil
	case nil:
		return "0", 0, nil
	default:
		s := fmt.Sprintf("%v", r)
		return s, extractLeadingInt(s), nil
	}
}

// extractLeadingInt extracts the leading integer from a string like "40 (Min/Max 25/55)".
func extractLeadingInt(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}

	// Find the end of the leading numeric portion.
	end := 0
	for end < len(s) && (s[end] >= '0' && s[end] <= '9') {
		end++
	}
	if end == 0 {
		return 0
	}

	val, err := strconv.ParseInt(s[:end], 10, 64)
	if err != nil {
		return 0
	}
	return val
}

// toInt64 converts an interface{} (typically float64 from JSON) to int64.
func toInt64(v interface{}) (int64, error) {
	switch n := v.(type) {
	case float64:
		return int64(n), nil
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case string:
		return strconv.ParseInt(n, 10, 64)
	default:
		return 0, fmt.Errorf("unsupported type %T", v)
	}
}
