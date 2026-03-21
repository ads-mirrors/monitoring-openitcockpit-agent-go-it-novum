package packagemanager

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/openITCOCKPIT/openitcockpit-agent-go/utils"
	log "github.com/sirupsen/logrus"
)

// BrewManager collects installed Homebrew formulae and casks on macOS
type BrewManager struct{}

// brew info --json=v2 --installed output structure
type brewInfoJson struct {
	Formulae []brewFormula `json:"formulae"`
	Casks    []brewCask    `json:"casks"`
}

type brewFormula struct {
	Name     string             `json:"name"`
	Desc     string             `json:"desc"`
	Versions brewFormulaVersion `json:"versions"`
}

type brewFormulaVersion struct {
	Stable string `json:"stable"`
}

type brewCask struct {
	Token   string `json:"token"`
	Name    []string `json:"name"`
	Desc    string `json:"desc"`
	Version string `json:"version"`
}

// brew outdated --json=v2 output structure
type brewOutdatedJson struct {
	Formulae []brewOutdatedFormula `json:"formulae"`
	Casks    []brewOutdatedCask    `json:"casks"`
}

type brewOutdatedFormula struct {
	Name              string   `json:"name"`
	InstalledVersions []string `json:"installed_versions"`
	CurrentVersion    string   `json:"current_version"`
	Pinned            bool     `json:"pinned"`
}

// Note: InstalledVersions is a string for casks (not []string like formulae)
// because brew's JSON output differs between the two types.
type brewOutdatedCask struct {
	Name              string `json:"name"`
	InstalledVersions string `json:"installed_versions"`
	CurrentVersion    string `json:"current_version"`
}

func (b BrewManager) IsAvailable() bool {
	_, err := exec.LookPath("brew")
	return err == nil
}

func (b BrewManager) ListInstalledPackages(ctx context.Context) ([]Package, error) {
	output, err := b.getInstalledJson(ctx)
	if err != nil {
		return nil, err
	}
	return b.parseInstalledJson(output)
}

func (b BrewManager) getInstalledJson(ctx context.Context) (string, error) {
	timeout := 30 * time.Second
	result, err := utils.RunCommand(ctx, utils.CommandArgs{
		Command: "brew info --json=v2 --installed",
		Timeout: timeout,
	})
	if err != nil {
		return "", fmt.Errorf("error fetching brew installed packages: %s", err)
	}
	if result.RC != 0 {
		return "", fmt.Errorf("brew info exited with code %d: %s", result.RC, result.Stdout)
	}
	return result.Stdout, nil
}

func (b BrewManager) parseInstalledJson(output string) ([]Package, error) {
	var data brewInfoJson
	if err := json.Unmarshal([]byte(output), &data); err != nil {
		return nil, fmt.Errorf("error parsing brew info json: %s", err)
	}

	pkgs := make([]Package, 0, len(data.Formulae)+len(data.Casks))

	for _, f := range data.Formulae {
		pkgs = append(pkgs, Package{
			Name:        f.Name,
			Version:     f.Versions.Stable,
			Description: f.Desc,
		})
	}

	for _, c := range data.Casks {
		name := c.Token
		if len(c.Name) > 0 {
			name = c.Name[0]
		}
		pkgs = append(pkgs, Package{
			Name:        name,
			Version:     c.Version,
			Description: c.Desc,
		})
	}

	return pkgs, nil
}

func (b BrewManager) ListOutdatedPackages(ctx context.Context) ([]PackageUpdate, error) {
	output, err := b.getOutdatedJson(ctx)
	if err != nil {
		return nil, err
	}
	return b.parseOutdatedJson(output)
}

func (b BrewManager) getOutdatedJson(ctx context.Context) (string, error) {
	timeout := 30 * time.Second
	result, err := utils.RunCommand(ctx, utils.CommandArgs{
		Command: "brew outdated --json=v2",
		Timeout: timeout,
	})
	if err != nil {
		return "", fmt.Errorf("error fetching brew outdated: %s", err)
	}
	// brew outdated returns exit code 0 regardless of whether packages are outdated.
	// A non-zero exit code indicates an actual error (e.g. broken brew installation).
	if result.RC != 0 {
		return "", fmt.Errorf("brew outdated exited with code %d: %s", result.RC, result.Stdout)
	}
	return result.Stdout, nil
}

func (b BrewManager) parseOutdatedJson(output string) ([]PackageUpdate, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}

	var data brewOutdatedJson
	if err := json.Unmarshal([]byte(output), &data); err != nil {
		return nil, fmt.Errorf("error parsing brew outdated json: %s", err)
	}

	updates := make([]PackageUpdate, 0, len(data.Formulae)+len(data.Casks))

	for _, f := range data.Formulae {
		current := ""
		if len(f.InstalledVersions) > 0 {
			current = f.InstalledVersions[0]
		}
		updates = append(updates, PackageUpdate{
			Name:             f.Name,
			CurrentVersion:   current,
			AvailableVersion: f.CurrentVersion,
		})
	}

	for _, c := range data.Casks {
		updates = append(updates, PackageUpdate{
			Name:             c.Name,
			CurrentVersion:   c.InstalledVersions,
			AvailableVersion: c.CurrentVersion,
		})
	}

	return updates, nil
}

// CollectBrewPackages collects Homebrew formulae and casks and merges them
// into the existing PackageInfo from system_profiler.
func CollectBrewPackages(ctx context.Context, existing *PackageInfo, limitDescriptionLength int64, enableUpdateCheck bool) {
	brew := BrewManager{}
	if !brew.IsAvailable() {
		log.Debugln("Packagemanager: Homebrew is not installed, skipping")
		return
	}

	log.Infoln("Packagemanager: Collecting Homebrew packages")

	installedPkgs, err := brew.ListInstalledPackages(ctx)
	if err != nil {
		log.Errorln("Packagemanager: Error collecting Homebrew packages:", err)
		return
	}

	// Truncate descriptions
	for i := range installedPkgs {
		installedPkgs[i].Description = truncateDescription(installedPkgs[i].Description, limitDescriptionLength)
	}

	// Deduplicate: skip brew packages that already exist in system_profiler by name (case-insensitive)
	existingNames := make(map[string]bool, len(existing.MacosApps))
	for _, app := range existing.MacosApps {
		existingNames[strings.ToLower(app.Name)] = true
	}

	added := 0
	for _, pkg := range installedPkgs {
		if !existingNames[strings.ToLower(pkg.Name)] {
			existing.MacosApps = append(existing.MacosApps, pkg)
			added++
		}
	}
	existing.Stats.InstalledPackages = int64(len(existing.MacosApps))

	log.Infof("Packagemanager: Homebrew added %d packages (%d formulae+casks total, %d deduplicated)", added, len(installedPkgs), len(installedPkgs)-added)

	// Collect outdated packages
	if enableUpdateCheck {
		outdated, err := brew.ListOutdatedPackages(ctx)
		if err != nil {
			log.Errorln("Packagemanager: Error collecting Homebrew updates:", err)
			return
		}

		// Convert to MacosUpdate format and append
		for _, u := range outdated {
			existing.MacosUpdates = append(existing.MacosUpdates, MacosUpdate{
				Name:        u.Name,
				Version:     u.AvailableVersion,
				Description: fmt.Sprintf("Homebrew: %s -> %s", u.CurrentVersion, u.AvailableVersion),
			})
		}
		existing.Stats.UpgradablePackages = int64(len(existing.MacosUpdates))
	}
}
