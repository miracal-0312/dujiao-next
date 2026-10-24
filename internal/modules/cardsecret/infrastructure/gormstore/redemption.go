package gormstore

import (
	"time"

	"github.com/dujiao-next/internal/modules/cardsecret/contract"
	"github.com/dujiao-next/internal/modules/cardsecret/domain"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ contract.RedemptionRepository = (*Store)(nil)

// RecordRedemption only marks an exactly matched, delivered inventory item.
// The receipt and item are one row, so retries cannot leave partial receipt records.
func (r *Store) RecordRedemption(receipt contract.RedemptionReceipt) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var items []domain.Secret
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("secret = ? AND deleted_at IS NULL", receipt.CDK).Limit(2).Find(&items).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return contract.ErrRedemptionNotFound
		}
		if len(items) != 1 {
			return contract.ErrRedemptionConflict
		}
		item := items[0]
		if item.Secret != receipt.CDK || item.Status != domain.StatusUsed {
			return contract.ErrRedemptionConflict
		}
		if item.RedeemedAt != nil {
			if item.RedemptionEventID != nil && *item.RedemptionEventID == receipt.EventID &&
				item.RedemptionSource == receipt.Source && item.RedemptionOrderID == receipt.OrderID &&
				item.RedeemedAt.Equal(receipt.RedeemedAt) {
				return nil
			}
			return contract.ErrRedemptionConflict
		}
		var count int64
		if err := tx.Model(&domain.Secret{}).Where("redemption_event_id = ?", receipt.EventID).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return contract.ErrRedemptionConflict
		}
		result := tx.Model(&domain.Secret{}).Where("id = ? AND status = ? AND redeemed_at IS NULL AND deleted_at IS NULL", item.ID, domain.StatusUsed).
			Updates(map[string]interface{}{"redeemed_at": receipt.RedeemedAt.UTC(), "redemption_event_id": receipt.EventID,
				"redemption_source": receipt.Source, "redemption_order_id": receipt.OrderID, "updated_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return contract.ErrRedemptionConflict
		}
		return nil
	})
}
