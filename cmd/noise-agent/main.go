package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"noise-agent/internal/agent"
	"noise-agent/internal/collector"
)

func main() {
	var (
		endpoint    string
		token       string
		monthRotate int
		showVersion bool
	)

	flag.StringVar(&endpoint, "e", "", "Noise server endpoint URL")
	flag.StringVar(&endpoint, "endpoint", "", "Noise server endpoint URL")
	flag.StringVar(&token, "t", "", "Node authentication token")
	flag.StringVar(&token, "token", "", "Node authentication token")
	flag.IntVar(&monthRotate, "month-rotate", 1, "Monthly traffic reset day (1-31)")
	flag.BoolVar(&showVersion, "v", false, "Print version and exit")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Println(collector.Version)
		return
	}

	if endpoint == "" {
		endpoint = os.Getenv("NOISE_ENDPOINT")
	}
	if token == "" {
		token = os.Getenv("NOISE_TOKEN")
	}
	if envRotate := os.Getenv("NOISE_MONTH_ROTATE"); envRotate != "" && monthRotate == 1 {
		if parsed, err := strconv.Atoi(envRotate); err == nil && parsed >= 1 && parsed <= 31 {
			monthRotate = parsed
		}
	}

	endpoint = strings.TrimSpace(endpoint)
	token = strings.TrimSpace(token)
	if endpoint == "" || token == "" {
		fmt.Fprintln(os.Stderr, "Usage: noise-agent -e <endpoint> -t <token> [--month-rotate <1-31>]")
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("starting noise-agent", "version", collector.Version, "endpoint", endpoint, "monthRotate", monthRotate)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	runner := agent.New(agent.Options{
		Endpoint:    endpoint,
		Token:       token,
		MonthRotate: monthRotate,
		Logger:      logger,
	})
	if err := runner.Run(ctx); err != nil {
		logger.Error("noise-agent exited with error", "err", err)
		os.Exit(1)
	}
}
