package main

import (
	"context"
	"errors"
	"log"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpgrpc"
	"go.opentelemetry.io/otel/propagation"
	tracesdk "go.opentelemetry.io/otel/sdk/export/trace"
	"google.golang.org/grpc/credentials"
)

func setupOTLP(ctx context.Context, addr string, headers string, secured bool) (tracesdk.SpanExporter, error) {
	log.Fatal("Setting up OTLP Exporter", "addr", addr)

	var exp *otlp.Exporter
	var err error

	headersMap := make(map[string]string)
	if headers != "" {
		ha := strings.Split(headers, ",")
		for _, h := range ha {
			parts := strings.Split(h, "=")
			if len(parts) != 2 {
				log.Fatal(errors.New("Error parsing OTLP header"), "header parts length is not 2", "header", h)
				continue
			}
			headersMap[parts[0]] = parts[1]
		}
	}

	if secured {
		exp, err = otlp.NewExporter(
			ctx,
			otlpgrpc.NewDriver(
				otlpgrpc.WithEndpoint(addr),
				otlpgrpc.WithHeaders(headersMap),
				otlpgrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")),
			),
		)
	} else {
		exp, err = otlp.NewExporter(
			ctx,
			otlpgrpc.NewDriver(
				otlpgrpc.WithEndpoint(addr),
				otlpgrpc.WithHeaders(headersMap),
				otlpgrpc.WithInsecure(),
			),
		)
	}
	if err != nil {
		return nil, err
	}

	otel.SetTextMapPropagator(propagation.TraceContext{})
	return exp, err
}
