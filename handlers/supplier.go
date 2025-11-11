package handlers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"tender_bot_go/db"
	"tender_bot_go/menu"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/telebot.v3"
)

type MessageManager struct {
	userMessages map[int64][]int // userID -> []messageIDs
	mu           sync.RWMutex
}

var messageManager = &MessageManager{
	userMessages: make(map[int64][]int),
}

// Добавление сообщения в историю
func (mm *MessageManager) AddMessage(userID int64, messageID int) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.userMessages[userID] = append(mm.userMessages[userID], messageID)
}

// Очистка старых сообщений (оставляем keepLast последних)
func (mm *MessageManager) CleanupOldMessages(bot *telebot.Bot, userID int64, keepLast int) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	messages := mm.userMessages[userID]
	if len(messages) <= keepLast {
		return
	}

	// Удаляем все кроме последних keepLast сообщений
	toDelete := messages[:len(messages)-keepLast]
	mm.userMessages[userID] = messages[len(messages)-keepLast:]

	// Асинхронно удаляем сообщения
	go func() {
		for _, msgID := range toDelete {
			err := bot.Delete(&telebot.Message{
				Chat: &telebot.Chat{ID: userID},
				ID:   msgID,
			})
			if err != nil {
				fmt.Printf("Ошибка удаления сообщения %d: %v\n", msgID, err)
			}
			time.Sleep(100 * time.Millisecond) // Задержка между удалениями
		}
	}()
}

func (mm *MessageManager) StartNewSession(userID int64) []int {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// Возвращаем текущие сообщения (они станут "старыми")
	oldMessages := mm.userMessages[userID]
	// Начинаем новую сессию с пустым списком
	mm.userMessages[userID] = []int{}

	return oldMessages
}

