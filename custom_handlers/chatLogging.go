package custom_handlers

import (
	"fmt"
	"strings"

	"github.com/HyPE-Network/vanilla-proxy/proxy"
	"github.com/HyPE-Network/vanilla-proxy/proxy/player/human"
	"github.com/HyPE-Network/vanilla-proxy/utils"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type ChatLoggingHandler struct{}

func (h ChatLoggingHandler) Handle(pk packet.Packet, player human.Human) (bool, packet.Packet, error) {
	dataPacket := pk.(*packet.Text)

	// Only care if player is sending message
	if dataPacket.TextType != packet.TextTypeChat {
		return true, pk, nil
	}

	// Check if its a packet from the client
	if dataPacket.SourceName != player.GetName() {
		return true, pk, nil
	}

	// Ignore commands
	if strings.HasPrefix(dataPacket.Message, "-") || strings.HasPrefix(dataPacket.Message, "/") {
		return true, pk, nil
	}

	avatar_url, err := utils.GetXboxIconLink(player.GetSession().IdentityData.XUID, proxy.ProxyInstance.Config.Api.XboxApiKey)
	if err != nil {
		avatar_url = "https://media.forgecdn.net/avatars/121/268/636409261203329160.png"
	}

	// Log message to discord.
	utils.SendJsonToDiscord(proxy.ProxyInstance.Config.Logging.DiscordChatLogsWebhook, map[string]any{
		"username":   fmt.Sprintf("[%s] %s", proxy.ProxyInstance.Config.Server.Prefix, player.GetName()),
		"avatar_url": avatar_url,
		"content":    dataPacket.Message,
		"allowed_mentions": map[string]any{
			"parse": []string{},
		},
	})

	return true, pk, nil
}
