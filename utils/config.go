package utils

import (
	"fmt"
	"os"
	"strconv"
	"strings"

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
		MaxX    int32
		MinZ    int32
		MaxZ    int32
	}
	Api struct {
		ApiHost    string
		ApiKey     string
		XboxApiKey string
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
		DiscordLoggingEnabled     bool
		DiscordCommandLogsWebhook string
		DiscordChatLogsWebhook    string
		DiscordSignLogsWebhook    string
		DiscordStaffAlertsWebhook string
		ProfilerHost              string
		SentryDsn                 string
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
		Server: struct {
			SecuredSlots    int
			ViewDistance    int32
			Whitelist       bool
			DisableXboxAuth bool
			Prefix          string
			FlushRate       int
		}{
			SecuredSlots:    5,
			ViewDistance:    10,
			Whitelist:       true,
			DisableXboxAuth: false,
			Prefix:          "VANILLA",
			FlushRate:       10,
		},
		WorldBorder: struct {
			Enabled bool
			MinX    int32
			MaxX    int32
			MinZ    int32
			MaxZ    int32
		}{
			Enabled: true,
			MinX:    -12000,
			MinZ:    -12000,
			MaxX:    12000,
			MaxZ:    12000,
		},
		Logging: struct {
			DiscordLoggingEnabled     bool
			DiscordCommandLogsWebhook string
			DiscordChatLogsWebhook    string
			DiscordSignLogsWebhook    string
			DiscordStaffAlertsWebhook string
			ProfilerHost              string
			SentryDsn                 string
		}{
			DiscordLoggingEnabled: false,
			ProfilerHost:          "0.0.0.0:19135",
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

	if c.Server.SecuredSlots <= 0 {
		panic("SecuredSlots must be a value greater than 0!")
	}

	if c.Server.ViewDistance <= 0 {
		panic("ViewDistance must be a value greater than 0!")
	}

	if c.Server.FlushRate <= 0 {
		panic("FlushRate must be a value greater than 0!")
	}

	// Verify world border is set correctly
	if c.WorldBorder.MinX >= c.WorldBorder.MaxX || c.WorldBorder.MinZ >= c.WorldBorder.MaxZ {
		panic("WorldBorder is not set correctly!")
	}

	if c.Api.ApiHost == "" {
		panic("API Host must be a valid address!")
	}

	if c.Api.ApiKey == "" {
		panic("API Key must be provided!")
	}

	if c.Api.XboxApiKey == "" {
		panic("Xbox API Key must be provided for Xbox API authentication!")
	}

	if c.Database.Host == "" {
		panic("Database Host must be a valid address!")
	}

	if c.Database.Key == "" {
		panic("Database Key must be provided!")
	}

	if c.Database.Name == "" {
		panic("Database Name must be provided!")
	}

	if c.Logging.DiscordLoggingEnabled {
		if c.Logging.DiscordCommandLogsWebhook == "" {
			panic("Discord Command Logs Webhook must be provided when Discord logging is enabled!")
		}

		if c.Logging.DiscordChatLogsWebhook == "" {
			panic("Discord Chat Logs Webhook must be provided when Discord logging is enabled!")
		}

		if c.Logging.DiscordSignLogsWebhook == "" {
			panic("Discord Sign Logs Webhook must be provided when Discord logging is enabled!")
		}

		if c.Logging.DiscordStaffAlertsWebhook == "" {
			panic("Discord Staff Alerts Webhook must be provided when Discord logging is enabled!")
		}
	}

	if c.Logging.ProfilerHost == "" {
		// Set the port to 3 more than the proxy port
		port, err := strconv.Atoi(strings.Split(c.Connection.ProxyAddress, ":")[1])
		if err != nil {
			panic("Failed to parse proxy port: " + err.Error())
		}
		c.Logging.ProfilerHost = fmt.Sprintf("0.0.0.0:%d", port+3)
	}

	return c
}
