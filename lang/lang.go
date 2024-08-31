package lang

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"

	"github.com/HyPE-Network/vanilla-proxy/log"
	"github.com/HyPE-Network/vanilla-proxy/utils"

	"github.com/zhyee/zipstream"
)

// Map of translation token as key, and translated string as value
type LangMap map[string]string

// Map of language as key, and LangMap as value
type Langs map[string]LangMap

// All available server lang files parsed
var langs = Langs{}

// Maybe move to config?
const defaultLocale = "en_US"

type RawMessage struct {
	RawText   []*RawMessage `json:"rawtext,omitempty"`
	Text      *string       `json:"text,omitempty"`
	Translate *string       `json:"translate,omitempty"`
	With      any           `json:"with,omitempty"` // Actually []string or []RawMessage
}

type RawText struct {
	RawText []*RawMessage `json:"rawtext,omitempty"`
}

// Translates plain text like "string" or json rawtext like "{"rawtext":{"translate":"this.is.rawtext"}}" into text
func Translate(stringOrJsonRawText string, toLocale string) string {
	txts := langs[toLocale]

	parsed := RawText{}
	err := json.Unmarshal([]byte(stringOrJsonRawText), &parsed)
	if err != nil {
		// Common text
		return stringOrJsonRawText
	} else {
		// RawText
		var result = ""
		parsed.toString(&result, &txts)
		return result
	}
}

// Retrieves all packs from urls and paths, parses lang files and keeps parsed
// Content in memory to be later used by lang.Translate function
func GetLangsFromPackConfig(config utils.Config) {
	readAndParseFromArray(&config.Resources.PackURLs, getLangsFromMcpackUrl)
	readAndParseFromArray(&config.Resources.PackPaths, getLangsFromPackPath)
}

// Shorthand for the GetLangsFromPack config
func readAndParseFromArray(arr *[]string, parse func(arrayElement string) (*Langs, error)) {
	for _, path := range *arr {
		langMap, err := parse(path)
		if err != nil {
			fmt.Printf("unable to parse lang files for %s, error: %s", path, err)
		} else {
			langs.mergeWith(langMap)
		}
	}
}

// Merges one Langs struct into another
func (langs *Langs) mergeWith(toAdd *Langs) {
	for locale, entries := range *toAdd {
		for key, entry := range entries {
			if (*langs)[locale] == nil {
				(*langs)[locale] = LangMap{}
			}
			(*langs)[locale][key] = entry
		}
	}
}

// Parses content of the .lang file into the LangMap
func parseLangFileToMap(fileContent string) *LangMap {
	result := make(LangMap)
	lines := strings.Split(string(fileContent), "\n")
	re := regexp.MustCompile(`([^=]+)=([^#]*)`)

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "#") {
			continue
		}

		match := re.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		result[match[1]] = match[2]
	}

	return &result
}

// It takes path to pack, goes through texts/ directory,
// parses all .lang files and returns Langs struct
// which contains them.
func getLangsFromPackPath(pathToPack string) (*Langs, error) {
	textsPath := pathToPack + "texts/"
	dir, err := os.ReadDir(textsPath)
	if err != nil {
		return nil, err
	}

	packLangs := make(Langs)
	for _, file := range dir {
		if !strings.HasSuffix(file.Name(), ".lang") || file.IsDir() {
			continue
		}

		content, err := os.ReadFile(textsPath + file.Name())

		if err != nil {
			return nil, err
		}

		packLangs[strings.Replace(file.Name(), ".lang", "", 1)] = *parseLangFileToMap(string(content))
	}

	return &packLangs, nil
}

// It takes URL of the pack, makes request to it,
// Parses .mcpack file, finds all .lang files in the ./texts/
// directory and then parses them and returns Langs struct containing them
func getLangsFromMcpackUrl(url string) (*Langs, error) {
	// Make a GET request to download the mcpack (ZIP) file
	response, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// Create regex to filter files we need
	textsLangsPathRegex := regexp.MustCompile(`[^\/]*texts/([^\/]+).lang$`)
	langs := make(Langs)
	zipReader := zipstream.NewReader(response.Body)
	for {
		entry, err := zipReader.GetNextEntry()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Logger.Fatalf("unable to get next entry: %s", err)
		}

		match := textsLangsPathRegex.FindStringSubmatch(entry.Name)
		if !entry.IsDir() && match != nil {
			entryContent, err := entry.Open()
			if err != nil {
				log.Logger.Fatalf("unable to open zip file: %s", err)
			}

			content, err := io.ReadAll(entryContent)
			if err != nil {
				log.Logger.Fatalf("read zip file content fail: %s", err)
			}

			// Parse actual file content
			langs[match[1]] = *parseLangFileToMap(string(content))

			if err := entryContent.Close(); err != nil {
				log.Logger.Fatalf("close zip entry reader fail: %s", err)
			}
		}
	}
	return &langs, nil
}

func (text *RawText) toString(result *string, langMap *LangMap) {
	for _, t := range text.RawText {
		t.toString(result, langMap)
	}
}

func (message *RawMessage) toString(result *string, langMap *LangMap) {
	if message.Text != nil && *message.Text != "" {
		*result += *message.Text
	}

	if message.Translate != nil && *message.Translate != "" {
		translated := (*langMap)[*message.Translate]

		if translated == "" {
			translated = (langs)[defaultLocale][*message.Translate]
		}

		if translated != "" {
			if message.With != nil {
				parsedWith := []any{}
				switch with := message.With.(type) {
				case []interface{}:
					parsedWith = make([]any, len(with))
					copy(parsedWith, with)
				case map[string]any:
					stringWith, err := json.Marshal(with)
					if err != nil {
						panic(err)
					}
					text := RawText{}
					err = json.Unmarshal(stringWith, &text)
					if err != nil {
						panic(err)
					}

					parsedWith = make([]any, len(text.RawText))
					for i, rawmessage := range text.RawText {
						var message = ""
						rawmessage.toString(&message, langMap)
						parsedWith[i] = message
					}
				default:
					fmt.Println("unkown RawMessage.With type:", reflect.TypeOf(with), with)
				}

				*result += fmt.Sprintf(translated, parsedWith...)
			} else {
				*result += translated
			}
		} else {
			fmt.Println("unkown translation token:", *message.Translate)
			*result += "%" + *message.Translate
		}

		return
	}

	if message.RawText != nil {
		for _, tt := range message.RawText {
			tt.toString(result, langMap)
		}
		return
	}

	stringified, err := json.MarshalIndent(message, "", " ")
	if err != nil {
		fmt.Println("possibly empty json invalid RawMessage:", message)
	} else {
		fmt.Println("possibly empty RawMessage:", string(stringified))
	}
}
