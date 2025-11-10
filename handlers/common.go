package handlers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"tender_bot_go/db"
	"tender_bot_go/settings"
	"time"
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

func getUserRole(userID int64, queries *db.Queries) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	user, err := queries.GetUserByTelegramID(ctx, userID)
	if err != nil {
		return ""
	}
	return user.Role
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