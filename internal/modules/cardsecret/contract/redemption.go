package contract

import (
	"errors"
	"time"
)

var (
	ErrRedemptionNotFound = errors.New("redemption card not found")
	ErrRedemptionConflict = errors.New("redemption receipt or inventory conflict")
	ErrRedemptionInvalid  = errors.New("invalid redemption receipt")
)

type RedemptionReceipt struct {
	Version    int       `json:"version"`
	EventID    string    `json:"event_id"`
	EventType  string    `json:"event_type"`
	Source     string    `json:"source"`
	CDK        string    `json:"cdk"`
	OrderID    string    `json:"order_id"`
	RedeemedAt time.Time `json:"redeemed_at"`
}

type RedemptionRepository interface {
	RecordRedemption(RedemptionReceipt) error
}
