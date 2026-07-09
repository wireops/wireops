package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"

	"github.com/wireops/wireops/internal/contextutil"
)

// LogCommandStart creates or updates a worker_commands record for a dispatch
// attempt. idempotencyKey stays stable across redelivery attempts of the same
// logical command (defaults to commandID); messageID identifies this specific
// delivery attempt and is used to correlate the worker's receipt ack.
func (s *Service) LogCommandStart(ctx context.Context, workerID, commandID, commandType string, payload interface{}) (*core.Record, error) {
	return s.logCommandDispatch(ctx, workerID, commandID, commandID, "", commandType, payload)
}

// LogCommandDispatch is the durable-queue variant of LogCommandStart: it lets
// callers pass an explicit messageID (unique per delivery attempt) and
// idempotencyKey (stable across redeliveries of the same logical command).
// Each call bumps attempt_count so retries/redispatches after reconnect are
// distinguishable from the first attempt.
func (s *Service) LogCommandDispatch(ctx context.Context, workerID, commandID, idempotencyKey, messageID, commandType string, payload interface{}) (*core.Record, error) {
	if idempotencyKey == "" {
		idempotencyKey = commandID
	}
	return s.logCommandDispatch(ctx, workerID, commandID, idempotencyKey, messageID, commandType, payload)
}

func (s *Service) logCommandDispatch(ctx context.Context, workerID, commandID, idempotencyKey, messageID, commandType string, payload interface{}) (*core.Record, error) {
	collection, err := s.app.FindCollectionByNameOrId("worker_commands")
	if err != nil {
		return nil, fmt.Errorf("LogCommandStart: find collection failed: %w", err)
	}

	var record *core.Record
	attemptCount := 1
	records, err := s.app.FindAllRecords("worker_commands", dbx.HashExp{"command_id": commandID})
	if err == nil && len(records) > 0 {
		record = records[0]
		attemptCount = int(record.GetFloat("attempt_count")) + 1
	} else {
		record = core.NewRecord(collection)
		record.Set("command_id", commandID)
	}

	record.Set("worker", workerID)
	record.Set("command_type", commandType)
	record.Set("status", "dispatched")
	record.Set("idempotency_key", idempotencyKey)
	record.Set("message_id", messageID)
	record.Set("attempt_count", attemptCount)
	record.Set("next_attempt_at", nil)
	record.Set("expires_at", time.Now().AddDate(0, 0, 7)) // TTL: 7 days
	record.Set("result", nil)
	record.Set("duration_ms", 0)

	if ctx != nil {
		if userID := contextutil.GetUserID(ctx); userID != "" {
			record.Set("created_by", userID)
		}
	}

	if payload != nil {
		record.Set("payload", payload)
	}

	if err := s.app.Save(record); err != nil {
		return nil, fmt.Errorf("LogCommandStart: save failed: %w", err)
	}

	return record, nil
}

// LogCommandQueued persists a command as queued (not yet sent over the wire,
// e.g. because the worker is currently disconnected) so it survives a server
// restart and can be replayed later.
func (s *Service) LogCommandQueued(ctx context.Context, workerID, commandID, idempotencyKey, commandType string, payload interface{}, nextAttemptAt time.Time) (*core.Record, error) {
	collection, err := s.app.FindCollectionByNameOrId("worker_commands")
	if err != nil {
		return nil, fmt.Errorf("LogCommandQueued: find collection failed: %w", err)
	}

	if idempotencyKey == "" {
		idempotencyKey = commandID
	}

	var record *core.Record
	records, err := s.app.FindAllRecords("worker_commands", dbx.HashExp{"command_id": commandID})
	if err == nil && len(records) > 0 {
		record = records[0]
	} else {
		record = core.NewRecord(collection)
		record.Set("command_id", commandID)
		record.Set("attempt_count", 0)
	}

	record.Set("worker", workerID)
	record.Set("command_type", commandType)
	record.Set("status", "queued")
	record.Set("idempotency_key", idempotencyKey)
	record.Set("next_attempt_at", nextAttemptAt)
	record.Set("expires_at", time.Now().AddDate(0, 0, 7))

	if ctx != nil {
		if userID := contextutil.GetUserID(ctx); userID != "" {
			record.Set("created_by", userID)
		}
	}

	if payload != nil {
		record.Set("payload", payload)
	}

	if err := s.app.Save(record); err != nil {
		return nil, fmt.Errorf("LogCommandQueued: save failed: %w", err)
	}

	return record, nil
}

// LogCommandAck marks the command matching the given messageID as 'acked',
// meaning the worker has received the envelope and is about to execute it.
// It is a no-op (not an error) if the message is unknown or the command
// already reached a terminal state, since acks can race with results.
func (s *Service) LogCommandAck(messageID string) error {
	if messageID == "" {
		return nil
	}
	records, err := s.app.FindAllRecords("worker_commands", dbx.HashExp{"message_id": messageID})
	if err != nil || len(records) == 0 {
		return nil
	}

	record := records[0]
	switch record.GetString("status") {
	case "success", "error", "timed_out", "cancelled":
		return nil
	}
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
	record.Set("next_attempt_at", nil)
	if result != nil {
		record.Set("result", result)
	}

	return s.app.Save(record)
}

// PendingCommandsForWorker returns worker_commands rows for the given worker
// that have not yet reached a terminal state, ordered oldest first so replay
// on reconnect preserves the original dispatch order.
func (s *Service) PendingCommandsForWorker(workerID string) ([]*core.Record, error) {
	return s.app.FindRecordsByFilter(
		"worker_commands",
		"worker = {:worker} && (status = 'queued' || status = 'dispatched' || status = 'acked')",
		"+created",
		0,
		0,
		dbx.Params{"worker": workerID},
	)
}
