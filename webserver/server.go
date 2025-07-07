package webserver

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/openITCOCKPIT/openitcockpit-agent-go/config"
	"github.com/openITCOCKPIT/openitcockpit-agent-go/utils"
	log "github.com/sirupsen/logrus"
)

func testPortOpen(address string) bool {
	timeout := time.Second
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return false
	}
	defer func() {
		_ = conn.Close()
	}()
	return true
}

// Reloader interface contains a pointer to the agent instance the we can reload the agent on config push
type Reloader interface {
	Reload()
}

type reloadConfig struct {
	Configuration *config.Configuration
	// reloadDone will be set by the reload func
	reloadDone chan struct{}
}

// Server handling for http, should be created by New
type Server struct {
	StateInput      <-chan []byte
	PrometheusInput <-chan map[string]string
	Reloader        Reloader

	reload   chan *reloadConfig
	shutdown chan struct{}

	server  *http.Server
	handler *handler

	wg sync.WaitGroup
}

func isAutosslEnabled(cfg *config.Configuration) bool {
	if cfg.AutoSslEnabled {
		log.Debugln("Webserver: AutoSSL is enabled: checking for certificate files")
		if utils.FileNotExists(cfg.AutoSslCrtFile) {
			log.Infoln("Webserver: AutoSSL certificate is missing: ", cfg.AutoSslCrtFile)
			return false
		}
		if utils.FileNotExists(cfg.AutoSslKeyFile) {
			log.Infoln("Webserver: AutoSSL key is missing: ", cfg.AutoSslKeyFile)
			return false
		}
		if utils.FileNotExists(cfg.AutoSslCaFile) {
			log.Infoln("Webserver: AutoSSL ca certificate is missing: ", cfg.AutoSslCaFile)
			return false
		}
		return true
	}
	return false
}

