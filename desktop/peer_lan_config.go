package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerlan"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/quicktunnel"
)

const defaultPeerLANPort = 8484

const peerLANMDNSBrowseTimeout = 600 * time.Millisecond

// PeerManualLANRoute is a local reachability hint bound to one active Peer
// identity. The fingerprint is the expected device signing-key digest, not a
// password or authorization token.
type PeerManualLANRoute struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Fingerprint string `json:"fingerprint"`
}

// PeerLANConfig is the user-visible local listener, discovery state and route list.
type PeerLANConfig struct {
	Enabled            bool                 `json:"enabled"`
	AutoDiscovery      bool                 `json:"auto_discovery"`
	AdvertiseHost      string               `json:"advertise_host"`
	Port               int                  `json:"port"`
	Routes             []PeerManualLANRoute `json:"routes"`
	Running            bool                 `json:"running"`
	DiscoveryRunning   bool                 `json:"discovery_running"`
	ListenAddress      string               `json:"listen_address,omitempty"`
	LastError          string               `json:"last_error,omitempty"`
	DiscoveryLastError string               `json:"discovery_last_error,omitempty"`
}

type SetPeerLANConfigReq struct {
	Enabled       bool                 `json:"enabled"`
	AutoDiscovery bool                 `json:"auto_discovery"`
	AdvertiseHost string               `json:"advertise_host"`
	Port          int                  `json:"port"`
	Routes        []PeerManualLANRoute `json:"routes"`
}

type peerLANListener struct {
	gateway        *quicktunnel.Gateway
	publisher      peerlan.Publisher
	address        string
	lastError      string
	discoveryError string
}

type peerLANPublishFunc func(peerlan.Advertisement) (peerlan.Publisher, error)
type peerLANBrowseFunc func(context.Context, time.Duration) ([]peerlan.Entry, error)

type resolvedPeerLANRoute struct {
	URL        string
	PeerID     string
	Membership peerproto.VerifiedGrant
}

func (a *App) GetPeerLANConfig() (PeerLANConfig, error) {
	if a == nil || a.cfgStore == nil {
		return PeerLANConfig{}, errors.New("config store not ready")
	}
	cfg := a.cfgStore.Get()
	port := cfg.PeerLANPort
	if port == 0 {
		port = defaultPeerLANPort
	}
	result := PeerLANConfig{
		Enabled: cfg.PeerLANEnabled, AutoDiscovery: cfg.PeerLANAutoDiscovery,
		AdvertiseHost: cfg.PeerLANAdvertiseHost, Port: port,
		Routes: append([]PeerManualLANRoute(nil), cfg.PeerLANRoutes...),
	}
	a.peerLANMu.Lock()
	if a.peerLAN != nil {
		result.Running = a.peerLAN.gateway != nil
		result.DiscoveryRunning = a.peerLAN.publisher != nil
		result.ListenAddress = a.peerLAN.address
		result.LastError = a.peerLAN.lastError
		result.DiscoveryLastError = a.peerLAN.discoveryError
	}
	a.peerLANMu.Unlock()
	return result, nil
}

func (a *App) SetPeerLANConfig(req SetPeerLANConfigReq) error {
	if a == nil || a.cfgStore == nil {
		return errors.New("config store not ready")
	}
	port := req.Port
	if port == 0 {
		port = defaultPeerLANPort
	}
	if port < 1 || port > 65535 {
		return errors.New("manual LAN listener port must be between 1 and 65535")
	}
	advertiseHost := strings.TrimSpace(req.AdvertiseHost)
	if advertiseHost != "" {
		var err error
		advertiseHost, err = canonicalPeerLANHost(advertiseHost)
		if err != nil {
			return errors.New("invalid manual LAN advertised host")
		}
	}
	if req.Enabled && advertiseHost == "" {
		return errors.New("manual LAN advertised host is required")
	}
	routes, err := a.validatePeerLANRoutes(req.Routes)
	if err != nil {
		return err
	}
	cfg := a.cfgStore.Get()
	cfg.PeerLANEnabled = req.Enabled
	cfg.PeerLANAutoDiscovery = req.AutoDiscovery
	cfg.PeerLANAdvertiseHost = advertiseHost
	cfg.PeerLANPort = port
	cfg.PeerLANRoutes = routes
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	return a.reconcilePeerLAN(cfg)
}

