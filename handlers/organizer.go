package handlers

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"tender_bot_go/db"
	"tender_bot_go/menu"
	"time"

	"github.com/gofiber/fiber/v2/log"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/telebot.v3"
)

type OrganizerState int

const (
	StateNone OrganizerState = iota
	StateTitle
	StateDescription
	StateStartPrice
	StateStartDate
	StateClassification
	StateConditions
)

var organizerStates = make(map[int64]OrganizerState)
var organizerData = make(map[int64]map[string]string)
var deleteTenderData = make(map[int64][]db.Tender)

func RegisterOrganizerHandlers(bot *telebot.Bot, pool *pgxpool.Pool) {
	queries := db.New(pool)

	// Обработчики inline кнопок для организатора
	for code := range classificationNames {
		classCode := code
		bot.Handle(&telebot.InlineButton{Unique: "org_class_" + classCode}, func(c telebot.Context) error {
			return handleOrgClassification(c, queries, classCode)
		})
	}

	bot.Handle(&telebot.InlineButton{Unique: "org_class_done"}, func(c telebot.Context) error {
		return handleOrgClassificationDone(c, queries)
	})

	bot.Handle(&menu.BtnDeleteTender, func(c telebot.Context) error {
		return handleDeleteTender(c, queries)
	})

	// Обработчик документов для организатора
	bot.Handle(telebot.OnDocument, func(c telebot.Context) error {
		userID := c.Sender().ID
		role := getUserRole(userID, queries)
		if role == "organizer" {
			return HandleOrganizerDocument(c, queries, userID)
		}
		return nil
	})
}

func HandleOrganizerText(c telebot.Context, queries *db.Queries, text string, userID int64) error {
	// Инициализируем данные пользователя, если их нет
	if _, exists := organizerData[userID]; !exists {
		organizerData[userID] = make(map[string]string)
	}

	if text == "Создать тендер" {
		organizerStates[userID] = StateTitle
		organizerData[userID] = make(map[string]string)
		return c.Send("Введите название тендера:", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizerCancel,
		})
	}
	if text == "Мои тендеры" {
		return sendOrganizerTendersList(c, queries)
	}
	if text == "История" {
		return sendOrganizerHistory(c, queries)
	}
	if text == "Удалить тендер" {
		return sendTendersForDeletion(c, queries)
	}
	if text == "Отмена" {
		delete(organizerStates, userID)
		delete(organizerData, userID)
		return c.Send("Создание тендера отменено.", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizer,
		})
	}

	state := organizerStates[userID]
	switch state {
	case StateTitle:
		organizerData[userID]["title"] = text
		organizerStates[userID] = StateDescription
		return c.Send("Введите описание тендера:", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizerCancel,
		})
	case StateDescription:
		organizerData[userID]["description"] = text
		organizerStates[userID] = StateStartPrice
		return c.Send("Введите стартовую цену в рублях:", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizerCancel,
		})
	case StateStartPrice:
		organizerData[userID]["start_price"] = text
		organizerStates[userID] = StateStartDate
		return c.Send("Введите дату и время начала тендера в формате ДД.ММ.ГГГГ ЧЧ:ММ:", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizerCancel,
		})
	case StateStartDate:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		db_location, err := queries.TimeZone(ctx)
		log.Info(db_location)
		if err != nil {
			log.Info("Failed to get db location")
		}
		location, err := time.LoadLocation(db_location)
		if err != nil {
			location = time.UTC
		}
		startDateTime, err := time.ParseInLocation("02.01.2006 15:04", text, location)
		if err != nil {
			return c.Send("Введите дату и время в формате ДД.ММ.ГГГГ ЧЧ:ММ, например: 25.12.2024 14:30", &telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizerCancel,
			})
		}

		if startDateTime.Before(time.Now()) {
			return c.Send("Дата начала тендера должна быть в будущем!", &telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizerCancel,
			})
		}

		organizerData[userID]["start_date"] = text
		organizerData[userID]["start_date_parsed"] = startDateTime.Format(time.RFC3339)
		organizerStates[userID] = StateClassification
		markup := showOrganizerClassificationKeyboard(userID)
		return c.Send("Выберите одну классификацию для тендера:", &telebot.SendOptions{
			ReplyMarkup: markup,
		})
	case StateConditions:
		if text == "нет" || text == "Нет" {
			organizerData[userID]["conditions_path"] = ""
			successMessage, _, err := saveTenderToDB(userID, queries, c)
			if err != nil {
				return err
			}
			delete(organizerStates, userID)
			delete(organizerData, userID)
			return c.Send(successMessage, &telebot.SendOptions{
				ParseMode:   telebot.ModeMarkdown,
				ReplyMarkup: menu.MenuOrganizer,
			})
		} else {
			return c.Send("Пожалуйста, отправьте файл или напишите 'нет'.", &telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizerCancel,
			})
		}
	default:
		return nil
	}
}

