package supplier
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

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/telebot.v3"
)


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

type SupplierState int

const (
	StateNull SupplierState = iota
	StateOrgName
	StateINN
	StateOGRN
	StatePhone
	StateSelectClassification // 👈 добавили новое состояние
	StateFIO
)

var supplierStates = map[int64]SupplierState{}
var supplierData = map[int64]map[string]string{}

func SupplierHandlers(bot *telebot.Bot, pool *pgxpool.Pool) {
	queries := db.New(pool)

	bot.Handle(telebot.OnText, func(c telebot.Context) error {
		
		userID := c.Sender().ID
		text := c.Text()

		if text == "Регистрация" {
			supplierStates[userID] = StateOrgName
			supplierData[userID] = make(map[string]string)
			return c.Send("Введите наименование вашей организации:")
		}

		if text == "Тендеры" {
			return sendListOfTenders(c, queries, userID)
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
			markup := showClassificationKeyboard(userID)
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
	})

	// ===== Обработчики классификаций =====
	for code := range classificationNames {
		classCode := code
		bot.Handle(&telebot.InlineButton{Unique: "class_" + classCode}, func(c telebot.Context) error {
			userID := c.Sender().ID
			// защищаем от случаев, когда сессии нет
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
			for _, code := range allCodes { // фиксированный порядок
				if selectedSet[code] {
					newSelected = append(newSelected, code)
				}
			}
			supplierData[userID]["classifications"] = strings.Join(newSelected, ",")

			// Создаём новую клавиатуру
			markup := showClassificationKeyboard(userID)

			// Редактируем то же самое сообщение: оставляем текст прежним, обновляем ReplyMarkup
			// Получаем текущий текст сообщения (если не нужен — можно передавать новый текст)
			msg := c.Message()
			currentText := "Выберите до двух классификаций вашей организации:"
			// если msg != nil, можно попробовать взять msg.Text (на всякий случай ставим дефолт)
			if msg != nil && msg.Text != "" {
				currentText = msg.Text
			}

			// c.Edit(text, &telebot.SendOptions{ReplyMarkup: markup}) — обновит текст и/или клавиатуру в том же сообщении
			return c.Edit(currentText, &telebot.SendOptions{ReplyMarkup: markup})
		})
	}

	doneBtn := &telebot.InlineButton{Unique: "class_done"}
	bot.Handle(doneBtn, func(c telebot.Context) error {
		userID := c.Sender().ID
		data := supplierData[userID]["classifications"]

		if data == "" {
			return c.Respond(&telebot.CallbackResponse{
				Text:      "Выберите хотя бы одну классификацию!",
				ShowAlert: true,
			})
		}

		// Разделяем выбранные коды и формируем имена
		codes := strings.Split(data, ",")
		var selectedNames []string
		for _, code := range codes {
			if name, ok := classificationNames[code]; ok {
				selectedNames = append(selectedNames, name)
			}
		}

		supplierStates[userID] = StateFIO

		// Редактируем сообщение: убираем клавиатуру и выводим выбранные классификации
		return c.Edit(
			fmt.Sprintf("Выбранные классификации:\n%s\n\nВведите ФИО участника:", strings.Join(selectedNames, ", ")),
			&telebot.SendOptions{
				ReplyMarkup: nil, // убираем клавиатуру
			},
		)
	})

	// Обработчик для участия в тендере
	bot.Handle(&menu.BtnJoinTender, func(c telebot.Context) error {
		data := c.Data()
		parts := strings.Split(data, "|")
		if len(parts) != 2 {
			return c.Respond(&telebot.CallbackResponse{
				Text:      "❌ Ошибка: неверный формат данных",
				ShowAlert: true,
			})
		}

		tenderID, _ := strconv.ParseInt(parts[0], 10, 32)
		userID, _ := strconv.ParseInt(parts[1], 10, 64)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := queries.JoinTender(ctx, db.JoinTenderParams{
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
		tender, err := queries.GetTender(ctx, int32(tenderID))
		if err != nil {
			return c.Respond(&telebot.CallbackResponse{
				Text: "✅ Вы участвуете в тендере!",
			})
		}

		// Обновляем сообщение с новой кнопкой
		return updateTenderMessage(c, tender, userID, queries, true)
	})

	// Обработчик для отмены участия
	bot.Handle(&menu.BtnLeaveTender, func(c telebot.Context) error {
		data := c.Data()
		parts := strings.Split(data, "|")
		if len(parts) != 2 {
			return c.Respond(&telebot.CallbackResponse{
				Text:      "❌ Ошибка: неверный формат данных",
				ShowAlert: true,
			})
		}

		tenderID, _ := strconv.ParseInt(parts[0], 10, 32)
		userID, _ := strconv.ParseInt(parts[1], 10, 64)

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

		// Получаем актуальную информацию о тендере
		tender, err := queries.GetTender(ctx, int32(tenderID))
		if err != nil {
			return c.Respond(&telebot.CallbackResponse{
				Text: "❌ Вы больше не участвуете в тендере",
			})
		}

		// Обновляем сообщение с новой кнопкой
		return updateTenderMessage(c, tender, userID, queries, false)
	})

}

func sendListOfTenders(c telebot.Context, queries *db.Queries, userId int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	user, err := queries.GetUserByTelegramID(ctx, userId)
	if err != nil {
		fmt.Printf("Ошибка получения информации о пользователе: %v\n", err)
		return c.Send("Не удалось получить информацию о пользователе", &telebot.SendOptions{
			ReplyMarkup: menu.MenuSupplierRegistered,
		})
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
		return c.Send("Не удалось получить список тендеров", &telebot.SendOptions{
			ReplyMarkup: menu.MenuSupplierRegistered,
		})
	}

	if len(tenders) == 0 {
		return c.Send("Нет доступных тендеров", &telebot.SendOptions{
			ReplyMarkup: menu.MenuSupplierRegistered,
		})
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

		// Форматируем статус с эмодзи
		statusEmoji, statusText := getStatusWithEmoji(tender.Status)

		// Создаем сообщение с информацией о тендере
		tenderInfo := fmt.Sprintf(
			"📋 *Тендер:* %s\n\n"+
				"📝 *Описание:* %s\n"+
				"💰 *Стартовая цена:* %s руб.\n"+
				"📅 *Дата начала:* %s\n"+
				"🗂️ *Классификация:* %s\n"+
				"%s *Статус:* %s\n\n"+
				"👥 *Участников:* %d",

			tender.Title,
			tender.Description.String,
			formattedPrice,
			formattedDate,
			classificationNames[tender.Classification.String],
			statusEmoji,
			statusText,
			tender.ParticipantsCount,
		)

		// Создаем кнопку в зависимости от участия пользователя
		var actionButton telebot.InlineButton
		if isParticipating {
			actionButton = telebot.InlineButton{
				Unique: "leave_tender",
				Text:   "❌ Отменить участие",
				Data:   fmt.Sprintf("%d|%d", tender.ID, userId),
			}
		} else {
			actionButton = telebot.InlineButton{
				Unique: "join_tender",
				Text:   "📝 Участвовать в тендере",
				Data:   fmt.Sprintf("%d|%d", tender.ID, userId),
			}
		}

		// Отправляем информацию о тендере
		msg, err := c.Bot().Send(c.Sender(), tenderInfo, &telebot.SendOptions{
			ParseMode: telebot.ModeMarkdown,
			ReplyMarkup: &telebot.ReplyMarkup{
				InlineKeyboard: [][]telebot.InlineButton{
					{actionButton},
				},
			},
		})
		if err != nil {
			fmt.Printf("Ошибка при отправке информации о тендере: %v\n", err)
			continue
		}

		// Сохраняем ID сообщения для возможного обновления кнопки
		_ = msg // можно сохранить в кеш если нужно обновлять сообщение

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
		ReplyMarkup: menu.MenuSupplierRegistered,
	})
}

func updateTenderMessage(c telebot.Context, tender db.Tender, userID int64, queries *db.Queries, justJoined bool) error {
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
	tenderInfo := fmt.Sprintf(
		"📋 *Тендер:* %s\n\n"+
			"📝 *Описание:* %s\n"+
			"💰 *Стартовая цена:* %s руб.\n"+
			"📅 *Дата начала:* %s\n"+
			"🗂️ *Классификация:* %s\n"+
			"%s *Статус:* %s\n\n"+
			"👥 *Участников:* %d",

		tender.Title,
		tender.Description.String,
		formattedPrice,
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

// ===== Функция для вывода клавиатуры классификаций =====
func showClassificationKeyboard(userID int64) *telebot.ReplyMarkup {
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
		btn := telebot.InlineButton{Unique: "class_" + code, Text: text}
		rows = append(rows, []telebot.InlineButton{btn})
	}

	if len(selectedSet) > 0 {
		rows = append(rows, []telebot.InlineButton{{Unique: "class_done", Text: "Завершить выбор"}})
	}

	markup := &telebot.ReplyMarkup{InlineKeyboard: rows}
	return markup
}


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