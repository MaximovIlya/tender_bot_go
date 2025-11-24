//go:build dropdb
// +build dropdb

package main

import (
	"context"
	"database/sql"
	"log"
	"tender_bot_go/settings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	settings := settings.LoadSettings()

	// Подключаемся к БД
	db, err := sql.Open("pgx", settings.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to open DB: %v", err)
	}
	defer db.Close()

	// Проверяем подключение
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("failed to ping DB: %v", err)
	}

	log.Println("Connected to database. Starting cleanup...")

	// Очищаем все таблицы одной командой (CASCADE автоматически обработает зависимости)
	log.Println("Truncating all tables...")
	_, err = db.ExecContext(ctx, `
		TRUNCATE TABLE 
			history, 
			tender_bids, 
			tender_participants, 
			pending_users,
			tenders,
			users
		CASCADE;
	`)
	if err != nil {
		log.Fatalf("Failed to truncate tables: %v", err)
	}
	log.Println("All tables cleared successfully")

	// Сбрасываем последовательности (SERIAL/auto-increment)
	sequences := []string{
		"tenders_id_seq",
		"tender_participants_id_seq",
		"tender_bids_id_seq",
		"history_id_seq",
		"pending_users_id_seq",
	}

	for _, seq := range sequences {
		query := "ALTER SEQUENCE " + seq + " RESTART WITH 1"
		_, err := db.ExecContext(ctx, query)
		if err != nil {
			log.Printf("Warning: failed to reset sequence %s: %v", seq, err)
		} else {
			log.Printf("Reset sequence: %s", seq)
		}
	}

	log.Println("\nDatabase cleanup completed successfully!")
}
