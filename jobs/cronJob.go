package jobs

import (
	"context"
	"tender_bot_go/db"

	"github.com/gofiber/fiber/v2/log"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"
	"gopkg.in/telebot.v3"
)

func ActivatePendingTenders(bot *telebot.Bot, pool *pgxpool.Pool) {
	queries := db.New(pool)

	c := cron.New(cron.WithSeconds())

	// Каждые 5 минут
	c.AddFunc("0 */5 * * * *", func() {
		ctx := context.Background()
		err := queries.ActivatePendingTenders(ctx)
		if err != nil {
			log.Errorf("Failed to activate tenders: %v", err)
			return
		}
		log.Info("Pending tenders activation check completed")
	})

	c.Start()
	log.Info("Tender activation job started - checking every 5 minutes")
}