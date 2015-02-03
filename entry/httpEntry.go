package entry

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strconv"

	"github.com/buaazp/uq/queue"
	"github.com/coreos/etcd/pkg/ioutils"
	"github.com/gorilla/mux"
)

type HttpEntry struct {
	Host          string
	Port          int
	maxBodyLength int
	server        *http.Server
	stopListener  *StopListener
	messageQueue  queue.MessageQueue
}

func NewHttpEntry(host string, port int, messageQueue queue.MessageQueue) (*HttpEntry, error) {
	h := new(HttpEntry)

	router := mux.NewRouter()
	router.HandleFunc("/create", h.createHandler).Methods("POST")
	router.HandleFunc("/pop/{topic}/{line}", h.popHandler).Methods("GET")
	router.HandleFunc("/push/{topic}", h.pushHandler).Methods("POST")
	router.HandleFunc("/confirm", h.confirmHandler).Methods("POST")

	addr := fmt.Sprintf("%s:%d", host, port)
	server := new(http.Server)
	server.Addr = addr
	server.Handler = router

	h.Host = host
	h.Port = port
	h.maxBodyLength = MaxBodyLength
	h.server = server
	h.messageQueue = messageQueue

	return h, nil
}

func (h *HttpEntry) createHandler(w http.ResponseWriter, req *http.Request) {
	limitedr := ioutils.NewLimitedBufferReader(req.Body, h.maxBodyLength)
	data, err := ioutil.ReadAll(limitedr)
	if err != nil {
		http.Error(w, "400 Bad Request!", http.StatusBadRequest)
		return
	}

	// len: 56
	// json: {"TopicName":"foo","LineName":"x","Recycle":10000000000}
	cr := new(queue.CreateRequest)
	err = json.Unmarshal(data, cr)
	if err != nil {
		log.Printf("create error: %s", err)
		http.Error(w, "400 Bad Request!", http.StatusBadRequest)
		return
	}

	err = h.messageQueue.Create(cr)
	if err != nil {
		log.Printf("create error: %s", err)
		http.Error(w, "500 Bad Request!", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HttpEntry) popHandler(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	t := vars["topic"]
	l := vars["line"]
	key := fmt.Sprintf("%s/%s", t, l)

	id, data, err := h.messageQueue.Pop(key)
	if err != nil || len(data) <= 0 {
		// log.Printf("pop error: %s", err)
		http.Error(w, "404 Not Found!", http.StatusNotFound)
		return
	} else {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-UQ-MessageID", strconv.FormatUint(id, 10))
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}

func (h *HttpEntry) pushHandler(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	t := vars["topic"]

	limitedr := ioutils.NewLimitedBufferReader(req.Body, h.maxBodyLength)
	data, err := ioutil.ReadAll(limitedr)
	if err != nil {
		http.Error(w, "400 Bad Request!", http.StatusBadRequest)
		return
	}

	err = h.messageQueue.Push(t, data)
	if err != nil {
		log.Printf("push error: %s", err)
		http.Error(w, "500 Bad Request!", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HttpEntry) confirmHandler(w http.ResponseWriter, req *http.Request) {
	limitedr := ioutils.NewLimitedBufferReader(req.Body, h.maxBodyLength)
	data, err := ioutil.ReadAll(limitedr)
	if err != nil {
		http.Error(w, "400 Bad Request!", http.StatusBadRequest)
		return
	}

	cr := new(queue.ConfirmRequest)
	err = json.Unmarshal(data, cr)
	if err != nil {
		log.Printf("confirm error: %s", err)
		http.Error(w, "400 Bad Request!", http.StatusBadRequest)
		return
	}

	err = h.messageQueue.Confirm(cr)
	if err != nil {
		log.Printf("confirm error: %s", err)
		http.Error(w, "500 Bad Request!", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HttpEntry) ListenAndServe() error {
	ln, err := net.Listen("tcp", h.server.Addr)
	if err != nil {
		return err
	}

	stopListener, err := NewStopListener(ln)
	if err != nil {
		return err
	}
	h.stopListener = stopListener

	return h.server.Serve(h.stopListener)
	// return h.server.ListenAndServe()
}

func (h *HttpEntry) Stop() {
	log.Printf("http entry stoping...")
	h.messageQueue.Close()
	h.stopListener.Stop()
}
