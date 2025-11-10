package handlers

import (
	"tender_bot_go/db"
	"gopkg.in/telebot.v3"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerTextHandler(bot *telebot.Bot, pool *pgxpool.Pool) {
	queries := db.New(pool)
	
	bot.Handle(telebot.OnText, func(c telebot.Context) error {
		userID := c.Sender().ID
		text := c.Text()

		// Получаем роль пользователя
		role := getUserRole(userID, queries)
		if role == "" {
			return c.Send("Ошибка получения данных пользователя")
		}

		// Перенаправляем в соответствующий обработчик
		switch role {
		case "organizer":
			return HandleOrganizerText(c, queries, text, userID)
		case "supplier":
			return HandleSupplierText(c, queries, text, userID)
		case "admin":
			return HandleAdminText(c, queries, text, userID)
		default:
			return c.Send("Роль пользователя не определена")
		}
	})
}