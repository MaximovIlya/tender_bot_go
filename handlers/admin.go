package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"tender_bot_go/db"
	"tender_bot_go/menu"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/telebot.v3"
)

func RegisterAdminHandlers(bot *telebot.Bot, pool *pgxpool.Pool) {
	queries := db.New(pool)

	bot.Handle(&telebot.InlineButton{Unique: "approve_tender"}, func(c telebot.Context) error {
		return handleApproveTender(c, queries, bot)
	})

	bot.Handle(&telebot.InlineButton{Unique: "user_management"}, func(c telebot.Context) error {
		return handleUserManagement(c, queries, bot)
	})

	bot.Handle(&telebot.InlineButton{Unique: "approve_registration"}, func(c telebot.Context) error {
		return handleApproveRegistration(c, queries, bot)
	})

	bot.Handle(&telebot.InlineButton{Unique: "reject_registration"}, func(c telebot.Context) error {
		return handleRejectRegistration(c, queries, bot)
	})
}

func handleApproveRegistration(c telebot.Context, queries *db.Queries, bot *telebot.Bot) error {
	// Проверка прав администратора
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
			Text:      "❌ У вас нет прав для одобрения регистраций",
			ShowAlert: true,
		})
	}

	data := c.Data()
	parts := strings.Split(data, "|")
	if len(parts) != 2 {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка формата данных",
			ShowAlert: true,
		})
	}

	targetUserID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка ID пользователя",
			ShowAlert: true,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Получаем данные pending пользователя
	pendingUser, err := queries.GetPendingUser(ctx, targetUserID)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Заявка не найдена",
			ShowAlert: true,
		})
	}

	// Регистрируем пользователя
	err = queries.UpdateUser(ctx, db.UpdateUserParams{
		TelegramID:       targetUserID,
		OrganizationName: pendingUser.OrganizationName,
		Inn:              pendingUser.Inn,
		PhoneNumber:      pendingUser.PhoneNumber,
		Name:             pendingUser.Name,
		Classification:   pendingUser.Classification,
	})
	if err != nil {
		fmt.Printf("Ошибка регистрации пользователя: %v\n", err)
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка регистрации",
			ShowAlert: true,
		})
	}

	// Удаляем из pending
	err = queries.ApprovePendingUser(ctx, targetUserID)
	if err != nil {
		fmt.Printf("Ошибка удаления pending пользователя: %v\n", err)
	}

	// Уведомляем пользователя
	msg, err := bot.Send(&telebot.User{ID: targetUserID},
		"✅ *Ваша регистрация одобрена!*\n\nТеперь вы можете участвовать в тендерах.",
		&telebot.SendOptions{
			ParseMode:   telebot.ModeMarkdown,
			ReplyMarkup: menu.MenuSupplierRegistered,
		})
	if err != nil {
		fmt.Printf("Ошибка уведомления пользователя: %v\n", err)
	}
    MessageManagerOperator.AddMessage(targetUserID, msg.ID)

	// Обновляем сообщение админа
	approvedBtn := telebot.InlineButton{
		Unique: "approve_registration",
		Text:   "✅ Одобрено",
		Data:   fmt.Sprintf("approved|%d", targetUserID),
	}

	_, err = c.Bot().EditReplyMarkup(c.Message(), &telebot.ReplyMarkup{
		InlineKeyboard: [][]telebot.InlineButton{
			{approvedBtn},
		},
	})

	return c.Respond(&telebot.CallbackResponse{
		Text: "✅ Регистрация одобрена",
	})
}

