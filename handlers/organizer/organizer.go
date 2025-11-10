package organizer

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"tender_bot_go/db"
	"tender_bot_go/menu"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/telebot.v3"
	"tender_bot_go/settings"
)

var config = settings.LoadSettings()

var classificationNames = map[string]string{
	"1":  "Сантехника",
	"2":  "Вентиляция и кондиционирование",
	"3":  "Отопление",
	"4":  "Освещение",
	"5":  "Розетки/выключатели",
	"6":  "Камень натуральный",
	"7":  "Керамогранит",
	"8":  "Краска",
	"9":  "Декоративная штукатурка",
	"10": "Стеклянные перегородки и зеркала",
	"11": "Двери",
	"12": "Мебель индивидуального изготовления",
	"13": "Мебель",
	"14": "Портьеры",
	"15": "Постельное белье",
	"16": "Декор",
	"17": "Обои",
	"18": "Камины",
	"19": "Посуда",
	"20": "Озеленение",
	"21": "Ковры",
}

var allCodes = []string{
	"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19", "20", "21",
}

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

// Храним состояние для каждого пользователя
var userStates = make(map[int64]OrganizerState)
var userData = make(map[int64]map[string]string)

// Храним данные для удаления тендеров
var deleteTenderData = make(map[int64][]db.Tender)