func (mm *MessageManager) CleanupSessionMessages(bot *telebot.Bot, userID int64, oldMessages []int) {
	// Асинхронно удаляем сообщения из предыдущей сессии
	go func() {
		for _, msgID := range oldMessages {
			err := bot.Delete(&telebot.Message{
				Chat: &telebot.Chat{ID: userID},
				ID:   msgID,
			})
			if err != nil {
				fmt.Printf("Ошибка удаления сообщения %d: %v\n", msgID, err)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
}

type SupplierState int

const (
	StateNull SupplierState = iota
	StateOrgName
	StateINN
	StateOGRN
	StatePhone
	StateSelectClassification
	StateFIO
)

type BidState int

const (
	BidStateEnterPrice BidState = iota
	BidStateConfirm
)

var supplierStates = make(map[int64]SupplierState)
var supplierData = make(map[int64]map[string]string)

var bidData = make(map[int64]map[string]interface{})
var bidStates = make(map[int64]BidState)

func RegisterSupplierHandlers(bot *telebot.Bot, pool *pgxpool.Pool) {
	queries := db.New(pool)

	// Обработчики inline кнопок для поставщика
	for code := range classificationNames {
		classCode := code
		bot.Handle(&telebot.InlineButton{Unique: "supplier_class_" + classCode}, func(c telebot.Context) error {
			return handleSupplierClassification(c, classCode)
		})
	}

	bot.Handle(&telebot.InlineButton{Unique: "supplier_class_done"}, func(c telebot.Context) error {
		return handleSupplierClassificationDone(c)
	})

	bot.Handle(&menu.BtnJoinTender, func(c telebot.Context) error {
		return handleJoinTender(c, queries)
	})

	bot.Handle(&menu.BtnLeaveTender, func(c telebot.Context) error {
		return handleLeaveTender(c, queries)
	})

	bot.Handle(&telebot.InlineButton{Unique: "view_bids"}, func(c telebot.Context) error {
		return handleViewBids(c, queries)
	})

	bot.Handle(&telebot.InlineButton{Unique: "make_bid"}, func(c telebot.Context) error {
		return handleMakeBid(c, queries)
	})

	bot.Handle(&telebot.InlineButton{Unique: "cancel_bid"}, func(c telebot.Context) error {
		return handleCancelBid(c)
	})

	bot.Handle(&telebot.InlineButton{Unique: "confirm_bid"}, func(c telebot.Context) error {
		return handleConfirmBid(c, queries)
	})
}

func HandleSupplierText(c telebot.Context, queries *db.Queries, text string, userID int64) error {

	if state, exists := bidStates[userID]; exists {
		return handleBidText(c, queries, text, userID, state)
	}
	if _, exists := supplierData[userID]; !exists {
		supplierData[userID] = make(map[string]string)
	}

	if text == "Регистрация" {
		supplierStates[userID] = StateOrgName
		supplierData[userID] = make(map[string]string)
		return c.Send("Введите наименование вашей организации:")
	}

	if text == "Тендеры" {
		return sendSupplierTendersList(c, queries, userID)
	}

	if text == "Подать заявку" {
		return bidTender(c, queries)
	}

	state := supplierStates[userID]
	switch state {
	case StateOrgName:
		supplierData[userID]["org_name"] = text
		supplierStates[userID] = StateINN
		return c.Send("Введите ИНН организации:")
	case StateINN:
		if len(text) != 10 && len(text) != 12 {
			return c.Send("ИНН должен содержать 10 или 12 цифр. Попробуйте снова:")
		}
		supplierData[userID]["inn"] = text
		supplierStates[userID] = StateOGRN
		return c.Send("Введите ОГРН организации:")
	case StateOGRN:
		if len(text) != 13 && len(text) != 15 {
			return c.Send("ОГРН должен содержать 13 или 15 цифр. Попробуйте снова:")
		}
		supplierData[userID]["ogrn"] = text
		supplierStates[userID] = StatePhone
		return c.Send("Введите контактный телефон:")
	case StatePhone:
		phone := ""
		for _, r := range text {
			if r >= '0' && r <= '9' {
				phone += string(r)
			}
		}
		if len(phone) < 10 {
			return c.Send("Введите корректный номер телефона:")
		}
		supplierData[userID]["phone"] = phone
		supplierData[userID]["classifications"] = ""
		supplierStates[userID] = StateSelectClassification
		markup := showSupplierClassificationKeyboard(userID)
		return c.Send("Выберите до двух классификаций вашей организации:", markup)
	case StateFIO:
		supplierData[userID]["fio"] = text
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := queries.UpdateUser(ctx, db.UpdateUserParams{
			TelegramID: userID,
			OrganizationName: pgtype.Text{
				String: supplierData[userID]["org_name"],
				Valid:  true,
			},
			Inn: pgtype.Text{
				String: supplierData[userID]["inn"],
				Valid:  true,
			},
			Ogrn: pgtype.Text{
				String: supplierData[userID]["ogrn"],
				Valid:  true,
			},
			PhoneNumber: pgtype.Text{
				String: supplierData[userID]["phone"],
				Valid:  true,
			},
			Name: pgtype.Text{
				String: supplierData[userID]["fio"],
				Valid:  true,
			},
			Classification: pgtype.Text{
				String: supplierData[userID]["classifications"],
				Valid:  true,
			},
		})
		if err != nil {
			return c.Send("Ошибка при сохранении данных. Попробуйте снова.")
		}

		delete(supplierStates, userID)
		delete(supplierData, userID)
		return c.Send("✅ Регистрация завершена!", &telebot.SendOptions{
			ReplyMarkup: menu.MenuSupplierRegistered,
		})
	default:
		return nil
	}
}

func handleCancelBid(c telebot.Context) error {
	userID := c.Sender().ID

	// УДАЛЯЕМ ВСЕ СООБЩЕНИЯ СЕССИИ СИНХРОННО
	oldMessages := messageManager.StartNewSession(userID)
	messageManager.CleanupSessionMessages(c.Bot(), userID, oldMessages)

	// Очищаем состояние
	delete(bidStates, userID)
	delete(bidData, userID)

	// Ждем немного чтобы удаление завершилось
	time.Sleep(300 * time.Millisecond)

	// Отправляем новое сообщение
	msg, err := c.Bot().Send(c.Sender(), "❌ Подача ставки отменена", &telebot.SendOptions{
		ReplyMarkup: menu.MenuSupplierRegistered,
	})
	
	if err == nil {
		messageManager.AddMessage(userID, msg.ID)
	}

	// Удаляем сообщение с кнопкой отмены
	go func() {
		time.Sleep(500 * time.Millisecond)
		c.Bot().Delete(c.Message())
	}()

	return c.Respond()
}
func bidTender(c telebot.Context, queries *db.Queries) error {
	userId := c.Sender().ID

	// Получаем тендер, в котором участвует пользователь
	tenderId, err := queries.GetTenderFromParticipants(context.Background(), userId)
	if err != nil {
		errorMsg := "❌ Вы не участвуете ни в одном тендере или произошла ошибка."
		msg, err := c.Bot().Send(c.Sender(), errorMsg)
		if err == nil {
			messageManager.AddMessage(userId, msg.ID)
		}
		return err
	}
	
	tender, err := queries.GetTenderById(context.Background(), tenderId)
	if err != nil {
		errorMsg := "❌ Произошла ошибка при получении информации о тендере. Попробуйте снова."
		msg, err := c.Bot().Send(c.Sender(), errorMsg)
		if err == nil {
			messageManager.AddMessage(userId, msg.ID)
		}
		return err
	}

	// Проверяем статус тендера
	if tender.Status != "active" {
		errorMsg := "❌ Тендер не активен. Подача ставок невозможна."
		msg, err := c.Bot().Send(c.Sender(), errorMsg)
		if err == nil {
			messageManager.AddMessage(userId, msg.ID)
		}
		return fmt.Errorf(errorMsg)
	}

	// Получаем предыдущие ставки пользователя в этом тендере
	previousBids, err := queries.GetUserBidsForTender(context.Background(), db.GetUserBidsForTenderParams{
		TenderID: tender.ID,
		UserID:   userId,
	})
	if err != nil {
		fmt.Printf("Ошибка получения предыдущих ставок: %v\n", err)
	}

	// Инициализируем данные для ставки
	if _, exists := bidData[userId]; !exists {
		bidData[userId] = make(map[string]interface{})
	}

	bidData[userId]["tender_id"] = tender.ID
	bidData[userId]["tender_title"] = tender.Title
	bidData[userId]["start_price"] = tender.StartPrice
	bidData[userId]["previous_bids"] = previousBids
	bidData[userId]["current_price"] = tender.CurrentPrice

	bidStates[userId] = BidStateEnterPrice

	// Получаем минимально возможную ставку
	minBid := tender.CurrentPrice - 10000.0
	formattedMinBid := formatPriceFloat(minBid)
	formattedCurrentPrice := formatPriceFloat(tender.CurrentPrice)
	formattedStartPrice := formatPriceFloat(tender.StartPrice)
	
	// Формируем сообщение с предыдущими ставками
	message := fmt.Sprintf(
		"📋 *Тендер:* %s\n"+
			"💰 *Стартовая цена:* %s руб.\n"+
			"💰 *Текущая цена:* %s руб.\n"+
			"📉 *Минимальная ставка:* %s руб.\n",
		tender.Title,
		formattedStartPrice,
		formattedCurrentPrice,
		formattedMinBid,
	)

	// Добавляем информацию о предыдущих ставках
	if len(previousBids) > 0 {
		message += "\n📊 *Ваши предыдущие ставки:*\n"
		for i, bid := range previousBids {
			formattedBidAmount := formatPriceFloat(bid.Amount)
			message += fmt.Sprintf("%d. %s руб. (%s)\n",
				i+1,
				formattedBidAmount,
				bid.BidTime.Time.Format("02.01.2006 15:04"))
		}
	}

	message += "\nВведите вашу новую ставку в рублях:"

	// УДАЛЯЕМ СТАРЫЕ СООБЩЕНИЯ СИНХРОННО
	oldMessages := messageManager.StartNewSession(userId)
	messageManager.CleanupSessionMessages(c.Bot(), userId, oldMessages)

	// Ждем немного чтобы удаление завершилось
	time.Sleep(300 * time.Millisecond)

	// Отправляем новое сообщение и сохраняем его ID
	msg, err := c.Bot().Send(c.Sender(), message, &telebot.SendOptions{
		ParseMode: telebot.ModeMarkdown,
	})
	
	if err != nil {
		// Сохраняем ID сообщения об ошибке, если отправка не удалась
		errorMsg := "❌ Произошла ошибка при отправке сообщения. Попробуйте снова."
		errorMsgObj, sendErr := c.Bot().Send(c.Sender(), errorMsg)
		if sendErr == nil {
			messageManager.AddMessage(userId, errorMsgObj.ID)
		}
		return err
	}

	// СОХРАНЯЕМ ID НОВОГО СООБЩЕНИЯ
	messageManager.AddMessage(userId, msg.ID)

	return nil
}
func handleMakeBid(c telebot.Context, queries *db.Queries) error {
	data := c.Data()
	parts := strings.Split(data, "|")
	if len(parts) != 2 {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка формата данных",
			ShowAlert: true,
		})
	}

	tenderID, _ := strconv.ParseInt(parts[0], 10, 32)
	userID := c.Sender().ID

	// Получаем информацию о тендере
	tender, err := queries.GetTender(context.Background(), int32(tenderID))
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка получения данных тендера",
			ShowAlert: true,
		})
	}

	// Проверяем, участвует ли пользователь в тендере
	isParticipating, err := queries.CheckTenderParticipation(context.Background(), db.CheckTenderParticipationParams{
		TenderID: int32(tenderID),
		UserID:   userID,
	})
	if err != nil || !isParticipating {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Вы не участвуете в этом тендере",
			ShowAlert: true,
		})
	}

	// Проверяем статус тендера
	if tender.Status != "active" {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Тендер не активен",
			ShowAlert: true,
		})
	}

	// Получаем предыдущие ставки
	previousBids, err := queries.GetUserBidsForTender(context.Background(), db.GetUserBidsForTenderParams{
		TenderID: int32(tenderID),
		UserID:   userID,
	})
	if err != nil {
		fmt.Printf("Ошибка получения предыдущих ставок: %v\n", err)
	}

	// Инициализируем данные ставки
	if _, exists := bidData[userID]; !exists {
		bidData[userID] = make(map[string]interface{})
	}

	bidData[userID]["tender_id"] = tender.ID
	bidData[userID]["tender_title"] = tender.Title
	bidData[userID]["start_price"] = tender.StartPrice
	bidData[userID]["previous_bids"] = previousBids
	bidData[userID]["current_price"] = tender.CurrentPrice

	bidStates[userID] = BidStateEnterPrice

	// Вычисляем диапазон ставок по вашей формуле
	minBid := tender.CurrentPrice - 10000.0
	maxBid := tender.CurrentPrice - (tender.StartPrice * 0.1)
	formattedCurrentPrice := formatPriceFloat(tender.CurrentPrice)
	formattedStartPrice := formatPriceFloat(tender.StartPrice)
	formattedMinBid := formatPriceFloat(minBid)
	formattedMaxBid := formatPriceFloat(maxBid)

	// Формируем сообщение
	message := fmt.Sprintf(
		"📋 *Тендер:* %s\n"+
			"💰 *Стартовая цена:* %s руб.\n"+
			"💰 *Текущая цена:* %s руб.\n"+
			"📊 *Диапазон ставок:* от %s до %s руб.\n",
		tender.Title,
		formattedStartPrice,
		formattedCurrentPrice,
		formattedMinBid,
		formattedMaxBid,
	)

	if len(previousBids) > 0 {
		message += "\n📊 *Ваши предыдущие ставки:*\n"
		for i, bid := range previousBids {
			formattedBidAmount := formatPriceFloat(bid.Amount)
			message += fmt.Sprintf("%d. %s руб. (%s)\n",
				i+1,
				formattedBidAmount,
				bid.BidTime.Time.Format("02.01.2006 15:04"))
		}
	}

	message += "\nВведите вашу новую ставку в рублях:"

	// Редактируем сообщение и сохраняем его ID
	err = c.Edit(message, &telebot.SendOptions{
		ParseMode: telebot.ModeMarkdown,
	})

	if err != nil {
		fmt.Printf("Ошибка при редактировании сообщения: %v\n", err)
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка при обновлении сообщения",
			ShowAlert: true,
		})
	}

	// СОХРАНЯЕМ ID СООБЩЕНИЯ ДЛЯ ПОСЛЕДУЮЩЕГО УДАЛЕНИЯ
	// Получаем ID текущего сообщения (которое мы только что отредактировали)
	messageID := c.Message().ID
	messageManager.AddMessage(userID, messageID)

	return nil
}
func handleViewBids(c telebot.Context, queries *db.Queries) error {
	data := c.Data()
	parts := strings.Split(data, "|")
	if len(parts) != 2 {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка формата данных",
			ShowAlert: true,
		})
	}

	tenderID, _ := strconv.ParseInt(parts[0], 10, 32)
	userID := c.Sender().ID

	// Получаем все ставки пользователя
	bids, err := queries.GetUserBidsForTender(context.Background(), db.GetUserBidsForTenderParams{
		TenderID: int32(tenderID),
		UserID:   userID,
	})
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка получения ставок",
			ShowAlert: true,
		})
	}

	if len(bids) == 0 {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "📭 У вас нет ставок в этом тендере",
			ShowAlert: true,
		})
	}

	// Формируем сообщение с историей ставок
	message := "📊 *История ваших ставок*\n\n"
	for i, bid := range bids {
		message += fmt.Sprintf("%d. *%.2f руб.* - %s\n",
			i+1,
			bid.Amount,
			bid.BidTime.Time.Format("02.01.2006 15:04"))
	}

	// Редактируем сообщение
	err = c.Edit(message, &telebot.SendOptions{
		ParseMode: telebot.ModeMarkdown,
		ReplyMarkup: &telebot.ReplyMarkup{
			InlineKeyboard: [][]telebot.InlineButton{
				{
					{Unique: "make_bid", Text: "💵 Сделать новую ставку", Data: fmt.Sprintf("%d|%d", tenderID, userID)},
				},
			},
		},
	})

	if err != nil {
		fmt.Printf("Ошибка при редактировании сообщения: %v\n", err)
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка при обновлении сообщения",
			ShowAlert: true,
		})
	}

	// СОХРАНЯЕМ ID СООБЩЕНИЯ ДЛЯ ПОСЛЕДУЮЩЕГО УДАЛЕНИЯ
	messageID := c.Message().ID
	messageManager.AddMessage(userID, messageID)
	_, err = c.Bot().Send(c.Sender(), " ", &telebot.SendOptions{
		ReplyMarkup: menu.MenuSupplierRegistered,
	})
	if err != nil {
		fmt.Printf("Ошибка при восстановлении клавиатуры: %v\n", err)
	}
	// ОТПРАВЛЯЕМ ПУСТОЙ RESPONSE ЧТОБЫ УБРАТЬ "ЧАСИКИ"
	return c.Respond()
}

