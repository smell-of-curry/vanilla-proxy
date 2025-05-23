package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/HyPE-Network/vanilla-proxy/log"
	"github.com/tailscale/hujson"
)

func Format(a []any) string {
	return strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintln(a...), "\n"), "\n")
}

func GetPluralForm(num int, form1 string, form2 string, form3 string) string {
	n := int(math.Abs(float64(num))) % 100
	n1 := n % 10
	if n > 10 && n < 20 {
		return form3
	} else if n1 > 1 && n1 < 5 {
		return form2
	} else if n1 == 1 {
		return form1
	}
	return form3
}

func GetFullPluralForm(num int, form1 string, form2 string, form3 string) string {
	n := int(math.Abs(float64(num))) % 100
	n1 := n % 10
	if n > 10 && n < 20 {
		return strconv.Itoa(num) + " " + form3
	} else if n1 > 1 && n1 < 5 {
		return strconv.Itoa(num) + " " + form2
	} else if n1 == 1 {
		return strconv.Itoa(num) + " " + form1
	}
	return strconv.Itoa(num) + " " + form3
}

func GetTimestamp() int64 {
	return time.Now().Unix()
}

func GetMillis() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func MillisToDate(t int64) string {
	return TimestampToDate(t / 1000)
}

func TimestampToDate(t int64) string {
	return FormatTime(time.Unix(t, 0))
}

func FormatTime(t time.Time) string {
	return t.Format("15:04:05 2006.01.02") // magic numbers https://go.dev/src/time/format.go
}

