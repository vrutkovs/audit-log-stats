package main

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/afiskon/promtail-client/promtail"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	var (
		otlpAddr     string
		lokiAddr     string
		auditLogPath string
	)
	flag.StringVar(&otlpAddr, "otlp-addr", "localhost:4317", "Address to send traces to")
	flag.StringVar(&lokiAddr, "loki-addr", "http://localhost:3100/api/prom/push", "URL to push logs to")
	flag.StringVar(&auditLogPath, "audit-log-path", "", "Path to audit log")
	flag.Parse()

	ctx := context.Background()
	tracer, shutdownTracerProvider, err := prepareTracer(ctx, otlpAddr)
	if err != nil {
		logrus.Fatal(err)
	}
	defer func() {
		if err := shutdownTracerProvider(ctx); err != nil {
			logrus.Warnf("failed to shutdown TracerProvider: %s", err)
		}
	}()

	labels := fmt.Sprintf(`{filename="%s"}`, auditLogPath)
	loki, err := prepareLoki(labels, lokiAddr)
	if err != nil {
		logrus.Fatal(err)
	}
	if err = parseAuditLogAndSendToOLTP(ctx, auditLogPath, tracer, loki); err != nil {
		logrus.Fatal(err)
	}
	logrus.Infof("Done")
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
		SendLevel:          promtail.INFO,
		PrintLevel:         promtail.ERROR,
	}
	return promtail.NewClientProto(conf)
}