func handleBidText(c telebot.Context, queries *db.Queries, text string, userID int64, state BidState) error {
	switch state {
	case BidStateEnterPrice:
		// Проверяем, что все необходимые данные существуют
		if bidData[userID] == nil {
			errorMsg := "❌ Ошибка данных. Начните процесс подачи ставки заново."
			msg, err := c.Bot().Send(c.Sender(), errorMsg)
			if err == nil {
				messageManager.AddMessage(userID, msg.ID)
			}
			return err
		}

		// Парсим введенную сумму
		bidAmount, err := strconv.ParseFloat(text, 64)
		if err != nil {
			errorMsg := "❌ Введите корректную сумму (например: 15000.50):"
			msg, err := c.Bot().Send(c.Sender(), errorMsg)
			if err == nil {
				messageManager.AddMessage(userID, msg.ID)
			}
			return err
		}

		// Безопасно получаем данные тендера с проверкой типов
		currentPrice, ok := bidData[userID]["current_price"].(float64)
		if !ok {
			errorMsg := "❌ Ошибка данных. Начните процесс подачи ставки заново."
			msg, err := c.Bot().Send(c.Sender(), errorMsg)
			if err == nil {
				messageManager.AddMessage(userID, msg.ID)
			}
			return err
		}

		startPrice, ok := bidData[userID]["start_price"].(float64)
		if !ok {
			errorMsg := "❌ Ошибка данных. Начните процесс подачи ставки заново."
			msg, err := c.Bot().Send(c.Sender(), errorMsg)
			if err == nil {
				messageManager.AddMessage(userID, msg.ID)
			}
			return err
		}

		previousBids, ok := bidData[userID]["previous_bids"].([]db.TenderBid)
		if !ok {
			// Если предыдущих ставок нет, создаем пустой слайс
			previousBids = []db.TenderBid{}
		}

		tenderTitle, ok := bidData[userID]["tender_title"].(string)
		if !ok {
			errorMsg := "❌ Ошибка данных. Начните процесс подачи ставки заново."
			msg, err := c.Bot().Send(c.Sender(), errorMsg)
			if err == nil {
				messageManager.AddMessage(userID, msg.ID)
			}
			return err
		}

		// Вычисляем минимальную и максимальную ставки по вашей формуле
		minBid := currentPrice - 10000.0
		maxBid := currentPrice - (startPrice * 0.1) // Исправленная формула

		if bidAmount > minBid {
			errorMsg := fmt.Sprintf(
				"❌ Ставка не может превышать %.2f руб. Введите другую сумму:",
				minBid,
			)
			msg, err := c.Bot().Send(c.Sender(), errorMsg)
			if err == nil {
				messageManager.AddMessage(userID, msg.ID)
			}
			return err
		}

		if bidAmount < maxBid {
			errorMsg := fmt.Sprintf(
				"❌ Ставка не может быть меньше %.2f руб. Введите другую сумму:",
				maxBid,
			)
			msg, err := c.Bot().Send(c.Sender(), errorMsg)
			if err == nil {
				messageManager.AddMessage(userID, msg.ID)
			}
			return err
		}

		// Проверяем, не делал ли пользователь такую же ставку ранее
		for _, prevBid := range previousBids {
			if prevBid.Amount == bidAmount {
				errorMsg := "❌ Вы уже делали ставку на эту сумму. Введите другую сумму:"
				msg, err := c.Bot().Send(c.Sender(), errorMsg)
				if err == nil {
					messageManager.AddMessage(userID, msg.ID)
				}
				return err
			}
		}

		// Сохраняем ставку
		bidData[userID]["bid_amount"] = bidAmount
		bidStates[userID] = BidStateConfirm

		// Создаем клавиатуру подтверждения
		markup := &telebot.ReplyMarkup{
			InlineKeyboard: [][]telebot.InlineButton{
				{
					{Unique: "confirm_bid", Text: "✅ Подтвердить ставку"},
					{Unique: "cancel_bid", Text: "❌ Отменить"},
				},
			},
		}
		formattedBidAmount := formatPriceFloat(bidAmount)
		formattedMinBid := formatPriceFloat(minBid)
		formattedMaxBid := formatPriceFloat(maxBid)

		// Формируем сообщение с информацией о всех ставках
		message := fmt.Sprintf(
			"📊 *Подтверждение ставки*\n\n"+
				"📋 Тендер: %s\n"+
				"💰 Новая ставка: *%s руб.*\n"+
				"📊 *Диапазон ставок:* от %s до %s руб.\n",
			tenderTitle,
			formattedBidAmount,
			formattedMinBid,
			formattedMaxBid,
		)

		// Добавляем информацию о предыдущих ставках
		if len(previousBids) > 0 {
			message += "\n📈 *Все ваши ставки в этом тендере:*\n"
			for i, bid := range previousBids {
				formattedBidAmount := formatPriceFloat(bid.Amount)
				message += fmt.Sprintf("%d. %s руб. (%s)\n",
					i+1,
					formattedBidAmount,
					bid.BidTime.Time.Format("02.01.2006 15:04"))
			}
			message += fmt.Sprintf("%d. 🆕 *%s руб.* (новая)\n", len(previousBids)+1, formattedBidAmount)
		}

		message += "\nПодтверждаете новую ставку?"

		msg, err := c.Bot().Send(c.Sender(), message, &telebot.SendOptions{
			ParseMode:   telebot.ModeMarkdown,
			ReplyMarkup: markup,
		})

		if err != nil {
			fmt.Printf("Ошибка при отправке сообщения подтверждения: %v\n", err)
			// Сохраняем сообщение об ошибке отправки
			errorMsg := "❌ Произошла ошибка при отправке сообщения. Попробуйте снова."
			errorMsgObj, sendErr := c.Bot().Send(c.Sender(), errorMsg)
			if sendErr == nil {
				messageManager.AddMessage(userID, errorMsgObj.ID)
			}
			return err
		}

		// СОХРАНЯЕМ ID СООБЩЕНИЯ
		messageManager.AddMessage(userID, msg.ID)

		return nil

	default:
		return nil
	}
}

