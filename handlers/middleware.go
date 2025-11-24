// middleware.go
package handlers

import (
	"context"
	"tender_bot_go/db"
	"time"

	"gopkg.in/telebot.v3"
)

func BlockedUserMiddleware(queries *db.Queries) telebot.MiddlewareFunc {
	return func(next telebot.HandlerFunc) telebot.HandlerFunc {
		return func(c telebot.Context) error {
			userID := c.Sender().ID
			
			// Проверяем, не заблокирован ли пользователь
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			
			user, err := queries.GetUserByTelegramID(ctx, userID)
			if err == nil && user.Banned.Bool {
				// Пользователь заблокирован - отправляем сообщение и прерываем выполнение
				return c.Send("🚫 *Ваш аккаунт заблокирован*\n\nВы не можете использовать функции бота.", &telebot.SendOptions{
					ParseMode: telebot.ModeMarkdown,
				})
			}
			
			// Пользователь не заблокирован - продолжаем выполнение
			return next(c)
		}
	}
}