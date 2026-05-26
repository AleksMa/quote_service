package storage

import (
	"context"
	"testing"
)

func TestOpenRejectsUnsupportedDriver(t *testing.T) {
	_, err := Open(context.Background(), Config{Driver: "memory"})
	if err == nil {
		t.Fatal("expected error")
	}
}
