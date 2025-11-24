package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"tender_bot_go/db"
	"tender_bot_go/handlers"
	"tender_bot_go/jobs"
	"tender_bot_go/menu"
	"tender_bot_go/settings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/telebot.v3"
)

type SupplierState int

func main() {
	settings := settings.LoadSettings()
	runMigrations(settings.DatabaseURL)

	pool, err := pgxpool.New(context.Background(), settings.DatabaseURL)
	if err != nil {
		log.Fatal("DB connection error:", err)
	}
	defer pool.Close()

	queries := db.New(pool)

	pref := telebot.Settings{
		Token:  settings.BotToken,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	}

	bot, err := telebot.NewBot(pref)
	if err != nil {
		log.Fatal(err)
	}

	jobs.ActivatePendingTenders(bot, pool, handlers.MessageManagerOperator)

	// ===== /start =====
	bot.Handle("/start", func(c telebot.Context) error {
		userID := c.Sender().ID

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		user, err := queries.GetUserByTelegramID(ctx, userID)
		if err != nil {
			if err == pgx.ErrNoRows {
				role := "supplier"

				// Проверяем, является ли пользователь организатором
				isOrganizer := false
				for _, organizerID := range settings.OrganizerIDs {
					if userID == organizerID {
						isOrganizer = true
						break
					}
				}

				if isOrganizer {
					role = "organizer"
				} else {
					// Проверяем, является ли пользователь админом
					for _, adminID := range settings.AdminIDs {
						if userID == adminID {
							role = "admin"
							break
						}
					}
				}

				user, err = queries.CreateUser(ctx, db.CreateUserParams{
					TelegramID: userID,
					Role:       role,
				})
				if err != nil {
					log.Println("DB create user error:", err)
					return c.Send("Не удалось создать пользователя")
				}
			} else {
				log.Println("DB error:", err)
				return c.Send("Сервер временно недоступен. Попробуйте позже.")
			}
		}

		if user.Banned.Bool {
			return c.Send("Ваш аккаунт заблокирован. Обратитесь к администратору.")
		}

		switch user.Role {
		case "organizer":
			return c.Send("Панель организатора", &telebot.SendOptions{
				ReplyMarkup: menu.MenuOrganizer,
			})
		case "supplier":
			if user.OrganizationName.Valid && user.OrganizationName.String != "" {
				return c.Send("Панель поставщика", &telebot.SendOptions{
					ReplyMarkup: menu.MenuSupplierRegistered,
				})
			}
			return c.Send("Для участия в тендерах необходимо зарегистрироваться.\nНажмите кнопку 'Регистрация'",
				menu.MenuSupplierUnregistered)
		case "admin":
			return c.Send("Панель администратора", menu.MenuAdmin)
		default:
			return c.Send("Роль пользователя неизвестна")
		}
	})

	// ===== РЕГИСТРАЦИЯ ПОЛЬЗОВАТЕЛЕЙ =====
	bot.Use(handlers.BlockedUserMiddleware(queries))
	handlers.RegisterHandlers(bot, pool)

	bot.Start()
}

// Функция для проверки роли пользователя

func runMigrations(databaseURL string) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatalf("failed to open DB for migrations: %v", err)
	}
	defer db.Close()

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		log.Fatalf("failed to create migration driver: %v", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://db/migrations",
		"postgres",
		driver,
	)
	if err != nil {
		log.Fatalf("failed to create migrate instance: %v", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("failed to run migrations: %v", err)
	}

	log.Println("Migrations applied successfully")
}
