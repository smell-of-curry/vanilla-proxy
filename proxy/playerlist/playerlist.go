package playerlist

import (
	"encoding/json"
	"errors"
	"os"
	"sync"

	"github.com/HyPE-Network/vanilla-proxy/log"
	"github.com/gofrs/flock"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

type Player struct {
	PlayerName         string `json:"playerName"`
	Identity           string `json:"identity"`
	ClientSelfSignedID string `json:"clientSelfSignedID"`
}

type PlayerlistManager struct {
	mu      sync.Mutex
	Players map[string]Player `json:"players"`
}

// Init initializes the playerlist manager
func Init() (*PlayerlistManager, error) {
	plm := &PlayerlistManager{
		Players: make(map[string]Player),
	}

	plm.mu.Lock()
	defer plm.mu.Unlock()

	// Create a file lock
	lock := flock.New("playerlist.json.lock")
	if err := lock.Lock(); err != nil {
		log.Logger.Error("Error locking playerlist file", "error", err)
		return nil, err
	}
	defer lock.Unlock()

	// Open or create the playerlist.json file
	file, err := os.OpenFile("playerlist.json", os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		log.Logger.Error("Error opening/creating playerlist", "error", err)
		return nil, err
	}
	defer file.Close()

	// Check if the file is empty (newly created)
	info, err := file.Stat()
	if err != nil {
		log.Logger.Error("Error stating playerlist file", "error", err)
		return nil, err
	}
	if info.Size() == 0 {
		data, err := json.Marshal(plm.Players)
		if err != nil {
			log.Logger.Error("Error encoding default playerlist", "error", err)
			return nil, err
		}
		if _, err := file.Write(data); err != nil {
			log.Logger.Error("Error writing encoded default playerlist", "error", err)
			return nil, err
		}
	} else {
		// Read the existing data from the file
		data := make([]byte, info.Size())
		if _, err := file.Read(data); err != nil {
			log.Logger.Error("Error reading playerlist", "error", err)
			return nil, err
		}

		// Unmarshal the data into the player map
		if err := json.Unmarshal(data, &plm.Players); err != nil {
			log.Logger.Error("Error decoding playerlist", "error", err)
			return nil, err
		}
	}

	return plm, nil
}

// GetConnIdentityData returns the identity data for a player's connection
func (plm *PlayerlistManager) GetConnIdentityData(conn *minecraft.Conn) (login.IdentityData, error) {
	plm.mu.Lock()
	defer plm.mu.Unlock() // Keep mutex locked for the entire operation

	identityData := conn.IdentityData()
	xuid := identityData.XUID

	if player, ok := plm.Players[xuid]; ok {
		return login.IdentityData{
			XUID:        xuid,
			DisplayName: player.PlayerName,
			Identity:    player.Identity,
			TitleID:     identityData.TitleID,
		}, nil
	}

	// Set player with mutex still locked
	player := Player{
		PlayerName:         conn.IdentityData().DisplayName,
		Identity:           conn.IdentityData().Identity,
		ClientSelfSignedID: conn.ClientData().SelfSignedID,
	}
	plm.Players[xuid] = player

	// Save the playerlist to disk
	err := plm.savePlayerlist()
	if err != nil {
		return login.IdentityData{}, err
	}

	return identityData, nil
}

// GetConnClientData returns the client data for a player's connection
func (plm *PlayerlistManager) GetConnClientData(conn *minecraft.Conn) (login.ClientData, error) {
	plm.mu.Lock()
	defer plm.mu.Unlock() // Keep mutex locked for the entire operation

	xuid := conn.IdentityData().XUID
	clientData := conn.ClientData()

	if player, ok := plm.Players[xuid]; ok {
		clientData.SelfSignedID = player.ClientSelfSignedID
		return clientData, nil
	}

	// Set player with mutex still locked
	player := Player{
		PlayerName:         conn.IdentityData().DisplayName,
		Identity:           conn.IdentityData().Identity,
		ClientSelfSignedID: conn.ClientData().SelfSignedID,
	}
	plm.Players[xuid] = player

	// Save the playerlist to disk
	err := plm.savePlayerlist()
	if err != nil {
		return login.ClientData{}, err
	}

	return clientData, nil
}

// GetXUIDFromName returns the XUID of a player by their name
func (plm *PlayerlistManager) GetXUIDFromName(playerName string) (string, error) {
	plm.mu.Lock()
	defer plm.mu.Unlock()

	for xuid, player := range plm.Players {
		if player.PlayerName == playerName {
			return xuid, nil
		}
	}

	return "", errors.New("player not found")
}

// GetPlayer returns a player from the playerlist by their XUID
func (plm *PlayerlistManager) GetPlayer(xuid string) (Player, error) {
	plm.mu.Lock()
	defer plm.mu.Unlock()

	player, ok := plm.Players[xuid]
	if !ok {
		return Player{}, errors.New("player not found")
	}

	return player, nil
}

// savePlayerlist saves the current playerlist to the JSON file
func (plm *PlayerlistManager) savePlayerlist() error {
	// Create a file lock
	lock := flock.New("playerlist.json.lock")
	if err := lock.Lock(); err != nil {
		log.Logger.Error("Error locking playerlist file", "error", err)
		return err
	}

	defer func() {
		if err := lock.Unlock(); err != nil {
			log.Logger.Error("Error unlocking playerlist file", "error", err)
		}
	}()

	// Save the playerlist to disk
	data, err := json.MarshalIndent(plm.Players, "", "  ")
	if err != nil {
		log.Logger.Error("Error encoding playerlist", "error", err)
		return err
	}

	// Open the file for writing
	file, err := os.OpenFile("playerlist.json", os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Logger.Error("Error opening playerlist for writing", "error", err)
		return err
	}
	defer file.Close()

	if _, err := file.Write(data); err != nil {
		log.Logger.Error("Error writing playerlist", "error", err)
		return err
	}

	return nil
}
