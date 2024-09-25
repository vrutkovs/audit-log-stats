package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/afiskon/promtail-client/promtail"
	"github.com/simonfrey/jsonl"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	auditapi "k8s.io/apiserver/pkg/apis/audit/v1"
	"k8s.io/klog/v2"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func parseAuditLogAndSendToOLTP(ctx context.Context, path string, tracer trace.Tracer, loki promtail.Client) error {
	eventCh := make(chan auditapi.Event)

	err := parseAuditLog(path, eventCh)
	if err != nil {
		return err
	}

	var errs []error
	var auditIDToSpan = map[types.UID]context.Context{}

	klog.Infof("Sending audit log events to Jaeger")
	for event := range eventCh {
		currentCtx := ctx
		if existingCtx, found := auditIDToSpan[event.AuditID]; found {
			currentCtx = existingCtx
		}

		// Send to loki
		err := sendEventToLoki(loki, event)
		if err != nil {
			errs = append(errs, err)
		}

		// Convert to span, send to Tempo
		spanCtx, err := sendEventToTempo(currentCtx, tracer, event)
		if err != nil {
			errs = append(errs, err)
		}
		auditIDToSpan[event.AuditID] = spanCtx
	}
	return errors.Join(errs...)
}

func parseAuditLog(path string, eventCh chan<- auditapi.Event) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	r := jsonl.NewReader(file)
	go func() {
		err := r.ReadLines(func(data []byte) error {
			var event auditapi.Event
			if err := json.Unmarshal(data, &event); err != nil {
				return err
			}
			eventCh <- event
			return nil
		})
		if err != nil {
			klog.Fatal(err)
		}
		close(eventCh)
	}()
	return nil
}

func sendEventToTempo(ctx context.Context, tracer trace.Tracer, event auditapi.Event) (context.Context, error) {
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

func sendEventToLoki(loki promtail.Client, event auditapi.Event) error {
	eventJson, err := json.Marshal(event)
	if err != nil {
		return err
	}
	loki.JSON(string(eventJson))
	return nil
}
