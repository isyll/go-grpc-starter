package env

import (
	"os"
	"regexp"
	"strings"
)

const (
	Production  = "production"
	Testing     = "testing"
	Development = "development"

	prodShort = "prod"
	testShort = "test"
	devShort  = "dev"
)

var (
	envMultiSpace  = regexp.MustCompile(`\s+`)
	envNonAlphaNum = regexp.MustCompile(`[^a-z0-9\s]`)
)

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = envNonAlphaNum.ReplaceAllString(s, "")
	s = envMultiSpace.ReplaceAllString(s, " ")
	return s
}

func GetOrDefault(key, defaultVal string) string {
	val := os.Getenv(key)
	if val == "" {
		val = defaultVal
	}
	return sanitize(val)
}

func InitApp() string {
	switch GetOrDefault("APP_ENV", os.Getenv("GO_ENV")) {
	case Production, prodShort:
		return Production
	case Testing, testShort:
		return Testing
	default:
		return Development
	}
}

func IsDev() bool {
	appEnv := strings.ToLower(
		GetOrDefault("APP_ENV", os.Getenv("GO_ENV")),
	)
	return appEnv == Development || appEnv == devShort ||
		appEnv == Testing ||
		appEnv == testShort
}
