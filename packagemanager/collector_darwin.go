package packagemanager

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
)

func (s *SoftwareCollector) runCollection(parent context.Context, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	macos := MacOSUpdatesManager{}
	pkgInfo, err := macos.CollectPackageInfo(ctx, s.Configuration.Packagemanager.LimitDescriptionLength, s.Configuration.Packagemanager.EnableUpdateCheck)
	if err != nil {
		log.Errorln("Error collecting software info for macOS:", err)
	}

	// Collect Homebrew formulae and casks (merged into pkgInfo)
	CollectBrewPackages(ctx, &pkgInfo, s.Configuration.Packagemanager.LimitDescriptionLength, s.Configuration.Packagemanager.EnableUpdateCheck)

	log.Debugln("Packagemanager: Software inventory collection for macOS completed")
	s.Result <- &pkgInfo
}
