package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var (
	app *fiber.App = fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ErrorHandler: func(ctx *fiber.Ctx, err error) error {
			var fiberError *fiber.Error

			if errors.As(err, &fiberError) {
				return ctx.SendStatus(fiberError.Code)
			}

			log.Printf("Error: %v - URI: %s\n", err, ctx.Request().URI())

			return ctx.SendStatus(http.StatusInternalServerError)
		},
	})
	r          *Redis  = &Redis{}
	config     *Config = DefaultConfig
	instanceID uint16  = 0
)

func init() {
	var err error

	if err = config.ReadFile("config.yml"); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("config.yml does not exist, writing default config\n")

			if err = config.WriteFile("config.yml"); err != nil {
				log.Fatalf("Failed to write config file: %v", err)
			}
		} else {
			log.Printf("Failed to read config file: %v", err)
		}
	}

	if err = GetBlockedServerList(); err != nil {
		log.Fatalf("Failed to retrieve EULA blocked servers: %v", err)
	}

	log.Println("Successfully retrieved EULA blocked servers")

	if config.Redis != nil {
		if err = r.Connect(); err != nil {
			log.Fatalf("Failed to connect to Redis: %v", err)
		}

		log.Println("Successfully connected to Redis")
	}

	if instanceID, err = GetInstanceID(); err != nil {
		panic(err)
	}

	app.Hooks().OnListen(func(ld fiber.ListenData) error {
		log.Printf("Listening on %s:%d\n", config.Host, config.Port+instanceID)

		return nil
	})
}

func main() {
	defer r.Close()

	ctx, cancel := context.WithCancel(context.Background())
	CreateWatcher().Start(ctx, 10*time.Minute)

	go func() {
		if err := app.Listen(fmt.Sprintf("%s:%d", config.Host, config.Port+instanceID)); err != nil {
			panic(err)
		}
	}()

	exit := make(chan os.Signal, 1)
	signal.Notify(exit, syscall.SIGINT, syscall.SIGTERM)
	<-exit
	cancel()
}
