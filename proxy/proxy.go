package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/HyPE-Network/vanilla-proxy/handler"
	"github.com/HyPE-Network/vanilla-proxy/log"
	"github.com/HyPE-Network/vanilla-proxy/math"
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
	Config            utils.Config
	Handlers          handler.HandlerManager
	Listener          *minecraft.Listener
	WhitelistManager  *whitelist.WhitelistManager
	PlayerListManager *playerlist.PlayerlistManager
	ResourcePacks     []*resource.Pack
	ctx               context.Context
	cancel            context.CancelFunc
}

func New(config utils.Config) *Proxy {
	playerListManager, err := playerlist.Init()
	if err != nil {
		log.Logger.Fatalln("Error in initializing playerListManager: ", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	Proxy := &Proxy{
		Config:            config,
		PlayerListManager: playerListManager,
		WhitelistManager:  whitelist.Init(),
		ctx:               ctx,
		cancel:            cancel,
	}

	// Initialize an empty slice of *resource.Pack
	var resourcePacks []*resource.Pack

	// Loop through all the pack URLs and append each pack to the slice
	for _, url := range Proxy.Config.Resources.PackURLs {
		resourcePack, err := resource.ReadURL(url)
		if err != nil {
			log.Logger.Errorln("Failed to read resource pack from URL:", url, err)
		}
		resourcePacks = append(resourcePacks, resourcePack)
	}

	// Loop through all the pack paths and append each pack to the slice
	for _, path := range Proxy.Config.Resources.PackPaths {
		resourcePack, err := resource.ReadPath(path)
		if err != nil {
			log.Logger.Errorln("Failed to read resource pack from path:", path, err)
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
		log.Logger.Warnln("Failed to ping server, retrying in 5 seconds:", err)
		time.Sleep(time.Second * 5)
		arg.Start(h)
		return nil
	}
	// Server is online, parse data
	status := minecraft.ParsePongData(res)
	log.Logger.Infoln("Server", status.ServerName, "is online with MOTD", status.ServerSubName)
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
	}.Listen("raknet", arg.Config.Connection.ProxyAddress)

	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	log.Logger.Debugln("Original server address:", arg.Config.Connection.RemoteAddress, "public address:", arg.Config.Connection.ProxyAddress)
	log.Logger.Println("Proxy has been started on Version", protocol.CurrentVersion, "protocol", protocol.CurrentProtocol)
	arg.Handlers = h

	defer func() {
		if r := recover(); r != nil {
			log.Logger.Errorf("Recovered from panic in Handling Listener: %v", r)
		}
		log.Logger.Errorf("Closing listener: %v", arg.Listener.Close())
	}()
	for {
		select {
		case <-arg.ctx.Done():
			log.Logger.Infoln("Proxy shutting down")
			return nil
		default:
			c, err := arg.Listener.Accept()
			if err != nil {
				if arg.ctx.Err() != nil {
					return nil // Exit if context is cancelled
				}

				// The listener closed, so we should restart it. c==nil
				log.Logger.Errorf("Listener accept error: %v", err)
				utils.SendStaffAlertToDiscord("Proxy Listener Closed", "```"+err.Error()+"```", 16711680, []map[string]interface{}{})

				time.Sleep(time.Second * 5) // Wait 5 seconds before restarting the listener
				return arg.Start(h)
			}
			log.Logger.Debugln("New connection from", c.RemoteAddr())
			go arg.handleConn(c.(*minecraft.Conn))
		}
	}
}

// handleConn handles a new incoming minecraft.Conn from the minecraft.Listener passed.
func (arg *Proxy) handleConn(conn *minecraft.Conn) {
	defer func() {
		if r := recover(); r != nil {
			log.Logger.Errorf("Recovered from panic in handleConn: %v", r)
			arg.Listener.Disconnect(conn, "An internal error occurred")
		}
	}()

	go func() {
		<-arg.ctx.Done()
		conn.Close() // Ensure the connection is closed when context is cancelled
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
		log.Logger.Errorln("Error in getting clientData: ", err)
		arg.Listener.Disconnect(conn, strings.Split(err.Error(), ": ")[1])
		return
	}
	identityData, err := arg.PlayerListManager.GetConnIdentityData(conn)
	if err != nil {
		log.Logger.Errorln("Error in getting identityData: ", err)
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
		arg.Listener.Disconnect(conn, strings.Split(err.Error(), ": ")[1])
		return
	}

	log.Logger.Debugln("Server connection established for", serverConn.IdentityData().DisplayName)

	if !arg.initializeConnection(conn, serverConn) {
		return
	}

	player := player.GetPlayer(conn, serverConn)
	log.Logger.Infoln(player.GetName(), "joined the server")
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
			log.Logger.Errorln("Failed to start game on client:", err)
			success = false
		}
		g.Done()
	}()
	go func() {
		if err := serverConn.DoSpawn(); err != nil {
			log.Logger.Errorln("Failed to spawn on server:", err)
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
			if r := recover(); r != nil {
				log.Logger.Errorf("Recovered from panic in HandlePacket from Client: %v", r)
				arg.DisconnectPlayer(player, "An internal error occurred")
			} else {
				arg.DisconnectPlayer(player, "Client Connection closed")
			}
		}()
		for {
			select {
			case <-arg.ctx.Done():
				return
			default:
				pk, err := conn.ReadPacket()
				if err != nil {
					arg.handlePacketError(err, player, "Failed to read packet from client")
					return
				}

			ok, pk, err := arg.Handlers.HandlePacket(pk, player, "Client")
			if err != nil {
				log.Logger.Errorln("Error handling packet from client", err)
			}

			if ok {
				if err := serverConn.WritePacket(pk); err != nil {
					if !arg.handlePacketError(err, player, "Failed to write packet to proxy") {
						return
					}
				}
			}
		}
	}()

	go func() { // proxy->server
		defer func() {
			if r := recover(); r != nil {
				log.Logger.Errorf("Recovered from panic in HandlePacket from Server: %v", r)
				arg.DisconnectPlayer(player, "An internal error occurred")
			}
			arg.DisconnectPlayer(player, "Server Connection closed")
		}()
		for {
			select {
			case <-arg.ctx.Done():
				return
			default:
				pk, err := serverConn.ReadPacket()
				if err != nil {
					arg.handlePacketError(err, player, "Failed to read packet from proxy")
					return
				}

			ok, pk, err := arg.Handlers.HandlePacket(pk, player, "Server")
			if err != nil {
				log.Logger.Errorln(err)
			}

			if ok {
				if err := conn.WritePacket(pk); err != nil {
					if !arg.handlePacketError(err, player, "Failed to write packet to server") {
						return
					}
					continue
				}
			}
		}
	}()
}

func (arg *Proxy) Shutdown() {
	log.Logger.Infoln("Shutting down proxy")
	arg.cancel() // This will cancel the context and stop all goroutines
	if arg.Listener != nil {
		arg.Listener.Close() // Close the listener if it's open
	}
}

// handlePacketError handles an error that occurred while reading a packet.
// It returns true if the player was disconnected, and false if it wasn't.
func (arg *Proxy) handlePacketError(err error, player human.Human, msg string) bool {
	var disc minecraft.DisconnectError
	if ok := errors.As(err, &disc); ok {
		arg.DisconnectPlayer(player, disc.Error())
		return false
	}
	if !strings.Contains(err.Error(), "use of closed network connection") {
		// Error is not a disconnect error, so log the error.
		log.Logger.Errorln(msg, err)
	}
	return true
}

// DisconnectPlayer disconnects a player from the proxy.
func (arg *Proxy) DisconnectPlayer(player human.Human, message string) {
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
		log.Logger.Println(player.GetName(), "has open container:", openContainerId, "while disconnecting, *prob trying to dupe*", lastLocationString)

		utils.SendStaffAlertToDiscord("Disconnect with open container!", "A Player Has disconnected with an open container, please investigate!", 16711680, []map[string]interface{}{
			{
				"name":   "Player Name",
				"value":  "```" + player.GetName() + "```",
				"inline": true,
			},
			{
				"name":   "Player Location",
				"value":  "```" + lastLocationString + "```",
				"inline": true,
			},
			{
				"name":   "Item Count",
				"value":  "```" + fmt.Sprintf("%d", len(itemInContainers)) + "```",
				"inline": true,
			},
		})

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
		log.Logger.Debugln("Sending ItemStackRequest to clear container:")
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
	log.Logger.Debugln("Disconnecting player:", player.GetName(), "with reason:", message)

	// Disconnect
	player.GetSession().Connection.ServerConn.Close()
	arg.Listener.Disconnect(player.GetSession().Connection.ClientConn, message)
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
	log.Logger.Printf("Sending \"%s\" playerDetails to: \"%s\"\n", player.GetName(), uri)

	// Create the player details payload
	playerDetails := PlayerDetails{
		Xuid: xuid,
		Name: player.GetName(),
		IP:   strings.Split(player.GetSession().Connection.ClientConn.RemoteAddr().String(), ":")[0],
	}

	// Convert player details to JSON
	jsonData, err := json.Marshal(playerDetails)
	if err != nil {
		log.Logger.Errorln("Failed to marshal player details:", err)
		return
	}

	// Create a new HTTP POST request
	req, err := http.NewRequest("POST", uri, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Logger.Errorln("Failed to create new request:", err)
		return
	}

	// Set the headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("authorization", arg.Config.Api.ApiKey)

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Logger.Errorln("Failed to send request:", err)
		return
	}
	defer resp.Body.Close()

	// Log the response status
	log.Logger.Printf("Sent playerDetails to: \"%s\", status: %d\n", uri, resp.StatusCode)
}
