package custom_handlers

import (
	"github.com/HyPE-Network/vanilla-proxy/lang"
	"github.com/HyPE-Network/vanilla-proxy/log"
	"github.com/HyPE-Network/vanilla-proxy/proxy"
	"github.com/HyPE-Network/vanilla-proxy/proxy/player/human"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func ReadLangs() {
	lang.GetLangsFromPackConfig(proxy.ProxyInstance.Config)
}

type NameTagTranslateHandlerAddEntity struct{}

func (NameTagTranslateHandlerAddEntity) Handle(pk packet.Packet, player human.Human) (bool, packet.Packet, error) {
	dataPacket := pk.(*packet.AddActor)

	dataPacket.ActorData[protocol.EntityDataKeyName] = translate(dataPacket.ActorData[protocol.EntityDataKeyName], player)

	return true, dataPacket, nil
}

type NameTagTranslateHandlerUpdateEntity struct{}

func (NameTagTranslateHandlerUpdateEntity) Handle(pk packet.Packet, player human.Human) (bool, packet.Packet, error) {
	dataPacket := pk.(*packet.SetActorData)

	dataPacket.EntityMetadata[protocol.EntityDataKeyName] = translate(dataPacket.EntityMetadata[protocol.EntityDataKeyName], player)

	return true, dataPacket, nil
}

func translate(name any, player human.Human) any {
	nameTag, ok := name.(string)
	if ok && nameTag != "" {
		translated := lang.Translate(nameTag, player.GetSession().ClientData.LanguageCode)
		log.Logger.Debugln("Name: ", name, "Translated:", translated)
		return translated
	}

	return name
}