func OrganizerHandlers(bot *telebot.Bot, pool *pgxpool.Pool) {
	queries := db.New(pool)

	// Обработчик текстовых сообщений
	bot.Handle(telebot.OnText, func(c telebot.Context) error {
		
		text := c.Text()
		userID := c.Sender().ID

		// Инициализируем данные пользователя, если их нет
		if _, exists := userData[userID]; !exists {
			userData[userID] = make(map[string]string)
		}

		if text == "Создать тендер" {
			userStates[userID] = StateTitle
			userData[userID] = make(map[string]string)
			return c.Send("Введите название тендера:", &telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizerCancel,
			})
		}
		if text == "Мои тендеры" {
			return sendTendersList(c, queries)
		}
		if text == "История" {
			return sendHistory(c, queries)
		}
		if text == "Удалить тендер" {
			return sendTendersForDeletion(c, queries)
		}
		if text == "Отмена" {
			delete(userStates, userID)
			delete(userData, userID)
			return c.Send("Создание тендера отменено.", &telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizer,
			})
		}

		state := userStates[userID]
		switch state {
		case StateTitle:
			userData[userID]["title"] = text
			userStates[userID] = StateDescription
			return c.Send("Введите описание тендера:", &telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizerCancel,
			})
		case StateDescription:
			userData[userID]["description"] = text
			userStates[userID] = StateStartPrice
			return c.Send("Введите стартовую цену в рублях:", &telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizerCancel,
			})
		case StateStartPrice:
			userData[userID]["start_price"] = text
			userStates[userID] = StateStartDate
			return c.Send("Введите дату и время начала тендера в формате ДД.ММ.ГГГГ ЧЧ:ММ:", &telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizerCancel,
			})
		case StateStartDate:
			// Парсим дату в новом формате
			location, err := time.LoadLocation("Europe/Moscow")
			if err != nil {
				location = time.UTC // fallback
			}
			startDateTime, err := time.ParseInLocation("02.01.2006 15:04", text, location)
			if err != nil {
				return c.Send("Введите дату и время в формате ДД.ММ.ГГГГ ЧЧ:ММ, например: 25.12.2024 14:30", &telebot.SendOptions{
					ReplyMarkup: menu.MenuOrganizerCancel,
				})
			}

			// Проверяем, что дата в будущем
			if startDateTime.Before(time.Now()) {
				return c.Send("Дата начала тендера должна быть в будущем!", &telebot.SendOptions{
					ReplyMarkup: menu.MenuOrganizerCancel,
				})
			}

			userData[userID]["start_date"] = text
			userData[userID]["start_date_parsed"] = startDateTime.Format(time.RFC3339) // сохраняем для БД
			userStates[userID] = StateClassification                                         // переходим к выбору классификации
			markup := showSingleClassificationKeyboard(userID)
			return c.Send("Выберите одну классификацию для тендера:", &telebot.SendOptions{
				ReplyMarkup: markup,
			})
		case StateConditions:
			// Обработка текста в состоянии Conditions
			if text == "нет" || text == "Нет" {
				userData[userID]["conditions_path"] = ""

				// Сохраняем тендер в БД и получаем сообщение об успехе
				successMessage, _, err := saveTenderToDB(userID, queries, c)
				if err != nil {
					return err
				}

				// Очищаем данные ПОСЛЕ использования
				delete(userStates, userID)
				delete(userData, userID)

				return c.Send(successMessage, &telebot.SendOptions{
					ParseMode:   telebot.ModeMarkdown, // ← ДОБАВЬТЕ ЭТУ СТРОКУ
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
	})

	// Обработчики для выбора классификации организатором
	for code := range classificationNames {
		classCode := code
		bot.Handle(&telebot.InlineButton{Unique: "org_class_" + classCode}, func(c telebot.Context) error {
			userID := c.Sender().ID

			// Устанавливаем выбранную классификацию
			userData[userID]["classification"] = classCode

			// Создаём новую клавиатуру
			markup := showSingleClassificationKeyboard(userID)

			// Обновляем сообщение
			return c.Edit("Выберите одну классификацию для тендера:", &telebot.SendOptions{
				ReplyMarkup: markup,
			})
		})
	}

	// Обработчик для кнопки завершения выбора
	doneOrgBtn := &telebot.InlineButton{Unique: "org_class_done"}
	bot.Handle(doneOrgBtn, func(c telebot.Context) error {
		userID := c.Sender().ID
		selectedCode := userData[userID]["classification"]

		if selectedCode == "" {
			return c.Respond(&telebot.CallbackResponse{
				Text:      "Выберите классификацию!",
				ShowAlert: true,
			})
		}

		// Получаем название выбранной классификации
		selectedName := classificationNames[selectedCode]

		// Переходим к следующему шагу
		userStates[userID] = StateConditions

		// Сначала отвечаем на callback
		err := c.Respond()
		if err != nil {
			fmt.Printf("Ошибка при ответе на callback: %v\n", err)
		}

		// Затем отправляем новое сообщение с reply-клавиатурой
		return c.Send(
			fmt.Sprintf("Выбранная классификация: %s\n\nПрикрепите файл с условиями или отправьте 'нет'", selectedName),
			&telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizerCancel,
			},
		)
	})

	// Обработчик для кнопок удаления тендеров
	bot.Handle(&menu.BtnDeleteTender, func(c telebot.Context) error {
		// Получаем ID тендера из данных кнопки
		tenderIDStr := c.Data()
		tenderID, err := strconv.ParseInt(tenderIDStr, 10, 32)
		if err != nil {
			return c.Send("❌ Ошибка: неверный ID тендера")
		}

		// Удаляем тендер из БД
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err = queries.DeleteTender(ctx, int32(tenderID))
		if err != nil {
			fmt.Printf("Ошибка при удалении тендера: %v\n", err)
			return c.Send("❌ Не удалось удалить тендер", &telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizer,
			})
		}

		// Очищаем кэш удаляемых тендеров для пользователя
		userID := c.Sender().ID
		delete(deleteTenderData, userID)

		return c.Send("✅ Тендер успешно удален", &telebot.SendOptions{
			ReplyMarkup: menu.MenuOrganizer,
		})
	})

	// Отдельный обработчик для документов
	bot.Handle(telebot.OnDocument, func(c telebot.Context) error {
		userID := c.Sender().ID
		state := userStates[userID]

		// Обрабатываем документ только если мы в состоянии Conditions
		if state != StateConditions {
			return nil
		}

		// Инициализируем данные пользователя, если их нет
		if _, exists := userData[userID]; !exists {
			userData[userID] = make(map[string]string)
		}

		doc := c.Message().Document
		if doc == nil {
			return c.Send("Файл не найден. Попробуйте еще раз.", &telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizerCancel,
			})
		}

		// создаём уникальное имя файла
		timestamp := time.Now().UnixNano()
		filename := fmt.Sprintf("%d_%s", timestamp, doc.FileName)
		filePath := filepath.Join("files", filename)

		// создаем директорию если её нет
		if err := os.MkdirAll("files", 0755); err != nil {
			fmt.Printf("Ошибка создания директории: %v\n", err)
			return c.Send("Не удалось создать директорию для файлов.", &telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizerCancel,
			})
		}

		// скачиваем файл
		f, err := os.Create(filePath)
		if err != nil {
			fmt.Printf("Ошибка создания файла: %v\n", err)
			return c.Send("Не удалось сохранить файл.", &telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizerCancel,
			})
		}
		defer f.Close()

		reader, err := bot.File(&doc.File)
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

		// Проверяем, что файл действительно создан
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			fmt.Printf("Файл не создан: %s\n", filePath)
			return c.Send("Файл не был сохранен на сервере.", &telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizerCancel,
			})
		}

		userData[userID]["conditions_path"] = filePath
		fmt.Printf("Файл сохранен: %s\n", filePath)

		// Сохраняем тендер в БД и получаем сообщение об успехе
		successMessage, _, err := saveTenderToDB(userID, queries, c)
		if err != nil {
			return err
		}

		// 1. Сначала отправляем сообщение о создании тендера
		if err := c.Send(successMessage, &telebot.SendOptions{
			ParseMode: telebot.ModeMarkdown,
		}); err != nil {
			return err
		}

		// 2. Затем отправляем сообщение "файл с условиями"
		if err := c.Send("📎 Файл с условиями:"); err != nil {
			return err
		}

		// 3. Проверяем существование файла перед отправкой
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			fmt.Printf("Файл не найден для отправки: %s\n", filePath)
			return c.Send("❌ Файл не найден для отправки")
		}

		// 4. И только потом отправляем сам файл
		fileToSend := &telebot.Document{
			File:     telebot.FromDisk(filePath),
			FileName: doc.FileName,
		}

		fmt.Printf("Пытаемся отправить файл: %s\n", filePath)
		if err := c.Send(fileToSend); err != nil {
			fmt.Printf("Ошибка при отправке файла: %v\n", err)
			return c.Send("❌ Не удалось отправить файл. Попробуйте еще раз.")
		}

		fmt.Printf("Файл успешно отправлен: %s\n", filePath)

		// Очищаем данные ПОСЛЕ использования
		delete(userStates, userID)
		delete(userData, userID)

		// Возвращаем основное меню
		return c.Send("Тендер успешно создан! Что хотите сделать дальше?", &telebot.SendOptions{
			ParseMode: telebot.ModeMarkdown,
		})
	})
}

