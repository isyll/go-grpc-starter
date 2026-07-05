package config

import "time"

type AppConfig struct {
	Info struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"app"`

	Server struct {
		Port string `yaml:"port"`

		// Reflection exposes the gRPC reflection service (grpcurl, buf curl).
		// Enable it in development; disable it on internet-facing servers.
		Reflection bool `yaml:"reflection"`

		// Transport limits. Zero values keep the grpc-go defaults.
		MaxRecvMsgSizeBytes int           `yaml:"max_recv_msg_size_bytes"`
		MaxSendMsgSizeBytes int           `yaml:"max_send_msg_size_bytes"`
		ConnectionTimeout   time.Duration `yaml:"connection_timeout"`

		Keepalive struct {
			// Server-initiated pings on idle connections.
			Time    time.Duration `yaml:"time"`
			Timeout time.Duration `yaml:"timeout"`
			// Connection lifecycle bounds; useful behind load balancers.
			MaxConnectionIdle     time.Duration `yaml:"max_connection_idle"`
			MaxConnectionAge      time.Duration `yaml:"max_connection_age"`
			MaxConnectionAgeGrace time.Duration `yaml:"max_connection_age_grace"`
			// Enforcement: the minimum interval clients may ping at.
			MinClientInterval time.Duration `yaml:"min_client_interval"`
		} `yaml:"keepalive"`

		// ShutdownGrace bounds GracefulStop before in-flight RPCs are cut off.
		ShutdownGrace time.Duration `yaml:"shutdown_grace"`
	} `yaml:"server"`

	I18n struct {
		DefaultLanguage string `yaml:"default_language"`
		LocalesDir      string `yaml:"locales_dir"`
	} `yaml:"i18n"`
}

func (c *AppConfig) GetServerAddress() string {
	return "0.0.0.0:" + c.Server.Port
}
