package worker

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/wireops/wireops/internal/protocol"
)

const (
	TokenStatusStaging = "STAGING"
	TokenStatusActive  = "ACTIVE"
	TokenStatusRevoked = "REVOKED"
	TokenStatusExpired = "EXPIRED"
)

var (
	ErrTokenMissing = errors.New("worker token missing")
	ErrTokenInvalid = errors.New("worker token invalid")
	ErrTokenExpired = errors.New("worker token expired")
	ErrTokenRevoked = errors.New("worker token revoked")
)

type Service struct {
	app core.App
	mu  sync.Mutex
}

func NewService(app core.App) *Service {
	return &Service{app: app}
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// IssueToken creates a new staging token valid for one hour.
func (s *Service) IssueToken(createdBy string) (string, *core.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	collection, err := s.app.FindCollectionByNameOrId("worker_tokens")
	if err != nil {
		return "", nil, err
	}

	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", nil, err
	}
	token := hex.EncodeToString(bytes)

	record := core.NewRecord(collection)
	record.Set("token_hash", HashToken(token))
	record.Set("status", TokenStatusStaging)
	record.Set("expires_at", time.Now().UTC().Add(time.Hour))
	record.Set("created_by", createdBy)

	if err := s.app.Save(record); err != nil {
		return "", nil, err
	}

	log.Printf("[WORKER] Issued new worker token %s. Expires in 1 hour.", record.Id)
	return token, record, nil
}

