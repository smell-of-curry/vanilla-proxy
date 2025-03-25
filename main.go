package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/HyPE-Network/vanilla-proxy/custom_handlers"
	"github.com/HyPE-Network/vanilla-proxy/handler"
	"github.com/HyPE-Network/vanilla-proxy/handler/handlers"
	"github.com/HyPE-Network/vanilla-proxy/log"
	"github.com/HyPE-Network/vanilla-proxy/proxy"
	"github.com/HyPE-Network/vanilla-proxy/utils"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func main() {
	log.Logger = log.New()
	log.Logger.Debug("Logger has been started!")

	// Load configuration
	config := utils.ReadConfig()

	proxy.ProxyInstance = proxy.New(config)

	// Create a channel to catch shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start the handlers
	handlerManager := loadHandlers()

	// Start the proxy in a goroutine
	go func() {
		err := proxy.ProxyInstance.Start(handlerManager)
		if err != nil {
			log.Logger.Error("Error while starting server", "error", err)
		}
	}()

	// Wait for shutdown signal
	<-sigCh

	// Perform graceful shutdown
	proxy.ProxyInstance.Shutdown()
}

func loadHandlers() handler.HandlerManager {
	// Store the repeating task so it can be stopped if needed
	claimTask := utils.NewRepeatingTask(60, func() {
		custom_handlers.FetchClaims()
	})

	// Register the task for cleanup
	proxy.ProxyInstance.RegisterCleanupTask(func() {
		claimTask.Stop()
	})

	h := handlers.New()
	h.RegisterHandler(packet.IDAvailableCommands, custom_handlers.AvailableCommandsHandler{})
	h.RegisterHandler(packet.IDCommandRequest, custom_handlers.CommandRequestHandler{})
	h.RegisterHandler(packet.IDBlockActorData, custom_handlers.SignEditHandler{})
	h.RegisterHandler(packet.IDInventoryTransaction, custom_handlers.ClaimInventoryTransactionHandler{})
	h.RegisterHandler(packet.IDPlayerAuthInput, custom_handlers.ClaimPlayerAuthInputHandler{})
	h.RegisterHandler(packet.IDText, custom_handlers.CustomCommandRegisterHandler{})
	h.RegisterHandler(packet.IDText, custom_handlers.ChatLoggingHandler{})
	h.RegisterHandler(packet.IDItemRegistry, custom_handlers.ItemComponentHandler{})
	h.RegisterHandler(packet.IDContainerOpen, custom_handlers.OpenContainerHandler{})
	h.RegisterHandler(packet.IDContainerClose, custom_handlers.ContainerCloseHandler{})
	h.RegisterHandler(packet.IDItemStackRequest, custom_handlers.ItemStackRequestHandler{})
	h.RegisterHandler(packet.IDPlayerList, custom_handlers.PlayerListHandler{})
	h.RegisterHandler(packet.IDAddActor, &custom_handlers.AddActorNameTagHandler{})
	h.RegisterHandler(packet.IDSetActorData, &custom_handlers.SetActorDataNameTagHandler{})

	return h
}
