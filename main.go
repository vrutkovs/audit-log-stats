package main

import (
	"context"
	"flag"
	"os"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
)

func main() {
	var (
		otlpAddr     string
		auditLogPath string
	)
	flag.StringVar(&otlpAddr, "otlp-addr", "otlp-collector.default:55680", "Address to send traces to")
	flag.StringVar(&auditLogPath, "audit-log-path", "", "Path to audit log")
	flag.Parse()

	ctx := context.Background()
	conn, err := initConn(otlpAddr)
	if err != nil {
		logrus.Fatal(err)
	}
	shutdownTracerProvider, err := setupOTLP(ctx, conn)
	if err != nil {
		logrus.Fatal(err)
	}
	defer func() {
		if err := shutdownTracerProvider(ctx); err != nil {
			logrus.Fatalf("failed to shutdown TracerProvider: %s", err)
		}
	}()

	name := "github.com/vrutkovs/audit-span"
	tracer := otel.Tracer(name)

	if err := parseAuditLogAndSendToOLTP(ctx, auditLogPath, tracer); err != nil {
		logrus.Fatal(err)
		os.Exit(1)
	}
	logrus.Info("Done!")
}
