package agentrt

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/openITCOCKPIT/openitcockpit-agent-go/checkrunner"
	"github.com/openITCOCKPIT/openitcockpit-agent-go/checks"
	"github.com/openITCOCKPIT/openitcockpit-agent-go/config"
	"github.com/openITCOCKPIT/openitcockpit-agent-go/loghandler"
	"github.com/openITCOCKPIT/openitcockpit-agent-go/pushclient"
	"github.com/openITCOCKPIT/openitcockpit-agent-go/webserver"
	log "github.com/sirupsen/logrus"
)

type AgentInstance struct {
	ConfigurationPath  string
	LogPath            string
	LogRotate          int
	Verbose            bool
	Debug              bool
	DisableErrorOutput bool

	wg       sync.WaitGroup
	shutdown chan struct{}
	reload   chan chan struct{}

	stateWebserver               chan []byte
	statePushClient              chan []byte
	prometheusStateWebserver     chan map[string]string
	checkResult                  chan map[string]interface{}
	customCheckResultChan        chan *checkrunner.CustomCheckResult
	prometheusExporterResultChan chan *checkrunner.PrometheusExporterResult

	customCheckResults map[string]interface{}

	prometheusExporterResults map[string]string

	logHandler             *loghandler.LogHandler
	webserver              *webserver.Server
	checkRunner            *checkrunner.CheckRunner
	customCheckHandler     *checkrunner.CustomCheckHandler
	prometheusCheckHandler *checkrunner.PrometheusCheckHandler
	pushClient             *pushclient.PushClient
}

func (a *AgentInstance) processCheckResult(result map[string]interface{}) {
	if a.customCheckResults == nil {
		result["customchecks"] = map[string]interface{}{}
	} else {
		// Merge custom check results into "normal" check results
		result["customchecks"] = a.customCheckResults
	}

	prometheus_results_data := make(map[string]string, len(a.prometheusExporterResults))
	if a.prometheusExporterResults == nil {
		result["prometheus_exporters"] = "[]"
	} else {
		// Merge the name of all available prometheus exporters into the check result
		keys := make([]string, len(a.prometheusExporterResults))
		i := 0
		for k, result := range a.prometheusExporterResults {
			keys[i] = k
			prometheus_results_data[k] = result
			i++
		}

		result["prometheus_exporters"] = keys
	}

	data, err := json.Marshal(result)
	if err != nil {
		log.Errorln("Internal error: could not serialize check result: ", err)
		errorResult := map[string]string{
			"error": err.Error(),
		}
		data, err = json.Marshal(errorResult)
		if err != nil {
			log.Fatalln("Internal error: could also not serialize error result: ", err)
		}
	}

	if a.webserver != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()

			t := time.NewTimer(time.Second * 10)
			defer t.Stop()

			// we may have to give the webserver some time to think about it
			select {
			case a.stateWebserver <- data: // Pass checkresult json to webserver
			case a.prometheusStateWebserver <- prometheus_results_data: // Pass Prometheus Exporter data to webserver
			case <-t.C:
				log.Errorln("Internal error: could not store check result for webserver: timeout")
			}
		}()
	}

	if a.pushClient != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()

			t := time.NewTimer(time.Second * 10)
			defer t.Stop()

			// we may have to give the push client some time to think about it
			select {
			case a.statePushClient <- data: // Pass checkresult json to push client
			case <-t.C:
				log.Errorln("Internal error: could not store check result for push client: timeout")
			}
		}()
	}
}

func (a *AgentInstance) doReload(ctx context.Context, cfg *config.Configuration) {
	if a.stateWebserver == nil {
		a.stateWebserver = make(chan []byte)
	}
	if a.checkResult == nil {
		a.checkResult = make(chan map[string]interface{})
	}
	if a.prometheusStateWebserver == nil {
		a.prometheusStateWebserver = make(chan map[string]string)
	}

	// we do not stop the webserver on every reload for better availability during the wizard setup

	if cfg.OITC.Push && !cfg.OITC.EnableWebserver && a.webserver != nil {
		a.webserver.Shutdown()
		a.webserver = nil
	}

	if a.webserver == nil && (!cfg.OITC.Push || (cfg.OITC.Push && cfg.OITC.EnableWebserver)) {
		a.webserver = &webserver.Server{
			StateInput:      a.stateWebserver,
			PrometheusInput: a.prometheusStateWebserver,
			Reloader:        a, // Set agent instance to Reloader interface for the webserver handler
		}
		a.webserver.Start(ctx)
	}

	if a.webserver != nil {
		a.webserver.Reload(cfg)
	}

	if a.checkRunner != nil {
		a.checkRunner.Shutdown()
	}

	cList, err := checks.ChecksForConfiguration(cfg)
	if err != nil {
		log.Fatalln(err)
	}
	a.checkRunner = &checkrunner.CheckRunner{
		Configuration: cfg,
		Result:        a.checkResult,
		Checks:        cList,
	}
	if err := a.checkRunner.Start(ctx); err != nil {
		log.Fatalln(err)
	}

	if a.pushClient != nil {
		a.pushClient.Shutdown()
		a.pushClient = nil
	}
	if cfg.OITC.Push {
		a.pushClient = &pushclient.PushClient{
			StateInput: a.statePushClient,
		}
		if err := a.pushClient.Start(ctx, cfg); err != nil {
			log.Fatalln("Could not load push client: ", err)
		}
	}
	a.doCustomCheckReload(ctx, cfg.CustomCheckConfiguration)
	a.doPrometheusExporterCheckReload(ctx, cfg.PrometheusExporterConfiguration)
}

