package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/micromdm/micromdm/mdm"
	"github.com/micromdm/micromdm/workflow/webhook"
	"github.com/sirupsen/logrus"
)

// Device represents a device
type Device struct {
	UDID     string
	Enrolled bool
}

// Server represents an MDM server
type Server struct {
	MDMServerURL string
	MDMAPIKey    string
	Devices      map[string]Device
}

// Command represents an MDM command
type Command struct {
	UDID        string `json:"udid"`
	RequestType string `json:"request_type"`
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	var event webhook.Event
	err := json.NewDecoder(r.Body).Decode(&event)
	if err != nil {
		logrus.Errorf("decode JSON: %v", err)
		http.Error(w, fmt.Sprintf("decode JSON: %v", err), http.StatusBadRequest)
		return
	}

	switch event.Topic {
	case mdm.AuthenticateTopic:
		s.handleAuthenticate(event, w)
	case mdm.TokenUpdateTopic:
		s.handleTokenUpdate(event, w)
	case mdm.ConnectTopic:
		s.handleConnect(event, w)
	case mdm.CheckoutTopic:
		s.handleCheckOut(event, w)
	default:
		logrus.Warnf("The event's topic was not mdm.Authenticate, mdm.TokenUpdate, mdm.Connect, or mdm.Checkout. It was %q", event.Topic)
	}
}

// Authenticate messages are sent when the device is installing a MDM payload.
func (s *Server) handleAuthenticate(event webhook.Event, w http.ResponseWriter) {
	logrus.Infof("handleAuthenticate for event %+v", event)
	if event.CheckinEvent == nil {
		logrus.Error("The event has no CheckinEvent")
		http.Error(w, "The event has no CheckinEvent", http.StatusBadRequest)
		return
	}

	d, exists := s.Devices[event.CheckinEvent.UDID]
	d.UDID = event.CheckinEvent.UDID
	d.Enrolled = false
	s.Devices[event.CheckinEvent.UDID] = d

	if exists {
		log.Println("re-enrolling device", d.UDID)
	} else {
		log.Println("enrolling new device", d.UDID)
	}
}

// A device sends a token update message to the MDM server whenever its device
// push token, push magic, or unlock token change. The device sends an initial
// token update message to the server when it has installed the MDM payload.
// The server should send push messages to the device only after receiving the
// first token update message.
func (s *Server) handleTokenUpdate(event webhook.Event, w http.ResponseWriter) {
	logrus.Infof("handleTokenUpdate for event %+v", event)
	if event.CheckinEvent == nil {
		logrus.Error("The event has no CheckinEvent")
		http.Error(w, "The event has no CheckinEvent", http.StatusBadRequest)
		return
	}

	d := s.Devices[event.CheckinEvent.UDID]
	d.UDID = event.CheckinEvent.UDID
	d.Enrolled = true
	s.Devices[event.CheckinEvent.UDID] = d

	s.sendCommandToDevice(d, "InstalledApplicationList")
}

// Connect events occur when a device is responding to a MDM command. They
// contain the raw responses from the device.
//
// https://developer.apple.com/enterprise/documentation/MDM-Protocol-Reference.pdf
func (s *Server) handleConnect(event webhook.Event, w http.ResponseWriter) {
	logrus.Infof("handleConnect for event %+v", event)
	if event.AcknowledgeEvent == nil {
		logrus.Error("The event has no AcknowledgeEvent")
		http.Error(w, "The event has no AcknowledgeEvent", http.StatusBadRequest)
		return
	}

	xml := string(event.AcknowledgeEvent.RawPayload)
	if strings.Contains(xml, "InstalledApplicationList") {
		log.Println(xml)
	}
}

// In iOS 5.0 and later, and in macOS v10.9, if the CheckOutWhenRemoved key in
// the MDM payload is set to true, the device attempts to send a CheckOut
// message when the MDM profile is removed.
func (s *Server) handleCheckOut(event webhook.Event, w http.ResponseWriter) {
	logrus.Infof("handeCheckOUt for event %+v", event)
	if event.CheckinEvent == nil {
		logrus.Error("The event has no CheckinEvent")
		http.Error(w, "The event has no CheckinEvent", http.StatusBadRequest)
		return
	}

	d := s.Devices[event.CheckinEvent.UDID]
	d.UDID = event.CheckinEvent.UDID
	d.Enrolled = false
	s.Devices[event.CheckinEvent.UDID] = d
}

func (s *Server) sendCommandToDevice(d Device, requestType string) {
	c := Command{
		UDID:        d.UDID,
		RequestType: requestType,
	}
	b := new(bytes.Buffer)
	json.NewEncoder(b).Encode(c)

	client := &http.Client{}
	req, err := http.NewRequest("POST", s.MDMServerURL+"/v1/commands", b)
	req.SetBasicAuth("micromdm", s.MDMAPIKey)
	_, err = client.Do(req)
	if err != nil {
		logrus.Errorf("send command to device: %v", err)
		log.Fatal(err)
	}
}

func main() {
	var (
		flPort      = flag.Int("port", 80, "port for the webhook server to listen on")
		flServerURL = flag.String("server-url", "", "public HTTPS url of your MicroMDM server")
		flAPIKey    = flag.String("api-token", "", "API Token for your MicroMDM server")
	)
	flag.Parse()

	if *flServerURL == "" || *flAPIKey == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}

	s := &Server{
		MDMServerURL: strings.TrimRight(*flServerURL, "/"),
		MDMAPIKey:    *flAPIKey,
		Devices:      make(map[string]Device),
	}

	log.Println("webhook server listening on port", *flPort)
	http.HandleFunc("/webhook", s.handleWebhook)

	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "Hello, world!")
	})

	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(*flPort), nil))
}
