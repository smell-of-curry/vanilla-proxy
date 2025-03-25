package custom_handlers

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/HyPE-Network/vanilla-proxy/log"
	"github.com/HyPE-Network/vanilla-proxy/proxy"
	"github.com/HyPE-Network/vanilla-proxy/proxy/command"
	"github.com/HyPE-Network/vanilla-proxy/proxy/player/human"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// --- New enum definition for command types ---

type EngineResponseCommandType string

const (
	EngineResponseCommandTypeLiteral    EngineResponseCommandType = "literal"
	EngineResponseCommandTypeString     EngineResponseCommandType = "string"
	EngineResponseCommandTypeInt        EngineResponseCommandType = "int"
	EngineResponseCommandTypeFloat      EngineResponseCommandType = "float"
	EngineResponseCommandTypeLocation   EngineResponseCommandType = "location"
	EngineResponseCommandTypeBoolean    EngineResponseCommandType = "boolean"
	EngineResponseCommandTypePlayer     EngineResponseCommandType = "player"
	EngineResponseCommandTypeTarget     EngineResponseCommandType = "target"
	EngineResponseCommandTypeArray      EngineResponseCommandType = "array"
	EngineResponseCommandTypeDuration   EngineResponseCommandType = "duration"
	EngineResponseCommandTypePlayerName EngineResponseCommandType = "playerName"
)

// enumIndexShift: we use 0 here (i.e. no bit shifting) so that the enum index is ORed directly.
const enumIndexShift = 0

// getDerivationSuffix returns the expected suffix for literal/array derivation rule keys.
// For most commands, "f0" is used; if a particular command requires a special suffix (for example, "schedule"),
// adjust this function.
func getDerivationSuffix(name string) string {
	lower := strings.ToLower(name)
	if lower == "schedule" {
		return "f2"
	}
	return "f0"
}

// lowerOptions forces all option strings to lowercase.
func lowerOptions(options []string) []string {
	result := make([]string, len(options))
	for i, o := range options {
		result[i] = strings.ToLower(o)
	}
	return result
}

// --- Update IEngineResponseCommand to use the new enum type ---

type IEngineResponseCommand struct {
	BaseCommand       string                        `json:"baseCommand"`
	Name              string                        `json:"name"`
	Description       string                        `json:"description"`
	Aliases           []string                      `json:"aliases,omitempty"`
	Type              EngineResponseCommandType     `json:"type"`
	AllowedTypeValues []string                      `json:"allowedTypeValues,omitempty"`
	Children          []IEngineResponseCommandChild `json:"children"`
	CanBeCalled       bool                          `json:"canBeCalled"`
	RequiresOp        bool                          `json:"requiresOp"`
}

type IEngineResponseCommandChild struct {
	IEngineResponseCommand
	Parent string `json:"parent"`
	Depth  int    `json:"depth"`
}

type IMinecraftRawText struct {
	Text string `json:"text"`
}

type IMinecraftTextMessage struct {
	RawText []IMinecraftRawText `json:"rawtext"`
}

type commandEnum struct {
	// Type is the derivation rule key for this parameter (must be lowercase).
	Type    string
	Options []string
	Dynamic bool
}

// valueToParamType maps a parameter's type to a protocol.CommandParameter base type plus optional enum data.
// For literal, boolean, and array types we return 0 and build a derivation rule key.
// For array, we now incorporate the parent's name if available so that the key is unique.
func valueToParamType(child IEngineResponseCommandChild) (t uint32, en commandEnum) {
	switch child.Type {
	case EngineResponseCommandTypeLiteral:
		lit := strings.ToLower(child.Name)
		return 0, commandEnum{
			Type:    lit + "_" + getDerivationSuffix(child.Name),
			Options: []string{lit},
		}
	case EngineResponseCommandTypeString:
		return protocol.CommandArgTypeString, en
	case EngineResponseCommandTypeInt:
		return protocol.CommandArgTypeInt, en
	case EngineResponseCommandTypeFloat:
		return protocol.CommandArgTypeFloat, en
	case EngineResponseCommandTypeLocation:
		return protocol.CommandArgTypePosition, en
	case EngineResponseCommandTypeBoolean:
		return 0, commandEnum{
			Type:    "bool",
			Options: []string{"true", "false"},
		}
	case EngineResponseCommandTypePlayer:
		return protocol.CommandArgTypeTarget, en
	case EngineResponseCommandTypeTarget:
		return protocol.CommandArgTypeTarget, en
	case EngineResponseCommandTypeArray:
		var baseKey string
		// If a Parent is provided, use it to differentiate this array parameter.
		if child.Parent != "" {
			baseKey = strings.ToLower(child.Parent) + "_" + strings.ToLower(child.Name)
		} else {
			baseKey = strings.ToLower(child.Name)
		}
		return 0, commandEnum{
			Type:    baseKey + "_" + getDerivationSuffix(child.Name),
			Options: lowerOptions(child.AllowedTypeValues),
		}
	case EngineResponseCommandTypeDuration:
		return protocol.CommandArgTypeString, en
	case EngineResponseCommandTypePlayerName:
		return protocol.CommandArgTypeString, en
	default:
		return protocol.CommandArgTypeString, en
	}
}

// flatParam represents a single token (literal or argument) in the command.
type flatParam struct {
	name string // e.g. "set", "level", "pokemontype"
	t    uint32 // base type (with CommandArgValid flag)
	en   commandEnum
}

