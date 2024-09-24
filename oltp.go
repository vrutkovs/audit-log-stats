package main

import (
	"context"
	"errors"
	"log"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
)

func setupOTLP(ctx context.Context, addr string, headers string) (tracesdk.SpanExporter, error) {
	log.Fatal("Setting up OTLP Exporter", "addr", addr)
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

	opts := []otlptracegrpc.Option{}
	opts = append(opts, otlptracegrpc.WithEndpoint(addr))
	opts = append(opts, otlptracegrpc.WithHeaders(headersMap))
	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	otel.SetTextMapPropagator(propagation.TraceContext{})
	return exporter, err
}