func (a *AgentInstance) doCustomCheckReload(ctx context.Context, ccc []*config.CustomCheck) {
	if a.customCheckHandler != nil {
		a.customCheckHandler.Shutdown()
		a.customCheckHandler = nil
	}
	if len(ccc) > 0 {
		a.customCheckHandler = &checkrunner.CustomCheckHandler{
			Configuration: ccc,
			ResultOutput:  a.customCheckResultChan,
		}
		a.customCheckHandler.Start(ctx)
	}
}

func (a *AgentInstance) doPrometheusExporterCheckReload(ctx context.Context, exporters []*config.PrometheusExporter) {
	if a.prometheusCheckHandler != nil {
		a.prometheusCheckHandler.Shutdown()
		a.prometheusCheckHandler = nil
	}
	if len(exporters) > 0 {
		a.prometheusCheckHandler = &checkrunner.PrometheusCheckHandler{
			Configuration: exporters,
			ResultOutput:  a.prometheusExporterResultChan,
		}
		a.prometheusCheckHandler.Start(ctx)
	}
}

func (a *AgentInstance) stop() {
	wg := sync.WaitGroup{}
	if a.logHandler != nil {
		wg.Add(1)
		go func() {
			a.logHandler.Shutdown()
			a.logHandler = nil
			wg.Done()
		}()
	}
	if a.webserver != nil {
		wg.Add(1)
		go func() {
			a.webserver.Shutdown()
			a.webserver = nil
			wg.Done()
		}()
	}
	if a.customCheckHandler != nil {
		wg.Add(1)
		go func() {
			a.customCheckHandler.Shutdown()
			a.customCheckHandler = nil
			wg.Done()
		}()
	}
	if a.checkRunner != nil {
		wg.Add(1)
		go func() {
			a.checkRunner.Shutdown()
			a.checkRunner = nil
			wg.Done()
		}()
	}
	if a.pushClient != nil {
		wg.Add(1)
		go func() {
			a.pushClient.Shutdown()
			a.pushClient = nil
			wg.Done()
		}()
	}
	wg.Wait()
}

func (a *AgentInstance) Start(parent context.Context) {
	a.stateWebserver = make(chan []byte)
	a.statePushClient = make(chan []byte)
	a.checkResult = make(chan map[string]interface{})
	a.customCheckResultChan = make(chan *checkrunner.CustomCheckResult)
	a.customCheckResults = map[string]interface{}{}
	a.prometheusExporterResultChan = make(chan *checkrunner.PrometheusExporterResult)
	a.prometheusExporterResults = make(map[string]string)
	a.shutdown = make(chan struct{})
	a.reload = make(chan chan struct{})
	a.logHandler = &loghandler.LogHandler{
		Verbose:              a.Verbose,
		Debug:                a.Debug,
		LogPath:              a.LogPath,
		LogRotate:            a.LogRotate,
		DefaultWriter:        os.Stderr,
		DisableDefaultWriter: a.DisableErrorOutput,
	}

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()

		ctx, cancel := context.WithCancel(parent)
		defer cancel()

		a.logHandler.Start(ctx)

		defer a.stop()

		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-a.shutdown:
				if !ok {
					// a.shutdown channel was closed - Exit agent
					return
				}
			case done := <-a.reload:
				// Got reload signal
				cfg, err := config.Load(ctx, a.ConfigurationPath)
				if err != nil {
					log.Fatalln("could not load configuration: ", err)
				}
				a.doReload(ctx, cfg)

				// Notify caller that reload is done
				done <- struct{}{}
			case res := <-a.checkResult:
				// received check result from checkrunner
				a.processCheckResult(res)
			case res := <-a.customCheckResultChan:
				// received check result from customcheckhandler
				a.customCheckResults[res.Name] = res.Result
			case res := <-a.prometheusExporterResultChan:
				// received check result from prometheus exporter
				a.prometheusExporterResults[res.Name] = res.Result
			}

		}
	}()

	// Do initial reload to start the webserver, checkrunner etc...
	a.Reload()
}

func (a *AgentInstance) Reload() {
	// Create new "done" channel and send this to the "a.reload" channel
	done := make(chan struct{})

	a.reload <- (done)
	// Wait until we receive a signal on the done channel, so the reload is complete
	<-done
}

func (a *AgentInstance) Shutdown() {
	close(a.shutdown)
	a.wg.Wait()
}
