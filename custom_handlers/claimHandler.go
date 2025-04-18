package custom_handlers

import (
	"strconv"

	"github.com/HyPE-Network/vanilla-proxy/log"
	"github.com/HyPE-Network/vanilla-proxy/proxy"
	"github.com/HyPE-Network/vanilla-proxy/proxy/player/human"
	"github.com/HyPE-Network/vanilla-proxy/utils"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type VectorXZ struct {
	X float32 `json:"x"`
	Z float32 `json:"z"`
}

type Location struct {
	Dimension string   `json:"dimension"`
	Pos1      VectorXZ `json:"pos1"`
	Pos2      VectorXZ `json:"pos2"`
}

type IPlayerClaim struct {
	ClaimId    string   `json:"claimId"`
	PlayerXUID string   `json:"playerXUID"`
	Location   Location `json:"location"`
	Trusts     []string `json:"trusts"`
}

var RegisteredClaims (map[string]IPlayerClaim)

func FetchClaims() error {
	claims, err := utils.FetchDatabase[IPlayerClaim]("claims")
	if err != nil {
		return err
	}

	RegisteredClaims = claims
	log.Logger.Info("Loaded claims from database", "count", len(RegisteredClaims))

	return nil
}

// Dimension is the ID of the dimension that the player spawns in. It is a value from 0-2, with 0 being
// the overworld, 1 being the nether and 2 being the end.
func ClaimDimensionToInt(dimension string) int32 {
	if dimension == "minecraft:overworld" {
		return 0
	} else if dimension == "minecraft:nether" {
		return 1
	} else if dimension == "minecraft:end" {
		return 2
	} else {
		return -1
	}
}

// PlayerInsideClaim checks if a player is inside a claim
func PlayerInsideClaim(playerData minecraft.GameData, claim IPlayerClaim) bool {
	dimensionInt := ClaimDimensionToInt(claim.Location.Dimension)
	if dimensionInt != playerData.Dimension {
		return false
	}
	Pos1X, Pos1Z := float32(claim.Location.Pos1.X), float32(claim.Location.Pos1.Z)
	Pos2X, Pos2Z := float32(claim.Location.Pos2.X), float32(claim.Location.Pos2.Z)

	if playerData.PlayerPosition.X() >= Pos1X && playerData.PlayerPosition.X() <= Pos2X {
		if playerData.PlayerPosition.Z() >= Pos1Z && playerData.PlayerPosition.Z() <= Pos2Z {
			return true
		}
	}

	return false
}

func getClaimAt(dimension int32, x, z int32) IPlayerClaim {
	for _, claim := range RegisteredClaims {
		if ClaimDimensionToInt(claim.Location.Dimension) == dimension {
			Pos1X, Pos1Z := int32(claim.Location.Pos1.X), int32(claim.Location.Pos1.Z)
			Pos2X, Pos2Z := int32(claim.Location.Pos2.X), int32(claim.Location.Pos2.Z)

			if x >= Pos1X && x <= Pos2X {
				if z >= Pos1Z && z <= Pos2Z {
					return claim
				}
			}
		}
	}

	return IPlayerClaim{}
}

func canPreformActionInClaim(player human.Human, claim IPlayerClaim, action string) bool {
	// if player.GetData().GameData.PlayerPermissions == 2 {
	// 	return true
	// }

	if action == "interactWithBlock" && claim.PlayerXUID == "*" {
		// Players can interact with blocks in admin claims
		return true
	}

	if action == "interactWithEntity" && claim.PlayerXUID == "*" {
		// Players can interact with entities in admin claims
		return true
	}

	playerXuid := player.GetSession().IdentityData.XUID

	if claim.PlayerXUID == playerXuid || utils.StringInSlice(playerXuid, claim.Trusts) {
		return true
	}

	return false
}

type ClaimPlayerAuthInputHandler struct{}

func (ClaimPlayerAuthInputHandler) Handle(pk packet.Packet, player human.Human) (bool, packet.Packet, error) {
	dataPacket := pk.(*packet.PlayerAuthInput)

	if dataPacket.Tick%20 == 0 {
		playerPing := player.GetPing()
		formattedPing := strconv.FormatUint(uint64(playerPing), 10)
		pingStatus := "§a"
		if playerPing < 20 {
			pingStatus = "§a"
		} else if playerPing < 50 {
			pingStatus = "§e"
		} else if playerPing < 100 {
			pingStatus = "§6"
		} else if playerPing < 200 {
			pingStatus = "§c"
		} else {
			pingStatus = "§4"
		}
		titlePk := &packet.SetTitle{
			ActionType: packet.TitleActionSetTitle,
			Text:       "&_playerPing:Current Ping: " + pingStatus + formattedPing,
		}
		player.DataPacket(titlePk)
	}

	// Loop through block actions, and check if player can interact with block
	for _, blockAction := range dataPacket.BlockActions {
		claim := getClaimAt(player.GetData().GameData.Dimension, int32(blockAction.BlockPos.X()), int32(blockAction.BlockPos.Z()))
		if claim.ClaimId == "" {
			continue
		}
		actionName := "interactWithBlock"
		if blockAction.Action == 1 {
			actionName = "breakBlock"
		}
		if !canPreformActionInClaim(player, claim, actionName) {
			return false, pk, nil
		}
	}

	return true, pk, nil
}

type ClaimInventoryTransactionHandler struct {
}

func (ClaimInventoryTransactionHandler) Handle(pk packet.Packet, player human.Human) (bool, packet.Packet, error) {
	dataPacket := pk.(*packet.InventoryTransaction)

	playerData := player.GetData().GameData

	switch td := dataPacket.TransactionData.(type) {
	case *protocol.UseItemTransactionData:
		// Can stop players from using items like GUI, Pokedex, Potions, etc
		// So use this to stop players from throwing projectiles
		// This cannot stop interactions with entities or blocks.

		if td.HeldItem.Stack.ItemType.NetworkID == 0 {
			// Using hand
			return true, pk, nil
		}

		claim := getClaimAt(player.GetData().GameData.Dimension, int32(td.Position.X()), int32(td.Position.Z()))
		if claim.ClaimId == "" {
			return true, pk, nil
		}
		if canPreformActionInClaim(player, claim, "useItem") {
			return true, pk, nil
		}

		item := proxy.ProxyInstance.Worlds.GetItemEntry(td.HeldItem.Stack.ItemType.NetworkID)
		if item == nil {
			// Item not sent over, most likely a minecraft item
			return true, pk, nil
		}
		itemComponents := proxy.ProxyInstance.Worlds.GetItemComponentEntry(item.Name)
		if itemComponents == nil {
			// Item does not have any components, most-likely a minecraft item
			return true, pk, nil
		}

		if components, ok := itemComponents.Data["components"].(map[string]any); ok {
			if _, ok := components["minecraft:throwable"]; ok {
				// Item is throwable, stop the player from using it
				player.SendMessage("§cYou cannot use throwable items in this claim!")
				player.PlaySound("note.bass", playerData.PlayerPosition, 1, 1)
				return false, pk, nil
			}
		}
	}

	return true, pk, nil
}
