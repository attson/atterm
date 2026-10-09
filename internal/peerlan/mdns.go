// Package peerlan provides local-network reachability hints for Peer Space.
// Discovery results are never authorization; callers must still authenticate
// the expected active membership before using an endpoint.
package peerlan

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/hashicorp/mdns"
)

const (
	ServiceName      = "_atterm-peer._tcp"
	maxBrowseEntries = 64
	tagBytes         = 16
	tagInfo          = "atterm-peer-mdns-v1"
)

// Advertisement is one local Peer listener published through DNS-SD.
type Advertisement struct {
	Tag  string
	Port int
	IPs  []net.IP
}

// Entry is an untrusted DNS-SD reachability result.
type Entry struct {
	Tag  string
	IP   net.IP
	Port int
}

// Publisher owns the multicast responder for one advertisement.
type Publisher interface {
	Close() error
}

// DeriveTag creates an epoch-scoped opaque identifier. Devices with the
// current sync key can map it back to an active member; other LAN observers
// cannot use it as a stable Peer fingerprint.
func DeriveTag(key configsync.EpochKey, spaceID, peerID string) (string, error) {
	if key.Class != configsync.KeyClassSync || key.Epoch == 0 || len(key.Bytes()) != configsync.EpochKeySize ||
		strings.TrimSpace(spaceID) == "" || strings.TrimSpace(peerID) == "" {
		return "", errors.New("peer LAN discovery inputs are invalid")
	}
	mac := hmac.New(sha256.New, key.Bytes())
	_, _ = mac.Write([]byte(tagInfo))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strconv.FormatUint(key.Epoch, 10)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(spaceID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(peerID))
	return hex.EncodeToString(mac.Sum(nil)[:tagBytes]), nil
}

// Publish starts a local DNS-SD responder. Only the opaque tag and listener
// port are included; IPs come from the explicit listener configuration or
// local interface enumeration performed by the caller.
func Publish(ad Advertisement) (Publisher, error) {
	if !validTag(ad.Tag) || ad.Port < 1 || ad.Port > 65535 || len(ad.IPs) == 0 {
		return nil, errors.New("peer LAN advertisement is invalid")
	}
	ips := make([]net.IP, 0, len(ad.IPs))
	for _, ip := range ad.IPs {
		ipv4 := ip.To4()
		if ipv4 == nil || ipv4.IsUnspecified() || ipv4.IsMulticast() {
			return nil, errors.New("peer LAN advertisement IP is invalid")
		}
		ips = append(ips, append(net.IP(nil), ipv4...))
	}
	instance := "atterm-" + ad.Tag
	service, err := mdns.NewMDNSService(
		instance, ServiceName, "local.", instance+".local.", ad.Port, ips,
		[]string{"v=1", "tag=" + ad.Tag},
	)
	if err != nil {
		return nil, err
	}
	server, err := mdns.NewServer(&mdns.Config{
		Zone: service, Logger: log.New(io.Discard, "", 0),
	})
	if err != nil {
		return nil, err
	}
	return mdnsPublisher{server: server}, nil
}

// Browse queries the local multicast domain for Peer listeners. Returned
// entries are bounded, canonical and still untrusted.
func Browse(ctx context.Context, timeout time.Duration) ([]Entry, error) {
	if timeout <= 0 {
		return nil, errors.New("peer LAN browse timeout is invalid")
	}
	entries := make(chan *mdns.ServiceEntry, maxBrowseEntries)
	params := mdns.DefaultParams(ServiceName)
	params.Timeout = timeout
	params.Entries = entries
	params.DisableIPv6 = true
	params.Logger = log.New(io.Discard, "", 0)
	queryDone := make(chan error, 1)
	go func() { queryDone <- mdns.QueryContext(ctx, params) }()
	result := make([]Entry, 0, maxBrowseEntries)
	seen := make(map[string]struct{}, maxBrowseEntries)
	for {
		select {
		case raw := <-entries:
			entry, ok := parseEntry(raw)
			if !ok || len(result) >= maxBrowseEntries {
				continue
			}
			key := entry.Tag + "|" + entry.IP.String() + "|" + strconv.Itoa(entry.Port)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, entry)
		case err := <-queryDone:
			if err != nil {
				return nil, err
			}
			for len(entries) > 0 {
				raw := <-entries
				entry, ok := parseEntry(raw)
				if !ok || len(result) >= maxBrowseEntries {
					continue
				}
				key := entry.Tag + "|" + entry.IP.String() + "|" + strconv.Itoa(entry.Port)
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				result = append(result, entry)
			}
			goto sortResults
		}
	}

sortResults:
	sort.Slice(result, func(i, j int) bool {
		if result[i].Tag != result[j].Tag {
			return result[i].Tag < result[j].Tag
		}
		if result[i].IP.String() != result[j].IP.String() {
			return result[i].IP.String() < result[j].IP.String()
		}
		return result[i].Port < result[j].Port
	})
	return result, nil
}

type mdnsPublisher struct{ server *mdns.Server }

func (p mdnsPublisher) Close() error {
	if p.server == nil {
		return nil
	}
	return p.server.Shutdown()
}

func parseEntry(raw *mdns.ServiceEntry) (Entry, bool) {
	if raw == nil || raw.Port < 1 || raw.Port > 65535 || raw.AddrV4 == nil ||
		raw.AddrV4.IsUnspecified() || raw.AddrV4.IsMulticast() {
		return Entry{}, false
	}
	version, tag := "", ""
	for _, field := range raw.InfoFields {
		key, value, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		switch key {
		case "v":
			version = value
		case "tag":
			tag = value
		}
	}
	if version != "1" || !validTag(tag) {
		return Entry{}, false
	}
	return Entry{Tag: tag, IP: append(net.IP(nil), raw.AddrV4...), Port: raw.Port}, true
}

func validTag(tag string) bool {
	if len(tag) != tagBytes*2 || strings.ToLower(tag) != tag {
		return false
	}
	decoded, err := hex.DecodeString(tag)
	return err == nil && len(decoded) == tagBytes
}
