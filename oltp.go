package main

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/klog/v2"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

func initConn(otlpAddr string) (*grpc.ClientConn, error) {
	klog.Infof("Connecting to %s", otlpAddr)
	conn, err := grpc.NewClient(otlpAddr,
		// Note the use of insecure transport here. TLS is recommended in production.
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to collector: %w", err)
	}

	return conn, err
}

func setupOTLP(ctx context.Context, conn *grpc.ClientConn) (func(context.Context) error, error) {
	var err error

	opts := []otlptracegrpc.Option{}
	opts = append(opts, otlptracegrpc.WithGRPCConn(conn))
	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}
	bsp := tracesdk.NewBatchSpanProcessor(exporter)
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("apiserver"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}
	tracerProvider := tracesdk.NewTracerProvider(
		// tracesdk.WithIDGenerator(x),
		tracesdk.WithResource(res),
		tracesdk.WithSampler(tracesdk.AlwaysSample()),
		tracesdk.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tracerProvider.Shutdown, nil
}