func handleConfirmBid(c telebot.Context, queries *db.Queries) error {
	userID := c.Sender().ID

	if _, exists := bidData[userID]; !exists {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Данные ставки не найдены",
			ShowAlert: true,
		})
	}

	tenderID := bidData[userID]["tender_id"].(int32)
	bidAmount := bidData[userID]["bid_amount"].(float64)
	tenderTitle := bidData[userID]["tender_title"].(string)

	ctx := context.Background()

	// Сохраняем ставку в базу данных
	err := queries.CreateBid(ctx, db.CreateBidParams{
		TenderID: tenderID,
		UserID:   userID,
		Amount:   bidAmount,
		BidTime:  pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		fmt.Printf("Ошибка сохранения ставки: %v\n", err)
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка сохранения ставки",
			ShowAlert: true,
		})
	}

	// ОБНОВЛЯЕМ ТЕКУЩУЮ ЦЕНУ ТЕНДЕРА
	err = queries.UpdateTenderCurrentPrice(ctx, db.UpdateTenderCurrentPriceParams{
		ID:           tenderID,
		CurrentPrice: bidAmount, // Новая ставка становится текущей ценой
	})
	if err != nil {
		fmt.Printf("Ошибка обновления текущей цены тендера: %v\n", err)
		// Не прерываем процесс, так как ставка уже сохранена
	}

	// Получаем все ставки пользователя в этом тендере для отображения
	allBids, err := queries.GetUserBidsForTender(ctx, db.GetUserBidsForTenderParams{
		TenderID: tenderID,
		UserID:   userID,
	})
	if err != nil {
		fmt.Printf("Ошибка получения списка ставок: %v\n", err)
	}

	// Получаем актуальную информацию о тендере (с обновленной ценой)
	updatedTender, err := queries.GetTender(ctx, tenderID)
	if err != nil {
		fmt.Printf("Ошибка получения обновленной информации о тендере: %v\n", err)
		// Если не удалось получить обновленный тендер, используем bidAmount как текущую цену
		updatedTender.CurrentPrice = bidAmount
	}

	formattedBidAmount := formatPriceFloat(bidAmount)
	formattedCurrentPrice := formatPriceFloat(updatedTender.CurrentPrice)

	// Формируем сообщение для пользователя, который сделал ставку
	message := fmt.Sprintf(
		"✅ *Новая ставка успешно подана!*\n\n"+
			"📋 Тендер: %s\n"+
			"💰 Новая ставка: *%s руб.*\n"+
			"💰 *Новая текущая цена тендера:* %s руб.\n",
		tenderTitle,
		formattedBidAmount,
		formattedCurrentPrice,
	)

	if len(allBids) > 0 {
		message += "\n📊 *Все ваши ставки в этом тендере:*\n"
		for i, bid := range allBids {
			indicator := ""
			if bid.Amount == bidAmount {
				indicator = " 🆕"
			}
			formattedAmount := formatPriceFloat(bid.Amount)
			message += fmt.Sprintf("%d. %s руб. (%s)%s\n",
				i+1,
				formattedAmount,
				bid.BidTime.Time.Format("02.01.2006 15:04"),
				indicator)
		}
	}

	message += "\nВы можете сделать еще одну ставку, нажав кнопку ниже:"

	// Создаем кнопку для новой ставки
	markup := &telebot.ReplyMarkup{
		InlineKeyboard: [][]telebot.InlineButton{
			{
				{Unique: "make_bid", Text: "💵 Сделать еще одну ставку", Data: fmt.Sprintf("%d|%d", tenderID, userID)},
			},
		},
	}

	// Обновляем сообщение для пользователя, который сделал ставку
	_, err = c.Bot().Edit(c.Message(), message, &telebot.SendOptions{
		ParseMode:   telebot.ModeMarkdown,
		ReplyMarkup: markup,
	})

	if err != nil {
		fmt.Printf("Ошибка обновления сообщения: %v\n", err)
		return c.Respond(&telebot.CallbackResponse{
			Text: "✅ Ставка успешно подана!",
		})
	}

	keyboardMsg, err := c.Bot().Send(c.Sender(), " ", &telebot.SendOptions{
		ReplyMarkup: menu.MenuSupplierRegistered,
	})
	if err == nil {
		messageManager.AddMessage(userID, keyboardMsg.ID)
	}

	// РАССЫЛАЕМ УВЕДОМЛЕНИЯ ДРУГИМ УЧАСТНИКАМ ТЕНДЕРА
	go sendBidNotificationToOtherParticipants(c.Bot(), queries, tenderID, userID, tenderTitle, bidAmount, updatedTender.CurrentPrice)

	// Очищаем состояние
	delete(bidStates, userID)
	delete(bidData, userID)

	

	messageManager.CleanupOldMessages(c.Bot(), userID, 2)

	return c.Respond()
}

