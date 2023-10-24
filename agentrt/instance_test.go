package agentrt

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func dynamicPort() int64 {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		log.Fatal(err)
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()
	port := int64(l.Addr().(*net.TCPAddr).Port)
	l.Close()
	return port
}

const exampleConfig = `[default]
port = "%d"
customchecks = "%s"
`

const exampleConfigShort = `[default]
port = "%d"
customchecks = "%s"
interval = 1
`

const exampleCCConfigWin = `[check1]
shell = powershell_command
command = "Write-Host Test"
`

const exampleCCConfigNix = `[check1]
command = "echo 'hallo welt'"
`

const exampleCCConfigWinLong = `[check1]
shell = powershell_command
command = "start-sleep 120"
`

const exampleCCConfigNixLong = `[check1]
command = "sleep 120"
`

func writeTestConfig(t *testing.T, tempDir, config, cccLin, cccWin string) {
	cfgPath := filepath.Join(tempDir, "config.ini")
	cccPath := filepath.Join(tempDir, "customchecks.ini")
	if err := os.WriteFile(cfgPath, []byte(fmt.Sprintf(config, dynamicPort(), cccPath)), 0600); err != nil {
		t.Fatal(err)
	}
	cccConfig := cccLin
	if runtime.GOOS == "windows" {
		cccConfig = cccWin
	}
	if err := os.WriteFile(cccPath, []byte(cccConfig), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAgentReload(t *testing.T) {
	tempDir, err := os.MkdirTemp(os.TempDir(), "*-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	writeTestConfig(t, tempDir, exampleConfig, exampleCCConfigNix, exampleCCConfigWin)

	rt := &AgentInstance{
		ConfigurationPath: filepath.Join(tempDir, "config.ini"),
		LogPath:           filepath.Join(tempDir, "agent.log"),
		LogRotate:         3,
		Debug:             true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt.Start(ctx)
	t.Log("Reload")
	rt.Reload()

	rt.Shutdown()
}

func TestAgentCancel(t *testing.T) {
	tempDir, err := os.MkdirTemp(os.TempDir(), "*-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	writeTestConfig(t, tempDir, exampleConfig, exampleCCConfigNix, exampleCCConfigWin)

	rt := &AgentInstance{
		ConfigurationPath: filepath.Join(tempDir, "config.ini"),
		LogPath:           filepath.Join(tempDir, "agent.log"),
		LogRotate:         3,
		Debug:             true,
	}

	ctx, cancel := context.WithCancel(context.Background())

	rt.Start(ctx)
	rt.Reload()
	cancel()

	ticker := time.NewTicker(time.Microsecond * 200)
	defer ticker.Stop()
	timeout := time.NewTimer(time.Second * 10)
	defer timeout.Stop()
	select {
	case <-timeout.C:
		t.Fatal("timeout waiting for shutdown")
	case <-ticker.C:
		if rt.logHandler == nil {
			return
		}
	}
}

func TestAgentReloadWithLongRunningTask(t *testing.T) {
	tempDir, err := os.MkdirTemp(os.TempDir(), "*-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	writeTestConfig(t, tempDir, exampleConfig, exampleCCConfigNixLong, exampleCCConfigWinLong)

	rt := &AgentInstance{
		ConfigurationPath: filepath.Join(tempDir, "config.ini"),
		LogPath:           filepath.Join(tempDir, "agent.log"),
		LogRotate:         3,
		Debug:             true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt.Start(ctx)
	t.Log("Reload ")
	rt.Reload()

	rt.Shutdown()
}

func TestAgentShortInterval(t *testing.T) {
	tempDir, err := os.MkdirTemp(os.TempDir(), "*-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	writeTestConfig(t, tempDir, exampleConfigShort, exampleCCConfigNix, exampleCCConfigWin)

	rt := &AgentInstance{
		ConfigurationPath: filepath.Join(tempDir, "config.ini"),
		LogPath:           filepath.Join(tempDir, "agent.log"),
		LogRotate:         3,
		Debug:             true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt.Start(ctx)
	rt.Reload()

	time.Sleep(time.Second * 2)

	rt.Shutdown()
}
