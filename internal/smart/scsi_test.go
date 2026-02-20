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