func (a *App) validatePeerLANRoutes(routes []PeerManualLANRoute) ([]PeerManualLANRoute, error) {
	if len(routes) > 32 {
		return nil, errors.New("manual LAN route limit exceeded")
	}
	active, localPeerID, err := a.activePeerMembershipDirectory()
	if err != nil && len(routes) != 0 {
		return nil, errors.New("Peer Space must be available before adding manual LAN routes")
	}
	seen := make(map[string]struct{}, len(routes))
	canonical := make([]PeerManualLANRoute, 0, len(routes))
	for _, route := range routes {
		host, err := canonicalPeerLANHost(route.Host)
		if err != nil || route.Port < 1 || route.Port > 65535 {
			return nil, errors.New("invalid manual LAN host or port")
		}
		peerID, err := peerIDFromFingerprint(route.Fingerprint)
		if err != nil || peerID == localPeerID {
			return nil, errors.New("manual LAN device fingerprint is invalid")
		}
		if _, ok := active[peerID]; !ok {
			return nil, errors.New("manual LAN device fingerprint is not an active Peer member")
		}
		if _, exists := seen[peerID]; exists {
			return nil, errors.New("manual LAN device fingerprint is duplicated")
		}
		seen[peerID] = struct{}{}
		canonical = append(canonical, PeerManualLANRoute{
			Host: host, Port: route.Port, Fingerprint: peerFingerprint(peerID),
		})
	}
	return canonical, nil
}

func (a *App) activePeerMembershipDirectory() (map[string]peerproto.VerifiedGrant, string, error) {
	manager, err := a.peerManager()
	if err != nil {
		return nil, "", err
	}
	state, err := manager.store.Load()
	if err != nil {
		return nil, "", err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return nil, "", err
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return nil, "", err
	}
	now := time.Now()
	if manager.now != nil {
		now = manager.now()
	}
	memberships, err := activePeerMemberships(state, genesis, now)
	if err != nil {
		return nil, "", err
	}
	byPeer := make(map[string]peerproto.VerifiedGrant, len(memberships))
	for _, membership := range memberships {
		byPeer[membership.Document.SubjectPeerID] = membership
	}
	return byPeer, identity.PeerID(), nil
}

func (a *App) resolvedPeerLANRoutes(ctx context.Context) ([]resolvedPeerLANRoute, error) {
	if a == nil || a.cfgStore == nil {
		return nil, errors.New("config store not ready")
	}
	cfg := a.cfgStore.Get()
	active, localPeerID, err := a.activePeerMembershipDirectory()
	if err != nil {
		return nil, err
	}
	byPeer := make(map[string]resolvedPeerLANRoute, len(cfg.PeerLANRoutes))
	for _, route := range cfg.PeerLANRoutes {
		host, hostErr := canonicalPeerLANHost(route.Host)
		peerID, fingerprintErr := peerIDFromFingerprint(route.Fingerprint)
		membership, ok := active[peerID]
		if hostErr != nil || fingerprintErr != nil || !ok || peerID == localPeerID || route.Port < 1 || route.Port > 65535 {
			continue
		}
		byPeer[peerID] = resolvedPeerLANRoute{
			URL:    "http://" + net.JoinHostPort(host, strconv.Itoa(route.Port)),
			PeerID: peerID, Membership: membership,
		}
	}
	if cfg.PeerLANAutoDiscovery {
		entries, discoveryErr := a.browsePeerLAN(ctx)
		if discoveryErr != nil && len(byPeer) == 0 {
			return nil, discoveryErr
		}
		if discoveryErr == nil {
			tags, tagErr := a.peerLANDiscoveryTags(active, localPeerID)
			if tagErr != nil && len(byPeer) == 0 {
				return nil, tagErr
			}
			if tagErr == nil {
				for _, entry := range entries {
					peerID, ok := tags[entry.Tag]
					if !ok {
						continue
					}
					if _, exists := byPeer[peerID]; exists {
						continue
					}
					byPeer[peerID] = resolvedPeerLANRoute{
						URL:    "http://" + net.JoinHostPort(entry.IP.String(), strconv.Itoa(entry.Port)),
						PeerID: peerID, Membership: active[peerID],
					}
				}
			}
		}
	}
	peerIDs := make([]string, 0, len(byPeer))
	for peerID := range byPeer {
		peerIDs = append(peerIDs, peerID)
	}
	sort.Strings(peerIDs)
	result := make([]resolvedPeerLANRoute, 0, len(peerIDs))
	for _, peerID := range peerIDs {
		result = append(result, byPeer[peerID])
	}
	return result, nil
}