func handleRejectRegistration(c telebot.Context, queries *db.Queries, bot *telebot.Bot) error {
	// Проверка прав администратора
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
			Text:      "❌ У вас нет прав для отклонения регистраций",
			ShowAlert: true,
		})
	}

	data := c.Data()
	parts := strings.Split(data, "|")
	if len(parts) != 2 {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка формата данных",
			ShowAlert: true,
		})
	}

	targetUserID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка ID пользователя",
			ShowAlert: true,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Получаем данные pending пользователя для информации
	pendingUser, err := queries.GetPendingUser(ctx, targetUserID)
	if err != nil {
		fmt.Printf("Ошибка получения данных pending пользователя: %v\n", err)
		// Все равно продолжаем, чтобы очистить запись
	}

	// Удаляем pending запись
	err = queries.ApprovePendingUser(ctx, targetUserID)
	if err != nil {
		fmt.Printf("Ошибка удаления pending пользователя: %v\n", err)
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка при отклонении заявки",
			ShowAlert: true,
		})
	}

	// Уведомляем пользователя об отклонении
	rejectionMessage := "❌ *Ваша заявка на регистрацию отклонена администратором.*\n\n" +
		"По вопросам обращайтесь к администрации."

	_, err = bot.Send(&telebot.User{ID: targetUserID}, rejectionMessage, &telebot.SendOptions{
		ParseMode:   telebot.ModeMarkdown,
		ReplyMarkup: menu.MenuSupplierUnregistered,
	})
	if err != nil {
		fmt.Printf("Ошибка уведомления пользователя об отклонении: %v\n", err)
	}

	// Обновляем сообщение админа
	rejectedBtn := telebot.InlineButton{
		Unique: "reject_registration",
		Text:   "❌ Отклонено",
		Data:   fmt.Sprintf("rejected|%d", targetUserID),
	}

	_, err = c.Bot().EditReplyMarkup(c.Message(), &telebot.ReplyMarkup{
		InlineKeyboard: [][]telebot.InlineButton{
			{rejectedBtn},
		},
	})
	if err != nil {
		fmt.Printf("Ошибка при обновлении кнопки: %v\n", err)
	}

	// Отправляем подтверждение админу
	var orgName string
	if pendingUser.OrganizationName.Valid {
		orgName = pendingUser.OrganizationName.String
	} else {
		orgName = "не указано"
	}

	adminConfirmation := fmt.Sprintf(
		"✅ *Заявка отклонена*\n\n"+
			"👤 Пользователь: ID: %d\n"+
			"🏢 Организация: %s\n"+
			"🆔 ИНН: %s\n"+
			"⏰ Время: %s",
		targetUserID,
		orgName,
		pendingUser.Inn.String,
		time.Now().Format("02.01.2006 15:04"),
	)

	_, err = c.Bot().Send(c.Sender(), adminConfirmation, &telebot.SendOptions{
		ParseMode: telebot.ModeMarkdown,
	})
	if err != nil {
		fmt.Printf("Ошибка отправки подтверждения админу: %v\n", err)
	}

	return c.Respond(&telebot.CallbackResponse{
		Text: "✅ Регистрация отклонена",
	})
}

