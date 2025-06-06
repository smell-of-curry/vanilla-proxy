package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/HyPE-Network/vanilla-proxy/handler"
	"github.com/HyPE-Network/vanilla-proxy/log"
	"github.com/HyPE-Network/vanilla-proxy/math"
	"github.com/HyPE-Network/vanilla-proxy/proxy/entity"
	"github.com/HyPE-Network/vanilla-proxy/proxy/player"
	"github.com/HyPE-Network/vanilla-proxy/proxy/player/human"
	"github.com/HyPE-Network/vanilla-proxy/proxy/playerlist"
	"github.com/HyPE-Network/vanilla-proxy/proxy/whitelist"
	"github.com/HyPE-Network/vanilla-proxy/proxy/world"
	"github.com/HyPE-Network/vanilla-proxy/utils"
	"github.com/google/uuid"

	"github.com/sandertv/go-raknet"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/sandertv/gophertunnel/minecraft/resource"

	"github.com/sandertv/gophertunnel/minecraft"
)

var ProxyInstance *Proxy

type Proxy struct {
	Worlds            *world.Worlds
	Entities          *entity.Entities
	Config            utils.Config
	Handlers          handler.HandlerManager
	Listener          *minecraft.Listener
	WhitelistManager  *whitelist.WhitelistManager
	PlayerListManager *playerlist.PlayerlistManager
	ResourcePacks     []*resource.Pack
	ctx               context.Context
	cancel            context.CancelFunc
	cleanupTasks      []func()
	tasksMu           sync.Mutex
}

