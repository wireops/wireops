package remote

import (
	"testing"
)

func TestRegisterAndNewUnknownProvider(t *testing.T) {
	if _, err := New("does-not-exist", nil, nil); err == nil {
		t.Fatal("expected error for unknown storage provider")
	}
	if _, err := NewKMS("does-not-exist", nil, nil); err == nil {
		t.Fatal("expected error for unknown KMS provider")
	}
}

func TestRegisterPanicsOnDuplicate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic registering a duplicate storage provider")
		}
	}()
	Register("s3", func(map[string]any, map[string]any) (Storage, error) { return nil, nil })
}

func TestRegisterKMSPanicsOnDuplicate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic registering a duplicate KMS provider")
		}
	}()
	RegisterKMS("aws_kms", func(map[string]any, map[string]any) (KeyManager, error) { return nil, nil })
}