func handleUserManagement(c telebot.Context, queries *db.Queries, bot *telebot.Bot) error {
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
			Text:      "❌ У вас нет прав для управления пользователями",
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

	action := parts[0]
	targetUserIDStr := parts[1]
	targetUserID, err := strconv.ParseInt(targetUserIDStr, 10, 64)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка: неверный ID пользователя",
			ShowAlert: true,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var resultMessage string

	switch action {
	case "block_user":
		err = queries.BlockUser(ctx, targetUserID)
		if err != nil {
			fmt.Printf("Ошибка при блокировке пользователя %d: %v\n", targetUserID, err)
			return c.Respond(&telebot.CallbackResponse{
				Text:      "❌ Не удалось заблокировать пользователя",
				ShowAlert: true,
			})
		}
		resultMessage = "✅ Пользователь заблокирован"

		// Отправляем уведомление пользователю о блокировке
		blockMessage := "🚫 *Ваш аккаунт был заблокирован администратором*\n\n" +
			"Вы больше не можете участвовать в тендерах и использовать функционал бота."

		_, err = bot.Send(&telebot.User{ID: targetUserID}, blockMessage, &telebot.SendOptions{
			ParseMode: telebot.ModeMarkdown,
		})
		if err != nil {
			fmt.Printf("Ошибка при отправке уведомления о блокировке пользователю %d: %v\n", targetUserID, err)
		}

	case "unblock_user":
		err = queries.UnblockUser(ctx, targetUserID)
		if err != nil {
			fmt.Printf("Ошибка при разблокировке пользователя %d: %v\n", targetUserID, err)
			return c.Respond(&telebot.CallbackResponse{
				Text:      "❌ Не удалось разблокировать пользователя",
				ShowAlert: true,
			})
		}
		resultMessage = "✅ Пользователь разблокирован"

		// Отправляем уведомление пользователю о разблокировке
		unblockMessage := "✅ *Ваш аккаунт был разблокирован*\n\n" +
			"Теперь вы снова можете участвовать в тендерах и использовать весь функционал бота."

		_, err = bot.Send(&telebot.User{ID: targetUserID}, unblockMessage, &telebot.SendOptions{
			ParseMode: telebot.ModeMarkdown,
		})
		if err != nil {
			fmt.Printf("Ошибка при отправке уведомления о разблокировке пользователю %d: %v\n", targetUserID, err)
		}

	default:
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Неизвестное действие",
			ShowAlert: true,
		})
	}

	// Обновляем кнопку в сообщении
	var newButtonText string
	var newButtonData string

	if action == "block_user" {
		newButtonText = "🔓 Разблокировать"
		newButtonData = fmt.Sprintf("unblock_user|%d", targetUserID)
	} else {
		newButtonText = "🚫 Заблокировать"
		newButtonData = fmt.Sprintf("block_user|%d", targetUserID)
	}

	updatedButton := telebot.InlineButton{
		Unique: "user_management",
		Text:   newButtonText,
		Data:   newButtonData,
	}

	_, err = c.Bot().EditReplyMarkup(c.Message(), &telebot.ReplyMarkup{
		InlineKeyboard: [][]telebot.InlineButton{
			{updatedButton},
		},
	})
	if err != nil {
		fmt.Printf("Ошибка при обновлении кнопки: %v\n", err)
	}

	return c.Respond(&telebot.CallbackResponse{
		Text: resultMessage,
	})
}

func HandleAdminText(c telebot.Context, queries *db.Queries, text string, userID int64) error {
	// Админ обычно работает через inline кнопки
	if text == "Пользователи" {
		return sendListOfUsers(c, queries)
	}
	if text == "История" {
		return sendAdminHistory(c, queries)
	}
	if text == "Заявки на регистрацию" {
		return sendPendingRegistrations(c, queries)
	}

	return nil

}

func sendPendingRegistrations(c telebot.Context, queries *db.Queries) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pendingUsers, err := queries.GetAllPendingUsers(ctx)
	if err != nil {
		fmt.Printf("Ошибка при получении pending пользователей: %v\n", err)
		return c.Send("❌ Не удалось загрузить заявки на регистрацию", &telebot.SendOptions{
			ReplyMarkup: menu.MenuAdmin,
		})
	}

	if len(pendingUsers) == 0 {
		return c.Send("📭 Нет заявок на регистрацию", &telebot.SendOptions{
			ReplyMarkup: menu.MenuAdmin,
		})
	}

	// Отправляем сообщение о начале списка
	if err := c.Send(fmt.Sprintf("📋 *Заявки на регистрацию (%d)*:", len(pendingUsers)), &telebot.SendOptions{
		ParseMode: telebot.ModeMarkdown,
	}); err != nil {
		return err
	}

	// Отправляем каждую заявку отдельным сообщением
	for i, pendingUser := range pendingUsers {
		// Форматируем классификации
		classifications := strings.Split(pendingUser.Classification.String, ",")
		var classificationNamesList []string
		for _, code := range classifications {
			if name, exists := classificationNames[code]; exists {
				classificationNamesList = append(classificationNamesList, name)
			}
		}

		userInfo := fmt.Sprintf(
			"🆕 *Заявка #%d*\n\n"+
				"👤 *ID пользователя:* %d\n"+
				"🏢 *Организация:* %s\n"+
				"🆔 *ИНН:* %s\n"+
				"📞 *Телефон:* %s\n"+
				"👨‍💼 *ФИО:* %s\n"+
				"🗂️ *Классификации:* %s\n"+
				"⏰ *Подана:* %s",
			i+1,
			pendingUser.TelegramID,
			pendingUser.OrganizationName.String,
			pendingUser.Inn.String,
			pendingUser.PhoneNumber.String,
			pendingUser.Name.String,
			strings.Join(classificationNamesList, ", "),
			pendingUser.CreatedAt.Time.Format("02.01.2006 15:04"),
		)

		// Кнопки для админа
		inlineKeyboard := [][]telebot.InlineButton{
			{
				{
					Unique: "approve_registration",
					Text:   "✅ Одобрить",
					Data:   fmt.Sprintf("approve|%d", pendingUser.TelegramID),
				},
				{
					Unique: "reject_registration",
					Text:   "❌ Отклонить",
					Data:   fmt.Sprintf("reject|%d", pendingUser.TelegramID),
				},
			},
		}

		if err := c.Send(userInfo, &telebot.SendOptions{
			ParseMode: telebot.ModeMarkdown,
			ReplyMarkup: &telebot.ReplyMarkup{
				InlineKeyboard: inlineKeyboard,
			},
		}); err != nil {
			fmt.Printf("Ошибка при отправке информации о заявке: %v\n", err)
			continue
		}

		time.Sleep(300 * time.Millisecond)
	}

	return c.Send("✅ Список заявок завершен", &telebot.SendOptions{
		ReplyMarkup: menu.MenuAdmin,
	})
}

