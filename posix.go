// +build linux darwin

package main

import (
	"os"

	"github.com/openITCOCKPIT/openitcockpit-agent-go/cmd"
)

func platform_main() {
	if err := cmd.New().Execute(); err != nil {
		os.Exit(1)
	}
}
