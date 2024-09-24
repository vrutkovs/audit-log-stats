package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/simonfrey/jsonl"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	auditapi "k8s.io/apiserver/pkg/apis/audit/v1"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func parseAuditLogAndSendToOLTP(ctx context.Context, path string, tracer trace.Tracer) error {
	events, err := parseAuditLog(path)
	logrus.Infof("Found %d audit log events", len(events))
	if err != nil {
		return err
	}

	var errs []error
	var auditIDToSpan = map[types.UID]context.Context{}

	logrus.Infof("Sending audit log events to Jaeger")
	for _, event := range events {
		currentCtx := ctx
		if existingCtx, found := auditIDToSpan[event.AuditID]; found {
			currentCtx = existingCtx
		}

		spanCtx, err := sendEvent(currentCtx, tracer, event)
		if err != nil {
			errs = append(errs, err)
		}
		auditIDToSpan[event.AuditID] = spanCtx
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

func sendEvent(ctx context.Context, tracer trace.Tracer, event auditapi.Event) (context.Context, error) {
	if event.ObjectRef == nil || event.ResponseStatus == nil {
		return ctx, nil
	}
	attrs := []attribute.KeyValue{
		attribute.String("audit-id", string(event.AuditID)),
		attribute.String("request-uri", string(event.RequestURI)),
		attribute.String("k8s.apigroup", event.ObjectRef.APIGroup),
		attribute.String("k8s.apiversion", event.ObjectRef.APIVersion),
		attribute.String("k8s.resource", event.ObjectRef.Resource),
		attribute.String("k8s.resource-version", event.ObjectRef.ResourceVersion),
		attribute.String("k8s.namespace", event.ObjectRef.Namespace),
		attribute.String("k8s.name", event.ObjectRef.Name),
		attribute.String("k8s.verb", event.Verb),
		attribute.String("k8s.user.name", event.User.Username),
		attribute.Int64("http.code", int64(event.ResponseStatus.Code)),
		attribute.String("http.user-agent", event.UserAgent),
	}
	statusCode := codes.Ok
	message := ""
	if event.ResponseStatus.Status == metav1.StatusFailure {
		statusCode = codes.Error
		message = event.ResponseStatus.Message
	}

	spanName := fmt.Sprintf("%s.%s", event.ObjectRef.Resource, event.Verb)
	spanCtx, span := tracer.Start(ctx, spanName, trace.WithTimestamp(event.RequestReceivedTimestamp.Time))
	span.SetAttributes(attrs...)
	span.SetStatus(statusCode, message)
	span.End(trace.WithTimestamp(event.StageTimestamp.Time))
	return spanCtx, nil
}