func sendListOfUsers(c telebot.Context, queries *db.Queries) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Получаем всех пользователей
	users, err := queries.GetAllUsers(ctx)
	if err != nil {
		fmt.Printf("Ошибка при получении пользователей: %v\n", err)
		return c.Send("❌ Не удалось загрузить список пользователей", &telebot.SendOptions{
			ReplyMarkup: menu.MenuAdmin,
		})
	}

	if len(users) == 0 {
		return c.Send("📭 Нет зарегистрированных пользователей", &telebot.SendOptions{
			ReplyMarkup: menu.MenuAdmin,
		})
	}

	// Отправляем сообщение о начале списка
	if err := c.Send(fmt.Sprintf("👥 *Список пользователей (%d)*:", len(users)), &telebot.SendOptions{
		ParseMode: telebot.ModeMarkdown,
	}); err != nil {
		return err
	}

	// Отправляем каждого пользователя отдельным сообщением с кнопкой
	for i, user := range users {
		// Формируем статус пользователя
		status := "✅ Активен"
		if user.Banned.Bool {
			status = "❌ Заблокирован"
		}

		classifications := strings.Split(user.Classification.String, ",")
		var classification1, classification2 string
		if len(classifications) > 0 {
			classification1 = classificationNames[classifications[0]]
		}
		if len(classifications) > 1 {
			classification2 = classificationNames[classifications[1]]
		} else {
			classification2 = "не указана" // или какое-то значение по умолчанию
		}

		// Формируем информацию о пользователе
		userInfo := fmt.Sprintf(
			"👤 *Пользователь #%d*\n\n"+
				"🏢 *Организация:* %s\n"+
				"📞 *Телефон:* %s\n"+
				"🆔 *ИНН:* %s\n"+
				"👨‍💼 *ФИО:* %s\n"+
				"🗂️ *Классификации:* %s, %s\n"+
				"🔒 *Статус:* %s",
			i+1,
			user.OrganizationName.String,
			user.PhoneNumber.String,
			user.Inn.String,
			user.Name.String,
			classification1,
			classification2,
			status,
		)

		// Создаем кнопку в зависимости от статуса
		var buttonText string
		var buttonData string

		if user.Banned.Bool {
			buttonText = "🔓 Разблокировать"
			buttonData = fmt.Sprintf("unblock_user|%d", user.TelegramID)
		} else {
			buttonText = "🚫 Заблокировать"
			buttonData = fmt.Sprintf("block_user|%d", user.TelegramID)
		}

		inlineKeyboard := [][]telebot.InlineButton{
			{
				{
					Unique: "user_management",
					Text:   buttonText,
					Data:   buttonData,
				},
			},
		}

		// Отправляем сообщение с пользователем и кнопкой
		if err := c.Send(userInfo, &telebot.SendOptions{
			ParseMode: telebot.ModeMarkdown,
			ReplyMarkup: &telebot.ReplyMarkup{
				InlineKeyboard: inlineKeyboard,
			},
		}); err != nil {
			fmt.Printf("Ошибка при отправке информации о пользователе %d: %v\n", user.TelegramID, err)
			continue
		}

		// Небольшая задержка между отправками
		time.Sleep(300 * time.Millisecond)
	}

	return c.Send("✅ Список пользователей завершен", &telebot.SendOptions{
		ReplyMarkup: menu.MenuAdmin,
	})
}

