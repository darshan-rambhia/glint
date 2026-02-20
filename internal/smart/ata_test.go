package smart

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseATAAttributes(t *testing.T) {
	tests := []struct {
		name     string
		data     []map[string]any
		wantLen  int
		wantID   int
		wantRaw  int64
		wantRawS string
		wantErr  bool
	}{
		{
			name: "simple numeric raw string",
			data: []map[string]any{
				{
					"id": float64(5), "name": "Reallocated_Sector_Ct",
					"value": float64(100), "worst": float64(100),
					"thresh": float64(10), "raw": "0",
					"flags": "PO--CK", "type": "old_age", "when_failed": "",
				},
			},
			wantLen: 1, wantID: 5, wantRaw: 0, wantRawS: "0",
		},
		{
			name: "raw with extra info (Min/Max)",
			data: []map[string]any{
				{
					"id": float64(194), "name": "Temperature_Celsius",
					"value": float64(68), "worst": float64(55),
					"thresh": float64(0), "raw": "40 (Min/Max 25/55)",
					"flags": "-O---K", "type": "old_age", "when_failed": "",
				},
			},
			wantLen: 1, wantID: 194, wantRaw: 40, wantRawS: "40 (Min/Max 25/55)",
		},
		{
			name: "raw as float64 (numeric JSON)",
			data: []map[string]any{
				{
					"id": float64(9), "name": "Power_On_Hours",
					"value": float64(97), "worst": float64(97),
					"thresh": float64(0), "raw": float64(25000),
					"flags": "-O--CK", "type": "old_age", "when_failed": "",
				},
			},
			wantLen: 1, wantID: 9, wantRaw: 25000, wantRawS: "25000",
		},
		{
			name: "nil raw value",
			data: []map[string]any{
				{
					"id": float64(1), "name": "Raw_Read_Error_Rate",
					"value": float64(100), "worst": float64(100),
					"thresh": float64(6), "raw": nil,
					"flags": "POSR-K", "type": "pre-fail", "when_failed": "",
				},
			},
			wantLen: 1, wantID: 1, wantRaw: 0, wantRawS: "0",
		},
		{
			name: "raw with only digits followed by whitespace",
			data: []map[string]any{
				{
					"id": float64(187), "name": "Reported_Uncorrect",
					"value": float64(100), "worst": float64(100),
					"thresh": float64(0), "raw": "3  ",
					"flags": "-O--CK", "type": "old_age", "when_failed": "",
				},
			},
			wantLen: 1, wantID: 187, wantRaw: 3, wantRawS: "3  ",
		},
		{
			name:    "missing id field",
			data:    []map[string]any{{"name": "no_id"}},
			wantErr: true,
		},
		{
			name: "multiple attributes",
			data: []map[string]any{
				{"id": float64(5), "name": "Reallocated_Sector_Ct", "value": float64(100), "worst": float64(100), "thresh": float64(10), "raw": "0"},
				{"id": float64(9), "name": "Power_On_Hours", "value": float64(97), "worst": float64(97), "thresh": float64(0), "raw": "25000"},
			},
			wantLen: 2, wantID: 5, wantRaw: 0, wantRawS: "0",
		},
		{
			name:    "empty list",
			data:    []map[string]any{},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs, err := ParseATAAttributes(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, attrs, tt.wantLen)
			if tt.wantLen > 0 {
				assert.Equal(t, tt.wantID, attrs[0].ID)
				assert.Equal(t, tt.wantRaw, attrs[0].RawValue)
				assert.Equal(t, tt.wantRawS, attrs[0].RawString)
			}
		})
	}
}

func TestParseRaw_NilInput(t *testing.T) {
	s, v, err := parseRaw(nil)
	require.NoError(t, err)
	assert.Equal(t, "0", s)
	assert.Equal(t, int64(0), v)
}

func TestParseRaw_NonStringNonFloatType(t *testing.T) {
	// bool is not string/float64/int64/int/nil — hits the default case
	s, v, err := parseRaw(true)
	require.NoError(t, err)
	assert.Equal(t, "true", s)
	assert.Equal(t, int64(0), v) // extractLeadingInt("true") = 0
}

func TestToInt64_StringInput(t *testing.T) {
	v, err := toInt64("42")
	require.NoError(t, err)
	assert.Equal(t, int64(42), v)
}

func TestToInt64_NilInput(t *testing.T) {
	_, err := toInt64(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported type")
}

func TestToInt64_Int64Input(t *testing.T) {
	v, err := toInt64(int64(99))
	require.NoError(t, err)
	assert.Equal(t, int64(99), v)
}

func TestToInt64_IntInput(t *testing.T) {
	v, err := toInt64(int(77))
	require.NoError(t, err)
	assert.Equal(t, int64(77), v)
}

func TestParseRaw_Int64Input(t *testing.T) {
	s, v, err := parseRaw(int64(500))
	require.NoError(t, err)
	assert.Equal(t, "500", s)
	assert.Equal(t, int64(500), v)
}

func TestParseRaw_IntInput(t *testing.T) {
	s, v, err := parseRaw(int(250))
	require.NoError(t, err)
	assert.Equal(t, "250", s)
	assert.Equal(t, int64(250), v)
}

func TestExtractLeadingInt(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"0", 0},
		{"123", 123},
		{"40 (Min/Max 25/55)", 40},
		{"25000", 25000},
		{"", 0},
		{"abc", 0},
		{"  42", 42},
		{"100/200", 100},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, extractLeadingInt(tt.input))
		})
	}
}
