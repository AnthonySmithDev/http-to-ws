package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gookit/color"
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
				Value: "0.0.0.0",
				Usage: "host for server",
			},
			&cli.StringFlag{
				Name:  "port",
				Value: "9999",
				Usage: "port for server",
			},
		},
		Action: func(cCtx *cli.Context) error {
			wsURL := cCtx.Args().Get(0)
			if wsURL == "" {
				return errors.New("No websocket url specified")
			}
			addr := fmt.Sprint(cCtx.String("host"), ":", cCtx.String("port"))
			return run(wsURL, addr)
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal().Err(err).Msg("App Run")
	}
}

const TimeSleep = time.Second * 5

func run(wsURL, addr string) error {

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			log.Fatal().Err(err).Msg("Websocket not connected")
			time.Sleep(TimeSleep)
			continue
		}
		defer conn.CloseNow()

		log.Info().Msg("Websocket connected")

		server := fiber.New(fiber.Config{
			DisableStartupMessage: true,
		})

		server.Post("/", func(c *fiber.Ctx) error {
			data := c.Body()
			err := conn.Write(context.Background(), websocket.MessageText, data)
			if err != nil {
				log.Error().Err(err).Msg("Websocket Write")
			}
			color.Greenln(string(data))
			return nil
		})

		done := make(chan struct{})
		go func() {
			defer close(done)

			for {
				messageType, data, err := conn.Read(context.Background())
				if err != nil {
					if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
						log.Error().Msg("Websocket disconnected")
					} else {
						log.Error().Err(err).Msg("Websocket Read")
					}
					time.Sleep(TimeSleep)
					break
				}
				if messageType == websocket.MessageText {
					color.Cyanln(string(data))
				}
			}
		}()

		go func() {
			if err := server.Listen(addr); err != nil {
				log.Error().Err(err).Msg("Server Listen")
				time.Sleep(TimeSleep)
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