// Функция для рассылки уведомлений другим участникам
func sendBidNotificationToOtherParticipants(bot *telebot.Bot, queries *db.Queries, tenderID int32, bidderUserID int64, tenderTitle string, bidAmount float64, currentPrice float64) {

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Получаем информацию о пользователе, который сделал ставку
	bidderUser, err := queries.GetUserByTelegramID(ctx, bidderUserID)
	if err != nil {
		fmt.Printf("Ошибка получения информации о пользователе %d: %v\n", bidderUserID, err)
		return
	}

	// Получаем всех участников тендера
	userIds, err := queries.GetParticipantsForTender(ctx, tenderID)
	if err != nil {
		fmt.Printf("Ошибка получения участников тендера %d: %v\n", tenderID, err)
		return
	}

	// Форматируем цены для красивого отображения
	formattedBidAmount := formatPriceFloat(bidAmount)
	formattedCurrentPrice := formatPriceFloat(currentPrice)

	// Формируем сообщение для других участников
	messageForUsers := fmt.Sprintf(
		"📢 *Новая ставка в тендере!*\n\n"+
			"📋 Тендер: %s\n"+
			"👤 Участник: %s\n"+
			"💰 Новая ставка: *%s руб.*\n"+
			"💰 Текущая цена тендера: *%s руб.*\n\n"+
			"💡 *Не упустите возможность сделать свою ставку!*",
		tenderTitle,
		bidderUser.OrganizationName.String,
		formattedBidAmount,
		formattedCurrentPrice,
	)

	fmt.Printf("Тендер %s имеет %d участников\n", tenderTitle, len(userIds))

	// Отправляем уведомления всем участникам, кроме того, кто сделал ставку
	for _, userId := range userIds {
		if userId == bidderUserID {
			continue // Пропускаем пользователя, который сделал ставку
		}

		_, err := bot.Send(&telebot.User{ID: userId}, messageForUsers, &telebot.SendOptions{
			ParseMode: telebot.ModeMarkdown,
			ReplyMarkup: &telebot.ReplyMarkup{
				InlineKeyboard: [][]telebot.InlineButton{
					{
						{Unique: "make_bid", Text: "💵 Сделать ставку", Data: fmt.Sprintf("%d|%d", tenderID, userId)},
					},
				},
			},
		})
		if err != nil {
			fmt.Printf("Ошибка отправки уведомления пользователю %d: %v\n", userId, err)
			time.Sleep(100 * time.Millisecond) // Задержка чтобы не превысить лимиты Telegram
		} else {
			fmt.Printf("Уведомление отправлено пользователю %d для тендера %s\n", userId, tenderTitle)
		}
	}
}

func handleSupplierClassification(c telebot.Context, classCode string) error {
	userID := c.Sender().ID
	if _, ok := supplierData[userID]; !ok {
		supplierData[userID] = make(map[string]string)
	}

	data := supplierData[userID]["classifications"]
	selected := strings.Split(data, ",")
	selectedSet := make(map[string]bool)
	for _, s := range selected {
		if s != "" {
			selectedSet[s] = true
		}
	}

	if selectedSet[classCode] {
		delete(selectedSet, classCode)
	} else {
		if len(selectedSet) >= 2 {
			return c.Respond(&telebot.CallbackResponse{
				Text:      "Можно выбрать только две классификации!",
				ShowAlert: true,
			})
		}
		selectedSet[classCode] = true
	}

	var newSelected []string
	for _, code := range allCodes {
		if selectedSet[code] {
			newSelected = append(newSelected, code)
		}
	}
	supplierData[userID]["classifications"] = strings.Join(newSelected, ",")

	markup := showSupplierClassificationKeyboard(userID)

	msg := c.Message()
	currentText := "Выберите до двух классификаций вашей организации:"
	if msg != nil && msg.Text != "" {
		currentText = msg.Text
	}

	return c.Edit(currentText, &telebot.SendOptions{ReplyMarkup: markup})
}

func handleSupplierClassificationDone(c telebot.Context) error {
	userID := c.Sender().ID
	data := supplierData[userID]["classifications"]

	if data == "" {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "Выберите хотя бы одну классификацию!",
			ShowAlert: true,
		})
	}

	codes := strings.Split(data, ",")
	var selectedNames []string
	for _, code := range codes {
		if name, ok := classificationNames[code]; ok {
			selectedNames = append(selectedNames, name)
		}
	}

	supplierStates[userID] = StateFIO

	return c.Edit(
		fmt.Sprintf("Выбранные классификации:\n%s\n\nВведите ФИО участника:", strings.Join(selectedNames, ", ")),
		&telebot.SendOptions{
			ReplyMarkup: nil,
		},
	)
}

func handleJoinTender(c telebot.Context, queries *db.Queries) error {
	data := c.Data()
	parts := strings.Split(data, "|")
	if len(parts) != 2 {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка: неверный формат данных",
			ShowAlert: true,
		})
	}

	tenderID, _ := strconv.ParseInt(parts[0], 10, 32)
	userID := c.Sender().ID

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Получаем информацию о тендере для проверки статуса
	tender, err := queries.GetTender(ctx, int32(tenderID))
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка получения информации о тендере",
			ShowAlert: true,
		})
	}

	// Проверяем, активен ли тендер и начался ли он
	if !isTenderActiveAndStarted(tender) {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Тендер еще не начался или не активен",
			ShowAlert: true,
		})
	}

	// Проверяем, участвует ли пользователь уже в других тендерах
	hasOtherParticipation, err := queries.CheckUserHasAnyTenderParticipation(ctx, db.CheckUserHasAnyTenderParticipationParams{
		UserID:   userID,
		TenderID: int32(tenderID),
	})
	if err != nil {
		fmt.Printf("Ошибка при проверке участия пользователя: %v\n", err)
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка при проверке участия",
			ShowAlert: true,
		})
	}

	if hasOtherParticipation {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Вы уже участвуете в другом тендере. Для участия в этом тендере необходимо сначала отменить участие в текущем тендере.",
			ShowAlert: true,
		})
	}

	// Проверяем, не участвует ли пользователь уже в этом тендере
	isAlreadyParticipating, err := queries.CheckTenderParticipation(ctx, db.CheckTenderParticipationParams{
		TenderID: int32(tenderID),
		UserID:   userID,
	})
	if err != nil {
		fmt.Printf("Ошибка при проверке участия в тендере: %v\n", err)
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка при проверке участия",
			ShowAlert: true,
		})
	}

	if isAlreadyParticipating {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Вы уже участвуете в этом тендере",
			ShowAlert: true,
		})
	}

	// Добавляем пользователя в тендер
	err = queries.JoinTender(ctx, db.JoinTenderParams{
		ID:     int32(tenderID),
		UserID: userID,
	})
	if err != nil {
		fmt.Printf("Ошибка при попытке участвовать в тендере: %v\n", err)
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Не удалось присоединиться к тендеру",
			ShowAlert: true,
		})
	}

	// Получаем актуальную информацию о тендере
	updatedTender, err := queries.GetTender(ctx, int32(tenderID))
	if err != nil {
		fmt.Printf("Ошибка при получении информации о тендере: %v\n", err)
		return c.Respond(&telebot.CallbackResponse{
			Text: "✅ Вы участвуете в тендере!",
		})
	}

	// ОБНОВЛЯЕМ СООБЩЕНИЕ С ТЕНДЕРОМ с новыми кнопками
	return updateTenderMessageAfterJoin(c, updatedTender, userID)
}

