package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	gh "github.com/craigchandler/glance-github-status/internal/github"
	srv "github.com/craigchandler/glance-github-status/internal/server"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

var version = "dev"

func main() {
	configPath := flag.String("config", env("CONFIG_FILE", "/etc/github-status.json"), "config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	raw, err := os.ReadFile(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	var cfg srv.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Fatal(err)
	}
	if err := srv.ValidateConfig(cfg); err != nil {
		log.Fatal(err)
	}
	timeout := durationEnv("HTTP_TIMEOUT", 15*time.Second)
	refresh := durationEnv("REFRESH_INTERVAL", 5*time.Minute)
	listen := env("LISTEN_ADDR", "127.0.0.1:8794")
	clients := make(map[string]*gh.Client, len(cfg.Accounts))
	for name, account := range cfg.Accounts {
		token := os.Getenv(account.TokenEnv)
		if token == "" {
			log.Fatalf("environment variable %s for account %q is required", account.TokenEnv, name)
		}
		clients[name] = gh.New(token, timeout)
	}
	s := srv.New(cfg, clients)
	if err := s.Refresh(context.Background()); err != nil {
		log.Printf("initial refresh partial/failed: %v", err)
	}
	go func() {
		t := time.NewTicker(refresh)
		defer t.Stop()
		for range t.C {
			if err := s.Refresh(context.Background()); err != nil {
				log.Printf("refresh partial/failed: %v", err)
			}
		}
	}()
	fmt.Printf("github-status %s listening on %s\n", version, listen)
	log.Fatal(http.ListenAndServe(listen, s.Handler()))
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func durationEnv(k string, d time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second
	}
	if x, err := time.ParseDuration(v); err == nil {
		return x
	}
	return d
}
