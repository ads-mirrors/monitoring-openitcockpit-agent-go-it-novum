package packagemanager

import (
	"context"
	"strings"
	"testing"
)

const brewInfoTestData = `{
  "formulae": [
    {
      "name": "go",
      "full_name": "go",
      "desc": "Open source programming language to build simple/reliable/efficient software",
      "versions": {"stable": "1.26.1"}
    },
    {
      "name": "git",
      "full_name": "git",
      "desc": "Distributed revision control system",
      "versions": {"stable": "2.48.1"}
    }
  ],
  "casks": [
    {
      "token": "kitty",
      "name": ["kitty"],
      "desc": "GPU-based terminal emulator",
      "version": "0.46.1"
    },
    {
      "token": "firefox",
      "name": ["Mozilla Firefox"],
      "desc": "Web browser",
      "version": "137.0"
    }
  ]
}`

const brewOutdatedTestData = `{
  "formulae": [
    {
      "name": "uv",
      "installed_versions": ["0.10.11"],
      "current_version": "0.10.12",
      "pinned": false,
      "pinned_version": null
    }
  ],
  "casks": [
    {
      "name": "kitty",
      "installed_versions": "0.46.0",
      "current_version": "0.46.1"
    }
  ]
}`

func TestParseBrewInstalledJson(t *testing.T) {
	brew := BrewManager{}
	pkgs, err := brew.parseInstalledJson(brewInfoTestData)
	if err != nil {
		t.Fatalf("parseInstalledJson failed: %v", err)
	}

	if len(pkgs) != 4 {
		t.Fatalf("expected 4 packages, got %d", len(pkgs))
	}

	// Formulae
	if pkgs[0].Name != "go" {
		t.Errorf("expected go, got %s", pkgs[0].Name)
	}
	if pkgs[0].Version != "1.26.1" {
		t.Errorf("expected 1.26.1, got %s", pkgs[0].Version)
	}
	if pkgs[0].Description != "Open source programming language to build simple/reliable/efficient software" {
		t.Errorf("unexpected description: %s", pkgs[0].Description)
	}

	if pkgs[1].Name != "git" {
		t.Errorf("expected git, got %s", pkgs[1].Name)
	}

	// Casks use display name
	if pkgs[2].Name != "kitty" {
		t.Errorf("expected kitty, got %s", pkgs[2].Name)
	}
	if pkgs[3].Name != "Mozilla Firefox" {
		t.Errorf("expected Mozilla Firefox, got %s", pkgs[3].Name)
	}
	if pkgs[3].Version != "137.0" {
		t.Errorf("expected 137.0, got %s", pkgs[3].Version)
	}
}

func TestParseBrewInstalledJsonMalformed(t *testing.T) {
	brew := BrewManager{}
	_, err := brew.parseInstalledJson("not valid json")
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestParseBrewInstalledJsonEmpty(t *testing.T) {
	brew := BrewManager{}
	pkgs, err := brew.parseInstalledJson(`{"formulae": [], "casks": []}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 0 {
		t.Errorf("expected 0 packages, got %d", len(pkgs))
	}
}

func TestParseBrewInstalledJsonCaskNoName(t *testing.T) {
	brew := BrewManager{}
	pkgs, err := brew.parseInstalledJson(`{"formulae": [], "casks": [{"token": "my-app", "name": [], "desc": "", "version": "1.0"}]}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	// Falls back to token when name array is empty
	if pkgs[0].Name != "my-app" {
		t.Errorf("expected my-app (token fallback), got %s", pkgs[0].Name)
	}
}

func TestParseBrewOutdatedJson(t *testing.T) {
	brew := BrewManager{}
	updates, err := brew.parseOutdatedJson(brewOutdatedTestData)
	if err != nil {
		t.Fatalf("parseOutdatedJson failed: %v", err)
	}

	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(updates))
	}

	if updates[0].Name != "uv" {
		t.Errorf("expected uv, got %s", updates[0].Name)
	}
	if updates[0].CurrentVersion != "0.10.11" {
		t.Errorf("expected 0.10.11, got %s", updates[0].CurrentVersion)
	}
	if updates[0].AvailableVersion != "0.10.12" {
		t.Errorf("expected 0.10.12, got %s", updates[0].AvailableVersion)
	}

	if updates[1].Name != "kitty" {
		t.Errorf("expected kitty, got %s", updates[1].Name)
	}
}

func TestParseBrewOutdatedEmpty(t *testing.T) {
	brew := BrewManager{}
	updates, err := brew.parseOutdatedJson("")
	if err != nil {
		t.Fatalf("parseOutdatedJson failed on empty: %v", err)
	}
	if updates != nil {
		t.Errorf("expected nil, got %v", updates)
	}
}

func TestParseBrewOutdatedMalformed(t *testing.T) {
	brew := BrewManager{}
	_, err := brew.parseOutdatedJson("{broken")
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestBrewDeduplicationCaseInsensitive(t *testing.T) {
	existing := &PackageInfo{
		MacosApps: []Package{
			{Name: "Kitty", Version: "0.46.1"}, // Uppercase K from system_profiler
			{Name: "Safari", Version: "18.0"},
		},
	}

	brew := BrewManager{}
	brewPkgs, _ := brew.parseInstalledJson(brewInfoTestData)

	// Use same case-insensitive logic as production code
	existingNames := make(map[string]bool, len(existing.MacosApps))
	for _, app := range existing.MacosApps {
		existingNames[strings.ToLower(app.Name)] = true
	}

	added := 0
	for _, pkg := range brewPkgs {
		if !existingNames[strings.ToLower(pkg.Name)] {
			existing.MacosApps = append(existing.MacosApps, pkg)
			added++
		}
	}

	// "kitty" (brew) matches "Kitty" (system_profiler) case-insensitively -> deduplicated
	// Added: go, git, Mozilla Firefox = 3
	if added != 3 {
		t.Errorf("expected 3 added (Kitty/kitty deduplicated), got %d", added)
	}
	if len(existing.MacosApps) != 5 {
		t.Errorf("expected 5 total apps, got %d", len(existing.MacosApps))
	}
}

func TestCollectBrewPackagesNoBrewAvailable(t *testing.T) {
	// Test that CollectBrewPackages doesn't crash when brew is not in PATH
	// This works because the function checks IsAvailable() first
	existing := &PackageInfo{
		MacosApps: []Package{
			{Name: "Safari", Version: "18.0"},
		},
		Stats: PackageStats{InstalledPackages: 1},
	}

	// Clear PATH to simulate no brew
	t.Setenv("PATH", "/nonexistent")

	CollectBrewPackages(context.Background(), existing, 80, false)

	// Should not have changed anything
	if len(existing.MacosApps) != 1 {
		t.Errorf("expected 1 app (unchanged), got %d", len(existing.MacosApps))
	}
}