func isTenderActiveAndStarted(tender db.Tender) bool {
	// Проверяем статус тендера
	if tender.Status != "active" {
		return false
	}

	// Проверяем, начался ли тендер (текущее время после времени начала)
	if tender.StartAt.Valid {
		return time.Now().After(tender.StartAt.Time)
	}

	// Если время начала не указано, считаем что тендер начался
	return true
}

// Функция для обновления сообщения тендера после участия
func updateTenderMessageAfterJoin(c telebot.Context, tender db.Tender, userID int64) error {
	// Форматируем дату
	var formattedDate string
	if tender.StartAt.Valid {
		formattedDate = tender.StartAt.Time.Format("02.01.2006 15:04")
	} else {
		formattedDate = "не указана"
	}

	// Форматируем цены
	formattedPrice := formatPriceFloat(tender.StartPrice)
	formattedCurrentPrice := formatPriceFloat(tender.CurrentPrice)

	// Форматируем статус с эмодзи
	statusEmoji, statusText := getStatusWithEmoji(tender.Status)

	// Создаем сообщение с информацией о тендере
	tenderInfo := fmt.Sprintf(
		"📋 *Тендер:* %s\n\n"+
			"📝 *Описание:* %s\n"+
			"💰 *Стартовая цена:* %s руб.\n"+
			"💰 *Текущая цена:* %s руб.\n"+
			"📅 *Дата начала:* %s\n"+
			"🗂️ *Классификация:* %s\n"+
			"%s *Статус:* %s\n\n"+
			"👥 *Участников:* %d\n\n"+
			"✅ *Вы участвуете в этом тендере*",

		tender.Title,
		tender.Description.String,
		formattedPrice,
		formattedCurrentPrice,
		formattedDate,
		classificationNames[tender.Classification.String],
		statusEmoji,
		statusText,
		tender.ParticipantsCount,
	)

	// Создаем кнопки для участника
	var actionButtons []telebot.InlineButton

	// Кнопка подачи ставки
	actionButtons = append(actionButtons, telebot.InlineButton{
		Unique: "make_bid",
		Text:   "💵 Подать ставку",
		Data:   fmt.Sprintf("%d|%d", tender.ID, userID),
	})

	// Кнопка истории ставок (показываем сразу, даже если ставок еще нет)
	actionButtons = append(actionButtons, telebot.InlineButton{
		Unique: "view_bids",
		Text:   "📊 Мои ставки",
		Data:   fmt.Sprintf("%d|%d", tender.ID, userID),
	})

	// Кнопка отмены участия
	actionButtons = append(actionButtons, telebot.InlineButton{
		Unique: "leave_tender",
		Text:   "❌ Отменить участие",
		Data:   fmt.Sprintf("%d|%d", tender.ID, userID),
	})

	// Разбиваем кнопки на строки (максимум 2 кнопки в строке)
	var inlineKeyboard [][]telebot.InlineButton
	for i := 0; i < len(actionButtons); i += 2 {
		end := i + 2
		if end > len(actionButtons) {
			end = len(actionButtons)
		}
		inlineKeyboard = append(inlineKeyboard, actionButtons[i:end])
	}

	// Обновляем сообщение
	_, err := c.Bot().Edit(c.Message(), tenderInfo, &telebot.SendOptions{
		ParseMode: telebot.ModeMarkdown,
		ReplyMarkup: &telebot.ReplyMarkup{
			InlineKeyboard: inlineKeyboard,
		},
	})

	if err != nil {
		fmt.Printf("Ошибка при обновлении сообщения: %v\n", err)
		return c.Respond(&telebot.CallbackResponse{
			Text: "✅ Вы теперь участвуете в тендере!",
		})
	}

	return c.Respond(&telebot.CallbackResponse{
		Text: "✅ Вы участвуете в тендере!",
	})
}

func handleLeaveTender(c telebot.Context, queries *db.Queries) error {
	data := c.Data()
	parts := strings.Split(data, "|")
	if len(parts) != 2 {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Ошибка: неверный формат данных",
			ShowAlert: true,
		})
	}

	tenderID, _ := strconv.ParseInt(parts[0], 10, 32)
	userID := c.Sender().ID

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := queries.LeaveTender(ctx, db.LeaveTenderParams{
		ID:     int32(tenderID),
		UserID: userID,
	})
	if err != nil {
		fmt.Printf("Ошибка при отмене участия в тендере: %v\n", err)
		return c.Respond(&telebot.CallbackResponse{
			Text:      "❌ Не удалось отменить участие",
			ShowAlert: true,
		})
	}

	tender, err := queries.GetTender(ctx, int32(tenderID))
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{
			Text: "❌ Вы больше не участвуете в тендере",
		})
	}

	// ОБНОВЛЯЕМ СООБЩЕНИЕ С ТЕНДЕРОМ - возвращаем кнопку "Участвовать"
	return updateTenderMessageAfterLeave(c, tender, userID)
}

