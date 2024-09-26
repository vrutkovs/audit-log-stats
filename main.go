package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"time"

	"github.com/afiskon/promtail-client/promtail"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/klog/v2"
)

func main() {
	var (
		otlpAddr    string
		lokiAddr    string
		prowjob     string
		auditLogDir string
	)
	flag.StringVar(&otlpAddr, "otlp-addr", "localhost:4317", "Address to send traces to")
	flag.StringVar(&lokiAddr, "loki-addr", "http://localhost:3100/api/prom/push", "URL to push logs to")
	flag.StringVar(&prowjob, "prow-job", "", "prowjob URL")
	flag.StringVar(&auditLogDir, "audit-log-dir", "", "path to dir with audit logs")
	flag.Parse()

	prowjobUrl, err := url.Parse(prowjob)
	if err != nil {
		klog.Fatal(err)
	}

	ctx := context.Background()
	tracer, shutdownTracerProvider, err := prepareTracer(ctx, otlpAddr)
	if err != nil {
		klog.Fatal(err)
	}
	defer func() {
		if err := shutdownTracerProvider(ctx); err != nil {
			klog.Errorf("failed to shutdown TracerProvider: %s", err)
		}
	}()

	labels := fmt.Sprintf(`{prowjob="%s"}`, prowjob)
	loki, err := prepareLoki(labels, lokiAddr)
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
		if err = parseAuditLogAndSendToOLTP(ctx, auditLogPath, tracer, loki); err != nil {
			klog.Warning(err)
		}
	}
	klog.Infof("Done")
}

func prepareTracer(ctx context.Context, otlpAddr string) (trace.Tracer, func(context.Context) error, error) {
	conn, err := initConn(otlpAddr)
	if err != nil {
		return nil, func(context.Context) error { return nil }, err
	}
	shutdownTracerProvider, err := setupOTLP(ctx, conn)
	if err != nil {
		return nil, func(context.Context) error { return nil }, err
	}

	name := "github.com/vrutkovs/audit-span"
	return otel.Tracer(name), shutdownTracerProvider, nil
}

func prepareLoki(labels, lokiAddr string) (promtail.Client, error) {
	conf := promtail.ClientConfig{
		PushURL:            lokiAddr,
		Labels:             labels,
		BatchWait:          5 * time.Second,
		BatchEntriesNumber: 10000,
		SendLevel:          promtail.DEBUG,
		PrintLevel:         promtail.DISABLE,
	}
	return promtail.NewClientProto(conf)
}
