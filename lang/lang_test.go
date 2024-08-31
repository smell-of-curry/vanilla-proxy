package lang

import (
	"net/http"
	"testing"

	"github.com/HyPE-Network/vanilla-proxy/utils"
	"github.com/stretchr/testify/assert"
)

func TestFullProcess(t *testing.T) {
	go httpServe("../testdata/")

	GetLangsFromPackConfig(utils.Config{
		Resources: struct {
			PackURLs  []string
			PackPaths []string
		}{
			PackURLs:  []string{"not exists", "http://127.0.0.1:3000/rp.mcpack"},
			PackPaths: []string{"../testdata/rp-complex/", "../testdata/rp/"},
		},
	})

	assert.Equal(t, "%entity.unkown.name", Translate(`{"rawtext":[{"translate":"entity.unkown.name"}]}`, "en_US"))

	assert.Equal(t, "Raw name", Translate("Raw name", "en_US"))

	assert.Equal(t, "Complex entity", Translate(`{"rawtext":[{"translate":"entity.complex.name"}]}`, "en_US"))

	// Not enough args case
	// Not really good but i don't think we will use args anyway
	assert.Equal(t,
		"Some text with simple arg and with the %!s(MISSING) aaand finally %!s(MISSING)",
		Translate(`{"rawtext":[{"translate":"text.with.args","with":{"rawtext":[{"text":"simple arg"}]}}]}`, "en_US"),
	)

	assert.Equal(t,
		"Some text with %!s(MISSING) and with the %!s(MISSING) aaand finally %!s(MISSING)",
		Translate(`{"rawtext":[{"translate":"text.with.args","with":12}]}`, "en_US"),
	)

	assert.Equal(t,
		"Some text with simple text and with the %!s(MISSING) aaand finally %!s(MISSING)",
		Translate(`{"rawtext":[{"translate":"text.with.args","with":["simple text"]}]}`, "en_US"),
	)

	// All args case
	assert.Equal(t,
		"Some text with simple arg and with the common string aaand finally Test entity",
		Translate(`{"rawtext":[{"translate":"text.with.args","with":{"rawtext":[{"text":"simple arg"},{"text":"common string"},{"translate":"entity.test.name"}]}}]}`, "en_US"),
	)

	assert.Equal(t,
		"Some text with simple arg and with the common string aaand finally Test entity",
		Translate(`{"rawtext":[{"rawtext":[{"translate":"text.with.args","with":{"rawtext":[{"text":"simple arg"},{"text":"common string"},{"translate":"entity.test.name"}]}}]}]}`, "en_US"),
	)

	// It should take translation token from default locale if in target locale it does not exists
	assert.Equal(t, "Complex entity", Translate(`{"rawtext":[{"translate":"entity.complex.name"}]}`, "ru_RU"))
}

func TestMerge(t *testing.T) {
	resultLangs := Langs{
		"en_US": {
			"entity.test.name": "Test entity",
		},
		"ru_RU": {
			"entity.test.name": "Тестовая сущность",
		},
	}

	second := Langs{
		"en_US": {
			"entity.test.name": "Teeeeest entity",
			"another.key":      "New value",
		},
	}

	resultLangs.mergeWith(&second)
	assert.Equal(t, Langs{
		"en_US": {
			"entity.test.name": "Teeeeest entity",
			"another.key":      "New value",
		},
		"ru_RU": {
			"entity.test.name": "Тестовая сущность",
		},
	}, resultLangs)
}

func TestLang(t *testing.T) {
	_, err := getLangsFromPackPath("does not exists")
	assert.NotNil(t, err)

	locales, err := getLangsFromPackPath("../testdata/rp/")

	assert.Equal(t, Langs{
		"en_US": {
			"another.text":     "Text",
			"entity.test.name": "Test entity",
		},
		"ru_RU": {
			"entity.test.name": "Тестовая сущность",
			"item.test.name":   "Тестовый предмет  ",
		},
	}, *locales)
	assert.Nil(t, err)
}

func TestMcpack(t *testing.T) {
	locales, err := getLangsFromMcpackUrl("http://localhost:3000/rp.mcpack")

	assert.Equal(t, Langs{
		"en_US": {
			"another.text":     "Text",
			"entity.test.name": "Test entity",
		},
		"ru_RU": {
			"entity.test.name": "Тестовая сущность",
			"item.test.name":   "Тестовый предмет  ",
		},
	}, *locales)
	assert.Nil(t, err)
}

func httpServe(dir string) {
	http.Handle("/", http.FileServer(http.Dir(dir)))
	http.ListenAndServe(":3000", nil)
}
