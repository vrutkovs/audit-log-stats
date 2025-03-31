package main

import (
	"flag"
	"fmt"
	"net/url"
	"time"

	"github.com/afiskon/promtail-client/promtail"
	"github.com/sirupsen/logrus"
)

func main() {
	var (
		lokiAddr    string
		prowjob     string
		auditLogDir string
		debug       bool
	)
	logger := setupLogger()

	flag.StringVar(&lokiAddr, "loki-addr", "http://localhost:9428/insert/loki/api/v1/push", "URL to push logs to")
	flag.StringVar(&prowjob, "prow-job", "", "prowjob URL")
	flag.StringVar(&auditLogDir, "audit-log-dir", "", "path to dir with audit logs")
	flag.BoolVar(&debug, "debug", false, "set to true to print sent logs")
	flag.Parse()

	prowjobUrl, err := url.Parse(prowjob)
	if err != nil {
		logger.Fatal(err)
	}

	if len(auditLogDir) == 0 {
		var err error
		auditLogDir, err = fetchAuditLogsFromProwJob(logger, prowjobUrl)
		if err != nil {
			logger.Fatal(err)
		}
	}

	auditLogFiles, err := findAuditLogsInDir(logger, auditLogDir)
	if err != nil {
		logger.Fatal(err)
	}
	for _, auditLogPath := range auditLogFiles {
		labels := fmt.Sprintf(`{prowjob="%s", filename="%s"}`, prowjob, auditLogPath)
		loki, err := prepareLoki(logger, labels, lokiAddr, debug)
		if err != nil {
			logger.Fatal(err)
		}
		if err = parseAuditLogAndSendToOLTP(logger, auditLogPath, loki); err != nil {
			logger.Warning(err)
		}
	}
	logger.Info("Done")
}

func prepareLoki(logger *logrus.Logger, labels, lokiAddr string, debug bool) (promtail.Client, error) {
	printLevel := promtail.DISABLE
	if debug {
		printLevel = promtail.DEBUG
	}
	conf := promtail.ClientConfig{
		PushURL:            lokiAddr,
		Labels:             labels,
		BatchWait:          time.Second,
		BatchEntriesNumber: 10000,
		SendLevel:          promtail.DEBUG,
		PrintLevel:         printLevel,
	}
	return promtail.NewClientProto(conf, logger)
}

func setupLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		ForceColors: true, // Enable colors in the output
	})
	return logger
}
