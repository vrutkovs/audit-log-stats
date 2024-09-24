package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/simonfrey/jsonl"
	auditapi "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func parseAuditLogAndSendToOLTP(ctx context.Context, path string, tracer trace.Tracer) error {
	events, err := parseAuditLog(path)
	if err != nil {
		return err
	}

	var errs []error
	for _, event := range events {
		err := sendEvent(ctx, tracer, event)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func parseAuditLog(path string) ([]auditapi.Event, error) {
	result := []auditapi.Event{}

	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	r := jsonl.NewReader(file)
	err = r.ReadLines(func(data []byte) error {
		var event auditapi.Event
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		result = append(result, event)
		return nil
	})
	return result, err
}

func sendEvent(ctx context.Context, tracer trace.Tracer, event auditapi.Event) error {
	if event.ObjectRef == nil || event.ResponseStatus == nil {
		return nil
	}
	attrs := []attribute.KeyValue{
		attribute.String("audit-id", string(event.AuditID)),
		attribute.String("request-uri", string(event.RequestURI)),
		attribute.String("code", string(event.ResponseStatus.Code)),
		attribute.String("apigroup", event.ObjectRef.APIGroup),
		attribute.String("apiversion", event.ObjectRef.APIVersion),
		attribute.String("resource", event.ObjectRef.Resource),
		attribute.String("resource-version", event.ObjectRef.ResourceVersion),
		attribute.String("namespace", event.ObjectRef.Namespace),
		attribute.String("name", event.ObjectRef.Name),
	}

	spanName := fmt.Sprintf("%s.%s", event.ObjectRef.Resource, event.Verb)
	_, span := tracer.Start(ctx, spanName, trace.WithTimestamp(event.RequestReceivedTimestamp.Time))
	span.SetAttributes(attrs...)
	span.End(trace.WithTimestamp(event.StageTimestamp.Time))
	// exporter.ExportSpans(ctx, []*tracesdk.SpanSnapshot{spanData})
	return nil
}
