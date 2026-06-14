package spool

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wireops/wireops/internal/protocol"
	"golang.org/x/crypto/hkdf"
)

const (
	keyInfo          = "wireops-worker-spool"
	filePerm         = 0o600
	dirPerm          = 0o700
	pendingDirName   = "pending"
	quarantineDirName = "quarantine"
)

type Store struct {
	root          string
	pendingDir    string
	quarantineDir string
	aead          cipher.AEAD
}

type storedMessage struct {
	MessageID  string    `json:"message_id"`
	Kind       string    `json:"kind"`
	CreatedAt  time.Time `json:"created_at"`
	Attempts   int       `json:"attempts"`
	Nonce      string    `json:"nonce"`
	Ciphertext string    `json:"ciphertext"`
}

type PendingMessage struct {
	MessageID string
	Kind      string
	CreatedAt time.Time
	Attempts  int
	Envelope  protocol.Envelope
}

func New(rootDir, token string) (*Store, error) {
	if strings.TrimSpace(rootDir) == "" {
		return nil, fmt.Errorf("spool root directory is required")
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("worker token is required to derive spool key")
	}

	key, err := deriveKey(token)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	root := filepath.Join(rootDir, "spool")
	store := &Store{
		root:          root,
		pendingDir:    filepath.Join(root, pendingDirName),
		quarantineDir: filepath.Join(root, quarantineDirName),
		aead:          aead,
	}

	for _, dir := range []string{store.root, store.pendingDir, store.quarantineDir} {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return nil, err
		}
	}

	return store, nil
}

func deriveKey(token string) ([]byte, error) {
	reader := hkdf.New(sha256.New, []byte(token), nil, []byte(keyInfo))
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("derive spool key: %w", err)
	}
	return key, nil
}

func (s *Store) Enqueue(messageID, kind string, env protocol.Envelope) error {
	if messageID == "" {
		return fmt.Errorf("message id is required")
	}
	payload, err := json.Marshal(env)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	record := storedMessage{
		MessageID:  messageID,
		Kind:       kind,
		CreatedAt:  time.Now().UTC(),
		Attempts:   0,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(s.aead.Seal(nil, nonce, payload, []byte(messageID))),
	}
	return s.writeRecord(record)
}

func (s *Store) MarkAttempt(messageID string) error {
	record, path, err := s.readRecord(messageID)
	if err != nil {
		return err
	}
	record.Attempts++
	return s.writeRecordAt(path, record)
}

func (s *Store) Ack(messageID string) error {
	if messageID == "" {
		return nil
	}
	path := s.pathForMessage(messageID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Store) Pending() ([]PendingMessage, error) {
	entries, err := os.ReadDir(s.pendingDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var pending []PendingMessage
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		messageID := strings.TrimSuffix(entry.Name(), ".json")
		record, _, err := s.readRecord(messageID)
		if err != nil {
			_ = s.quarantineMessage(messageID)
			continue
		}
		env, err := s.decryptEnvelope(record)
		if err != nil {
			_ = s.quarantineMessage(messageID)
			continue
		}
		pending = append(pending, PendingMessage{
			MessageID: record.MessageID,
			Kind:      record.Kind,
			CreatedAt: record.CreatedAt,
			Attempts:  record.Attempts,
			Envelope:  env,
		})
	}

	sort.Slice(pending, func(i, j int) bool {
		if pending[i].CreatedAt.Equal(pending[j].CreatedAt) {
			return pending[i].MessageID < pending[j].MessageID
		}
		return pending[i].CreatedAt.Before(pending[j].CreatedAt)
	})

	return pending, nil
}

func (s *Store) CountPending() (int, error) {
	entries, err := os.ReadDir(s.pendingDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			count++
		}
	}
	return count, nil
}

func (s *Store) Purge() error {
	return os.RemoveAll(s.root)
}

func (s *Store) pathForMessage(messageID string) string {
	return filepath.Join(s.pendingDir, messageID+".json")
}

func (s *Store) writeRecord(record storedMessage) error {
	return s.writeRecordAt(s.pathForMessage(record.MessageID), record)
}

func (s *Store) writeRecordAt(path string, record storedMessage) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, filePerm); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (s *Store) readRecord(messageID string) (storedMessage, string, error) {
	path := s.pathForMessage(messageID)
	data, err := os.ReadFile(path)
	if err != nil {
		return storedMessage{}, path, err
	}
	var record storedMessage
	if err := json.Unmarshal(data, &record); err != nil {
		return storedMessage{}, path, err
	}
	return record, path, nil
}

func (s *Store) decryptEnvelope(record storedMessage) (protocol.Envelope, error) {
	var env protocol.Envelope
	nonce, err := base64.StdEncoding.DecodeString(record.Nonce)
	if err != nil {
		return env, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(record.Ciphertext)
	if err != nil {
		return env, err
	}
	plaintext, err := s.aead.Open(nil, nonce, ciphertext, []byte(record.MessageID))
	if err != nil {
		return env, err
	}
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.UseNumber()
	if err := decoder.Decode(&env); err != nil {
		return env, err
	}
	return env, nil
}

func (s *Store) quarantineMessage(messageID string) error {
	path := s.pathForMessage(messageID)
	dst := filepath.Join(s.quarantineDir, messageID+"-"+time.Now().UTC().Format("20060102150405")+".json")
	if err := os.MkdirAll(s.quarantineDir, dirPerm); err != nil {
		return err
	}
	if err := os.Rename(path, dst); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return nil
}