func (a *App) browsePeerLAN(ctx context.Context) ([]peerlan.Entry, error) {
	browse := a.peerLANBrowse
	if browse == nil {
		browse = peerlan.Browse
	}
	return browse(ctx, peerLANMDNSBrowseTimeout)
}

func (a *App) peerLANDiscoveryTags(active map[string]peerproto.VerifiedGrant, localPeerID string) (map[string]string, error) {
	manager, err := a.peerManager()
	if err != nil {
		return nil, err
	}
	state, err := manager.store.Load()
	if err != nil {
		return nil, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return nil, err
	}
	key, err := loadPeerEpochKey(genesis.Document.SpaceID, configsync.KeyClassSync)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(active))
	for peerID := range active {
		if peerID == localPeerID {
			continue
		}
		tag, err := peerlan.DeriveTag(key, genesis.Document.SpaceID, peerID)
		if err != nil {
			return nil, err
		}
		result[tag] = peerID
	}
	return result, nil
}

func canonicalPeerLANHost(raw string) (string, error) {
	host := strings.TrimSpace(raw)
	if host == "" || len(host) > 253 || strings.ContainsAny(host, "/\\?#@") {
		return "", errors.New("invalid manual LAN host")
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String(), nil
	}
	if strings.HasSuffix(host, ".") {
		host = strings.TrimSuffix(host, ".")
	}
	if host == "" {
		return "", errors.New("invalid manual LAN host")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("invalid manual LAN hostname")
		}
		for _, char := range label {
			if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' {
				continue
			}
			return "", errors.New("invalid manual LAN hostname")
		}
	}
	return strings.ToLower(host), nil
}

func peerFingerprint(peerID string) string { return "SHA256:" + peerID }

func peerIDFromFingerprint(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if !strings.HasPrefix(value, "SHA256:") {
		return "", errors.New("invalid Peer fingerprint prefix")
	}
	peerID := strings.TrimPrefix(value, "SHA256:")
	digest, err := base64.RawURLEncoding.Strict().DecodeString(peerID)
	if err != nil || len(digest) != 32 {
		return "", errors.New("invalid Peer fingerprint")
	}
	if base64.RawURLEncoding.EncodeToString(digest) != peerID {
		return "", errors.New("non-canonical Peer fingerprint")
	}
	return peerID, nil
}

