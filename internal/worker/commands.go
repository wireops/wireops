package worker

import (
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// LogCommandStart creates or updates a worker_commands record with status 'dispatched'.
func (s *Service) LogCommandStart(workerID, commandID, commandType string, payload interface{}) (*core.Record, error) {
	collection, err := s.app.FindCollectionByNameOrId("worker_commands")
	if err != nil {
		return nil, fmt.Errorf("LogCommandStart: find collection failed: %w", err)
	}

	var record *core.Record
	records, err := s.app.FindAllRecords("worker_commands", dbx.HashExp{"command_id": commandID})
	if err == nil && len(records) > 0 {
		record = records[0]
	} else {
		record = core.NewRecord(collection)
		record.Set("command_id", commandID)
	}

	record.Set("worker", workerID)
	record.Set("command_type", commandType)
	record.Set("status", "dispatched")
	record.Set("expires_at", time.Now().AddDate(0, 0, 7)) // TTL: 7 days
	record.Set("result", nil)
	record.Set("duration_ms", 0)

	if payload != nil {
		record.Set("payload", payload)
	}

	if err := s.app.Save(record); err != nil {
		return nil, fmt.Errorf("LogCommandStart: save failed: %w", err)
	}

	return record, nil
}

// LogCommandAck updates the status of a command to 'acked'.
func (s *Service) LogCommandAck(commandID string) error {
	records, err := s.app.FindAllRecords("worker_commands", dbx.HashExp{"command_id": commandID})
	if err != nil || len(records) == 0 {
		return fmt.Errorf("LogCommandAck: command %s not found", commandID)
	}

	record := records[0]
	record.Set("status", "acked")
	return s.app.Save(record)
}

// LogCommandFinish updates the command with its final status ('success' or 'error'),
// result payload (if any), and execution duration.
func (s *Service) LogCommandFinish(commandID string, status string, result interface{}, durationMs int64) error {
	records, err := s.app.FindAllRecords("worker_commands", dbx.HashExp{"command_id": commandID})
	if err != nil || len(records) == 0 {
		return fmt.Errorf("LogCommandFinish: command %s not found", commandID)
	}

	record := records[0]
	record.Set("status", status)
	record.Set("duration_ms", durationMs)
	if result != nil {
		record.Set("result", result)
	}

	return s.app.Save(record)
}