// Функция для отправки списка тендеров для удаления
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
        

		// Форматируем статус с эмодзи
		statusEmoji, statusText := getStatusWithEmoji(tender.Status)

		// Создаем сообщение с информацией о тендере
		tenderInfo := fmt.Sprintf(
			"📋 *Тендер*: %s\n\n"+
				"📝 *Описание:* %s\n"+
				"💰 *Стартовая цена:* %s руб.\n"+
				"📅 *Дата начала:* %s\n"+
				"🗂️ *Классификация:* %s\n"+
				"%s *Статус:* %s\n\n"+
				"🆔 ID: %d",
			tender.Title,
			tender.Description.String,
			formattedPrice,
			formattedDate,
			classificationNames[tender.Classification.String],
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

func showSingleClassificationKeyboard(userID int64) *telebot.ReplyMarkup {
	selectedCode := userData[userID]["classification"] // берем одну выбранную классификацию

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
			{Unique: "org_class_done", Text: "Завершить выбор"},
		})
	}

	markup := &telebot.ReplyMarkup{InlineKeyboard: rows}
	return markup
}

// Вспомогательная функция для сохранения тендера в БД
func saveTenderToDB(userID int64, queries *db.Queries, c telebot.Context) (string, int32, error) {
	data := userData[userID]

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

// Функция для отправки уведомления админам о новом тендере
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

// Функция для отправки списка тендеров
func sendTendersList(c telebot.Context, queries *db.Queries) error {
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

		// Создаем сообщение с информацией о тендере
		tenderInfo := fmt.Sprintf(
			"📋 *Тендер:* %s\n\n"+
				"📝 *Описание:* %s\n"+
				"💰 *Стартовая цена:* %s руб.\n"+
				"📅 *Дата начала:* %s\n"+
				"🗂️ *Классификация:* %s\n"+
				"%s *Статус:* %s",

			tender.Title,
			tender.Description.String,
			formattedPrice,
			formattedDate,
			classificationNames[tender.Classification.String],
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

func sendHistory(c telebot.Context, queries *db.Queries) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenders, err := queries.GetHistory(ctx)
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

		// Создаем сообщение с информацией о тендере
		tenderInfo := fmt.Sprintf(
			"📋 *Тендер:* %s\n\n"+
				"📝 *Описание:* %s\n"+
				"💰 *Стартовая цена:* %s руб.\n"+
				"📅 *Дата начала:* %s\n"+
				"🗂️ *Классификация:* %s\n"+
				"%s *Статус:* %s",

			tender.Title,
			tender.Description.String,
			formattedPrice,
			formattedDate,
			classificationNames[tender.Classification.String],
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

// Функция для получения эмодзи и текста статуса
func getStatusWithEmoji(status string) (string, string) {
	switch status {
	case "active":
		return "🟢", "Активный"
	case "completed":
		return "🔴", "Завершен"
	case "active_pending":
		return "🟡", "Ожидает начала"
	case "cancelled":
		return "❌", "Отменен"
	case "pending_approval":
		return "🟠", "Ожидает подтверждения"
	default:
		return "❓", "Неизвестный"
	}
}