func HandleOrganizerDocument(c telebot.Context, queries *db.Queries, userID int64) error {
	state := organizerStates[userID]
	if state != StateConditions {
		return nil
	}

	if _, exists := organizerData[userID]; !exists {
		organizerData[userID] = make(map[string]string)
	}

	doc := c.Message().Document
	if doc == nil {
		return c.Send("Файл не найден. Попробуйте еще раз.", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizerCancel,
		})
	}

	timestamp := time.Now().UnixNano()
	filename := fmt.Sprintf("%d_%s", timestamp, doc.FileName)
	filePath := filepath.Join("files", filename)

	if err := os.MkdirAll("files", 0755); err != nil {
		fmt.Printf("Ошибка создания директории: %v\n", err)
		return c.Send("Не удалось создать директорию для файлов.", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizerCancel,
		})
	}

	f, err := os.Create(filePath)
	if err != nil {
		fmt.Printf("Ошибка создания файла: %v\n", err)
		return c.Send("Не удалось сохранить файл.", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizerCancel,
		})
	}
	defer f.Close()

	reader, err := c.Bot().File(&doc.File)
	if err != nil {
		fmt.Printf("Ошибка получения файла от Telegram: %v\n", err)
		return c.Send("Не удалось прочитать файл.", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizerCancel,
		})
	}

	_, err = io.Copy(f, reader)
	if err != nil {
		fmt.Printf("Ошибка копирования файла: %v\n", err)
		return c.Send("Ошибка при сохранении файла.", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizerCancel,
		})
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fmt.Printf("Файл не создан: %s\n", filePath)
		return c.Send("Файл не был сохранен на сервере.", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizerCancel,
		})
	}

	organizerData[userID]["conditions_path"] = filePath
	fmt.Printf("Файл сохранен: %s\n", filePath)

	successMessage, _, err := saveTenderToDB(userID, queries, c)
	if err != nil {
		return err
	}

	if err := c.Send(successMessage, &telebot.SendOptions{
		ParseMode: telebot.ModeMarkdown,
	}); err != nil {
		return err
	}

	if err := c.Send("📎 Файл с условиями:"); err != nil {
		return err
	}

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		fmt.Printf("Файл не найден для отправки: %s\n", filePath)
		return c.Send("❌ Файл не найден для отправки")
	}

	fileToSend := &telebot.Document{
		File:     telebot.FromDisk(filePath),
		FileName: doc.FileName,
	}

	if err := c.Send(fileToSend); err != nil {
		fmt.Printf("Ошибка при отправке файла: %v\n", err)
		return c.Send("❌ Не удалось отправить файл. Попробуйте еще раз.")
	}

	delete(organizerStates, userID)
	delete(organizerData, userID)

	return c.Send("Тендер успешно создан! Что хотите сделать дальше?", &telebot.SendOptions{
		ParseMode: telebot.ModeMarkdown,
		ReplyMarkup: menu.MenuOrganizer,
	})
}

func handleOrgClassification(c telebot.Context, queries *db.Queries, classCode string) error {
	userID := c.Sender().ID
	organizerData[userID]["classification"] = classCode
	markup := showOrganizerClassificationKeyboard(userID)
	return c.Edit("Выберите одну классификацию для тендера:", &telebot.SendOptions{
		ReplyMarkup: markup,
	})
}

func handleOrgClassificationDone(c telebot.Context, queries *db.Queries) error {
	userID := c.Sender().ID
	selectedCode := organizerData[userID]["classification"]

	if selectedCode == "" {
		return c.Respond(&telebot.CallbackResponse{
			Text:      "Выберите классификацию!",
			ShowAlert: true,
		})
	}

	selectedName := classificationNames[selectedCode]
	organizerStates[userID] = StateConditions

	err := c.Respond()
	if err != nil {
		fmt.Printf("Ошибка при ответе на callback: %v\n", err)
	}

	return c.Send(
		fmt.Sprintf("Выбранная классификация: %s\n\nПрикрепите файл с условиями или отправьте 'нет'", selectedName),
		&telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizerCancel,
		},
	)
}