func (s *Server) doReload(ctx context.Context, cfg *reloadConfig) {
	log.Infoln("Webserver: Reload")
	newHandler := &handler{
		StateInput:      s.StateInput,
		PrometheusInput: s.PrometheusInput,
		Configuration:   cfg.Configuration,
		Reloader:        s.Reloader,
	}
	newHandler.Start(ctx)
	serverAddr := fmt.Sprintf("%s:%d", cfg.Configuration.Address, cfg.Configuration.Port)
	log.Debugln("Webserver: Listening to ", serverAddr)
	timeout := time.Second * 30
	if cfg.Configuration.EnablePPROF {
		timeout = time.Hour
	}
	newServer := &http.Server{
		Addr:           serverAddr,
		Handler:        newHandler.Handler(),
		ReadTimeout:    timeout,
		WriteTimeout:   timeout,
		IdleTimeout:    timeout,
		MaxHeaderBytes: 256 * 1024,
	}

	if isAutosslEnabled(cfg.Configuration) || (cfg.Configuration.KeyFile != "" && cfg.Configuration.CertificateFile != "") {
		// the values for "intermediate" and "modern" are taken from https://ssl-config.mozilla.org/
		// also see https://wiki.mozilla.org/Security/Server_Side_TLS for more information about client compatibility
		var tlsConfig *tls.Config
		switch cfg.Configuration.TlsSecurityLevel {
		case "intermediate":
			log.Infoln("Webserver: Using intermediate TLS configuration")
			tlsConfig = &tls.Config{
				MinVersion: tls.VersionTLS12,
				CurvePreferences: []tls.CurveID{
					tls.X25519, // Go 1.8+
					tls.CurveP256,
					tls.CurveP384,
					//tls.x25519Kyber768Draft00, // Go 1.23+
				},
				CipherSuites: []uint16{
					tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
					tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
					tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
					tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
					tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
					tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
				},
			}
		case "modern":
			log.Infoln("Webserver: Using modern TLS configuration")
			tlsConfig = &tls.Config{
				MinVersion: tls.VersionTLS13,
				CurvePreferences: []tls.CurveID{
					tls.X25519, // Go 1.8+
					tls.CurveP256,
					tls.CurveP384,
					//tls.x25519Kyber768Draft00, // Go 1.23+
				},
			}
		default:
			// Lax or any other typo in the config file
			// Lax is the default behevior
			log.Infoln("Webserver: Using lax TLS configuration")
			tlsConfig = &tls.Config{
				MinVersion: tls.VersionTLS12,
			}
		}

		log.Debugln("Webserver: TLS enabled")

		certFilePath := cfg.Configuration.CertificateFile
		keyFilePath := cfg.Configuration.KeyFile
		caFilePath := ""
		if cfg.Configuration.AutoSslEnabled {
			log.Debugln("Webserver: Using AutoSSL certificates")

			certFilePath = cfg.Configuration.AutoSslCrtFile
			keyFilePath = cfg.Configuration.AutoSslKeyFile
			caFilePath = cfg.Configuration.AutoSslCaFile

			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		}
		pem := bytes.Buffer{}

		certPem, err := os.ReadFile(certFilePath)
		if err != nil {
			log.Fatalln("Webserver: Could not read server certificate: ", err)
		}
		pem.Write(certPem)
		pem.WriteByte('\n')
		keyPem, err := os.ReadFile(keyFilePath)
		if err != nil {
			log.Fatalln("Webserver: Could not read server key: ", err)
		}

		if caFilePath != "" {
			pool, caPem, err := utils.CertPoolFromFiles(caFilePath)
			if err != nil {
				log.Fatalln("Webserver: ", err)
			}
			tlsConfig.ClientCAs = pool
			log.Debugln("Webserver: Loaded ca certificate")
			pem.Write(caPem)
		}

		cert, err := tls.X509KeyPair(pem.Bytes(), keyPem)
		if err != nil {
			log.Fatal("Webserver: Could not load tls certificate: ", err)
		}
		log.Debugln("Webserver: Loaded server cerificate")

		tlsConfig.Certificates = []tls.Certificate{cert}

		newServer.TLSConfig = tlsConfig
		newServer.TLSNextProto = make(map[string]func(*http.Server, *tls.Conn, http.Handler))
	} else if cfg.Configuration.AutoSslEnabled {
		log.Infoln("Webserver: autossl enabled, but no certificates found")
	}

	s.close()
	s.handler = newHandler

	// test that old server stopped
	if s.server != nil {
		for i := 0; i < 30; i++ {
			if !testPortOpen(s.server.Addr) {
				break
			}
			time.Sleep(time.Second)
		}
	}

	// test if new port is really ready
	for i := 0; i < 30; i++ {
		if !testPortOpen(newServer.Addr) {
			break
		}
		time.Sleep(time.Second)
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		log.Infoln("Webserver: Starting http server")
		err := listenServe(newServer)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("Webserver: ", err)
		}
		log.Debugln("Webserver: http listener stopped")
	}()

	s.server = newServer
	for i := 0; i < 30; i++ {
		if testPortOpen(newServer.Addr) {
			break
		}
		time.Sleep(time.Second)
	}
	log.Debugln("Webserver: Reload complete")
	cfg.reloadDone <- struct{}{}
}

func (s *Server) close() {
	if s.server != nil {
		log.Debugln("Webserver: Stopping http server")
		_ = s.server.Close()
		s.server = nil
		log.Infoln("Webserver: Server stopped")
	}
	if s.handler != nil {
		log.Debugln("Webserver: Stopping handler")
		s.handler.Shutdown()
		s.handler = nil
		log.Debugln("Webserver: Handler stopped")
	}
}

func listenServe(server *http.Server) error {
	if server.TLSConfig != nil {
		return server.ListenAndServeTLS("", "")
	}
	return server.ListenAndServe()
}

// Reload webserver configuration
func (s *Server) Reload(cfg *config.Configuration) {
	done := make(chan struct{})

	// Send new configuration to webserver thread
	s.reload <- &reloadConfig{
		Configuration: cfg,
		reloadDone:    done,
	}

	//Relaod done - notify the caller
	<-done
}

// Shutdown webserver
func (s *Server) Shutdown() {
	close(s.shutdown)
	s.wg.Wait()
}

// Run the server routine (should NOT be run in a go routine)
// You have to call Reload at least once to really start the webserver
func (s *Server) Start(ctx context.Context) {
	s.shutdown = make(chan struct{})
	s.reload = make(chan *reloadConfig)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		defer s.close()

		for {
			select {
			case <-ctx.Done():
				return
			case _, more := <-s.shutdown:
				if !more {
					return
				}
			case newConfig := <-s.reload:
				//Go a reload signal from Reload func with new config - do reload
				s.doReload(ctx, newConfig)
			}
		}
	}()
}
