package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"nhooyr.io/websocket"
)

var log = zerolog.
	New(zerolog.ConsoleWriter{Out: os.Stdout}).
	With().Timestamp().Logger()

func main() {
	app := &cli.App{
		Name:  "http-to-ws",
		Usage: "convert http request to websocket message",

		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "host",
				Value: "0.0.0.0:9999",
				Usage: "host for server",
			},
		},
		Action: func(cCtx *cli.Context) error {
			wsURL := cCtx.Args().Get(0)
			if wsURL == "" {
				return errors.New("No websocket url specified")
			}
			return run(wsURL, cCtx.String("host"))
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal().Err(err).Msg("App Run")
	}
}

func run(wsURL, httpHost string) error {

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			log.Fatal().Err(err).Msg("Websocket not connected")
			time.Sleep(time.Second * 5)
			continue
		}
		defer conn.CloseNow()

		log.Info().Msg("Websocket connected")

		server := fiber.New(fiber.Config{
			DisableStartupMessage: true,
		})

		server.Post("/", func(c *fiber.Ctx) error {
			data := c.Body()
			err := conn.Write(ctx, websocket.MessageText, data)
			if err != nil {
				log.Error().Err(err).Msg("Websocket Write")
			}
			log.Info().Msg(string(data))
			return nil
		})

		done := make(chan struct{})
		go func() {
			defer close(done)

			for {
				messageType, data, err := conn.Read(ctx)
				if err != nil {
					if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
						log.Error().Msg("Websocket disconnected")
					} else {
						log.Error().Err(err).Msg("Websocket Read")
					}
					time.Sleep(time.Second * 5)
					break
				}
				if messageType == websocket.MessageText {
					log.Debug().Msg(string(data))
				}
			}
		}()

		go func() {
			if err := server.Listen(httpHost); err != nil {
				log.Error().Err(err).Msg("Server Listen")
				time.Sleep(time.Second * 5)
			}
		}()

		select {
		case <-interrupt:
			log.Info().Msg("Received interrupt signal, shutting down...")
			server.Shutdown()
			conn.Close(websocket.StatusNormalClosure, "")
			return nil
		case <-done:
			server.Shutdown()
			conn.Close(websocket.StatusNormalClosure, "")
		}
	}
}
