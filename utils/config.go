package utils

import (
	"os"

	"github.com/HyPE-Network/vanilla-proxy/log"
	"github.com/pelletier/go-toml"
)

type Config struct {
	Connection struct {
		ProxyAddress  string
		RemoteAddress string
	}
	Server struct {
		SecuredSlots    int
		ViewDistance    int32
		Whitelist       bool
		DisableXboxAuth bool
		Prefix          string
		FlushRate       int
	}
	WorldBorder struct {
		Enabled bool
		MinX    int32
		MinZ    int32
		MaxX    int32
		MaxZ    int32
	}
	Api struct {
		ApiHost string
		ApiKey  string
	}
	Resources struct {
		PackURLs  []string
		PackPaths []string
	}
	Database struct {
		Host string
		Key  string
		Name string
	}
	Logging struct {
		DiscordCommandLogsWebhook string
		DiscordChatLogsWebhook    string
		DiscordSignLogsWebhook    string
		DiscordSignLogsIconURL    string
		DiscordStaffAlertsWebhook string
		ProfilerHost              string
	}
}

func ReadConfig() Config {
	// Initialize with default values
	defaultConfig := Config{
		Connection: struct {
			ProxyAddress  string
			RemoteAddress string
		}{
			ProxyAddress:  "0.0.0.0:19132",
			RemoteAddress: "0.0.0.0:19134",
		},
	}

	if _, err := os.Stat("config.toml"); os.IsNotExist(err) {
		log.Logger.Info("config.toml not found, creating default config")
		f, err := os.Create("config.toml")
		if err != nil {
			log.Logger.Error("Error creating config", "error", err)
			panic(err)
		}
		data, err := toml.Marshal(defaultConfig)
		if err != nil {
			log.Logger.Error("Error encoding default config", "error", err)
			panic(err)
		}
		if _, err := f.Write(data); err != nil {
			log.Logger.Error("Error writing encoded default config", "error", err)
			panic(err)
		}
		_ = f.Close()
	}

	data, err := os.ReadFile("config.toml")
	if err != nil {
		log.Logger.Error("Error reading config", "error", err)
		panic(err)
	}

	c := Config{}
	if err := toml.Unmarshal(data, &c); err != nil {
		log.Logger.Error("Error decoding config", "error", err)
		panic(err)
	}

	// Validate required fields and set defaults if necessary
	if c.Connection.ProxyAddress == "" {
		panic("ProxyAddress is not assigned in config!")
	}

	if c.Connection.RemoteAddress == "" {
		panic("RemoteAddress is not assigned in config!")
	}

	if c.Server.ViewDistance <= 0 {
		panic("ViewDistance must be a value greater than 0!")
	}

	if c.Database.Host == "" {
		panic("Database Host must be a valid address!")
	}

	if c.Api.ApiHost == "" {
		panic("API Host must be a valid address!")
	}

	if c.Logging.DiscordCommandLogsWebhook == "" {
		panic("Discord Command Logs Webhook must be provided!")
	}

	if c.Logging.DiscordChatLogsWebhook == "" {
		panic("Discord Chat Logs Webhook must be provided!")
	}

	if c.Logging.DiscordStaffAlertsWebhook == "" {
		panic("Discord Staff Alerts Webhook must be provided!")
	}

	if c.Logging.ProfilerHost == "" {
		c.Logging.ProfilerHost = "127.0.0.1:1010"
	}

	return c
}
