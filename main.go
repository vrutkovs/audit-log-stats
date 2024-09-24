package main

import (
	"context"
	"flag"
	"log"
	"os"
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
	spanExporter, err := setupOTLP(ctx, otlpAddr, otlpHeaders)
	if err != nil {
		log.Fatal(err, "unable to set up tracing")
		os.Exit(1)
	}
	defer spanExporter.Shutdown(ctx)

	if err := parseAuditLogAndSendToOLTP(auditLogPath, spanExporter); err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
	log.Print("Done!")
}
