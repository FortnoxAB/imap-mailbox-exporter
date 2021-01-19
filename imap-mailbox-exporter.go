package main

import (
	"crypto/tls"
	"flag"
	"net/http"
	"os"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

type ImapState struct {
	messagesCount float64
	up            int
}

type Exporter struct {
	mailserver string
	username   string
	password   string
	mailbox    string

	up            *prometheus.Desc
	messagesCount prometheus.Gauge
}

func NewExporter(mailserver, username, password string, mailbox string) *Exporter {
	return &Exporter{
		mailserver: mailserver,
		username:   username,
		password:   password,
		mailbox:    mailbox,

		up: prometheus.NewDesc(
			prometheus.BuildFQName("imap", "", "up"),
			"IMAP server is accessible and up",
			nil,
			map[string]string{
				"mailbox":  mailbox,
				"username": username,
			}),
		messagesCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "imap",
			Name:      "messages",
			Help:      "Current number of messages in mailbox",
			ConstLabels: map[string]string{
				"mailbox":  mailbox,
				"username": username,
			},
		}),
	}
}

func (exp *Exporter) Describe(ch chan<- *prometheus.Desc) {
	ch <- exp.up
	ch <- exp.messagesCount.Desc()
}

func (exp *Exporter) queryImapServer() ImapState {
	state := ImapState{}

	// Connect to the server
	imapClient, err := client.DialTLS(exp.mailserver, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		logrus.Error(err)
		return state
	}

	// Remember to log out and close the connection when finished
	defer imapClient.Logout()

	// Authenticate
	if imapClient.State() != imap.NotAuthenticatedState {
		logrus.Error("IMAP server in wrong state for Login!")
		return ImapState{}
	}
	err = imapClient.Login(exp.username, exp.password)
	if err != nil {
		logrus.Error(err)
		return ImapState{}
	}

	// Open a mailbox read-only (synchronous command - no need for imap.Wait)
	status, err := imapClient.Select(exp.mailbox, true)
	if err != nil {
		logrus.Error(err)
		return ImapState{}
	}

	state.up = 1
	state.messagesCount = float64(status.Messages)

	return state
}

func (exp *Exporter) Collect(ch chan<- prometheus.Metric) {
	state := exp.queryImapServer()
	exp.messagesCount.Set(state.messagesCount)
	ch <- exp.messagesCount
	ch <- prometheus.MustNewConstMetric(exp.up, prometheus.GaugeValue, float64(state.up))
}

var (
	imapServer   = flag.String("imap.server", os.Getenv("IMAP_SERVER"), "IMAP server to query")
	imapUsername = flag.String("imap.username", os.Getenv("IMAP_USERNAME"), "IMAP username for login")
	imapPassword = flag.String("imap.password", os.Getenv("IMAP_PASSWORD"), "IMAP password for login")
	imapMailbox  = flag.String("imap.mailbox", os.Getenv("IMAP_MAILBOX"), "IMAP mailbox to query")

	listenAddress   = flag.String("listen.address", os.Getenv("LISTEN_ADDRESS"), "")
	metricsEndpoint = flag.String("metrics.endpoint", os.Getenv("METRICS_ENDPOINT"), "")
)

func main() {
	flag.Parse()

	if *imapServer == "" {
		logrus.Fatal("Missing IMAP server configuration")
	}
	if *imapUsername == "" {
		logrus.Fatal("Missing IMAP username configuration")
	}
	if *imapPassword == "" {
		logrus.Fatal("Missing IMAP password configuration")
	}

	if *imapMailbox == "" {
		*imapMailbox = "INBOX"
	}
	if *listenAddress == "" {
		*listenAddress = ":9117"
	}
	if *metricsEndpoint == "" {
		*metricsEndpoint = "/metrics"
	}

	exporter := NewExporter(*imapServer, *imapUsername, *imapPassword, *imapMailbox)
	prometheus.MustRegister(exporter)

	http.Handle(*metricsEndpoint, promhttp.Handler())
	http.HandleFunc("/", func(writer http.ResponseWriter, req *http.Request) {
		_, err := writer.Write([]byte("<html><head><title>IMAP mailbox exporter</title></head><body><h1>IMAP mailbox exporter</h1></body></html>"))
		if err != nil {
			logrus.Error(err)
		}
	})

	logrus.Infof("Exporter listening on %s", *listenAddress)

	err := http.ListenAndServe(*listenAddress, nil)
	if err != nil {
		logrus.Error(err)
	}
}
