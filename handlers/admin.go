package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"tender_bot_go/db"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/telebot.v3"
)


func RegisterAdminHandlers(bot *telebot.Bot, pool *pgxpool.Pool) {
	queries := db.New(pool)

	bot.Handle(&telebot.InlineButton{Unique: "approve_tender"}, func(c telebot.Context) error {
		return handleApproveTender(c, queries)
	})
}

func HandleAdminText(c telebot.Context, queries *db.Queries, text string, userID int64) error {
	// Админ обычно работает через inline кнопки
	return nil
}

func handleApproveTender(c telebot.Context, queries *db.Queries) error {
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

	approvedBtn := telebot.InlineButton{
		Unique: "approve_tender",
		Text:   "✅ Одобрено",
		Data:   fmt.Sprintf("%d", tenderID),
	}

	_, err = c.Bot().EditReplyMarkup(c.Message(), &telebot.ReplyMarkup{
		InlineKeyboard: [][]telebot.InlineButton{
			{approvedBtn},
		},
	})
	if err != nil {
		fmt.Printf("Ошибка при обновлении кнопки: %v\n", err)
	}

	_, err = c.Bot().Send(&telebot.User{ID: config.OrganizerID},
		fmt.Sprintf("✅ Тендер \"%s\" успешно одобрен!", tenderTitle))
	if err != nil {
		fmt.Printf("Ошибка отправки уведомления организатору: %v", err)
	}

	return c.Respond(&telebot.CallbackResponse{
		Text: "✅ Тендер успешно одобрен!",
	})
}