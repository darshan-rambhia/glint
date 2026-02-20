package smart

import (
	"testing"
)

func TestParseSCSIText_Temperature(t *testing.T) {
	text := `
=== START OF READ SMART DATA SECTION ===
SMART Health Status: OK
Current Drive Temperature:     32 C
Drive Trip Temperature:        60 C
`
	attrs := ParseSCSIText(text)
	found := false
	for _, a := range attrs {
		if a.ID == SCSITemperature {
			found = true
			if a.RawValue != 32 {
				t.Errorf("expected temperature 32, got %d", a.RawValue)
			}
		}
	}
	if !found {
		t.Error("SCSITemperature attribute not found")
	}
}

func TestParseSCSIText_PowerOnHoursEquals(t *testing.T) {
	text := "Number of hours powered up = 12345.67\n"
	attrs := ParseSCSIText(text)
	for _, a := range attrs {
		if a.ID == SCSIPowerOnHours {
			if a.RawValue != 12345 {
				t.Errorf("expected power-on hours 12345, got %d", a.RawValue)
			}
			return
		}
	}
	t.Error("SCSIPowerOnHours attribute not found")
}

func TestParseSCSIText_PowerOnHoursLogPage(t *testing.T) {
	text := "Accumulated power on time, hours:minutes 21867:04\n"
	attrs := ParseSCSIText(text)
	for _, a := range attrs {
		if a.ID == SCSIPowerOnHours {
			if a.RawValue != 21867 {
				t.Errorf("expected power-on hours 21867, got %d", a.RawValue)
			}
			return
		}
	}
	t.Error("SCSIPowerOnHours attribute not found")
}

func TestParseSCSIText_NegativeHoursEquals(t *testing.T) {
	// Fuzz-found: "Number of hours powered up = -1" must not produce a negative RawValue.
	attrs := ParseSCSIText("Number of hours powered up = -1\n")
	if len(attrs) != 0 {
		t.Errorf("expected no attrs for negative hours, got %d", len(attrs))
	}
}

func TestParseSCSIText_NegativeHoursLogPage(t *testing.T) {
	// Log-page format with negative hours component must be rejected.
	attrs := ParseSCSIText("Accumulated power on time, hours:minutes -1:00\n")
	if len(attrs) != 0 {
		t.Errorf("expected no attrs for negative hours, got %d", len(attrs))
	}
}

func TestExtractIntAfterColon(t *testing.T) {
	// No colon at all → false.
	v, ok := extractIntAfterColon("no colon here")
	if ok {
		t.Errorf("expected ok=false for input without colon, got %d", v)
	}

	// Colon present but no digits follow → false.
	v, ok = extractIntAfterColon("Temperature:  C")
	if ok {
		t.Errorf("expected ok=false when no digits after colon, got %d", v)
	}

	// Normal case (digits after colon) → value returned.
	v, ok = extractIntAfterColon("Temperature:   42 C")
	if !ok || v != 42 {
		t.Errorf("expected (42, true), got (%d, %v)", v, ok)
	}
}

func TestParseSCSIText_Empty(t *testing.T) {
	attrs := ParseSCSIText("")
	if len(attrs) != 0 {
		t.Errorf("expected empty result for empty text, got %d attrs", len(attrs))
	}
}

func TestParseSCSIText_NoRelevantFields(t *testing.T) {
	text := "Elements in grown defect list: 0\nError counter log:\n"
	attrs := ParseSCSIText(text)
	if len(attrs) != 0 {
		t.Errorf("expected 0 attrs, got %d", len(attrs))
	}
}

func BenchmarkParseSCSIText(b *testing.B) {
	text := `=== START OF READ SMART DATA SECTION ===
SMART Health Status: OK
Current Drive Temperature:     38 C
Drive Trip Temperature:        60 C
Accumulated power on time, hours:minutes 21867:04
Number of hours powered up = 21867.07`
	b.ResetTimer()
	for b.Loop() {
		ParseSCSIText(text)
	}
}

func FuzzParseSCSIText(f *testing.F) {
	f.Add("Current Drive Temperature:     32 C\n")
	f.Add("Number of hours powered up = 12345.67\n")
	f.Add("Accumulated power on time, hours:minutes 21867:04\n")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		attrs := ParseSCSIText(s)
		for _, a := range attrs {
			if a.ID != SCSITemperature && a.ID != SCSIPowerOnHours {
				t.Fatalf("unexpected attr ID %d (want %d or %d)", a.ID, SCSITemperature, SCSIPowerOnHours)
			}
			if a.RawValue < 0 {
				t.Fatalf("negative raw value %d for attr ID %d", a.RawValue, a.ID)
			}
		}
	})
}