func (a *App) reconcilePeerLAN(cfg appConfig) error {
	a.stopPeerLAN()
	if !cfg.PeerLANEnabled {
		return nil
	}
	lifecycle := &peerLANListener{}
	manager, err := a.peerManager()
	if err != nil {
		lifecycle.lastError = "peer_space_unavailable"
		a.setPeerLANLifecycle(lifecycle)
		return err
	}
	status, err := manager.status()
	if err != nil || !status.Configured {
		lifecycle.lastError = "peer_space_unavailable"
		if err == nil {
			err = errors.New("Peer Space must be created before enabling LAN access")
		}
		a.setPeerLANLifecycle(lifecycle)
		return err
	}
	host, err := a.ensurePeerQuickTunnelHost()
	if err != nil {
		lifecycle.lastError = "host_unavailable"
		a.setPeerLANLifecycle(lifecycle)
		return err
	}
	port := cfg.PeerLANPort
	if port == 0 {
		port = defaultPeerLANPort
	}
	gateway, err := quicktunnel.OpenGatewayAt("tcp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(port)), host.handler)
	if err != nil {
		lifecycle.lastError = "listen_failed"
		a.setPeerLANLifecycle(lifecycle)
		return fmt.Errorf("open Peer LAN listener: %w", err)
	}
	lifecycle.gateway = gateway
	lifecycle.address = gateway.Address()
	if cfg.PeerLANAutoDiscovery {
		advertisement, advertiseErr := a.peerLANAdvertisement(cfg)
		if advertiseErr == nil {
			publish := a.peerLANPublish
			if publish == nil {
				publish = peerlan.Publish
			}
			lifecycle.publisher, advertiseErr = publish(advertisement)
		}
		if advertiseErr != nil {
			lifecycle.discoveryError = "publish_failed"
			logWarn("peer-lan", "publish mDNS reachability hint: %v", advertiseErr)
		}
	}
	a.setPeerLANLifecycle(lifecycle)
	logInfo("peer-lan", "manual Peer listener started address=%s", lifecycle.address)
	return nil
}

func (a *App) peerLANAdvertisement(cfg appConfig) (peerlan.Advertisement, error) {
	manager, err := a.peerManager()
	if err != nil {
		return peerlan.Advertisement{}, err
	}
	state, err := manager.store.Load()
	if err != nil {
		return peerlan.Advertisement{}, err
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		return peerlan.Advertisement{}, err
	}
	identity, err := manager.loadIdentity()
	if err != nil {
		return peerlan.Advertisement{}, err
	}
	key, err := loadPeerEpochKey(genesis.Document.SpaceID, configsync.KeyClassSync)
	if err != nil {
		return peerlan.Advertisement{}, err
	}
	tag, err := peerlan.DeriveTag(key, genesis.Document.SpaceID, identity.PeerID())
	if err != nil {
		return peerlan.Advertisement{}, err
	}
	ips, err := peerLANAdvertisementIPs(cfg.PeerLANAdvertiseHost)
	if err != nil {
		return peerlan.Advertisement{}, err
	}
	port := cfg.PeerLANPort
	if port == 0 {
		port = defaultPeerLANPort
	}
	return peerlan.Advertisement{Tag: tag, Port: port, IPs: ips}, nil
}

func peerLANAdvertisementIPs(advertiseHost string) ([]net.IP, error) {
	if ip := net.ParseIP(strings.TrimSpace(advertiseHost)); ip != nil {
		if ipv4 := ip.To4(); ipv4 != nil && !ipv4.IsUnspecified() && !ipv4.IsMulticast() {
			return []net.IP{append(net.IP(nil), ipv4...)}, nil
		}
		return nil, errors.New("Peer LAN mDNS currently requires an IPv4 listener address")
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var result []net.IP
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			host, _, err := net.ParseCIDR(address.String())
			if err != nil {
				continue
			}
			ipv4 := host.To4()
			if ipv4 == nil || ipv4.IsUnspecified() || ipv4.IsMulticast() {
				continue
			}
			key := ipv4.String()
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, append(net.IP(nil), ipv4...))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	if len(result) == 0 {
		return nil, errors.New("no IPv4 address is available for Peer LAN mDNS")
	}
	return result, nil
}

func (a *App) setPeerLANLifecycle(lifecycle *peerLANListener) {
	a.peerLANMu.Lock()
	a.peerLAN = lifecycle
	a.peerLANMu.Unlock()
}

func (a *App) peerLANConnectionRoute() (peerproto.ConnectionRoute, bool) {
	if a == nil || a.cfgStore == nil {
		return peerproto.ConnectionRoute{}, false
	}
	cfg := a.cfgStore.Get()
	a.peerLANMu.Lock()
	running := a.peerLAN != nil && a.peerLAN.gateway != nil
	a.peerLANMu.Unlock()
	if !running || !cfg.PeerLANEnabled || cfg.PeerLANAdvertiseHost == "" || cfg.PeerLANPort < 1 || cfg.PeerLANPort > 65535 {
		return peerproto.ConnectionRoute{}, false
	}
	host, err := canonicalPeerLANHost(cfg.PeerLANAdvertiseHost)
	if err != nil {
		return peerproto.ConnectionRoute{}, false
	}
	return peerproto.ConnectionRoute{
		Kind: peerproto.RouteManualLAN,
		URL:  "http://" + net.JoinHostPort(host, strconv.Itoa(cfg.PeerLANPort)),
	}, true
}

func (a *App) stopPeerLAN() {
	if a == nil {
		return
	}
	a.peerLANMu.Lock()
	lifecycle := a.peerLAN
	a.peerLAN = nil
	a.peerLANMu.Unlock()
	if lifecycle == nil || lifecycle.gateway == nil {
		if lifecycle != nil && lifecycle.publisher != nil {
			_ = lifecycle.publisher.Close()
		}
		return
	}
	if lifecycle.publisher != nil {
		if err := lifecycle.publisher.Close(); err != nil {
			logWarn("peer-lan", "stop mDNS publisher: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := lifecycle.gateway.Close(ctx); err != nil {
		logWarn("peer-lan", "stop manual Peer listener: %v", err)
	}
}