func handleDeleteTender(c telebot.Context, queries *db.Queries) error {
	tenderIDStr := c.Data()
	tenderID, err := strconv.ParseInt(tenderIDStr, 10, 32)
	if err != nil {
		return c.Send("❌ Ошибка: неверный ID тендера")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = queries.DeleteTender(ctx, int32(tenderID))
	if err != nil {
		fmt.Printf("Ошибка при удалении тендера: %v\n", err)
		return c.Send("❌ Не удалось удалить тендер", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizer,
		})
	}

	userID := c.Sender().ID
	delete(deleteTenderData, userID)

	return c.Send("✅ Тендер успешно удален", &telebot.SendOptions{
		ReplyMarkup: menu.MenuOrganizer,
	})
}

func showOrganizerClassificationKeyboard(userID int64) *telebot.ReplyMarkup {
	selectedCode := organizerData[userID]["classification"]

	var rows [][]telebot.InlineButton
	for _, code := range allCodes {
		name := classificationNames[code]
		text := name
		if code == selectedCode {
			text = "✅ " + name
		}
		btn := telebot.InlineButton{Unique: "org_class_" + code, Text: text}
		rows = append(rows, []telebot.InlineButton{btn})
	}

	if selectedCode != "" {
		rows = append(rows, []telebot.InlineButton{
			{Unique: "org_class_done", Text: "✅ Завершить выбор"},
		})
	}

	markup := &telebot.ReplyMarkup{InlineKeyboard: rows}
	return markup
}

// Остальные функции организатора (sendOrganizerTendersList, sendOrganizerHistory, sendTendersForDeletion, saveTenderToDB и т.д.)
// нужно скопировать из вашего кода и заменить userData на organizerData

func sendTendersForDeletion(c telebot.Context, queries *db.Queries) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Получаем тендеры, которые можно удалить (статус не "completed")
	tenders, err := queries.GetTendersForDeletion(ctx)
	if err != nil {
		fmt.Printf("Ошибка при получении тендеров для удаления: %v\n", err)
		return c.Send("❌ Не удалось загрузить список тендеров для удаления", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizer,
		})
	}

	if len(tenders) == 0 {
		return c.Send("📭 Нет тендеров для удаления (все тендеры завершены)", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizer,
		})
	}

	// Сохраняем тендеры для данного пользователя (для обработки кнопок)
	userID := c.Sender().ID
	deleteTenderData[userID] = tenders

	// Отправляем информацию о каждом тендере с кнопкой удаления
	for _, tender := range tenders {
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
			"📋 *Тендер*: %s\n\n"+
				"📝 *Описание:* %s\n"+
				"💰 *Стартовая цена:* %s руб.\n"+
				"📈 *Текущая цена:* %s руб.\n"+
				"📅 *Дата начала:* %s\n"+
				"🗂️ *Классификация:* %s\n"+
				"👥 *Участников:* %d\n"+
				"%s *Статус:* %s\n\n"+
				"🆔 ID: %d",
			tender.Title,
			tender.Description.String,
			formattedPrice,
			formattedCurrentPrice,
			formattedDate,
			classificationNames[tender.Classification.String],
			tender.ParticipantsCount,
			statusEmoji,
			statusText,
			tender.ID,
		)

		// Создаем кнопку удаления для этого тендера
		deleteBtn := telebot.InlineButton{
			Unique: "delete_tender", // общий уникальный идентификатор
			Text:   "🗑️ Удалить тендер",
			Data:   fmt.Sprintf("%d", tender.ID), // ID тендера храним в Data
		}

		// Отправляем информацию о тендере с кнопкой удаления
		_, err := c.Bot().Send(c.Sender(), tenderInfo, &telebot.SendOptions{
			ParseMode: telebot.ModeMarkdown,
			ReplyMarkup: &telebot.ReplyMarkup{
				InlineKeyboard: [][]telebot.InlineButton{
					{deleteBtn},
				},
			},
		})
		if err != nil {
			fmt.Printf("Ошибка при отправке информации о тендере для удаления: %v\n", err)
			continue
		}

		// Небольшая задержка между отправками чтобы не превысить лимиты Telegram
		time.Sleep(500 * time.Millisecond)
	}

	return c.Send(fmt.Sprintf("✅ Выберите тендер для удаления (всего доступно: %d)", len(tenders)), &telebot.SendOptions{
		ReplyMarkup: menu.MenuOrganizer,
	})
}

