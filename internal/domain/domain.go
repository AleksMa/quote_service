package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
)

var (
	ErrInvalidPair     = errors.New("invalid currency pair")
	ErrUnsupportedPair = errors.New("unsupported currency pair")
	ErrNotFound        = errors.New("not found")
	ErrAlreadyFinished = errors.New("already finished")
)

var (
	pairPattern = regexp.MustCompile(`^[A-Z]{3}/[A-Z]{3}$`)
)

type Pair struct {
	Raw   string
	Base  string
	Quote string
}

func ParsePair(input string, supported map[string]struct{}) (Pair, error) {
	raw := strings.ToUpper(strings.TrimSpace(input))
	if !pairPattern.MatchString(raw) {
		return Pair{}, ErrInvalidPair
	}
	if supported != nil {
		if _, ok := supported[raw]; !ok {
			return Pair{}, ErrUnsupportedPair
		}
	}
	return Pair{Raw: raw, Base: raw[:3], Quote: raw[4:]}, nil
}

func PairSet(pairs []string) (map[string]struct{}, error) {
	set := make(map[string]struct{}, len(pairs))
	for _, item := range pairs {
		pair, err := ParsePair(item, nil)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrInvalidPair, item)
		}
		set[pair.Raw] = struct{}{}
	}
	return set, nil
}

func IsUUID(input string) bool {
	_, err := uuid.Parse(strings.TrimSpace(input))
	return err == nil
}

func NewUUID() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

type UpdateRequest struct {
	ID         string
	JobID      string
	Pair       Pair
	Status     Status
	Price      *decimal.Decimal
	Provider   string
	Error      string
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

type UpdateJob struct {
	ID         string
	Pair       Pair
	Status     Status
	Price      *decimal.Decimal
	Provider   string
	Error      string
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

type LatestQuote struct {
	Pair      Pair
	Price     decimal.Decimal
	Provider  string
	UpdatedAt time.Time
	RequestID string
}