// flattenBranches recursively traverses the command tree and returns a slice of branches.
// Each branch is a slice of flatParam representing one complete overload.
// In this function, if a token's name ends with "_y*" or "_z*", we skip it so that composite
// location arguments (like pos1, pos1_y*, pos1_z*) are not split.
func flattenBranches(prefix []flatParam, children []IEngineResponseCommandChild) [][]flatParam {
	var branches [][]flatParam
	if len(children) == 0 {
		return [][]flatParam{prefix}
	}
	for _, child := range children {
		lowerName := strings.ToLower(child.Name)
		// Skip tokens that are extra coordinate parts.
		if strings.HasSuffix(lowerName, "_y*") || strings.HasSuffix(lowerName, "_z*") {
			continue
		}
		t, en := valueToParamType(child)
		fp := flatParam{
			name: strings.ToLower(child.Name),
			t:    t | protocol.CommandArgValid,
			en:   en,
		}
		newPrefix := append(append([]flatParam{}, prefix...), fp)
		if len(child.Children) > 0 {
			childBranches := flattenBranches(newPrefix, child.Children)
			branches = append(branches, childBranches...)
		} else {
			branches = append(branches, newPrefix)
		}
	}
	return branches
}

// formatAvailableCommands builds the AvailableCommands packet from the command definitions.
// It creates one overload per branch in the command tree.
func formatAvailableCommands(commands map[string]IEngineResponseCommand) packet.AvailableCommands {
	pk := &packet.AvailableCommands{}
	var enums []commandEnum
	enumIndices := make(map[string]uint32)
	var dynamicEnums []commandEnum
	dynamicEnumIndices := make(map[string]uint32)

	for alias, c := range commands {
		if c.Name != alias {
			continue
		}
		branches := flattenBranches([]flatParam{}, c.Children)
		var overloads []protocol.CommandOverload
		for _, branch := range branches {
			overload := protocol.CommandOverload{}
			for _, fp := range branch {
				t := fp.t
				if len(fp.en.Options) > 0 || fp.en.Type != "" {
					key := strings.ToLower(fp.en.Type)
					if !fp.en.Dynamic {
						index, ok := enumIndices[key]
						if !ok {
							index = uint32(len(enums))
							enumIndices[key] = index
							enums = append(enums, commandEnum{
								Type:    key,
								Options: lowerOptions(fp.en.Options),
								Dynamic: fp.en.Dynamic,
							})
						}
						t |= protocol.CommandArgEnum | (index << enumIndexShift)
					} else {
						index, ok := dynamicEnumIndices[key]
						if !ok {
							index = uint32(len(dynamicEnums))
							dynamicEnumIndices[key] = index
							dynamicEnums = append(dynamicEnums, commandEnum{
								Type:    key,
								Options: lowerOptions(fp.en.Options),
								Dynamic: fp.en.Dynamic,
							})
						}
						t |= protocol.CommandArgSoftEnum | (index << enumIndexShift)
					}
				}
				overload.Parameters = append(overload.Parameters, protocol.CommandParameter{
					Name:     fp.name,
					Type:     t,
					Optional: false,
					Options:  0,
				})
			}
			overloads = append(overloads, overload)
		}
		pk.Commands = append(pk.Commands, protocol.Command{
			Name:          strings.ToLower(c.Name),
			Description:   c.Description,
			AliasesOffset: uint32(math.MaxUint32),
			Overloads:     overloads,
		})
	}
	pk.DynamicEnums = make([]protocol.DynamicEnum, 0, len(dynamicEnums))
	for _, e := range dynamicEnums {
		pk.DynamicEnums = append(pk.DynamicEnums, protocol.DynamicEnum{Type: strings.ToLower(e.Type), Values: e.Options})
	}
	enumValueIndices := make(map[string]uint32, len(enums)*3)
	pk.EnumValues = make([]string, 0, len(enumValueIndices))
	pk.Enums = make([]protocol.CommandEnum, 0, len(enums))
	for _, enum := range enums {
		protoEnum := protocol.CommandEnum{Type: enum.Type}
		for _, opt := range enum.Options {
			lOpt := strings.ToLower(opt)
			index, ok := enumValueIndices[lOpt]
			if !ok {
				index = uint32(len(pk.EnumValues))
				enumValueIndices[lOpt] = index
				pk.EnumValues = append(pk.EnumValues, lOpt)
			}
			protoEnum.ValueIndices = append(protoEnum.ValueIndices, uint(index))
		}
		pk.Enums = append(pk.Enums, protoEnum)
	}
	return *pk
}

type CustomCommandRegisterHandler struct {
}

func (CustomCommandRegisterHandler) Handle(pk packet.Packet, player human.Human) (bool, packet.Packet, error) {
	dataPacket := pk.(*packet.Text)
	if dataPacket.TextType != packet.TextTypeObject {
		return true, dataPacket, nil
	}
	var messageData IMinecraftTextMessage
	if err := json.Unmarshal([]byte(dataPacket.Message), &messageData); err != nil {
		log.Logger.Error("Failed to parse message JSON", "error", err)
		return false, dataPacket, nil
	}
	message := messageData.RawText[0].Text
	if !strings.HasPrefix(message, "[PROXY_SYSTEM][COMMANDS]=") {
		return true, dataPacket, nil
	}
	commandsRaw := strings.TrimPrefix(message, "[PROXY_SYSTEM][COMMANDS]=")
	var commands map[string]IEngineResponseCommand
	if err := json.Unmarshal([]byte(commandsRaw), &commands); err != nil {
		log.Logger.Info("Failed to unmarshal commands", "error", err)
		return false, dataPacket, err
	}
	availableCommands := formatAvailableCommands(commands)
	bdsSentCommands := proxy.ProxyInstance.Worlds.BDSAvailableCommands
	availableCommands = command.MergeAvailableCommands(availableCommands, bdsSentCommands)
	player.DataPacket(&availableCommands)
	return false, dataPacket, nil
}
