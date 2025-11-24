package settings

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Settings struct {
    BotToken    string
    AdminIDs    []int64
    OrganizerIDs []int64
    DatabaseURL string
    FilesDir    string
}

func LoadSettings() *Settings {
    godotenv.Load()
    
    s := &Settings{}

    s.BotToken = os.Getenv("BOT_TOKEN")

    // Админы
    adminIDsStr := os.Getenv("ADMIN_IDS")
    s.AdminIDs = []int64{}
    for _, x := range strings.Split(adminIDsStr, ",") {
        x = strings.TrimSpace(x)
        if x == "" {
            continue
        }
        id, err := strconv.ParseInt(x, 10, 64)
        if err == nil {
            s.AdminIDs = append(s.AdminIDs, id)
        }
    }

    // Организатор
    organizerIDsStr := os.Getenv("ORGANIZER_ID")
    s.OrganizerIDs = []int64{}
    for _, x := range strings.Split(organizerIDsStr, ",") {
        x = strings.TrimSpace(x)
        if x == "" {
            continue
        }
        id, err := strconv.ParseInt(x, 10, 64)
        if err == nil {
            s.OrganizerIDs = append(s.OrganizerIDs, id)
        }
    }

    // Остальное
    s.DatabaseURL = os.Getenv("DATABASE_URL")
    if s.DatabaseURL == "" {
        s.DatabaseURL = "postgresql://postgres:admin@localhost:5432/tender_bot?sslmode=disable"
    }

    s.FilesDir = os.Getenv("FILES_DIR")
    if s.FilesDir == "" {
        s.FilesDir = "./files"
    }

    return s
}
