package main

import (
	"bytes"
	"encoding/json"
	"errors"
	log "github.com/inconshreveable/log15"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"
)

type WorkRequest struct {
	W http.ResponseWriter
	R *http.Request
}

type HttpConnection struct {
	Request  *http.Request
	Response *http.Response
	Error    error
}

type JsonRpc struct {
	Method string
}

type HttpConnectionChannel chan *HttpConnection

var WorkerQueue chan chan WorkRequest

var WorkQueue = make(chan WorkRequest, 100)

var connChannel = make(HttpConnectionChannel)

func route(r *http.Request) (*url.URL, string, string, error) {
	body, _ := ioutil.ReadAll(r.Body)
	cacheKey := string(body)
	var b JsonRpc
	var url *url.URL
	err := json.Unmarshal(body, &b)
	if err != nil {
		return nil, "", "", err
	}
	if b.Method == "eth_sendRawTransaction" {
		max := len(backend.Masternode) - 1
		pointer.Masternode = point(pointer.Masternode, max)
		if pointer.Masternode > max {
			return nil, "", "", errors.New("No endpoint")
		}
		url = backend.Masternode[pointer.Masternode]
		log.Info("RPC request", "type", "masternode", "method", b.Method, "index", pointer.Masternode, "host", url.Host)
		cacheKey = ""
	} else {
		max := len(backend.Fullnode) - 1
		pointer.Fullnode = point(pointer.Fullnode, max)
		if pointer.Fullnode > max {
			return nil, "", "", errors.New("No endpoint")
		}
		url = backend.Fullnode[pointer.Fullnode]
		log.Info("RPC request", "type", "fullnode", "method", b.Method, "index", pointer.Fullnode, "max", max, "host", url.Host)
	}
	r.Body = ioutil.NopCloser(bytes.NewBuffer(body))
	return url, b.Method, cacheKey, err
}

func Collector(w http.ResponseWriter, r *http.Request) {

	url, method, cacheKey, err := route(r)
	if err != nil {
		log.Error("RPC request", "method", method, "error", err)
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	r.URL.Host = url.Host
	r.URL.Scheme = url.Scheme

	if c := storage.Get(cacheKey); c != nil && cacheKey != "" {
		log.Debug("Get from cache", "method", method, "key", cacheKey)
		w.Header().Set("Content-Type", "application/json")
		w.Write(c)
		return
	}

	// serve only JSON RPC request
	if r.Method != "POST" {
		w.WriteHeader(http.StatusOK)
		return
	}

	work := WorkRequest{W: w, R: r}

	// Push the work onto the queue.
	WorkQueue <- work
	log.Debug("Work request queued", "method", method)

	for {
		select {
		case conn := <-connChannel:
			if conn.Error != nil {
				log.Error("RPC response", "method", method, "error", conn.Error)
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			if conn.Response != nil {
				for k, v := range conn.Response.Header {
					w.Header().Set(k, v[0])
				}
				w.WriteHeader(conn.Response.StatusCode)
				body, _ := ioutil.ReadAll(conn.Response.Body)

				if d, err := time.ParseDuration(*CacheExpiration); err == nil {
					storage.Set(cacheKey, body, d)
				}

				w.Write(body)
				defer conn.Response.Body.Close()
			} else {
				w.WriteHeader(http.StatusOK)
			}
			return
		}
	}
}

func StartDispatcher(nworkers int) {
	// First, initialize the channel we are going to but the workers' work channels into.
	WorkerQueue = make(chan chan WorkRequest, nworkers)

	// Now, create all of our workers.
	for i := 0; i < nworkers; i++ {
		// log.Debugf("Starting worker %d", i+1)
		worker := NewWorker(i+1, WorkerQueue)
		worker.Start()
	}

	go func() {
		for {
			select {
			case work := <-WorkQueue:
				go func() {
					worker := <-WorkerQueue

					worker <- work
				}()
			}
		}
	}()
}

func NewWorker(id int, workerQueue chan chan WorkRequest) Worker {
	// Create, and return the worker.
	worker := Worker{
		ID:          id,
		Work:        make(chan WorkRequest),
		WorkerQueue: workerQueue,
		QuitChan:    make(chan bool),
	}

	return worker
}

type Worker struct {
	ID          int
	Work        chan WorkRequest
	WorkerQueue chan chan WorkRequest
	QuitChan    chan bool
}

func (w Worker) Start() {
	go func() {
		for {
			// Add ourselves into the worker queue.
			w.WorkerQueue <- w.Work

			select {
			case work := <-w.Work:
				ServeHTTP(work.W, work.R)

			case <-w.QuitChan:
				return
			}
		}
	}()
}

func (w Worker) Stop() {
	go func() {
		w.QuitChan <- true
	}()
}
