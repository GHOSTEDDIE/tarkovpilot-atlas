package agent

import "testing"

func TestQuestStatusFromTemplate(t *testing.T) {
	cases := []struct {
		templateID string
		rawType    string
		wantID     string
		wantStatus string
		wantOK     bool
	}{
		{"59c124d686f774189b3c843f successMessageText", "24", "59c124d686f774189b3c843f", "completed", true},
		{"59c124d686f774189b3c843f failMessageText", "24", "59c124d686f774189b3c843f", "failed", true},
		{"59c124d686f774189b3c843f startMessageText", "24", "59c124d686f774189b3c843f", "started", true},
		{"59c124d686f774189b3c843f acceptMessageText", "24", "59c124d686f774189b3c843f", "started", true},
		{"59c124d686f774189b3c843f somethingElse", "24", "59c124d686f774189b3c843f", "24", true},
		{"59c124d686f774189b3c843f", "24", "59c124d686f774189b3c843f", "24", true},
		{"", "24", "", "", false},
	}
	for _, c := range cases {
		id, status, ok := questStatusFromTemplate(c.templateID, c.rawType)
		if id != c.wantID || status != c.wantStatus || ok != c.wantOK {
			t.Errorf("questStatusFromTemplate(%q, %q) = (%q, %q, %v), want (%q, %q, %v)",
				c.templateID, c.rawType, id, status, ok, c.wantID, c.wantStatus, c.wantOK)
		}
	}
}

func TestStatusToString(t *testing.T) {
	if got := statusToString(float64(24)); got != "24" {
		t.Errorf("number: %q", got)
	}
	if got := statusToString("questComplete"); got != "questComplete" {
		t.Errorf("string: %q", got)
	}
}
