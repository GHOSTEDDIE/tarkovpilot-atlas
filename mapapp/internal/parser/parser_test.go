package parser

import "testing"

func TestParseRealSample(t *testing.T) {
	// sample from the plan / public projects (older format: "-" is the minus sign)
	p, err := Parse("2025-12-20[02-09]-420.18, 1.00, 319.01-0.00089, -0.99307, -0.00012, -0.11748_15.11 (0).png")
	if err != nil {
		t.Fatal(err)
	}
	if p.Date != "2025-12-20" || p.Time != "02-09" {
		t.Errorf("date/time: %q %q", p.Date, p.Time)
	}
	if p.World.X != -420.18 || p.World.Y != 1.00 || p.World.Z != 319.01 {
		t.Errorf("world: %+v", p.World)
	}
	if p.Rotation.X != -0.00089 || p.Rotation.Y != -0.99307 || p.Rotation.Z != -0.00012 || p.Rotation.W != -0.11748 {
		t.Errorf("rotation: %+v", p.Rotation)
	}
	if p.Suffix != "15.11 (0)" {
		t.Errorf("suffix: %q", p.Suffix)
	}
}

func TestParseCurrentFormat(t *testing.T) {
	// real filename from the live game (2026-08): "_" separators
	p, err := Parse("2026-08-19[17-50]_585.73, 11.24, 146.56_0.00000, 0.94890, 0.00000, -0.31557_11.91 (0).png")
	if err != nil {
		t.Fatal(err)
	}
	if p.World.X != 585.73 || p.World.Y != 11.24 || p.World.Z != 146.56 {
		t.Errorf("world: %+v", p.World)
	}
	if p.Rotation.X != 0 || p.Rotation.Y != 0.94890 || p.Rotation.Z != 0 || p.Rotation.W != -0.31557 {
		t.Errorf("rotation: %+v", p.Rotation)
	}
	if p.Suffix != "11.91 (0)" {
		t.Errorf("suffix: %q", p.Suffix)
	}
}

func TestParseNoSpaces(t *testing.T) {
	p, err := Parse("2026-01-01[00-00]1,2,3-0,0,0,1_x.png")
	if err != nil {
		t.Fatal(err)
	}
	if p.World.X != 1 || p.World.Y != 2 || p.World.Z != 3 {
		t.Errorf("world: %+v", p.World)
	}
	if p.Rotation.W != 1 {
		t.Errorf("rotation: %+v", p.Rotation)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	for _, name := range []string{
		"",
		"screenshot.png",
		"2025-12-20[02-09]-420.18, 1.00, 319.01-0.00089, -0.99307, -0.00012_x.png", // missing a quat component
		"2025-12-20[02-09]-420.18, 1.00, 319.01-0.00089, -0.99307, -0.00012, -0.11748_15.11 (0).jpg",
	} {
		if _, err := Parse(name); err == nil {
			t.Errorf("expected error for %q", name)
		}
	}
}

func TestRemap(t *testing.T) {
	p, err := Parse("2026-01-01[00-00]1, 2, 3-0, 0, 0, 1_x.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Remap("y,z,x"); err != nil {
		t.Fatal(err)
	}
	if p.World.X != 3 || p.World.Y != 1 || p.World.Z != 2 {
		t.Errorf("after y,z,x remap: %+v", p.World)
	}
	if err := p.Remap("x,x,y"); err == nil {
		t.Error("expected error for duplicate axes")
	}
	if err := p.Remap("x,y"); err == nil {
		t.Error("expected error for 2 axes")
	}
}
