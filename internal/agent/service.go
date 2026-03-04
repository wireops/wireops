package agent

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

type Seat struct {
	Token     string
	ExpiresAt time.Time
}

type Service struct {
	app   core.App
	seats sync.Map
}

func NewService(app core.App) *Service {
	return &Service{
		app: app,
	}
}

// GenerateSeat creates a new short-lived bootstrap token.
func (s *Service) GenerateSeat() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)

	s.seats.Store(token, Seat{
		Token:     token,
		ExpiresAt: time.Now().Add(15 * time.Minute),
	})

	log.Printf("[AGENT] Generated new bootstrap seat. Expires in 15 minutes.")
	return token, nil
}

// ValidateAndConsumeSeat checks if the token is valid, not expired, and consumes it.
func (s *Service) ValidateAndConsumeSeat(token string) bool {
	val, ok := s.seats.LoadAndDelete(token)
	if !ok {
		return false
	}

	seat := val.(Seat)
	if time.Now().After(seat.ExpiresAt) {
		return false
	}

	return true
}

// RegisterAgent registers a new agent in the database.
func (s *Service) RegisterAgent(hostname, fingerprint string) (*core.Record, error) {
	collection, err := s.app.FindCollectionByNameOrId("agents")
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

	log.Printf("[AGENT] Registered new agent: %s (%s)", hostname, record.Id)
	return record, nil
}

// UpdateLastSeen updates the last_seen timestamp and status of an agent.
func (s *Service) UpdateLastSeen(agentID string) error {
	record, err := s.app.FindRecordById("agents", agentID)
	if err != nil {
		return err
	}

	record.Set("last_seen", time.Now().UTC())
	// Never promote a revoked agent back to ACTIVE.
	if record.GetString("status") != "REVOKED" {
		record.Set("status", "ACTIVE")
	}
	return s.app.Save(record)
}

// RevokeAgent marks an agent as revoked.
func (s *Service) RevokeAgent(agentID string) error {
	record, err := s.app.FindRecordById("agents", agentID)
	if err != nil {
		return err
	}

	record.Set("status", "REVOKED")
	if err := s.app.Save(record); err != nil {
		return err
	}

	log.Printf("[AGENT] Revoked agent: %s", agentID)
	return nil
}

// HealthEvent represents a single point in time health status for an agent.
type HealthEvent struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// RecordHealthEvent appends a new health status to the health_history JSON array, capping it at 20 events.
func (s *Service) RecordHealthEvent(agentID string, status string) error {
	record, err := s.app.FindRecordById("agents", agentID)
	if err != nil {
		return err
	}

	var history []HealthEvent
	// Ignore unmarshal errors, we just start fresh if the data is invalid or empty.
	_ = record.UnmarshalJSONField("health_history", &history)

	history = append(history, HealthEvent{
		Status:    status,
		Timestamp: time.Now().UTC(),
	})

	if len(history) > 10 {
		// Keep only the last 10
		history = history[len(history)-10:]
	}

	record.Set("health_history", history)
	return s.app.Save(record)
}
