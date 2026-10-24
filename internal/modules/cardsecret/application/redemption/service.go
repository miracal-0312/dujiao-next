package redemption

import (
	"regexp"
	"strings"
	"time"

	"github.com/dujiao-next/internal/modules/cardsecret/contract"
)

var eventPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var sourcePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
var cdkPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{4,32}$`)

type Service struct{ repo contract.RedemptionRepository }

func New(repo contract.RedemptionRepository) *Service { return &Service{repo: repo} }

func (s *Service) Receive(receipt contract.RedemptionReceipt) error {
	if receipt.Version != 1 || receipt.EventType != "cdk.redeemed" || !eventPattern.MatchString(receipt.EventID) ||
		!sourcePattern.MatchString(receipt.Source) || !cdkPattern.MatchString(receipt.CDK) ||
		strings.TrimSpace(receipt.OrderID) == "" || len(receipt.OrderID) > 128 ||
		receipt.RedeemedAt.IsZero() || receipt.RedeemedAt.After(time.Now().Add(5*time.Minute)) {
		return contract.ErrRedemptionInvalid
	}
	return s.repo.RecordRedemption(receipt)
}
