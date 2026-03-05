package config

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL    string
	Port           string
	Secret         string
	ReposPath      string
	AllowedOrigins string
}

func Load() *Config {
	godotenv.Load()
	return &Config{
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://devnook:devnook@localhost:5432/devnook?sslmode=disable"),
		Port:           getEnv("DEVNOOK_PORT", "8080"),
		Secret:         getEnv("DEVNOOK_SECRET", "changeme"),
		ReposPath:      getEnv("DEVNOOK_REPOS_PATH", "./repos"),
		AllowedOrigins: getEnv("DEVNOOK_ALLOWED_ORIGINS", ""),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
