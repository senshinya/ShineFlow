package config

import "os"

type Config struct {
	Port  string
	DBDSN string
}

const (
	defaultPort  = "8888"
	defaultDBDSN = "host=localhost port=5432 user=postgres password=postgres dbname=shineflow sslmode=disable TimeZone=Asia/Shanghai"
)

func Load() *Config {
	return &Config{
		Port:  getEnv("SHINEFLOW_PORT", defaultPort),
		DBDSN: getEnv("SHINEFLOW_DB_DSN", defaultDBDSN),
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