func saveTenderToDB(userID int64, queries *db.Queries, c telebot.Context) (string, int32, error) {
	data := organizerData[userID]

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startPrice, err := strconv.ParseFloat(data["start_price"], 64)
	if err != nil {
		return "", 0, c.Send("Введите корректную числовую стартовую цену!", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizerCancel,
		})
	}

	startDateTime, err := time.Parse(time.RFC3339, data["start_date_parsed"])
	if err != nil {
		return "", 0, c.Send("Ошибка формата даты. Попробуйте снова.", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizerCancel,
		})
	}

	fmt.Println("Создаём тендер:", data)
	tender, err := queries.CreateTender(ctx, db.CreateTenderParams{
		Title: data["title"],
		Description: pgtype.Text{
			String: data["description"],
			Valid:  true,
		},
		StartPrice: startPrice,
		StartAt: pgtype.Timestamptz{
			Time:  startDateTime,
			Valid: true,
		},
		ConditionsPath: pgtype.Text{
			String: data["conditions_path"],
			Valid:  data["conditions_path"] != "",
		},
		CurrentPrice: startPrice,
		Classification: pgtype.Text{
			String: data["classification"],
			Valid:  data["classification"] != "",
		},
	})

	if err != nil {
		fmt.Printf("Ошибка при создании тендера: %v\n", err)
		return "", 0, c.Send("Ошибка при сохранении данных в БД. Попробуйте снова.", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizerCancel,
		})
	}

	// Отправляем уведомление админам о новом тендере
	go sendTenderApprovalNotification(c.Bot(), config.AdminIDs, data, tender.ID, tender.Title)

	// Форматируем дату для красивого вывода
	parsedTime, _ := time.Parse(time.RFC3339, data["start_date_parsed"])
	formattedDate := parsedTime.Format("02.01.2006 15:04")

	// Форматируем цену в финансовом формате
	formattedPrice := formatPrice(data["start_price"])

	// Создаем сообщение об успехе ПЕРЕД тем как очистить данные
	successMessage := fmt.Sprintf(
		"✅ *Тендер успешно создан и отправлен на модерацию!*\n\n"+
			"📋 *Название:* %s\n"+
			"📝 *Описание:* %s\n"+
			"💰 *Стартовая цена:* %s руб.\n"+
			"📅 *Дата начала:* %s\n"+
			"🗂️ *Классификация:* %s\n\n"+
			"⏳ *Ожидайте одобрения администратора*",
		data["title"],
		data["description"],
		formattedPrice,
		formattedDate,
		classificationNames[data["classification"]],
	)

	return successMessage, tender.ID, nil
}

func sendTenderApprovalNotification(bot *telebot.Bot, adminIDs []int64, tenderData map[string]string, tenderID int32, tenderTitle string) {
	// Форматируем дату для красивого вывода
	parsedTime, _ := time.Parse(time.RFC3339, tenderData["start_date_parsed"])
	formattedDate := parsedTime.Format("02.01.2006 15:04")

	// Форматируем цену в финансовом формате
	formattedPrice := formatPrice(tenderData["start_price"])

	message := fmt.Sprintf(
		"🆕 *Новый тендер требует одобрения*\n\n"+
			"📋 *Название:* %s\n"+
			"📝 *Описание:* %s\n"+
			"💰 *Стартовая цена:* %s руб.\n"+
			"📅 *Дата начала:* %s\n"+
			"🗂️ *Классификация:* %s\n\n"+
			"✅ Для одобрения нажмите кнопку ниже",
		tenderData["title"],
		tenderData["description"],
		formattedPrice,
		formattedDate,
		classificationNames[tenderData["classification"]],
	)

	// Создаем кнопку для одобрения
	approveBtn := telebot.InlineButton{
		Unique: "approve_tender",
		Text:   "⏳ Одобрить тендер",
		Data:   fmt.Sprintf("%d|%s", tenderID, tenderTitle),
	}

	// Отправляем сообщение всем админам
	for _, adminID := range adminIDs {
		_, err := bot.Send(&telebot.User{ID: adminID}, message, &telebot.SendOptions{
			ParseMode: telebot.ModeMarkdown,
			ReplyMarkup: &telebot.ReplyMarkup{
				InlineKeyboard: [][]telebot.InlineButton{
					{approveBtn},
				},
			},
		})
		if err != nil {
			fmt.Printf("Ошибка отправки уведомления админу %d: %v\n", adminID, err)
		}
	}
}