func (s *Service) ActivateToken(token string, hostname string) (*core.Record, *core.Record, error) {
	if token == "" {
		return nil, nil, ErrTokenMissing
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tokenRecord, err := s.findTokenRecordByHash(HashToken(token))
	if err != nil {
		if errors.Is(err, ErrTokenInvalid) {
			return nil, nil, ErrTokenInvalid
		}
		return nil, nil, err
	}

	now := time.Now().UTC()
	switch tokenRecord.GetString("status") {
	case TokenStatusRevoked:
		return nil, nil, ErrTokenRevoked
	case TokenStatusExpired:
		return nil, nil, ErrTokenExpired
	case TokenStatusStaging:
		if now.After(tokenRecord.GetDateTime("expires_at").Time()) {
			tokenRecord.Set("status", TokenStatusExpired)
			tokenRecord.Set("last_used_at", now)
			_ = s.app.Save(tokenRecord)
			return nil, nil, ErrTokenExpired
		}
	case TokenStatusActive:
	default:
		return nil, nil, ErrTokenInvalid
	}

	workerID := tokenRecord.GetString("worker")
	if workerID == "" {
		workerRecord, err := s.registerRemoteWorker(hostname, tokenRecord.Id)
		if err != nil {
			return nil, nil, err
		}
		workerID = workerRecord.Id
		tokenRecord.Set("worker", workerID)
		tokenRecord.Set("status", TokenStatusActive)
		tokenRecord.Set("last_used_at", now)
		tokenRecord.Set("expires_at", nil)
		if err := s.app.Save(tokenRecord); err != nil {
			return nil, nil, err
		}
		return workerRecord, tokenRecord, nil
	}

	workerRecord, err := s.app.FindRecordById("workers", workerID)
	if err != nil {
		return nil, nil, err
	}

	if tokenRecord.GetString("status") == TokenStatusStaging {
		tokenRecord.Set("status", TokenStatusActive)
		tokenRecord.Set("expires_at", nil)
	}
	tokenRecord.Set("last_used_at", now)
	if err := s.app.Save(tokenRecord); err != nil {
		return nil, nil, err
	}

	return workerRecord, tokenRecord, nil
}

func (s *Service) GetTokenForWorker(workerID string) (*core.Record, error) {
	records, err := s.app.FindAllRecords("worker_tokens", dbx.HashExp{"worker": workerID})
	if err != nil {
		return nil, err
	}

	for _, rec := range records {
		if rec.GetString("status") != TokenStatusRevoked {
			return rec, nil
		}
	}

	if len(records) == 0 {
		return nil, nil
	}
	return records[0], nil
}

func (s *Service) RevokeToken(recordID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.app.FindRecordById("worker_tokens", recordID)
	if err != nil {
		return err
	}
	record.Set("status", TokenStatusRevoked)
	record.Set("last_used_at", time.Now().UTC())
	return s.app.Save(record)
}

func (s *Service) RevokeTokensForWorker(workerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := s.app.FindAllRecords("worker_tokens", dbx.HashExp{"worker": workerID})
	if err != nil {
		return err
	}
	for _, record := range records {
		record.Set("status", TokenStatusRevoked)
		record.Set("last_used_at", time.Now().UTC())
		if err := s.app.Save(record); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ExpireStagingTokens() error {
	records, err := s.app.FindAllRecords("worker_tokens", dbx.HashExp{"status": TokenStatusStaging})
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	for _, record := range records {
		if now.After(record.GetDateTime("expires_at").Time()) {
			record.Set("status", TokenStatusExpired)
			record.Set("last_used_at", now)
			if err := s.app.Save(record); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) findTokenRecordByHash(tokenHash string) (*core.Record, error) {
	records, err := s.app.FindAllRecords("worker_tokens", dbx.HashExp{"token_hash": tokenHash})
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, ErrTokenInvalid
	}
	return records[0], nil
}

func (s *Service) registerRemoteWorker(hostname string, tokenID string) (*core.Record, error) {
	collection, err := s.app.FindCollectionByNameOrId("workers")
	if err != nil {
		return nil, err
	}

	record := core.NewRecord(collection)
	record.Set("hostname", hostnameOrUnknown(hostname))
	record.Set("fingerprint", "remote:"+tokenID)
	record.Set("status", "ACTIVE")

	if err := s.app.Save(record); err != nil {
		return nil, err
	}

	log.Printf("[WORKER] Registered new remote worker: %s (%s)", record.GetString("hostname"), record.Id)
	return record, nil
}

func hostnameOrUnknown(hostname string) string {
	if hostname == "" {
		return "unknown-worker"
	}
	return hostname
}

// RegisterWorker registers a new worker in the database.
func (s *Service) RegisterWorker(hostname, fingerprint string) (*core.Record, error) {
	collection, err := s.app.FindCollectionByNameOrId("workers")
	if err != nil {
		return nil, err
	}

	record := core.NewRecord(collection)
	record.Set("hostname", hostname)
	record.Set("fingerprint", fingerprint)
	record.Set("status", "ACTIVE")

	if err := s.app.Save(record); err != nil {
		return nil, err
	}

	log.Printf("[WORKER] Registered new worker: %s (%s)", hostname, record.Id)
	return record, nil
}

// UpdateLastSeen updates the last_seen timestamp and status of a worker.
func (s *Service) UpdateLastSeen(workerID string) error {
	record, err := s.app.FindRecordById("workers", workerID)
	if err != nil {
		return err
	}

	record.Set("last_seen", time.Now().UTC())
	if record.GetString("status") != "REVOKED" {
		record.Set("status", "ACTIVE")
	}
	return s.app.Save(record)
}

// RevokeWorker marks a worker as revoked.
func (s *Service) RevokeWorker(workerID string) error {
	record, err := s.app.FindRecordById("workers", workerID)
	if err != nil {
		return err
	}

	record.Set("status", "REVOKED")
	if err := s.app.Save(record); err != nil {
		return err
	}

	if err := s.RevokeTokensForWorker(workerID); err != nil {
		return err
	}

	log.Printf("[WORKER] Revoked worker: %s", workerID)
	return nil
}

// HealthEvent represents a single point in time health status for a worker.
type HealthEvent struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// RecordHealthEvent appends a new health status to the health_history JSON array, capping it at 10 events.
func (s *Service) RecordHealthEvent(workerID string, status string) error {
	record, err := s.app.FindRecordById("workers", workerID)
	if err != nil {
		return err
	}

	var history []HealthEvent
	_ = record.UnmarshalJSONField("health_history", &history)

	history = append(history, HealthEvent{
		Status:    status,
		Timestamp: time.Now().UTC(),
	})

	if len(history) > 10 {
		history = history[len(history)-10:]
	}

	record.Set("health_history", history)
	return s.app.Save(record)
}

// UpdateWorkerInfo updates worker record with docker, compose, OS, arch details.
func (s *Service) UpdateWorkerInfo(workerID string, info protocol.WorkerInfo) error {
	record, err := s.app.FindRecordById("workers", workerID)
	if err != nil {
		return err
	}
	record.Set("version", info.Version)
	record.Set("docker_version", info.DockerVersion)
	record.Set("compose_version", info.ComposeVersion)
	record.Set("os", info.OS)
	record.Set("arch", info.Arch)
	return s.app.Save(record)
}

// UpdateWorkerTelemetry updates worker record with hardware telemetry details.
func (s *Service) UpdateWorkerTelemetry(workerID string, t protocol.TelemetryInfo) error {
	record, err := s.app.FindRecordById("workers", workerID)
	if err != nil {
		return err
	}
	record.Set("cpu_usage", t.CPUUsagePercent)
	record.Set("memory_usage", t.MemoryUsagePercent)
	record.Set("disk_usage", t.DiskUsagePercent)
	record.Set("docker_online", t.DockerOnline)
	return s.app.Save(record)
}
