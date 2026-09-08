package drivers

import "testing"

func TestIsValidObjectType(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"procedure", true},
		{"function", true},
		{"trigger", true},
		{"event", true},
		{"sequence", true},
		{"synonym", true},
		{"package", true},
		{"matview", true},
		{"type", true},
		{"table", false},
		{"view", false},
		{"", false},
		{"PROCEDURE", false},
		{"index", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsValidObjectType(tt.input); got != tt.want {
				t.Errorf("IsValidObjectType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestAllObjectTypesContainsExpected(t *testing.T) {
	expected := map[string]bool{
		"procedure": true, "function": true, "trigger": true, "event": true,
		"sequence": true, "synonym": true, "package": true, "matview": true, "type": true,
	}
	for _, ot := range AllObjectTypes {
		if !expected[ot] {
			t.Errorf("unexpected object type in AllObjectTypes: %s", ot)
		}
	}
	if len(AllObjectTypes) != len(expected) {
		t.Errorf("AllObjectTypes has %d entries, expected %d", len(AllObjectTypes), len(expected))
	}
}
