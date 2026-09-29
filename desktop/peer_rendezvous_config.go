package main

import (
	"errors"

	"github.com/attson/atterm/internal/rendezvousclient"
)

// PeerRendezvousConfig is the local-only effective reachability selection.
// It is intentionally separate from Relay config and Peer replicated config.
type PeerRendezvousConfig struct {
	Mode         string   `json:"mode"`
	URL          string   `json:"url"`
	WebSocketURL string   `json:"websocket_url"`
	HealthURL    string   `json:"health_url"`
	STUNMode     string   `json:"stun_mode"`
	STUNURLs     []string `json:"stun_urls"`
}

type SetPeerRendezvousConfigReq struct {
	Mode     string   `json:"mode"`
	URL      string   `json:"url"`
	STUNMode string   `json:"stun_mode"`
	STUNURLs []string `json:"stun_urls"`
}

// GetPeerRendezvousConfig returns canonical effective endpoints without
// contacting either the official or a custom service.
func (a *App) GetPeerRendezvousConfig() (PeerRendezvousConfig, error) {
	if a.cfgStore == nil {
		return PeerRendezvousConfig{}, errors.New("config store not ready")
	}
	cfg := a.cfgStore.Get()
	return resolvePeerRendezvousConfig(rendezvousclient.Config{
		Mode: rendezvousclient.Mode(cfg.PeerRendezvousMode), URL: cfg.PeerRendezvousURL,
		STUNMode: rendezvousclient.STUNMode(cfg.PeerSTUNMode), STUNURLs: cfg.PeerSTUNURLs,
	})
}

// SetPeerRendezvousConfig validates and persists only local route preferences.
func (a *App) SetPeerRendezvousConfig(req SetPeerRendezvousConfigReq) error {
	if a.cfgStore == nil {
		return errors.New("config store not ready")
	}
	resolved, err := rendezvousclient.ResolveConfig(rendezvousclient.Config{
		Mode: rendezvousclient.Mode(req.Mode), URL: req.URL,
		STUNMode: rendezvousclient.STUNMode(req.STUNMode), STUNURLs: req.STUNURLs,
	}, false)
	if err != nil {
		return err
	}
	cfg := a.cfgStore.Get()
	cfg.PeerRendezvousMode = string(resolved.Mode)
	cfg.PeerRendezvousURL = resolved.BaseURL
	cfg.PeerSTUNMode = string(resolved.STUNMode)
	cfg.PeerSTUNURLs = append([]string(nil), resolved.STUNURLs...)
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.reconcilePeerRendezvous(cfg)
	return nil
}

func resolvePeerRendezvousConfig(cfg rendezvousclient.Config) (PeerRendezvousConfig, error) {
	resolved, err := rendezvousclient.ResolveConfig(cfg, false)
	if err != nil {
		return PeerRendezvousConfig{}, err
	}
	return PeerRendezvousConfig{
		Mode: string(resolved.Mode), URL: resolved.BaseURL,
		WebSocketURL: resolved.WebSocketURL, HealthURL: resolved.HealthURL,
		STUNMode: string(resolved.STUNMode), STUNURLs: append([]string(nil), resolved.STUNURLs...),
	}, nil
}
