package domain

import (
	"errors"
	"testing"
)

func TestParsePair(t *testing.T) {
	supported := map[string]struct{}{"EUR/USD": {}, "USD/MXN": {}}

	pair, err := ParsePair(" eur/usd ", supported)
	if err != nil {
		t.Fatalf("ParsePair returned error: %v", err)
	}
	if pair.Raw != "EUR/USD" || pair.Base != "EUR" || pair.Quote != "USD" {
		t.Fatalf("unexpected pair: %+v", pair)
	}
}

func TestParsePairRejectsInvalid(t *testing.T) {
	_, err := ParsePair("EURUSD", nil)
	if !errors.Is(err, ErrInvalidPair) {
		t.Fatalf("expected ErrInvalidPair, got %v", err)
	}
}

func TestParsePairRejectsUnsupported(t *testing.T) {
	supported := map[string]struct{}{"EUR/USD": {}}
	_, err := ParsePair("USD/MXN", supported)
	if !errors.Is(err, ErrUnsupportedPair) {
		t.Fatalf("expected ErrUnsupportedPair, got %v", err)
	}
}

func TestNewUUID(t *testing.T) {
	id, err := NewUUID()
	if err != nil {
		t.Fatalf("NewUUID returned error: %v", err)
	}
	if !IsUUID(id) {
		t.Fatalf("generated invalid UUID: %s", id)
	}
}
