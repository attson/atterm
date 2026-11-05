package main

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"
)

func validTrafficTimeZone(zone string) bool {
	zone = strings.TrimSpace(zone)
	if zone == "" || zone == "Local" {
		return false
	}
	_, err := time.LoadLocation(zone)
	return err == nil
}

// GetTrafficTimeZone returns the raw account reporting zone. Empty means this
// installation has not seeded a choice yet; the renderer then offers its
// system IANA zone through SetTrafficTimeZone exactly once.
func (a *App) GetTrafficTimeZone() string {
	if a == nil || a.cfgStore == nil {
		return ""
	}
	zone := a.cfgStore.Get().TrafficTimeZone
	if validTrafficTimeZone(zone) {
		return zone
	}
	return ""
}

// SetTrafficTimeZone persists and syncs the account reporting-day zone.
func (a *App) SetTrafficTimeZone(zone string) error {
	zone = strings.TrimSpace(zone)
	if !validTrafficTimeZone(zone) {
		return fmt.Errorf("invalid traffic time zone: %s", zone)
	}
	return a.updatePref("traffic_time_zone", func(cfg *appConfig) error {
		cfg.TrafficTimeZone = zone
		return nil
	})
}
