package cardsecret

import (
	"github.com/dujiao-next/internal/app/container"
	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/modules/cardsecret/application/redemption"
	cardsecrethttp "github.com/dujiao-next/internal/modules/cardsecret/transport/http"
)

func NewRedemptionHandler(c *container.Container, cfg config.CardRedemptionConfig) *cardsecrethttp.RedemptionHandler {
	return cardsecrethttp.NewRedemptionHandler(redemption.New(c.CardSecretRepo), cfg.Enabled, cfg.Source, cfg.Secret)
}
