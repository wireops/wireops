package models

import "testing"

func TestCanonicalOrAliasLiteralPreservesWhitespace(t *testing.T) {
	got, err := CanonicalOrAliasLiteral("ERROR ", "", "pattern", "search")
	if err != nil || got != "ERROR " {
		t.Fatalf("CanonicalOrAliasLiteral() = %q, %v", got, err)
	}
	if _, err := CanonicalOrAliasLiteral("ERROR", "ERROR ", "pattern", "search"); err == nil {
		t.Fatal("expected conflicting literals to fail")
	}
}