// Функция для обновления сообщения после выхода из тендера
func updateTenderMessageAfterLeave(c telebot.Context, tender db.Tender, userID int64) error {
	// Форматируем дату
	var formattedDate string
	if tender.StartAt.Valid {
		formattedDate = tender.StartAt.Time.Format("02.01.2006 15:04")
	} else {
		formattedDate = "не указана"
	}

	// Форматируем цены
	formattedPrice := formatPriceFloat(tender.StartPrice)
	formattedCurrentPrice := formatPriceFloat(tender.CurrentPrice)

	// Форматируем статус с эмодзи
	statusEmoji, statusText := getStatusWithEmoji(tender.Status)

	// Создаем сообщение с информацией о тендере
	tenderInfo := fmt.Sprintf(
		"📋 *Тендер:* %s\n\n"+
			"📝 *Описание:* %s\n"+
			"💰 *Стартовая цена:* %s руб.\n"+
			"💰 *Текущая цена:* %s руб.\n"+
			"📅 *Дата начала:* %s\n"+
			"🗂️ *Классификация:* %s\n"+
			"%s *Статус:* %s\n\n"+
			"👥 *Участников:* %d",

		tender.Title,
		tender.Description.String,
		formattedPrice,
		formattedCurrentPrice,
		formattedDate,
		classificationNames[tender.Classification.String],
		statusEmoji,
		statusText,
		tender.ParticipantsCount,
	)

	// Создаем кнопку "Участвовать"
	inlineKeyboard := [][]telebot.InlineButton{
		{
			{
				Unique: "join_tender",
				Text:   "📝 Участвовать в тендере",
				Data:   fmt.Sprintf("%d|%d", tender.ID, userID),
			},
		},
	}

	// Обновляем сообщение
	_, err := c.Bot().Edit(c.Message(), tenderInfo, &telebot.SendOptions{
		ParseMode: telebot.ModeMarkdown,
		ReplyMarkup: &telebot.ReplyMarkup{
			InlineKeyboard: inlineKeyboard,
		},
	})

	if err != nil {
		fmt.Printf("Ошибка при обновлении сообщения: %v\n", err)
		return c.Respond(&telebot.CallbackResponse{
			Text: "❌ Вы больше не участвуете в тендере",
		})
	}

	return c.Respond(&telebot.CallbackResponse{
		Text: "❌ Вы больше не участвуете в тендере",
	})
}
func showSupplierClassificationKeyboard(userID int64) *telebot.ReplyMarkup {
	selectedCodes := strings.Split(supplierData[userID]["classifications"], ",")
	selectedSet := make(map[string]bool)
	for _, code := range selectedCodes {
		if code != "" {
			selectedSet[code] = true
		}
	}

	var rows [][]telebot.InlineButton
	for _, code := range allCodes {
		name := classificationNames[code]
		text := name
		if selectedSet[code] {
			text = "✅ " + name
		}
		btn := telebot.InlineButton{Unique: "supplier_class_" + code, Text: text}
		rows = append(rows, []telebot.InlineButton{btn})
	}

	if len(selectedSet) > 0 {
		rows = append(rows, []telebot.InlineButton{{Unique: "supplier_class_done", Text: "Завершить выбор"}})
	}

	markup := &telebot.ReplyMarkup{InlineKeyboard: rows}
	return markup
}

// Остальные функции поставщика (sendSupplierTendersList, updateTenderMessage и т.д.)
// нужно скопировать из вашего кода

func sendSupplierTendersList(c telebot.Context, queries *db.Queries, userId int64) error {
	oldMessages := messageManager.StartNewSession(userId)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	user, err := queries.GetUserByTelegramID(ctx, userId)
	if err != nil {
		fmt.Printf("Ошибка получения информации о пользователе: %v\n", err)
		msg, err := c.Bot().Send(c.Sender(), "Не удалось получить информацию о пользователе", &telebot.SendOptions{
			ReplyMarkup: menu.MenuSupplierRegistered,
		})
		if err == nil {
			messageManager.AddMessage(userId, msg.ID)
		}
		messageManager.CleanupSessionMessages(c.Bot(), userId, oldMessages)
		return err
	}

	classifications := strings.Split(user.Classification.String, ",")
	tenders, err := queries.GetTendersForSuppliers(ctx, db.GetTendersForSuppliersParams{
		Classification: pgtype.Text{
			String: classifications[0],
			Valid:  true,
		},
		Classification_2: pgtype.Text{
			String: classifications[1],
			Valid:  true,
		},
	})

	if err != nil {
		fmt.Printf("Ошибка получения тендеров: %v\n", err)
		msg, err := c.Bot().Send(c.Sender(), "Не удалось получить список тендеров", &telebot.SendOptions{
			ReplyMarkup: menu.MenuSupplierRegistered,
		})
		if err == nil {
			messageManager.AddMessage(userId, msg.ID)
		}
		messageManager.CleanupSessionMessages(c.Bot(), userId, oldMessages)
		return err
	}

	if len(tenders) == 0 {
		msg, err := c.Bot().Send(c.Sender(), "Нет доступных тендеров", &telebot.SendOptions{
			ReplyMarkup: menu.MenuSupplierRegistered,
		})
		if err == nil {
			messageManager.AddMessage(userId, msg.ID)
		}
		messageManager.CleanupSessionMessages(c.Bot(), userId, oldMessages)
		return err
	}

	for _, tender := range tenders {
		// Проверяем, участвует ли пользователь в тендере
		isParticipating, err := queries.CheckTenderParticipation(ctx, db.CheckTenderParticipationParams{
			TenderID: tender.ID,
			UserID:   userId,
		})
		if err != nil {
			fmt.Printf("Ошибка проверки участия в тендере %d: %v\n", tender.ID, err)
			isParticipating = false
		}

		// Форматируем дату для красивого вывода
		var formattedDate string
		if tender.StartAt.Valid {
			formattedDate = tender.StartAt.Time.Format("02.01.2006 15:04")
		} else {
			formattedDate = "не указана"
		}

		// Форматируем цену в финансовом формате
		formattedPrice := formatPriceFloat(tender.StartPrice)
		formattedCurrentPrice := formatPriceFloat(tender.CurrentPrice)

		// Форматируем статус с эмодзи
		statusEmoji, statusText := getStatusWithEmoji(tender.Status)

		// Создаем сообщение с информацией о тендере
		tenderInfo := fmt.Sprintf(
			"📋 *Тендер:* %s\n\n"+
				"📝 *Описание:* %s\n"+
				"💰 *Стартовая цена:* %s руб.\n"+
				"💰 *Текущая цена:* %s руб.\n"+
				"📅 *Дата начала:* %s\n"+
				"🗂️ *Классификация:* %s\n"+
				"%s *Статус:* %s\n\n"+
				"👥 *Участников:* %d",

			tender.Title,
			tender.Description.String,
			formattedPrice,
			formattedCurrentPrice,
			formattedDate,
			classificationNames[tender.Classification.String],
			statusEmoji,
			statusText,
			tender.ParticipantsCount,
		)

		// Создаем кнопки в зависимости от участия пользователя
		var inlineKeyboard [][]telebot.InlineButton

		if isParticipating {
			var actionButtons []telebot.InlineButton

			// Всегда показываем кнопку для подачи ставки
			actionButtons = append(actionButtons, telebot.InlineButton{
				Unique: "make_bid",
				Text:   "💵 Подать ставку",
				Data:   fmt.Sprintf("%d|%d", tender.ID, userId),
			})

			// Показываем кнопку для просмотра истории ставок
			bidCount, err := queries.GetUserBidCount(ctx, db.GetUserBidCountParams{
				TenderID: tender.ID,
				UserID:   userId,
			})

			if err != nil {
				fmt.Printf("Ошибка получения количества ставок: %v\n", err)
				bidCount = 0
			}

			if bidCount > 0 {
				actionButtons = append(actionButtons, telebot.InlineButton{
					Unique: "view_bids",
					Text:   fmt.Sprintf("📊 Мои ставки (%d)", bidCount),
					Data:   fmt.Sprintf("%d|%d", tender.ID, userId),
				})
			}

			actionButtons = append(actionButtons, telebot.InlineButton{
				Unique: "leave_tender",
				Text:   "❌ Выйти",
				Data:   fmt.Sprintf("%d|%d", tender.ID, userId),
			})

			// Разбиваем кнопки на строки (максимум 2 кнопки в строке)
			for i := 0; i < len(actionButtons); i += 2 {
				end := i + 2
				if end > len(actionButtons) {
					end = len(actionButtons)
				}
				inlineKeyboard = append(inlineKeyboard, actionButtons[i:end])
			}
		} else {
			// Если не участвует - показываем только кнопку участия
			inlineKeyboard = [][]telebot.InlineButton{
				{
					{
						Unique: "join_tender",
						Text:   "📝 Участвовать в тендере",
						Data:   fmt.Sprintf("%d|%d", tender.ID, userId),
					},
				},
			}
		}

		// Отправляем информацию о тендере
		msg, err := c.Bot().Send(c.Sender(), tenderInfo, &telebot.SendOptions{
			ParseMode: telebot.ModeMarkdown,
			ReplyMarkup: &telebot.ReplyMarkup{
				InlineKeyboard: inlineKeyboard,
			},
		})
		if err != nil {
			fmt.Printf("Ошибка при отправке информации о тендере: %v\n", err)
			continue
		}

		messageManager.AddMessage(userId, msg.ID)

		// Если есть прикрепленный файл, отправляем его
		if tender.ConditionsPath.Valid && tender.ConditionsPath.String != "" {
			filePath := tender.ConditionsPath.String

			// Проверяем существование файла
			if _, err := os.Stat(filePath); err == nil {
				// Отправляем сообщение о файле и сохраняем его ID
				fileCaptionMsg, err := c.Bot().Send(c.Sender(), "📎 Файл с условиями тендера:", &telebot.SendOptions{
					ReplyMarkup: menu.MenuSupplierRegistered,
				})
				if err != nil {
					fmt.Printf("Ошибка при отправке сообщения о файле: %v\n", err)
					continue
				}
				messageManager.AddMessage(userId, fileCaptionMsg.ID)

				// Отправляем сам файл и сохраняем его ID
				fileName := filepath.Base(filePath)
				fileToSend := &telebot.Document{
					File:     telebot.FromDisk(filePath),
					FileName: fileName,
				}

				fileMsg, err := c.Bot().Send(c.Sender(), fileToSend, &telebot.SendOptions{
					ReplyMarkup: menu.MenuSupplierRegistered,
				})
				if err != nil {
					fmt.Printf("Ошибка при отправке файла тендера: %v\n", err)
				} else {
					messageManager.AddMessage(userId, fileMsg.ID)
				}
			} else {
				fmt.Printf("Файл не найден: %s\n", filePath)
				// Отправляем сообщение об ошибке и сохраняем его ID
				errorMsg, err := c.Bot().Send(c.Sender(), "❌ Файл условий недоступен", &telebot.SendOptions{
					ReplyMarkup: menu.MenuSupplierRegistered,
				})
				if err != nil {
					fmt.Printf("Ошибка при отправке сообщения об отсутствии файла: %v\n", err)
				} else {
					messageManager.AddMessage(userId, errorMsg.ID)
				}
			}
		} else {
			// Если файла нет, отправляем сообщение об этом и сохраняем его ID
			noFileMsg, err := c.Bot().Send(c.Sender(), "📭 Файл условий не прикреплен", &telebot.SendOptions{
				ReplyMarkup: menu.MenuSupplierRegistered,
			})
			if err != nil {
				fmt.Printf("Ошибка при отправке сообщения об отсутствии файла: %v\n", err)
			} else {
				messageManager.AddMessage(userId, noFileMsg.ID)
			}
		}

		// Добавляем разделитель между тендерами и сохраняем его ID
		dividerMsg, err := c.Bot().Send(c.Sender(), "➖➖➖➖➖➖➖➖➖➖", &telebot.SendOptions{
			ReplyMarkup: menu.MenuSupplierRegistered,
		})
		if err != nil {
			fmt.Printf("Ошибка при отправке разделителя: %v\n", err)
		} else {
			messageManager.AddMessage(userId, dividerMsg.ID)
		}

		// Небольшая задержка между отправками чтобы не превысить лимиты Telegram
		time.Sleep(500 * time.Millisecond)
	}

	// Отправляем итоговое сообщение
	finalMsg, err := c.Bot().Send(c.Sender(), fmt.Sprintf("✅ Всего тендеров: %d", len(tenders)), &telebot.SendOptions{
		ReplyMarkup: menu.MenuSupplierRegistered,
	})
	if err == nil {
		messageManager.AddMessage(userId, finalMsg.ID)
	}

	// УДАЛЯЕМ ВСЕ СТАРЫЕ СООБЩЕНИЯ ИЗ ПРЕДЫДУЩЕЙ СЕССИИ
	messageManager.CleanupSessionMessages(c.Bot(), userId, oldMessages)
	return nil
}

