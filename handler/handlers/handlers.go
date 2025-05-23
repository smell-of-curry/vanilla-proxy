package handlers

import (
	"fmt"
	"slices"

	"github.com/HyPE-Network/vanilla-proxy/handler"
	"github.com/HyPE-Network/vanilla-proxy/log"
	"github.com/HyPE-Network/vanilla-proxy/proxy"
	"github.com/HyPE-Network/vanilla-proxy/proxy/player/human"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// TODO: DISABLE DEBUG BEFORE PRODUCTION RELEASE
var debug = disabled

var target = []uint32{
	packet.IDAddActor,
}

var ignored = []uint32{
	packet.IDAnimate,
	packet.IDSetActorData,
	packet.IDMoveActorDelta,
	packet.IDCreativeContent,
	packet.IDCraftingData,
	packet.IDBiomeDefinitionList,
	packet.IDPlayerList,
	packet.IDItemRegistry,
	packet.IDLevelEvent,
	packet.IDSetActorMotion,
	packet.IDUpdateAttributes,
	packet.IDPlayerAuthInput,
	packet.IDLevelChunk,
	packet.IDSubChunk,
	packet.IDSubChunkRequest,
}

type handlerManager struct {
	PacketHandlers map[uint32][]handler.PacketHandler
}

func New() handlerManager {
	return handlerManager{PacketHandlers: registerHandlers()}
}

func registerHandlers() map[uint32][]handler.PacketHandler {
	var handlers = make(map[uint32][]handler.PacketHandler)

	handlers[packet.IDSubChunk] = []handler.PacketHandler{SubChunkHandler{}}

	if proxy.ProxyInstance.Worlds != nil {
		handlers[packet.IDSubChunkRequest] = []handler.PacketHandler{SubChunkRequestHandler{}}
		handlers[packet.IDSubChunk] = append(handlers[packet.IDSubChunk], SubChunkHandlerBoarder{})
		handlers[packet.IDLevelChunk] = []handler.PacketHandler{LevelChunkHandler{}}
		handlers[packet.IDInventoryTransaction] = []handler.PacketHandler{InventoryTransactionHandler{}}
	}

	// handlers[packet.IDModalFormResponse] = []handler.PacketHandler{ModalFormResponseHandler{}}
	handlers[packet.IDPlayerAuthInput] = []handler.PacketHandler{PlayerInputHandler{}}

	handlers[packet.IDChunkRadiusUpdated] = []handler.PacketHandler{UpdateRadiusHandler{proxy.ProxyInstance.Config.Server.ViewDistance}}
	handlers[packet.IDRequestChunkRadius] = []handler.PacketHandler{RequestRadiusHandler{proxy.ProxyInstance.Config.Server.ViewDistance}}

	// handlers[packet.IDCommandRequest] = []handler.PacketHandler{CommandRequestHandler{}}
	// handlers[packet.IDAvailableCommands] = []handler.PacketHandler{AvailableCommandsHandler{}}

	handlers[packet.IDPacketViolationWarning] = []handler.PacketHandler{MalformedHandler{}}

	handlers[packet.IDAddActor] = []handler.PacketHandler{AddActorHandler{}}
	handlers[packet.IDRemoveActor] = []handler.PacketHandler{RemoveActorHandler{}}

	return handlers
}

func (hm *handlerManager) RegisterHandler(id int, packetHandler handler.PacketHandler) {
	_, ok := hm.PacketHandlers[uint32(id)]
	if ok {
		hm.PacketHandlers[uint32(id)] = append(hm.PacketHandlers[uint32(id)], packetHandler)
	} else {
		hm.PacketHandlers[uint32(id)] = []handler.PacketHandler{packetHandler}
	}
}

func (hm handlerManager) HandlePacket(pk packet.Packet, player human.Human, sender string) (bool, packet.Packet, error) {
	var err error
	var packetHandlers []handler.PacketHandler
	var sendPacket = true // is packet will be sent to original (true by default, may be switched by handlers)

	if debug != disabled {
		sendDebug(pk, sender)
	}

	if hm.PacketHandlers == nil {
		return false, pk, fmt.Errorf("packet handlers map is nil")
	}

	packetHandlers, hasHandler := hm.PacketHandlers[pk.ID()]
	if hasHandler {
		for _, packetHandler := range packetHandlers {
			if sendPacket {
				sendPacket, pk, err = packetHandler.Handle(pk, player)
			} else {
				_, pk, err = packetHandler.Handle(pk, player)
			}
			if err != nil {
				return false, pk, err
			}
		}
	}

	return sendPacket, pk, err
}

func sendDebug(pk packet.Packet, sender string) {
	switch debug {
	case debugLevelAll:
		log.Logger.Debug("Packet debug", "sender", sender, "id", pk.ID(), "packet", pk)

	case debugLevelNotIgnored:
		if !slices.Contains(ignored, pk.ID()) {
			log.Logger.Debug("Packet debug", "sender", sender, "id", pk.ID(), "packet", pk)
		}

	case debugLevelTarget:
		if slices.Contains(target, pk.ID()) {
			log.Logger.Debug("Packet debug", "sender", sender, "id", pk.ID(), "packet", pk)
		}
	}
}

const (
	disabled = iota
	debugLevelAll
	debugLevelNotIgnored
	debugLevelTarget
)