// FetchDatabase fetches data from the database and handles 429 rate-limiting errors with retries
func FetchDatabase[T any](tableName string) (map[string]T, error) {
	dbConfig := ReadConfig().Database
	uri := dbConfig.Host + "/api/database/" + dbConfig.Name + "/table/" + tableName

	log.Logger.Info("Fetching from database", "table", tableName, "uri", uri)

	client := &http.Client{
		Timeout: time.Second * 10, // Longer timeout for database operations
	}
	var resp *http.Response
	var err error

	// Implement retry logic with exponential backoff
	retryAttempts := 5
	for i := 0; i < retryAttempts; i++ {
		req, err := http.NewRequest("GET", uri, nil)
		if err != nil {
			log.Logger.Error("Failed to create new request", "error", err)
			return nil, err
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("authorization", dbConfig.Key)

		resp, err = client.Do(req)
		if err != nil {
			log.Logger.Warn("Database connection error, retrying...", "error", err, "attempts_left", retryAttempts-i)
			if i == retryAttempts-1 {
				LogErrorToDiscord(err)
			}
			time.Sleep(time.Duration(1<<i) * time.Second) // Exponential backoff
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			// Successful request
			break
		} else if resp.StatusCode == http.StatusTooManyRequests {
			// Handle 429 status code
			if i < retryAttempts-1 {
				retryAfter := time.Duration(1<<i) * time.Second // Exponential backoff
				log.Logger.Info("Rate limited, retrying", "delay", retryAfter)
				time.Sleep(retryAfter)
				continue
			}
		} else {
			// Other non-success status codes
			err := fmt.Errorf("failed to fetch data, status code: %d", resp.StatusCode)
			LogErrorToDiscord(err)
			return nil, err
		}
	}

	if resp == nil {
		err := fmt.Errorf("failed to fetch data after %d attempts", retryAttempts)
		LogErrorToDiscord(err)
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		LogErrorToDiscord(err)
		return nil, err
	}

	var response []struct {
		Data      T      `json:"data"`
		Key       string `json:"_key"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}

	err = json.Unmarshal(body, &response)
	if err != nil {
		log.Logger.Error("Failed to marshal json", "error", err)
		return nil, err
	}

	obj := make(map[string]T)
	for _, v := range response {
		obj[v.Key] = v.Data
	}
	return obj, nil
}

func LogErrorToDiscord(err error) {
	config := ReadConfig()
	log.Logger.Error("Error", "error", err)

	// Don't send to Discord if webhook is not configured
	if !config.Logging.DiscordLoggingEnabled {
		return
	}

	params := map[string]interface{}{
		"username":   fmt.Sprintf("[%s] Failed to Ping Database", config.Server.Prefix),
		"avatar_url": "https://encrypted-tbn0.gstatic.com/images?q=tbn:ANd9GcTGcSsrxGsP-WcwrSPJCalNokgnVFtho64ycreClTns3g&s",
		"content":    "@everyone",
		"embeds": []map[string]interface{}{
			{
				"title":       "Failed to Ping Database",
				"description": fmt.Sprintf("The Proxy has failed to ping the database on port: %s. Please check the database status!", fmt.Sprint(config.Database.Host)),
				"color":       16711680,
				"timestamp":   time.Now().Format(time.RFC3339),
				"fields": []map[string]any{
					{
						"name":   "Error",
						"value":  err.Error(),
						"inline": true,
					},
					{
						"name":   "Time",
						"value":  time.Now().Format("01/02/2006 3:04 PM"),
						"inline": true,
					},
				},
			},
		},
	}

	SendJsonToDiscord(config.Logging.DiscordStaffAlertsWebhook, params)
}

func SendStaffAlertToDiscord(title string, description string, color int, fields []map[string]interface{}) {
	config := ReadConfig()
	params := map[string]any{
		"username":   fmt.Sprintf("[%s] Staff Alert", config.Server.Prefix),
		"avatar_url": "https://media.forgecdn.net/avatars/121/268/636409261203329160.png",
		"content":    "@everyone",
		"embeds": []map[string]any{
			{
				"title":       title,
				"description": description,
				"color":       color,
				"timestamp":   time.Now().Format(time.RFC3339),
				"fields":      fields,
			},
		},
	}

	SendJsonToDiscord(config.Logging.DiscordStaffAlertsWebhook, params)
}

func SendJsonToDiscord(url string, params map[string]any) {
	jsonParams, err := json.Marshal(params)
	if err != nil {
		log.Logger.Error("Failed to marshal json", "error", err)
		return
	}
	req, err := http.NewRequest("POST", url, io.NopCloser(bytes.NewBuffer(jsonParams)))
	if err != nil {
		log.Logger.Error("Failed to create new request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Logger.Error("Failed to send json to discord", "error", err)
		return
	}
	defer resp.Body.Close()
}

func StringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

var (
	profilePictureUrls = sync.Map{} // Thread-safe map to cache profile picture URLs
)

// GetXboxIconLink retrieves the Xbox profile picture URL for the given XUID.
// It first checks if the URL is cached; if not, it fetches it from the API with error handling for 429 status codes.
func GetXboxIconLink(xuid string, xboxApiKey string) (string, error) {
	if cache, ok := profilePictureUrls.Load(xuid); ok {
		return cache.(string), nil
	}

	// Default values for maxRetries and backoff
	maxRetries := 5
	backoff := time.Second

	// Use a more resilient transport with longer timeouts
	transport := &http.Transport{
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     30 * time.Second,
		DisableKeepAlives:   false,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
	}

	url := fmt.Sprintf("https://xbl.io/api/v2/account/%s", xuid)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create new request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Authorization", xboxApiKey)

	var resp *http.Response

	for maxRetries > 0 {
		resp, err = client.Do(req)
		if err != nil {
			log.Logger.Warn("Xbox API connection error, retrying...", "error", err, "attempts_left", maxRetries)
			maxRetries--
			time.Sleep(backoff)
			backoff *= 2
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			// Handle 429 error with retry after exponential backoff
			maxRetries--
			log.Logger.Warn("Received 429 Too Many Requests, retrying...", "attempt", 5-maxRetries)

			retryAfter := resp.Header.Get("Retry-After")
			if retryAfter != "" {
				// If the server provides a "Retry-After" header, respect it
				delay, _ := strconv.Atoi(retryAfter)
				time.Sleep(time.Duration(delay) * time.Second)
			} else {
				// Exponential backoff if no "Retry-After" header is provided
				time.Sleep(backoff)
				backoff *= 2
			}
			continue
		}

		if resp.StatusCode != http.StatusOK {
			err := fmt.Errorf("failed to fetch Xbox Icon Link, status code: %d", resp.StatusCode)
			return "", err
		}

		break
	}

	if maxRetries <= 0 {
		return "", fmt.Errorf("exceeded maximum retries after connection issues")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var data struct {
		ProfileUsers []struct {
			Settings []struct {
				Value string `json:"value"`
			} `json:"settings"`
		} `json:"profileUsers"`
	}

	err = json.Unmarshal(body, &data)
	if err != nil {
		LogErrorToDiscord(err)
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(data.ProfileUsers) == 0 || len(data.ProfileUsers[0].Settings) == 0 {
		return "", fmt.Errorf("profile picture URL not found")
	}

	profilePictureUrl := data.ProfileUsers[0].Settings[0].Value
	profilePictureUrls.Store(xuid, profilePictureUrl) // Cache the result

	return profilePictureUrl, nil
}

// ParseCommentedJSON parses JSON with comments and returns a JSON byte slice.
func ParseCommentedJSON(b []byte) ([]byte, error) {
	ast, err := hujson.Parse(b)
	if err != nil {
		return b, err
	}
	ast.Standardize()
	return ast.Pack(), nil
}
