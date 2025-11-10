package admin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"tender_bot_go/db"
	"tender_bot_go/settings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/telebot.v3"
)

var config = settings.LoadSettings()

func AdminHandlers(bot *telebot.Bot, pool *pgxpool.Pool) {
	queries := db.New(pool)

	// Обработчик для кнопки одобрения тендера
	// Обработчик для кнопки одобрения тендера
	bot.Handle(&telebot.InlineButton{Unique: "approve_tender"}, func(c telebot.Context) error {
		
		// Проверяем, что пользователь - админ
		userID := c.Sender().ID
		isAdmin := false
		for _, adminID := range config.AdminIDs {
			if adminID == userID {
				isAdmin = true
				break
			}
		}

		if !isAdmin {
			return c.Respond(&telebot.CallbackResponse{
				Text:      "❌ У вас нет прав для одобрения тендеров",
				ShowAlert: true,
			})
		}

		// Получаем ID тендера из данных кнопки
		data := c.Data()
		parts := strings.Split(data, "|")
		if len(parts) != 2 {
			return c.Respond(&telebot.CallbackResponse{
				Text:      "❌ Ошибка: неверный формат данных",
				ShowAlert: true,
			})
		}

		tenderIDStr := parts[0]
		tenderTitle := parts[1]
		tenderID, err := strconv.ParseInt(tenderIDStr, 10, 32)
		if err != nil {
			return c.Respond(&telebot.CallbackResponse{
				Text:      "❌ Ошибка: неверный ID тендера",
				ShowAlert: true,
			})
		}

		// Одобряем тендер в БД
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = queries.ApproveTender(ctx, int32(tenderID))
		if err != nil {
			fmt.Printf("Ошибка при одобрении тендера: %v\n", err)
			return c.Respond(&telebot.CallbackResponse{
				Text:      "❌ Не удалось одобрить тендер",
				ShowAlert: true,
			})
		}

		// Обновляем кнопку на "одобрено" с галочкой
		approvedBtn := telebot.InlineButton{
			Unique: "approve_tender",
			Text:   "✅ Одобрено", // С галочкой после одобрения
			Data:   fmt.Sprintf("%d", tenderID),
		}

		// Обновляем сообщение с новой кнопкой
		_, err = c.Bot().EditReplyMarkup(c.Message(), &telebot.ReplyMarkup{
			InlineKeyboard: [][]telebot.InlineButton{
				{approvedBtn},
			},
		})
		if err != nil {
			fmt.Printf("Ошибка при обновлении кнопки: %v\n", err)
		}

		_, err = bot.Send(&telebot.User{ID: config.OrganizerID},
			fmt.Sprintf("✅ Тендер \"%s\" успешно одобрен!", tenderTitle))
		if err != nil {
			fmt.Printf("Ошибка отправки уведомления организатору: %v", err)
		}
		// Отправляем ответ пользователю
		return c.Respond(&telebot.CallbackResponse{
			Text: "✅ Тендер успешно одобрен!",
		})

	})
}