func handleApproveTender(c telebot.Context, queries *db.Queries, bot *telebot.Bot) error {
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

    for _, organizer := range config.OrganizerIDs {
        _, err = c.Bot().Send(&telebot.User{ID: organizer},
            fmt.Sprintf("✅ Тендер \"%s\" успешно одобрен!", tenderTitle))
        if err != nil {
            fmt.Printf("Ошибка отправки уведомления организатору: %v", err)
        }
    }

	

	tender, err := queries.GetTenderById(ctx, int32(tenderID))
	if err != nil {
		fmt.Printf("Ошибка получения нового тендера")
	}
	userIds, err := queries.GetUsersByClassification(ctx, tender.Classification)
	if err != nil {
		fmt.Printf("Ошибка получения userIds")
	}

	formattedPrice := formatPriceFloat(tender.StartPrice)
	var formattedDate string
	if tender.StartAt.Valid {
		formattedDate = tender.StartAt.Time.Format("02.01.2006 15:04")
	} else {
		formattedDate = "не указана"
	}

	message := fmt.Sprintf(
		"📋 *Доступен новый тендер:* %s\n\n"+
			"📝 *Описание:* %s\n"+
			"💰 *Стартовая цена:* %s руб.\n"+
			"📅 *Дата начала:* %s\n"+
			"🗂️ *Классификация:* %s\n",

		tender.Title,
		tender.Description.String,
		formattedPrice,
		formattedDate,
		classificationNames[tender.Classification.String],
	)

	successCount := 0
	for _, userId := range userIds {
		// Создаем клавиатуру для каждого пользователя
		inlineKeyboard := [][]telebot.InlineButton{
			{
				{
					Unique: "join_tender",
					Text:   "📝 Участвовать в тендере",
					Data:   fmt.Sprintf("%d|%d", tender.ID, userId),
				},
			},
		}

		// Отправляем основное сообщение о тендере
		msg, err := bot.Send(&telebot.User{ID: userId}, message, &telebot.SendOptions{
			ParseMode: telebot.ModeMarkdown,
			ReplyMarkup: &telebot.ReplyMarkup{
				InlineKeyboard: inlineKeyboard,
			},
		})
		if err != nil {
			fmt.Printf("Ошибка при отправке информации о тендере пользователю %d: %v\n", userId, err)
			continue
		}

		MessageManagerOperator.AddMessage(userId, msg.ID)
		successCount++

		// Отправляем файл условий, если он есть
		if tender.ConditionsPath.Valid && tender.ConditionsPath.String != "" {
			filePath := tender.ConditionsPath.String

			// Проверяем существование файла
			if _, err := os.Stat(filePath); err == nil {
				// Отправляем сообщение о файле
				fileCaptionMsg, err := bot.Send(&telebot.User{ID: userId}, "📎 Файл с условиями тендера:")
				if err != nil {
					fmt.Printf("Ошибка при отправке сообщения о файле пользователю %d: %v\n", userId, err)
					continue
				}
				MessageManagerOperator.AddMessage(userId, fileCaptionMsg.ID)

				// Отправляем сам файл
				fileName := filepath.Base(filePath)
				fileToSend := &telebot.Document{
					File:     telebot.FromDisk(filePath),
					FileName: fileName,
				}

				fileMsg, err := bot.Send(&telebot.User{ID: userId}, fileToSend)
				if err != nil {
					fmt.Printf("Ошибка при отправке файла тендера пользователю %d: %v\n", userId, err)
				} else {
					MessageManagerOperator.AddMessage(userId, fileMsg.ID)
				}
			} else {
				fmt.Printf("Файл не найден: %s\n", filePath)
				errorMsg, err := bot.Send(&telebot.User{ID: userId}, "❌ Файл условий недоступен")
				if err != nil {
					fmt.Printf("Ошибка при отправке сообщения об отсутствии файла пользователю %d: %v\n", userId, err)
				} else {
					MessageManagerOperator.AddMessage(userId, errorMsg.ID)
				}
			}
		} else {
			// Если файла нет
			noFileMsg, err := bot.Send(&telebot.User{ID: userId}, "📭 Файл условий не прикреплен")
			if err != nil {
				fmt.Printf("Ошибка при отправке сообщения об отсутствии файла пользователю %d: %v\n", userId, err)
			} else {
				MessageManagerOperator.AddMessage(userId, noFileMsg.ID)
			}
		}

		// Небольшая задержка между отправками
		time.Sleep(100 * time.Millisecond)
	}

	return c.Respond(&telebot.CallbackResponse{
		Text: "✅ Тендер успешно одобрен!",
	})
}

