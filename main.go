package main

import (
	"flag"
	"fmt"
	"net/url"
	"time"

	"github.com/afiskon/promtail-client/promtail"
	"k8s.io/klog/v2"
)

func main() {
	var (
		lokiAddr    string
		prowjob     string
		auditLogDir string
		debug       bool
	)
	flag.StringVar(&lokiAddr, "loki-addr", "http://localhost:9428/insert/loki/api/v1/push", "URL to push logs to")
	flag.StringVar(&prowjob, "prow-job", "", "prowjob URL")
	flag.StringVar(&auditLogDir, "audit-log-dir", "", "path to dir with audit logs")
	flag.BoolVar(&debug, "debug", false, "set to true to print sent logs")
	flag.Parse()

	prowjobUrl, err := url.Parse(prowjob)
	if err != nil {
		klog.Fatal(err)
	}

	if len(auditLogDir) == 0 {
		var err error
		auditLogDir, err = fetchAuditLogsFromProwJob(prowjobUrl)
		if err != nil {
			klog.Fatal(err)
		}
	}

	auditLogFiles, err := findAuditLogsInDir(auditLogDir)
	if err != nil {
		klog.Fatal(err)
	}
	for _, auditLogPath := range auditLogFiles {
		labels := fmt.Sprintf(`{prowjob="%s", filename="%s"}`, prowjob, auditLogPath)
		loki, err := prepareLoki(labels, lokiAddr, debug)
		if err != nil {
			klog.Fatal(err)
		}
		if err = parseAuditLogAndSendToOLTP(auditLogPath, loki); err != nil {
			klog.Warning(err)
		}
	}
	klog.Infof("Done")
}

func prepareLoki(labels, lokiAddr string, debug bool) (promtail.Client, error) {
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
	return promtail.NewClientProto(conf)
}
