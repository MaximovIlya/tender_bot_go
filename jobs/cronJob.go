package jobs

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"tender_bot_go/db"
	"tender_bot_go/settings"
	"time"

	"github.com/gofiber/fiber/v2/log"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"
	"gopkg.in/telebot.v3"
)

var config = settings.LoadSettings()

type MessageManager interface {
	AddMessage(userID int64, messageID int)
}

func ActivatePendingTenders(bot *telebot.Bot, pool *pgxpool.Pool, msgManager MessageManager) {
	queries := db.New(pool)

	c := cron.New(cron.WithSeconds())

	// Каждые 5 минут
	c.AddFunc("0 */5 * * * *", func() {
		ctx := context.Background()
		

		// Активируем pending тендеры
		err := queries.ActivatePendingTenders(ctx)
		if err != nil {
			log.Errorf("Failed to activate tenders: %v", err)
			return
		}
		log.Info("Pending tenders activation check completed")

		// Уведомления о начавшихся тендерах
		startingTenders, err := queries.GetStartingTenders(ctx)
		if err != nil {
			log.Errorf("Failed to get started tenders: %v", err)
			return
		}

		log.Infof("Found %d started tenders", len(startingTenders))

		for _, tender := range startingTenders {
			formattedCurrentPrice := formatPriceFloat(tender.CurrentPrice)
			messageForUsers := fmt.Sprintf(
				"🎉 *Тендер начался!*\n\n"+
					"Тендер *%s* начался. Вы можете подавать свои ставки на понижение цены\n"+
					"📈 *Текущая цена:* %s руб.\n",
				tender.Title,
				formattedCurrentPrice,
			)
			messageForOrganizer := fmt.Sprintf(
				"🎉 *Тендер начался!*\n\nТендер \"%s\" начался",
				tender.Title,
			)

			tenderId := tender.ID

			err := queries.MessageSent(ctx, tenderId)
			if err != nil {
				log.Errorf("Failed to set message_sent to true")
			}

			// Отправляем организатору
			for _, organizer := range config.OrganizerIDs {
				_, err = bot.Send(&telebot.User{ID: organizer}, messageForOrganizer, &telebot.SendOptions{
					ParseMode: telebot.ModeMarkdown,
				})
				if err != nil {
					log.Errorf("Failed to send notification to organizer %d: %v", organizer, err)
				} else {
					log.Infof("Notification sent to organizer for tender %s", tender.Title)
				}
			}

			// Получаем участников тендера
			userIds, err := queries.GetParticipantsForTender(ctx, tenderId)
			if err != nil {
				log.Errorf("Failed to get participants for tender %d: %v", tenderId, err)
				continue // продолжаем со следующим тендером
			}

			log.Infof("Tender %s has %d participants", tender.Title, len(userIds))

			// Отправляем участникам
			for _, userId := range userIds {
				inlineKeyboard := [][]telebot.InlineButton{
					{
						{
							Unique: "make_bid",
							Text:   "💵 Подать ставку",
							Data:   fmt.Sprintf("%d|%d", tender.ID, userId),
						},
					},
				}
				msg, err := bot.Send(&telebot.User{ID: userId}, messageForUsers, &telebot.SendOptions{
					ParseMode: telebot.ModeMarkdown,
					ReplyMarkup: &telebot.ReplyMarkup{
						InlineKeyboard: inlineKeyboard,
					},
				})
				msgManager.AddMessage(userId, msg.ID)
				if err != nil {
					log.Errorf("Failed to send notification to user %d: %v", userId, err)
					time.Sleep(100 * time.Millisecond)
				} else {
					log.Infof("Notification sent to user %d for tender %s", userId, tender.Title)
				}
			}
		}

		// Уведомления о тендерах, которые начнутся через 10 минут
		tenders, err := queries.GetTendersStartingIn10Minutes(ctx)
		if err != nil {
			log.Errorf("Failed to get tenders starting in 5 minutes: %v", err)
			return
		}

		log.Infof("Found %d tenders starting in 5 minutes", len(tenders))

		for _, tender := range tenders {
			message := fmt.Sprintf(
				"🔔 *НАПОМИНАНИЕ О ТЕНДЕРЕ*\n\n"+
					"📋 *Тендер:* %s\n"+
					"⏰ *Начало через:* 5 минут\n"+
					"🚀 *Будьте готовы к участию!*",
				tender.Title,
			)

			tenderId := tender.ID

			// Получаем участников тендера
			userIds, err := queries.GetParticipantsForTender(ctx, tenderId)
			if err != nil {
				log.Errorf("Failed to get participants for tender %d: %v", tenderId, err)
				continue // продолжаем со следующим тендером
			}

			log.Infof("Tender %s has %d participants", tender.Title, len(userIds))

			// Отправляем сообщение каждому участнику
			for _, userId := range userIds {
				msg, err := bot.Send(&telebot.User{ID: userId}, message, &telebot.SendOptions{
					ParseMode: telebot.ModeMarkdown,
				})
				msgManager.AddMessage(userId, msg.ID)
				if err != nil {
					log.Errorf("Failed to send notification to user %d: %v", userId, err)
					time.Sleep(100 * time.Millisecond)
				} else {
					log.Infof("Notification sent to user %d for tender %s", userId, tender.Title)
				}
			}
		}

	})

	c.Start()
	log.Info("Tender activation job started - checking every 5 minutes")
}

// Функция для форматирования цены в финансовый формат (из строки)
func formatPrice(priceStr string) string {
	// Пытаемся преобразовать строку в число
	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return priceStr // возвращаем как есть если не число
	}
	return formatPriceFloat(price)
}

// Функция для форматирования цены в финансовый формат (из float)
func formatPriceFloat(price float64) string {
	// Преобразуем в целое число если нет дробной части
	if price == float64(int64(price)) {
		return formatInteger(int64(price))
	}

	// Для дробных чисел форматируем с двумя знаками после запятой
	intPart := int64(price)
	fractional := int64((price - float64(intPart)) * 100)

	return fmt.Sprintf("%s.%02d", formatInteger(intPart), fractional)
}

// Функция для форматирования целого числа с пробелами
func formatInteger(n int64) string {
	if n == 0 {
		return "0"
	}

	var parts []string
	isNegative := n < 0
	if isNegative {
		n = -n
	}

	for n > 0 {
		part := n % 1000
		n = n / 1000
		if n > 0 {
			parts = append([]string{fmt.Sprintf("%03d", part)}, parts...)
		} else {
			parts = append([]string{fmt.Sprintf("%d", part)}, parts...)
		}
	}

	result := strings.Join(parts, " ")
	if isNegative {
		result = "-" + result
	}

	return result
}