func updateTenderMessage(c telebot.Context, tender db.Tender, userID int64, justJoined bool) error {
	// Форматируем дату
	var formattedDate string
	if tender.StartAt.Valid {
		formattedDate = tender.StartAt.Time.Format("02.01.2006 15:04")
	} else {
		formattedDate = "не указана"
	}

	// Форматируем цену
	formattedPrice := formatPriceFloat(tender.StartPrice)

	// Форматируем статус с эмодзи
	statusEmoji, statusText := getStatusWithEmoji(tender.Status)

	// Создаем сообщение с информацией о тендере
	// Форматируем текущую цену
	currentPriceFormatted := formatPriceFloat(tender.CurrentPrice)

	// Создаем сообщение с информацией о тендере
	tenderInfo := fmt.Sprintf(
		"📋 *Тендер:* %s\n\n"+
			"📝 *Описание:* %s\n"+
			"💰 *Стартовая цена:* %s руб.\n"+
			"💰 *Текущая цена:* %s руб.\n"+ // ДОБАВЬТЕ ЭТУ СТРОКУ
			"📅 *Дата начала:* %s\n"+
			"🗂️ *Классификация:* %s\n"+
			"%s *Статус:* %s\n\n"+
			"👥 *Участников:* %d",

		tender.Title,
		tender.Description.String,
		formattedPrice,
		currentPriceFormatted, // ТЕКУЩАЯ ЦЕНА
		formattedDate,
		classificationNames[tender.Classification.String],
		statusEmoji,
		statusText,
		tender.ParticipantsCount,
	)
	// Создаем кнопку в зависимости от участия пользователя
	var actionButton telebot.InlineButton
	if justJoined {
		// Если только что присоединились - показываем кнопку "Отменить участие"
		actionButton = telebot.InlineButton{
			Unique: "leave_tender",
			Text:   "❌ Отменить участие",
			Data:   fmt.Sprintf("%d|%d", tender.ID, userID),
		}
	} else {
		// Если только что отменили участие - показываем кнопку "Участвовать"
		actionButton = telebot.InlineButton{
			Unique: "join_tender",
			Text:   "📝 Участвовать в тендере",
			Data:   fmt.Sprintf("%d|%d", tender.ID, userID),
		}
	}

	// Обновляем сообщение
	_, err := c.Bot().Edit(c.Message(), tenderInfo, &telebot.SendOptions{
		ParseMode: telebot.ModeMarkdown,
		ReplyMarkup: &telebot.ReplyMarkup{
			InlineKeyboard: [][]telebot.InlineButton{
				{actionButton},
			},
		},
	})
	if err != nil {
		fmt.Printf("Ошибка при обновлении сообщения: %v\n", err)
		// Если не удалось обновить сообщение, отправляем текстовый ответ
		if justJoined {
			return c.Respond(&telebot.CallbackResponse{
				Text: "✅ Вы теперь участвуете в тендере!",
			})
		} else {
			return c.Respond(&telebot.CallbackResponse{
				Text: "❌ Вы больше не участвуете в тендере",
			})
		}
	}

	// Отправляем пустой callback response чтобы убрать "часики"
	return c.Respond()
}
