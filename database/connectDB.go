package database

import (
	"database/sql"
	"log/slog"
	"os"

	_ "github.com/lib/pq"
)

func ConnectDB() (*sql.DB, error) {

	db_url := os.Getenv("DATABASE_URL")

	psqlconn := db_url

	db, err := sql.Open("postgres", psqlconn)

	if err != nil {
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		return nil, err
	}

	slog.Info("Successfully connected!")

	return db, nil

}
