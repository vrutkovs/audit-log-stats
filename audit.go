package main

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/afiskon/promtail-client/promtail"
	"github.com/simonfrey/jsonl"
	auditapi "k8s.io/apiserver/pkg/apis/audit/v1"
	"k8s.io/klog/v2"
)

func parseAuditLogAndSendToOLTP(path string, loki promtail.Client) error {
	var errs []error
	counter := 0
	eventCh := make(chan auditapi.Event)

	go func() {
		err := parseAuditLog(path, eventCh)
		if err != nil {
			errs = append(errs, fmt.Errorf("Failed to parse %s: %v", path, err))
		}
	}()

	for event := range eventCh {
		// Send to loki
		err := sendEventToLoki(loki, event)
		if err != nil {
			errs = append(errs, err)
		} else {
			counter++
		}
	}
	klog.Infof("Sent %d audit log events from %s to Loki", counter, path)
	return errors.Join(errs...)
}

func parseAuditLog(path string, eventCh chan<- auditapi.Event) error {
	defer close(eventCh)

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Attempt to read as gzip
	var reader io.Reader
	fz, err := gzip.NewReader(file)
	if err != nil {
		reader = file
	} else {
		reader = fz
		defer fz.Close()
	}

	r := jsonl.NewReader(reader)
	err = r.ReadLines(func(data []byte) error {
		var event auditapi.Event
		if err := json.Unmarshal(data, &event); err != nil {
			return err
		}
		eventCh <- event
		return nil
	})
	if err != nil {
		return err
	}
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
