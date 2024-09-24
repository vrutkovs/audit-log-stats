package main

import (
	"context"
	"flag"
	"log"
	"os"

	"go.opentelemetry.io/otel"
)

func main() {
	var (
		metricsAddr string
		otlpAddr    string
		otlpHeaders string

		auditLogPath string
	)
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&otlpAddr, "otlp-addr", "otlp-collector.default:55680", "Address to send traces to")
	flag.StringVar(&otlpHeaders, "otlp-headers", "", "Add headers key/values pairs to OTLP communication")
	flag.StringVar(&auditLogPath, "audit-log-path", "", "Path to audit log")
	flag.Parse()

	ctx := context.Background()
	shutdownTracerProvider, err := setupOTLP(ctx, otlpAddr, otlpHeaders)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := shutdownTracerProvider(ctx); err != nil {
			log.Fatalf("failed to shutdown TracerProvider: %s", err)
		}
	}()

	name := "github.com/vrutkovs/audit-span"
	tracer := otel.Tracer(name)

	if err := parseAuditLogAndSendToOLTP(ctx, auditLogPath, tracer); err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
	log.Print("Done!")
}
