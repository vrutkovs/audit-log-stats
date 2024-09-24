package main

import (
	"errors"

	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	auditapi "k8s.io/apiserver/pkg/apis/audit/v1"
)

func parseAuditLogAndSendToOLTP(path string, spanExporter tracesdk.SpanExporter) error {
	events, err := parseAuditLog(path)
	if err != nil {
		return err
	}

	var errs []error
	for _, event := range events {
		err := sendEvent(spanExporter, event)
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func parseAuditLog(path string) ([]auditapi.Event, error) {
	return nil, nil
}

func sendEvent(spanExporter tracesdk.SpanExporter, event auditapi.Event) error {
	return nil
}
