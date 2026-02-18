package smart

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const nvmeSmartctlOutput = `=== START OF SMART DATA SECTION ===
SMART/Health Information (NVMe Log 0x02)
Critical Warning:                   0x00
Temperature:                        35 Celsius
Available Spare:                    100%
Available Spare Threshold:          10%
Percentage Used:                    2%
Data Units Read:                    12,345,678 [6.32 TB]
Data Units Written:                 9,876,543 [5.05 TB]
Host Read Commands:                 234,567,890
Host Write Commands:                123,456,789
Controller Busy Time:               1,234
Power Cycles:                       42
Power On Hours:                     8,760
Unsafe Shutdowns:                   5
Media and Data Integrity Errors:    0
Error Information Log Entries:      3
Warning  Comp. Temperature Time:    0
Critical Comp. Temperature Time:    0
`

func TestParseNVMeText(t *testing.T) {
	attrs, err := ParseNVMeText(nvmeSmartctlOutput)
	require.NoError(t, err)
	assert.Len(t, attrs, 10)

	// Build a map for easier lookup.
	byID := make(map[int]int64)
	for _, a := range attrs {
		byID[a.ID] = a.RawValue
	}

	assert.Equal(t, int64(0), byID[NVMeCriticalWarning])
	assert.Equal(t, int64(35), byID[NVMeTemperature])
	assert.Equal(t, int64(100), byID[NVMeAvailableSpare])
	assert.Equal(t, int64(10), byID[NVMeAvailableSpareThresh])
	assert.Equal(t, int64(2), byID[NVMePercentageUsed])
	assert.Equal(t, int64(12345678), byID[NVMeDataUnitsRead])
	assert.Equal(t, int64(9876543), byID[NVMeDataUnitsWritten])
	assert.Equal(t, int64(8760), byID[NVMePowerOnHours])
	assert.Equal(t, int64(0), byID[NVMeMediaErrors])
	assert.Equal(t, int64(3), byID[NVMeNumErrLogEntries])
}

func TestParseNVMeText_HexCriticalWarning(t *testing.T) {
	text := `Critical Warning:                   0x0004
Temperature:                        40 Celsius
Available Spare:                    80%
Available Spare Threshold:          10%
Percentage Used:                    15%
Data Units Read:                    500
Data Units Written:                 1000
Power On Hours:                     2000
Media and Data Integrity Errors:    5
Error Information Log Entries:      10
`
	attrs, err := ParseNVMeText(text)
	require.NoError(t, err)

	byID := make(map[int]int64)
	for _, a := range attrs {
		byID[a.ID] = a.RawValue
	}

	assert.Equal(t, int64(4), byID[NVMeCriticalWarning])
	assert.Equal(t, int64(40), byID[NVMeTemperature])
	assert.Equal(t, int64(5), byID[NVMeMediaErrors])
}

func TestParseNVMeText_Empty(t *testing.T) {
	_, err := ParseNVMeText("")
	assert.Error(t, err)
}

func TestParseNVMeText_NoRelevantFields(t *testing.T) {
	text := `Some irrelevant output
that has no SMART fields
at all.
`
	_, err := ParseNVMeText(text)
	assert.Error(t, err)
}

func TestParseNVMeValue(t *testing.T) {
	tests := []struct {
		input   string
		wantVal int64
	}{
		{"35 Celsius", 35},
		{"100%", 100},
		{"0x0004", 4},
		{"12,345,678 [6.32 TB]", 12345678},
		{"0", 0},
		{"", 0},
		{"abc", 0},
		{"0xZZZZ", 0},                        // invalid hex -> 0
		{"99999999999999999999 overflow", 0}, // int64 overflow -> 0
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			val, _ := parseNVMeValue(tt.input)
			assert.Equal(t, tt.wantVal, val)
		})
	}
}
