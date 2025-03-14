package main

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/afiskon/promtail-client/promtail"
	"github.com/simonfrey/jsonl"
	auditapi "k8s.io/apiserver/pkg/apis/audit/v1"
	"k8s.io/klog/v2"
)

func parseAuditLogAndSendToOLTP(path string, loki promtail.Client) error {
	eventCh := make(chan auditapi.Event)

	err := parseAuditLog(path, eventCh)
	if err != nil {
		return err
	}

	var errs []error

	klog.Infof("Sending audit log events from %s to Loki", path)
	for event := range eventCh {
		// Send to loki
		err := sendEventToLoki(loki, event)
		if err != nil {
			errs = append(errs, err)
		}
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

func sendEventToLoki(loki promtail.Client, event auditapi.Event) error {
	eventJson, err := json.Marshal(event)
	if err != nil {
		return err
	}
	loki.JSON(event.StageTimestamp.Time, string(eventJson))
	return nil
}
