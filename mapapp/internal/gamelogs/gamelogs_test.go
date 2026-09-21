package gamelogs

import "testing"

func TestResolveLatestMapPresets(t *testing.T) {
	for raw, want := range map[string]string{
		"sandbox_start_preset":   "groundzero",
		"laboratory_dark_preset": "lab",
		"labyrinth_preset":       "labyrinth",
		"icebreaker":             "icebreaker",
	} {
		got, ok := Resolve(raw)
		if !ok || got != want {
			t.Fatalf("Resolve(%q) = %q, %v; want %q, true", raw, got, ok, want)
		}
	}
}
