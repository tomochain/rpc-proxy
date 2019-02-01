package main

import (
	"flag"
	"fmt"
	log "github.com/inconshreveable/log15"
	"github.com/tomochain/proxy/config"
	"net/http"
	"net/url"
)

var (
	NWorkers   = flag.Int("n", 16, "The number of workers to start")
	HTTPAddr   = flag.String("http", "0.0.0.0:3000", "Address to listen for HTTP requests on")
	ConfigFile = flag.String("config", "./config/default.json", "Path to config file")
)

func main() {
	// Parse the command-line flags.
	flag.Parse()

	// setup config
	config.Init(*ConfigFile)
	c := config.GetConfig()
	var urls []*url.URL
	for i := 0; i < len(c.Masternode); i++ {
		url, _ := url.Parse(c.Masternode[i])
		urls = append(urls, url)
	}
	backend.Masternode = urls
	for i := 0; i < len(c.Fullnode); i++ {
		url, _ := url.Parse(c.Fullnode[i])
		urls = append(urls, url)
	}
	backend.Fullnode = urls

	// setup log
	logger := log.New("module", "app/server")

	logger.Debug("Starting the dispatcher")
	StartDispatcher(*NWorkers)

	logger.Debug("Registering the collector")
	http.HandleFunc("/", Collector)

	log.Info("HTTP server listening on", "addr", *HTTPAddr)
	if err := http.ListenAndServe(*HTTPAddr, nil); err != nil {
		fmt.Println(err.Error())
	}
}
