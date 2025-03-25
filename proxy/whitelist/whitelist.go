package whitelist

import (
	"encoding/json"
	"os"
	"strings"
	"sync"

	"github.com/HyPE-Network/vanilla-proxy/log"
)

type WhitelistManager struct {
	mu      sync.Mutex
	Players map[string]string
}

func Init() *WhitelistManager {
	wm := &WhitelistManager{
		Players: make(map[string]string, 0),
	}

	wm.mu.Lock()
	defer wm.mu.Unlock()

	if _, err := os.Stat("whitelist.json"); os.IsNotExist(err) {
		f, err := os.Create("whitelist.json")
		if err != nil {
			log.Logger.Error("Error creating whitelist", "error", err)
			panic(err)
		}
		data, err := json.Marshal(wm.Players)
		if err != nil {
			log.Logger.Error("Error encoding default whitelist", "error", err)
			panic(err)
		}
		if _, err := f.Write(data); err != nil {
			log.Logger.Error("Error writing encoded default whitelist", "error", err)
			panic(err)
		}
		_ = f.Close()
	}

	data, err := os.ReadFile("whitelist.json")
	if err != nil {
		log.Logger.Error("Error reading whitelist", "error", err)
		panic(err)
	}

	if err := json.Unmarshal(data, &wm.Players); err != nil {
		log.Logger.Error("Error decoding whitelist", "error", err)
		panic(err)
	}

	return wm
}

func (wm *WhitelistManager) HasPlayer(name string, xuid string) bool {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	for player_name, player_xuid := range wm.Players {
		if strings.EqualFold(player_xuid, xuid) {
			if !strings.EqualFold(player_name, name) {
				delete(wm.Players, player_name)
				wm.Players[name] = xuid
				wm.save()
			}
			return true
		}

		if strings.EqualFold(player_name, name) {
			if xuid != "" && player_xuid == "none" {
				wm.Players[player_name] = xuid
				wm.save()
			}
			return true
		}
	}

	return false
}

// save saves the whitelist to the whitelist.json file, mutex must be held by the calling function.
func (wm *WhitelistManager) save() {
	file, err := os.Create("whitelist.json")
	if err != nil {
		log.Logger.Error("Error creating whitelist file", "error", err)
		panic(err)
	}

	p, err := json.MarshalIndent(wm.Players, "", "\t")
	if err != nil {
		log.Logger.Error("Error marshaling whitelist", "error", err)
		panic(err)
	}

	_, err = file.Write(p)
	if err != nil {
		log.Logger.Error("Error writing whitelist", "error", err)
		panic(err)
	}
	file.Close()
}