func sendAdminHistory(c telebot.Context, queries *db.Queries) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenders, err := queries.GetTendersHistory(ctx)
	if err != nil {
		fmt.Printf("Ошибка при получении тендеров: %v\n", err)
		return c.Send("❌ Не удалось загрузить историю", &telebot.SendOptions{
			ReplyMarkup: menu.MenuAdmin,
		})
	}

	if len(tenders) == 0 {
		return c.Send("📭 Нет тендеров в истории", &telebot.SendOptions{
			ReplyMarkup: menu.MenuAdmin,
		})
	}
	for _, tender := range tenders {
		bidsHistory, err := queries.GetBidsHistoryByTenderID(ctx, tender.TenderID)
		if err != nil {
			fmt.Printf("Ошибка получения истории ставок для тендера %d: %v\n", tender.TenderID, err)
		}

		var bidsHistoryText string
		if len(bidsHistory) > 0 {
			bidsHistoryText = "\n\n📊 *История ставок:*\n"
			for i, bid := range bidsHistory {
				// Форматируем время
				bidTime := bid.BidTime.Time.Format("02.01.2006 15:04")
				// Форматируем сумму ставки
				formattedBidAmount := formatPriceFloat(bid.Amount)

				bidsHistoryText += fmt.Sprintf("%d. %s руб. - %s (%s)\n",
					i+1,
					formattedBidAmount,
					bid.OrganizationName.String,
					bidTime)
			}
		} else {
			bidsHistoryText = "\n\n📊 *История ставок:*\nСтавки отсутствуют"
		}

		// Форматируем цену в финансовом формате
		formattedPrice := formatPriceFloat(tender.StartPrice)

		formattedBidPrice := formatPriceFloat(tender.Bid)

		// Создаем сообщение с информацией о тендере
		tenderInfo := fmt.Sprintf(
			"📋 *Тендер*: %s\n\n"+
				"💰 *Стартовая цена:* %s руб.\n"+
				"💰 *Выигрышная ставка:* %s руб.\n"+
				"👑 Победитель: %s\n"+
				"📞 Контакты победителя:\n"+
				"   • Телефон: %s\n"+
				"   • ИНН: %s\n"+
				"   • ФИО: %s\n"+
				"%s",
			tender.Title,
			formattedPrice,
			formattedBidPrice,
			tender.Winner.String,
			tender.PhoneNumber.String,
			tender.Inn.String,
			tender.Fio.String,
			bidsHistoryText,
		)

		// Отправляем информацию о тендере
		if err := c.Send(tenderInfo, &telebot.SendOptions{
			ParseMode: telebot.ModeMarkdown,
		}); err != nil {
			fmt.Printf("Ошибка при отправке информации о тендере: %v\n", err)
			continue
		}

		// Если есть прикрепленный файл, отправляем его

		// Добавляем разделитель между тендерами
		if err := c.Send("➖➖➖➖➖➖➖➖➖➖"); err != nil {
			fmt.Printf("Ошибка при отправке разделителя: %v\n", err)
		}

		// Небольшая задержка между отправками чтобы не превысить лимиты Telegram
		time.Sleep(500 * time.Millisecond)
	}

	return c.Send(fmt.Sprintf("✅ Всего тендеров: %d", len(tenders)), &telebot.SendOptions{
		ReplyMarkup: menu.MenuAdmin,
	})
}
