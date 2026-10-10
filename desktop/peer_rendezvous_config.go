package main

import (
	"errors"

	"github.com/attson/atterm/internal/rendezvousclient"
)

// PeerRendezvousConfig is the local-only effective reachability selection.
// It is intentionally separate from Relay config and Peer replicated config.
type PeerRendezvousConfig struct {
	Mode                     string   `json:"mode"`
	URL                      string   `json:"url"`
	WebSocketURL             string   `json:"websocket_url"`
	HealthURL                string   `json:"health_url"`
	STUNMode                 string   `json:"stun_mode"`
	STUNURLs                 []string `json:"stun_urls"`
	TURNEnabled              bool     `json:"turn_enabled"`
	TURNURLs                 []string `json:"turn_urls"`
	TURNUsername             string   `json:"turn_username"`
	TURNCredentialConfigured bool     `json:"turn_credential_configured"`
}

type SetPeerRendezvousConfigReq struct {
	Mode           string   `json:"mode"`
	URL            string   `json:"url"`
	STUNMode       string   `json:"stun_mode"`
	STUNURLs       []string `json:"stun_urls"`
	TURNEnabled    bool     `json:"turn_enabled"`
	TURNURLs       []string `json:"turn_urls"`
	TURNUsername   string   `json:"turn_username"`
	TURNCredential string   `json:"turn_credential"`
}

// GetPeerRendezvousConfig returns canonical effective endpoints without
// contacting either the official or a custom service.
func (a *App) GetPeerRendezvousConfig() (PeerRendezvousConfig, error) {
	if a.cfgStore == nil {
		return PeerRendezvousConfig{}, errors.New("config store not ready")
	}
	cfg := a.cfgStore.Get()
	credential, err := loadPeerTURNCredential()
	if err != nil {
		return PeerRendezvousConfig{}, err
	}
	resolved, err := resolvePeerRendezvousConfig(peerRendezvousClientConfig(cfg, credential))
	if err != nil {
		return PeerRendezvousConfig{}, err
	}
	resolved.TURNCredentialConfigured = credential != ""
	return resolved, nil
}

// SetPeerRendezvousConfig validates and persists only local route preferences.
func (a *App) SetPeerRendezvousConfig(req SetPeerRendezvousConfigReq) error {
	if a.cfgStore == nil {
		return errors.New("config store not ready")
	}
	storedCredential, err := loadPeerTURNCredential()
	if err != nil {
		return err
	}
	credential := req.TURNCredential
	if credential == "" {
		credential = storedCredential
	}
	requested := rendezvousclient.Config{
		Mode: rendezvousclient.Mode(req.Mode), URL: req.URL,
		STUNMode: rendezvousclient.STUNMode(req.STUNMode), STUNURLs: req.STUNURLs,
		TURNEnabled: req.TURNEnabled, TURNURLs: req.TURNURLs,
		TURNUsername: req.TURNUsername, TURNCredential: credential,
	}
	resolved, err := rendezvousclient.ResolveConfig(requested, false)
	if err != nil {
		return err
	}
	targetCredential := ""
	if resolved.TURNEnabled {
		targetCredential = credential
	}
	credentialChanged := targetCredential != storedCredential
	if credentialChanged {
		if err := savePeerTURNCredential(targetCredential); err != nil {
			return err
		}
	}
	cfg := a.cfgStore.Get()
	cfg.PeerRendezvousMode = string(resolved.Mode)
	cfg.PeerRendezvousURL = resolved.BaseURL
	cfg.PeerSTUNMode = string(resolved.STUNMode)
	cfg.PeerSTUNURLs = append([]string(nil), resolved.STUNURLs...)
	cfg.PeerTURNEnabled = resolved.TURNEnabled
	cfg.PeerTURNURLs = append([]string(nil), resolved.TURNURLs...)
	cfg.PeerTURNUsername = resolved.TURNUsername
	if err := a.cfgStore.Set(cfg); err != nil {
		if credentialChanged {
			_ = savePeerTURNCredential(storedCredential)
		}
		return err
	}
	a.reconcilePeerRendezvous(cfg)
	return nil
}

func peerRendezvousClientConfig(cfg appConfig, credential string) rendezvousclient.Config {
	return rendezvousclient.Config{
		Mode: rendezvousclient.Mode(cfg.PeerRendezvousMode), URL: cfg.PeerRendezvousURL,
		STUNMode: rendezvousclient.STUNMode(cfg.PeerSTUNMode), STUNURLs: cfg.PeerSTUNURLs,
		TURNEnabled: cfg.PeerTURNEnabled, TURNURLs: cfg.PeerTURNURLs,
		TURNUsername: cfg.PeerTURNUsername, TURNCredential: credential,
	}
}

func (a *App) resolveStoredPeerRendezvousConfig(cfg appConfig) (rendezvousclient.ResolvedConfig, error) {
	credential := ""
	if cfg.PeerTURNEnabled {
		var err error
		credential, err = loadPeerTURNCredential()
		if err != nil {
			return rendezvousclient.ResolvedConfig{}, err
		}
	}
	return rendezvousclient.ResolveConfig(peerRendezvousClientConfig(cfg, credential), false)
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
		TURNEnabled: resolved.TURNEnabled, TURNURLs: append([]string(nil), resolved.TURNURLs...),
		TURNUsername: resolved.TURNUsername,
	}, nil
}
