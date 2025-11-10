package handlers

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/telebot.v3"
)

func RegisterHandlers(bot *telebot.Bot, pool *pgxpool.Pool) {
	// Регистрируем единый текстовый обработчик
	registerTextHandler(bot, pool)
	
	// Регистрируем обработчики из всех пакетов
	RegisterOrganizerHandlers(bot, pool)
	RegisterSupplierHandlers(bot, pool)
	RegisterAdminHandlers(bot, pool)
}