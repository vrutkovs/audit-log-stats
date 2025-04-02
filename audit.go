package main

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/afiskon/promtail-client/promtail"
	"github.com/simonfrey/jsonl"
	"github.com/sirupsen/logrus"
	auditapi "k8s.io/apiserver/pkg/apis/audit/v1"
)

func parseAuditLogAndSendToOLTP(logger *logrus.Logger, path string, loki promtail.Client) error {
	var errs []error
	foundEvents := 0
	sentEvents := 0
	eventCh := make(chan auditapi.Event)

	logger.WithFields(logrus.Fields{"path": path}).Info("Parsing audit log")

	go func() {
		err := parseAuditLog(path, eventCh, logger)
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
			sentEvents++
		}
		foundEvents++
	}
	logger.WithFields(logrus.Fields{"found": foundEvents, "sent": sentEvents}).Info("Log events sent")
	return errors.Join(errs...)
}

func parseAuditLog(filepath string, eventCh chan<- auditapi.Event, logger *logrus.Logger) error {
	defer close(eventCh)

	file, err := os.Open(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Attempt to read as gzip
	var reader io.Reader
	if path.Ext(filepath) == ".gz" {
		fz, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer fz.Close()
		reader = fz
	} else {
		reader = file
	}

	lineNum := 0
	r := jsonl.NewReader(reader)
	err = r.ReadLines(func(data []byte) error {
		lineNum++
		var event auditapi.Event
		if err := json.Unmarshal(data, &event); err != nil {
			logger.WithFields(logrus.Fields{"error": err, "line": lineNum}).Error("Unable to unmarshal audit event")
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