func New(config utils.Config) *Proxy {
	playerListManager, err := playerlist.Init()
	if err != nil {
		log.Logger.Error("Error in initializing playerListManager", "error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	Proxy := &Proxy{
		Config:            config,
		Entities:          entity.Init(),
		PlayerListManager: playerListManager,
		WhitelistManager:  whitelist.Init(),
		ctx:               ctx,
		cancel:            cancel,
		cleanupTasks:      make([]func(), 0),
	}

	// Initialize an empty slice of *resource.Pack
	var resourcePacks []*resource.Pack

	// Loop through all the pack URLs and append each pack to the slice
	for _, url := range Proxy.Config.Resources.PackURLs {
		log.Logger.Debug("Reading resource pack from URL", "url", url)
		resourcePack, err := resource.ReadURL(url)
		if err != nil {
			log.Logger.Error("Failed to read resource pack from URL", "url", url, "error", err)
		}
		resourcePacks = append(resourcePacks, resourcePack)
	}

	// Loop through all the pack paths and append each pack to the slice
	for _, path := range Proxy.Config.Resources.PackPaths {
		log.Logger.Debug("Reading resource pack from path", "path", path)
		resourcePack, err := resource.ReadPath(path)
		if err != nil {
			log.Logger.Error("Failed to read resource pack from path", "path", path, "error", err)
		}
		resourcePacks = append(resourcePacks, resourcePack)
	}

	Proxy.ResourcePacks = resourcePacks

	if config.WorldBorder.Enabled {
		Proxy.Worlds = world.Init(math.NewArea2(config.WorldBorder.MinX, config.WorldBorder.MinZ, config.WorldBorder.MaxX, config.WorldBorder.MaxZ))
	}

	return Proxy
}

// The following program implements a proxy that forwards players from one local address to a remote address.
func (arg *Proxy) Start(h handler.HandlerManager) error {
	res, err := raknet.Ping(arg.Config.Connection.RemoteAddress)
	if err != nil {
		// Server prob not online, retrying
		log.Logger.Warn("Failed to ping server, retrying in 5 seconds", "error", err)
		time.Sleep(time.Second * 5)
		arg.Start(h)
		return nil
	}
	// Server is online, parse data
	status := minecraft.ParsePongData(res)
	log.Logger.Info("Server online", "name", status.ServerName, "motd", status.ServerSubName)
	p, err := minecraft.NewForeignStatusProvider(arg.Config.Connection.RemoteAddress)
	if err != nil {
		return fmt.Errorf("failed to create foreign status provider: %w", err)
	}

	arg.Listener, err = minecraft.ListenConfig{ // server settings
		AuthenticationDisabled: arg.Config.Server.DisableXboxAuth,
		StatusProvider:         p,
		ResourcePacks:          arg.ResourcePacks,
		TexturePacksRequired:   true,
		ErrorLog:               log.Logger,
		Compression:            packet.FlateCompression,
		FlushRate:              time.Millisecond * time.Duration(arg.Config.Server.FlushRate),
	}.Listen("raknet", arg.Config.Connection.ProxyAddress)

	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	log.Logger.Debug("Original server address", "remote", arg.Config.Connection.RemoteAddress, "public", arg.Config.Connection.ProxyAddress)
	log.Logger.Info("Proxy started", "version", protocol.CurrentVersion, "protocol", protocol.CurrentProtocol)
	arg.Handlers = h

	defer func() {
		if r := recover(); r != nil {
			log.Logger.Error("Recovered from panic in Handling Listener", "error", r, "stack", debug.Stack())
			log.Logger.Error("Closing listener", "error", arg.Listener.Close())
		}
	}()
	for {
		select {
		case <-arg.ctx.Done():
			log.Logger.Info("Proxy shutting down")
			return nil
		default:
			c, err := arg.Listener.Accept()
			if err != nil {
				if arg.ctx.Err() != nil {
					return nil // Exit if context is cancelled
				}

				// The listener closed, so we should restart it. c==nil
				log.Logger.Error("Listener accept error", "error", err)
				utils.SendStaffAlertToDiscord("Proxy Listener Closed", "```"+err.Error()+"```", 16711680, []map[string]interface{}{})

				return arg.Start(h)
			}

			if c == nil {
				log.Logger.Warn("Accepted a nil connection")
				continue
			}

			log.Logger.Debug("New connection", "addr", c.RemoteAddr())
			go arg.handleConn(c.(*minecraft.Conn))
		}
	}
}

// handleConn handles a new incoming minecraft.Conn from the minecraft.Listener passed.
func (arg *Proxy) handleConn(conn *minecraft.Conn) {
	defer func() {
		if r := recover(); r != nil {
			log.Logger.Error("Recovered from panic in handleConn", "error", r, "stack", debug.Stack())
			if conn != nil {
				arg.Listener.Disconnect(conn, "An internal error occurred")
			}
		}
	}()

	if conn == nil {
		log.Logger.Warn("Received nil connection. Skipping handling")
		return
	}

	go func() {
		<-arg.ctx.Done()
		if conn != nil {
			conn.Close() // Ensure the connection is closed when context is cancelled
		}
	}()

	playerWhitelisted := arg.WhitelistManager.HasPlayer(conn.IdentityData().DisplayName, conn.IdentityData().XUID)
	if arg.Config.Server.Whitelist {
		if !playerWhitelisted {
			arg.Listener.Disconnect(conn, "You are not whitelisted on this server!")
			return
		}
	}

	res, err := raknet.Ping(arg.Config.Connection.RemoteAddress)
	if err != nil {
		// Server just went offline while player was connecting
		arg.Listener.Disconnect(conn, "Server just went offline, please try again later!")
		return
	}
	// Server is online, fetch data
	status := minecraft.ParsePongData(res)
	if !arg.canJoinServer(status, conn, playerWhitelisted) {
		return
	}

	clientData, err := arg.PlayerListManager.GetConnClientData(conn)
	if err != nil {
		log.Logger.Error("Error in getting clientData", "error", err)
		arg.Listener.Disconnect(conn, strings.Split(err.Error(), ": ")[1])
		return
	}
	identityData, err := arg.PlayerListManager.GetConnIdentityData(conn)
	if err != nil {
		log.Logger.Error("Error in getting identityData", "error", err)
		arg.Listener.Disconnect(conn, strings.Split(err.Error(), ": ")[1])
		return
	}

	serverConn, err := minecraft.Dialer{
		KeepXBLIdentityData: true,
		ClientData:          clientData,
		IdentityData:        identityData,
		DownloadResourcePack: func(id uuid.UUID, version string, current int, total int) bool {
			return false
		},
		ErrorLog: log.Logger,
	}.DialTimeout("raknet", arg.Config.Connection.RemoteAddress, time.Second*30)

	if err != nil {
		log.Logger.Error("Failed to dial server", "error", err)
		return
	}

	log.Logger.Debug("Server connection established", "player", serverConn.IdentityData().DisplayName)

	if !arg.initializeConnection(conn, serverConn) {
		return
	}

	playerXuid := conn.IdentityData().XUID
	if playerXuid == "" {
		log.Logger.Error("Player XUID is empty, disconnecting player")
		arg.Listener.Disconnect(conn, "Failed to get your XUID, please try again!")
		return
	}

	player := player.GetPlayer(conn, serverConn)
	log.Logger.Info("Player joined", "name", player.GetName())
	player.SendXUIDToAddon()
	arg.UpdatePlayerDetails(player)

	arg.startPacketHandlers(player, conn, serverConn)
}

// canJoinServer checks if a player can join the server based on its status.
// It returns true if the player can join, and false if the player can't join.
// If false, the player will be disconnected with a message.
func (arg *Proxy) canJoinServer(status minecraft.ServerStatus, conn *minecraft.Conn, whitelisted bool) bool {
	if status.PlayerCount >= status.MaxPlayers-arg.Config.Server.SecuredSlots {
		if whitelisted && status.PlayerCount >= status.MaxPlayers {
			// Player is whitelisted, but all secured slots are taken too, so we can't let them in
			arg.Listener.Disconnect(conn, fmt.Sprintf("Sorry %s, even though you have priority access, all secured slots are taken! (%d/%d)", conn.IdentityData().DisplayName, status.PlayerCount, status.MaxPlayers))
			return false
		} else if !whitelisted && status.PlayerCount < status.MaxPlayers {
			// Player is not whitelisted, but the server is full to non whitelisted players.
			arg.Listener.Disconnect(conn, fmt.Sprintf("Sorry %s, even though the server is not full, the remaining slots are reserved for our staff! (%d/%d)", conn.IdentityData().DisplayName, status.PlayerCount, status.MaxPlayers))
			return false
		} else if !whitelisted {
			// Player is not whitelisted and the server is completely full.
			arg.Listener.Disconnect(conn, fmt.Sprintf("Sorry %s, the server is full, please try again later! (%d/%d)", conn.IdentityData().DisplayName, status.PlayerCount, status.MaxPlayers))
			return false
		}
		// Player is whitelisted and there are secured slots available, let them in
	}
	return true
}

// initializeConnection handles the initial setup for a new connection.
// It returns true if the connection was successfully established, and false if it wasn't.
// If false, the player will be disconnected with a message.
func (arg *Proxy) initializeConnection(conn *minecraft.Conn, serverConn *minecraft.Conn) bool {
	gameData := serverConn.GameData()
	gameData.WorldSeed = 0
	gameData.ClientSideGeneration = false
	arg.Worlds.SetItems(gameData.Items)
	arg.Worlds.SetCustomBlocks(gameData.CustomBlocks)

	var success = true
	var g sync.WaitGroup
	g.Add(2)
	go func() {
		if err := conn.StartGame(gameData); err != nil {
			var disc minecraft.DisconnectError
			if ok := errors.As(err, &disc); !ok {
				log.Logger.Error("Failed to start game on client", "error", err)
			}
			success = false
		}
		g.Done()
	}()
	go func() {
		if err := serverConn.DoSpawn(); err != nil {
			var disc minecraft.DisconnectError
			if ok := errors.As(err, &disc); !ok {
				log.Logger.Error("Failed to spawn client on server", "error", err)
			}
			success = false
		}
		g.Done()
	}()
	g.Wait()

	if !success {
		arg.Listener.Disconnect(conn, "Failed to establish a connection, please try again!")
		serverConn.Close()
		return false
	}

	return true
}

func (arg *Proxy) startPacketHandlers(player human.Human, conn *minecraft.Conn, serverConn *minecraft.Conn) {
	go func() { // client->proxy
		defer func() {
			reason := "Client Connection closed"
			if r := recover(); r != nil {
				log.Logger.Error("Recovered from panic in HandlePacket from Client", "error", r, "stack", debug.Stack())
				reason = "An internal error occurred"
			}

			// Order is Important. We want to close the client connection first
			arg.PrePlayerDisconnect(player)
			arg.Listener.Disconnect(conn, reason)
			serverConn.Close()
		}()

		for {
			select {
			case <-arg.ctx.Done():
				return
			default:
				pk, err := conn.ReadPacket()
				if err != nil {
					arg.handlePacketError(err, conn, "Failed to read packet from client")
					return
				}

				ok, pk, err := arg.Handlers.HandlePacket(pk, player, "Client")
				if err != nil {
					log.Logger.Error("Error handling packet from client", "error", err)
				}

				if ok {
					if err := serverConn.WritePacket(pk); err != nil {
						arg.handlePacketError(err, conn, "Failed to write packet to proxy")
						return
					}
				}
			}
		}
	}()

	go func() { // proxy->server
		defer func() {
			reason := "Server Connection closed"
			if r := recover(); r != nil {
				log.Logger.Error("Recovered from panic in HandlePacket from Server", "error", r, "stack", debug.Stack())
				reason = "An internal error occurred"
			}

			// Order is Important. We want to close the server connection first
			arg.PrePlayerDisconnect(player)
			serverConn.Close()
			arg.Listener.Disconnect(conn, reason)
		}()

		for {
			select {
			case <-arg.ctx.Done():
				return
			default:
				pk, err := serverConn.ReadPacket()
				if err != nil {
					arg.handlePacketError(err, conn, "Failed to read packet from proxy")
					return
				}

				ok, pk, err := arg.Handlers.HandlePacket(pk, player, "Server")
				if err != nil {
					log.Logger.Error("Error", "error", err)
				}

				if ok {
					if err := conn.WritePacket(pk); err != nil {
						arg.handlePacketError(err, conn, "Failed to write packet to server")
						return
					}
				}
			}
		}
	}()
}

// RegisterCleanupTask adds a function to be called during proxy shutdown
func (arg *Proxy) RegisterCleanupTask(task func()) {
	arg.tasksMu.Lock()
	defer arg.tasksMu.Unlock()
	arg.cleanupTasks = append(arg.cleanupTasks, task)
}

func (arg *Proxy) Shutdown() {
	log.Logger.Info("Shutting down proxy")

	// Run all cleanup tasks first
	arg.tasksMu.Lock()
	for _, task := range arg.cleanupTasks {
		task()
	}
	arg.tasksMu.Unlock()

	arg.cancel() // This will cancel the context and stop all goroutines
	if arg.Listener != nil {
		arg.Listener.Close() // Close the listener if it's open
	}
}

// handlePacketError handles an error that occurred while reading a packet.
func (arg *Proxy) handlePacketError(err error, conn *minecraft.Conn, msg string) {
	var disc minecraft.DisconnectError
	if ok := errors.As(err, &disc); ok {
		arg.Listener.Disconnect(conn, disc.Error())
	}
	if !strings.Contains(err.Error(), "use of closed network connection") && !strings.Contains(err.Error(), "disconnect.kicked.reason") {
		// Error is not a disconnect error, so log the error.
		log.Logger.Error("Error", "msg", msg, "error", err)
	}
}

// PrePlayerDisconnect handles last second checks before a player disconnects.
func (arg *Proxy) PrePlayerDisconnect(player human.Human) {
	// Send close container packet
	if player.IsBeingDisconnected() {
		return // Player is already being disconnected, ignore this call
	}
	player.SetDisconnected(true)

	openContainerId := player.GetData().OpenContainerWindowId
	itemInContainers := player.GetData().ItemsInContainers
	playerLastLocation := player.GetData().LastUpdatedLocation
	lastLocationString := fmt.Sprintf("[%d, %d, %d]", int(playerLastLocation.X()), int(playerLastLocation.Y()), int(playerLastLocation.Z()))

	if openContainerId != 0 && len(itemInContainers) > 0 {
		log.Logger.Info("Player disconnecting with open container", "name", player.GetName(), "container", openContainerId, "location", lastLocationString)

		// utils.SendStaffAlertToDiscord("Disconnect with open container!", "A Player Has disconnected with an open container, please investigate!", 16711680, []map[string]interface{}{
		// 	{
		// 		"name":   "Player Name",
		// 		"value":  "```" + player.GetName() + "```",
		// 		"inline": true,
		// 	},
		// 	{
		// 		"name":   "Player Location",
		// 		"value":  "```" + lastLocationString + "```",
		// 		"inline": true,
		// 	},
		// 	{
		// 		"name":   "Item Count",
		// 		"value":  "```" + fmt.Sprintf("%d", len(itemInContainers)) + "```",
		// 		"inline": true,
		// 	},
		// })

		// Send Item Stack Requests to clear the container
		// Send Item Request to clear container id 13 (crafting table)
		// By sending from slot 32->40 (9 crafting slots) to `false` (throw on ground)
		request := protocol.ItemStackRequest{
			RequestID: player.GetNextItemStackRequestID(),
			Actions:   []protocol.StackRequestAction{},
		}
		// Loop through players container slots
		for _, slotInfo := range itemInContainers {
			action := &protocol.DropStackRequestAction{}
			action.Source = slotInfo
			action.Count = 64
			action.Randomly = false
			request.Actions = append(request.Actions, action)
		}
		pk := &packet.ItemStackRequest{
			Requests: []protocol.ItemStackRequest{request},
		}
		log.Logger.Debug("Sending ItemStackRequest to clear container")
		player.DataPacketToServer(pk)

		player.SetOpenContainerWindowID(0)
		player.SetOpenContainerType(0)

		// Sleep for 2 seconds to allow the packets to be sent
		time.Sleep(time.Second * 4)
	}

	cursorItem := player.GetItemFromContainerSlot(protocol.ContainerCombinedHotBarAndInventory, 0)
	if cursorItem.StackNetworkID != 0 {
		// Player left with a item in ContainerCombinedHotBarAndInventory
		utils.SendStaffAlertToDiscord("Disconnecting With Item", "A Player Has disconnected with a item in ContainerCombinedHotBarAndInventory, please investigate!", 16711680, []map[string]interface{}{
			{
				"name":   "Player Name",
				"value":  "```" + player.GetName() + "```",
				"inline": true,
			},
			{
				"name":   "Stack Network ID",
				"value":  "```" + fmt.Sprintf("%d", cursorItem.StackNetworkID) + "```",
				"inline": true,
			},
			{
				"name":   "Player Location",
				"value":  "```" + lastLocationString + "```",
				"inline": true,
			},
		})
	}
}

type PlayerDetails struct {
	Xuid string `json:"xuid"`
	Name string `json:"name"`
	IP   string `json:"ip"`
}

func (arg *Proxy) UpdatePlayerDetails(player human.Human) {
	xuid := player.GetSession().IdentityData.XUID

	// Build the URI for the API request
	uri := arg.Config.Api.ApiHost + "/api/moderation/playerDetails"
	log.Logger.Info("Sending playerDetails", "name", player.GetName(), "uri", uri)

	// Create the player details payload
	playerDetails := PlayerDetails{
		Xuid: xuid,
		Name: player.GetName(),
		IP:   strings.Split(player.GetSession().Connection.ClientConn.RemoteAddr().String(), ":")[0],
	}

	// Convert player details to JSON
	jsonData, err := json.Marshal(playerDetails)
	if err != nil {
		log.Logger.Error("Failed to marshal player details", "error", err)
		return
	}

	// Create a new HTTP POST request
	req, err := http.NewRequest("POST", uri, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Logger.Error("Failed to create new request", "error", err)
		return
	}

	// Set the headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("authorization", arg.Config.Api.ApiKey)

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Logger.Error("Failed to send request", "error", err)
		return
	}
	defer resp.Body.Close()

	// Log the response status
	log.Logger.Info("Sent playerDetails", "uri", uri, "status", resp.StatusCode)
}