func sendOrganizerTendersList(c telebot.Context, queries *db.Queries) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Получаем все тендеры из БД
	tenders, err := queries.GetTenders(ctx)
	if err != nil {
		fmt.Printf("Ошибка при получении тендеров: %v\n", err)
		return c.Send("❌ Не удалось загрузить список тендеров", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizer,
		})
	}

	if len(tenders) == 0 {
		return c.Send("📭 Список тендеров пуст", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizer,
		})
	}

	// Отправляем информацию о каждом тендере
	for _, tender := range tenders {
		// Форматируем дату для красивого вывода
		var formattedDate string
		if tender.StartAt.Valid {
			formattedDate = tender.StartAt.Time.Format("02.01.2006 15:04")
		} else {
			formattedDate = "не указана"
		}

		// Форматируем цену в финансовом формате
		formattedPrice := formatPriceFloat(tender.StartPrice)

		// Форматируем статус с эмодзи
		statusEmoji, statusText := getStatusWithEmoji(tender.Status)

		formattedCurrentPrice := formatPriceFloat(tender.CurrentPrice)

		// Создаем сообщение с информацией о тендере
		tenderInfo := fmt.Sprintf(
			"📋 *Тендер:* %s\n\n"+
				"📝 *Описание:* %s\n"+
				"💰 *Стартовая цена:* %s руб.\n"+
				"📈 *Текущая цена:* %s руб.\n"+
				"📅 *Дата начала:* %s\n"+
				"🗂️ *Классификация:* %s\n"+
				"👥 *Участников:* %d\n"+
				"%s *Статус:* %s",

			tender.Title,
			tender.Description.String,
			formattedPrice,
			formattedCurrentPrice,
			formattedDate,
			classificationNames[tender.Classification.String],
			tender.ParticipantsCount,
			statusEmoji,
			statusText,
		)

		// Отправляем информацию о тендере
		if err := c.Send(tenderInfo, &telebot.SendOptions{
			ParseMode: telebot.ModeMarkdown,
		}); err != nil {
			fmt.Printf("Ошибка при отправке информации о тендере: %v\n", err)
			continue
		}

		// Если есть прикрепленный файл, отправляем его
		if tender.ConditionsPath.Valid && tender.ConditionsPath.String != "" {
			filePath := tender.ConditionsPath.String

			// Проверяем существование файла
			if _, err := os.Stat(filePath); err == nil {
				// Отправляем сообщение о файле
				if err := c.Send("📎 Файл с условиями тендера:"); err != nil {
					fmt.Printf("Ошибка при отправке сообщения о файле: %v\n", err)
					continue
				}

				// Отправляем сам файл
				fileName := filepath.Base(filePath)
				fileToSend := &telebot.Document{
					File:     telebot.FromDisk(filePath),
					FileName: fileName,
				}

				if err := c.Send(fileToSend); err != nil {
					fmt.Printf("Ошибка при отправке файла тендера: %v\n", err)
					// Продолжаем с следующим тендером даже если не удалось отправить файл
				}
			} else {
				fmt.Printf("Файл не найден: %s\n", filePath)
				if err := c.Send("❌ Файл условий недоступен"); err != nil {
					fmt.Printf("Ошибка при отправке сообщения об отсутствии файла: %v\n", err)
				}
			}
		} else {
			// Если файла нет, отправляем сообщение об этом
			if err := c.Send("📭 Файл условий не прикреплен"); err != nil {
				fmt.Printf("Ошибка при отправке сообщения об отсутствии файла: %v\n", err)
			}
		}

		// Добавляем разделитель между тендерами
		if err := c.Send("➖➖➖➖➖➖➖➖➖➖"); err != nil {
			fmt.Printf("Ошибка при отправке разделителя: %v\n", err)
		}

		// Небольшая задержка между отправками чтобы не превысить лимиты Telegram
		time.Sleep(500 * time.Millisecond)
	}

	return c.Send(fmt.Sprintf("✅ Всего тендеров: %d", len(tenders)), &telebot.SendOptions{
		ReplyMarkup: menu.MenuOrganizer,
	})
}

func sendOrganizerHistory(c telebot.Context, queries *db.Queries) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenders, err := queries.GetTendersHistory(ctx)
	if err != nil {
		fmt.Printf("Ошибка при получении тендеров: %v\n", err)
		return c.Send("❌ Не удалось загрузить историю", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizer,
		})
	}

	if len(tenders) == 0 {
		return c.Send("📭 Нет тендеров в истории", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizer,
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
		ReplyMarkup: menu.MenuOrganizer,
	})
}
