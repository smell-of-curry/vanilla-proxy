package handlers

import (
	"github.com/HyPE-Network/vanilla-proxy/proxy"
	"github.com/HyPE-Network/vanilla-proxy/proxy/block/cube"
	"github.com/HyPE-Network/vanilla-proxy/proxy/player/human"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type InventoryTransactionHandler struct {
}

func (InventoryTransactionHandler) Handle(pk packet.Packet, player human.Human) (bool, packet.Packet, error) {
	dataPacket := pk.(*packet.InventoryTransaction)

	if len(dataPacket.Actions) > 0 && dataPacket.Actions[0].SourceType == 99999 { // cheats
		return false, pk, nil
	}

	switch td := dataPacket.TransactionData.(type) {
	case *protocol.UseItemTransactionData:
		if td.ActionType == protocol.UseItemActionClickBlock {
			pos := cube.Side(td.BlockPosition, td.BlockFace)

			if !proxy.ProxyInstance.Worlds.Border.IsXZInside(pos.X(), pos.Z()) {
				player.SendMessage("§cActions outside the world are prohibited!")
				return false, pk, nil
			}
		}
	}

	return true, pk, nil
}
