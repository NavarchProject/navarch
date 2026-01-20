package gpu

import (
	"testing"
)

func TestXIDParser_ParseOutput(t *testing.T) {
	parser := NewXIDParser()

	tests := []struct {
		name     string
		input    string
		expected int
		xidCode  int
		pciID    string
	}{
		{
			name:     "standard xid error",
			input:    `[12345.678901] NVRM: Xid (PCI:0000:3b:00.0): 79, pid=1234, GPU has fallen off the bus.`,
			expected: 1,
			xidCode:  79,
			pciID:    "0000:3b:00.0",
		},
		{
			name:     "xid with unknown pid",
			input:    `[12345.678901] NVRM: Xid (PCI:0000:00:00.0): 48, pid='<unknown>', name=<unknown>, DBE (double bit error)`,
			expected: 1,
			xidCode:  48,
			pciID:    "0000:00:00.0",
		},
		{
			name:     "multiple xid errors",
			input:    "[100.0] NVRM: Xid (PCI:0000:3b:00.0): 79, GPU fell off\n[200.0] NVRM: Xid (PCI:0000:86:00.0): 48, DBE",
			expected: 2,
			xidCode:  79, // First one
			pciID:    "0000:3b:00.0",
		},
		{
			name:     "no xid errors",
			input:    `[12345.678901] nvidia 0000:3b:00.0: vgaarb: VGA decodes changed`,
			expected: 0,
		},
		{
			name:     "xid with iso timestamp",
			input:    `2024-01-15T10:30:45,123456+00:00 hostname kernel: NVRM: Xid (PCI:0000:3b:00.0): 63, ECC page retirement`,
			expected: 1,
			xidCode:  63,
			pciID:    "0000:3b:00.0",
		},
		{
			name:     "mixed content with xid",
			input:    "some random log\n[100.0] NVRM: Xid (PCI:0000:3b:00.0): 13, Graphics Engine Exception\nmore logs",
			expected: 1,
			xidCode:  13,
			pciID:    "0000:3b:00.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := parser.parseOutput(tt.input)

			if len(errors) != tt.expected {
				t.Errorf("Expected %d errors, got %d", tt.expected, len(errors))
				return
			}

			if tt.expected > 0 {
				if errors[0].XIDCode != tt.xidCode {
					t.Errorf("Expected XID code %d, got %d", tt.xidCode, errors[0].XIDCode)
				}
				if errors[0].DeviceID != tt.pciID {
					t.Errorf("Expected PCI ID %s, got %s", tt.pciID, errors[0].DeviceID)
				}
			}
		})
	}
}

func TestXIDSeverity(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{79, "fatal"},
		{48, "critical"},
		{63, "warning"},
		{13, "fatal"},
		{31, "fatal"},
		{74, "critical"},
		{999, "info"}, // Unknown code
	}

	for _, tt := range tests {
		t.Run(XIDDescription(tt.code), func(t *testing.T) {
			severity := XIDSeverity(tt.code)
			if severity != tt.expected {
				t.Errorf("XIDSeverity(%d) = %s, want %s", tt.code, severity, tt.expected)
			}
		})
	}
}

func TestXIDDescription(t *testing.T) {
	tests := []struct {
		code     int
		contains string
	}{
		{79, "fallen off the bus"},
		{48, "Double Bit ECC"},
		{63, "ECC page retirement"},
		{13, "Graphics Engine Exception"},
		{999, "Unknown XID"},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.code)), func(t *testing.T) {
			desc := XIDDescription(tt.code)
			if desc == "" {
				t.Error("Expected non-empty description")
			}
			// Check that known codes have meaningful descriptions
			if tt.code != 999 && len(desc) < 10 {
				t.Errorf("Description too short: %s", desc)
			}
		})
	}
}

func TestIsFatalXID(t *testing.T) {
	fatalCodes := []int{13, 31, 32, 43, 45, 64, 68, 69, 79, 92, 94, 95, 119}
	nonFatalCodes := []int{48, 63, 74, 1, 2, 100}

	for _, code := range fatalCodes {
		if !IsFatalXID(code) {
			t.Errorf("Expected XID %d to be fatal", code)
		}
	}

	for _, code := range nonFatalCodes {
		if IsFatalXID(code) {
			t.Errorf("Expected XID %d to NOT be fatal", code)
		}
	}
}

func TestExtractTimestamp(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "iso timestamp",
			input: "2024-01-15T10:30:45,123456+00:00 hostname kernel: message",
			want:  "2024-01-15T10:30:45",
		},
		{
			name:  "kernel timestamp",
			input: "[12345.678901] NVRM: message",
			want:  "12345.678901",
		},
		{
			name:  "no timestamp",
			input: "just a message without timestamp",
			want:  "", // Will return current time, just check it's not empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTimestamp(tt.input)
			if tt.want != "" && got != tt.want {
				t.Errorf("extractTimestamp() = %v, want %v", got, tt.want)
			}
			if got == "" {
				t.Error("extractTimestamp() returned empty string")
			}
		})
	}
}

