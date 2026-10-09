//go:build !js

package peertransport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v3/vnet"
	"github.com/pion/turn/v4"
	"github.com/pion/webrtc/v4"
)

const (
	pionTestSTUNIP   = "1.2.3.4"
	pionTestSTUNPort = 3478
)

type pionTestNetwork struct {
	router    *vnet.Router
	clientNet *vnet.Net
	hostNet   *vnet.Net
	stun      *turn.Server
	config    webrtc.Configuration
}

func (n *pionTestNetwork) close() {
	if n.stun != nil {
		_ = n.stun.Close()
	}
	if n.router != nil {
		_ = n.router.Stop()
	}
}

type pionTestClose struct {
	side string
	err  error
}

type pionTestPair struct {
	client        *PionClientAttempt
	host          *PionHostAttempt
	cancel        context.CancelFunc
	authenticated chan string
	candidateType chan string
	closed        chan pionTestClose
}

func TestPionNATMatrix(t *testing.T) {
	t.Run("host candidate", func(t *testing.T) {
		network := newPionHostNetwork(t, nil)
		defer network.close()
		pair := newPionTestPair(t, network)
		defer pair.close()

		pair.start(t)
		pair.waitAuthenticated(t, webrtc.ICECandidateTypeHost.String())
	})

	t.Run("server reflexive candidate", func(t *testing.T) {
		natType := vnet.NATType{
			MappingBehavior:   vnet.EndpointIndependent,
			FilteringBehavior: vnet.EndpointIndependent,
		}
		network := newPionNATNetwork(t, natType)
		defer network.close()
		pair := newPionTestPair(t, network)
		defer pair.close()

		pair.start(t)
		pair.waitAuthenticated(t, webrtc.ICECandidateTypeHost.String())
		requirePionCandidateType(t, pair.client.pc, webrtc.ICECandidateTypeSrflx)
		requirePionCandidateType(t, pair.host.pc, webrtc.ICECandidateTypeSrflx)
	})

	t.Run("symmetric NAT fails closed", func(t *testing.T) {
		natType := vnet.NATType{
			MappingBehavior:   vnet.EndpointAddrPortDependent,
			FilteringBehavior: vnet.EndpointAddrPortDependent,
		}
		network := newPionNATNetwork(t, natType)
		defer network.close()
		pair := newPionTestPair(t, network)
		defer pair.close()

		pair.start(t)
		pair.waitDirectFailure(t)
	})

	t.Run("UDP blocked fails closed", func(t *testing.T) {
		network := newPionHostNetwork(t, func(vnet.Chunk) bool { return false })
		defer network.close()
		pair := newPionTestPair(t, network)
		defer pair.close()

		pair.start(t)
		pair.waitDirectFailure(t)
	})

	t.Run("network switch closes and fresh connection succeeds", func(t *testing.T) {
		var blocked atomic.Bool
		network := newPionHostNetwork(t, func(vnet.Chunk) bool { return !blocked.Load() })
		pair := newPionTestPair(t, network)

		pair.start(t)
		pair.waitAuthenticated(t, webrtc.ICECandidateTypeHost.String())
		blocked.Store(true)
		pair.waitDirectFailure(t)
		pair.close()
		network.close()

		freshNetwork := newPionHostNetwork(t, nil)
		defer freshNetwork.close()
		freshPair := newPionTestPair(t, freshNetwork)
		defer freshPair.close()
		freshPair.start(t)
		freshPair.waitAuthenticated(t, webrtc.ICECandidateTypeHost.String())
	})
}

func newPionHostNetwork(t *testing.T, filter vnet.ChunkFilter) *pionTestNetwork {
	t.Helper()
	loggerFactory := logging.NewDefaultLoggerFactory()
	router, err := vnet.NewRouter(&vnet.RouterConfig{
		CIDR:          "192.0.2.0/24",
		LoggerFactory: loggerFactory,
	})
	if err != nil {
		t.Fatal(err)
	}
	if filter != nil {
		router.AddChunkFilter(filter)
	}
	clientNet := mustPionTestNet(t, "192.0.2.10")
	hostNet := mustPionTestNet(t, "192.0.2.11")
	if err := router.AddNet(clientNet); err != nil {
		t.Fatal(err)
	}
	if err := router.AddNet(hostNet); err != nil {
		t.Fatal(err)
	}
	if err := router.Start(); err != nil {
		t.Fatal(err)
	}
	return &pionTestNetwork{router: router, clientNet: clientNet, hostNet: hostNet}
}

func newPionNATNetwork(t *testing.T, natType vnet.NATType) *pionTestNetwork {
	t.Helper()
	loggerFactory := logging.NewDefaultLoggerFactory()
	wan, err := vnet.NewRouter(&vnet.RouterConfig{
		CIDR:          "0.0.0.0/0",
		LoggerFactory: loggerFactory,
	})
	if err != nil {
		t.Fatal(err)
	}
	wanNet := mustPionTestNet(t, pionTestSTUNIP)
	if err := wan.AddNet(wanNet); err != nil {
		t.Fatal(err)
	}
	clientNet := addPionNATClient(t, wan, "27.1.1.1", "192.168.0.1", natType, loggerFactory)
	hostNet := addPionNATClient(t, wan, "28.1.1.1", "10.2.0.1", natType, loggerFactory)
	if err := wan.Start(); err != nil {
		t.Fatal(err)
	}
	stun, err := startPionTestSTUN(wanNet, loggerFactory)
	if err != nil {
		_ = wan.Stop()
		t.Fatal(err)
	}
	return &pionTestNetwork{
		router:    wan,
		clientNet: clientNet,
		hostNet:   hostNet,
		stun:      stun,
		config: webrtc.Configuration{ICEServers: []webrtc.ICEServer{{
			URLs: []string{fmt.Sprintf("stun:%s:%d", pionTestSTUNIP, pionTestSTUNPort)},
		}}},
	}
}

func mustPionTestNet(t *testing.T, ip string) *vnet.Net {
	t.Helper()
	network, err := vnet.NewNet(&vnet.NetConfig{StaticIPs: []string{ip}})
	if err != nil {
		t.Fatal(err)
	}
	return network
}

func addPionNATClient(
	t *testing.T,
	wan *vnet.Router,
	publicIP string,
	privateIP string,
	natType vnet.NATType,
	loggerFactory logging.LoggerFactory,
) *vnet.Net {
	t.Helper()
	lan, err := vnet.NewRouter(&vnet.RouterConfig{
		StaticIPs:     []string{publicIP},
		CIDR:          privateIP + "/24",
		NATType:       &natType,
		LoggerFactory: loggerFactory,
	})
	if err != nil {
		t.Fatal(err)
	}
	network := mustPionTestNet(t, privateIP)
	if err := lan.AddNet(network); err != nil {
		t.Fatal(err)
	}
	if err := wan.AddRouter(lan); err != nil {
		t.Fatal(err)
	}
	return network
}

func startPionTestSTUN(network *vnet.Net, loggerFactory logging.LoggerFactory) (*turn.Server, error) {
	packetConn, err := network.ListenPacket("udp", fmt.Sprintf("%s:%d", pionTestSTUNIP, pionTestSTUNPort))
	if err != nil {
		return nil, err
	}
	return turn.NewServer(turn.ServerConfig{
		AuthHandler: func(string, string, net.Addr) ([]byte, bool) { return nil, false },
		PacketConnConfigs: []turn.PacketConnConfig{{
			PacketConn: packetConn,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP(pionTestSTUNIP),
				Address:      "0.0.0.0",
				Net:          network,
			},
		}},
		Realm:         "atterm.test",
		LoggerFactory: loggerFactory,
	})
}

func newPionTestPair(t *testing.T, network *pionTestNetwork) *pionTestPair {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	auth, err := NewAccountKeyAuthenticator(bytes.Repeat([]byte{0x4e}, accountKeySize))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	authorization := testAuthorization(time.Now().Add(time.Minute))
	pair := &pionTestPair{
		cancel:        cancel,
		authenticated: make(chan string, 2),
		candidateType: make(chan string, 4),
		closed:        make(chan pionTestClose, 4),
	}
	host, err := NewPionHostAttempt(ctx, PionHostConfig{
		API:             newPionTestAPI(network.hostNet),
		Authorization:   authorization,
		Authenticator:   auth,
		WebRTC:          network.config,
		SendSignal:      func(signalType, payload string) error { return pair.client.HandleSignal(signalType, payload) },
		OnAuthenticated: func(*PionHostChannel) { pair.authenticated <- "host" },
		OnClosed: func(err error) {
			select {
			case pair.closed <- pionTestClose{side: "host", err: err}:
			default:
			}
		},
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	pair.host = host
	client, err := NewPionClientAttempt(ctx, PionClientConfig{
		API:             newPionTestAPI(network.clientNet),
		Authorization:   authorization,
		Authenticator:   auth,
		WebRTC:          network.config,
		SendSignal:      host.HandleSignal,
		OnAuthenticated: func(*PionClientChannel) { pair.authenticated <- "client" },
		OnDiagnostics: func(_ string, candidateType string) {
			if candidateType == "" {
				return
			}
			select {
			case pair.candidateType <- candidateType:
			default:
			}
		},
		OnClosed: func(err error) {
			select {
			case pair.closed <- pionTestClose{side: "client", err: err}:
			default:
			}
		},
	})
	if err != nil {
		_ = host.Close()
		cancel()
		t.Fatal(err)
	}
	pair.client = client
	return pair
}

func newPionTestAPI(network *vnet.Net) *webrtc.API {
	settings := webrtc.SettingEngine{}
	settings.SetNet(network)
	settings.SetICETimeouts(300*time.Millisecond, 700*time.Millisecond, 50*time.Millisecond)
	settings.SetSrflxAcceptanceMinWait(0)
	settings.SetPrflxAcceptanceMinWait(0)
	settings.SetICEMaxBindingRequests(3)
	return webrtc.NewAPI(webrtc.WithSettingEngine(settings))
}

func (p *pionTestPair) start(t *testing.T) {
	t.Helper()
	if err := p.client.Start(); err != nil {
		t.Fatal(err)
	}
}

func (p *pionTestPair) waitAuthenticated(t *testing.T, wantCandidateType string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	seen := make(map[string]bool, 2)
	gotCandidateType := ""
	for len(seen) < 2 || gotCandidateType == "" {
		select {
		case side := <-p.authenticated:
			seen[side] = true
		case candidateType := <-p.candidateType:
			gotCandidateType = candidateType
		case closed := <-p.closed:
			t.Fatalf("%s closed before authentication: %v", closed.side, closed.err)
		case <-timer.C:
			t.Fatalf("authentication timeout: authenticated=%v candidate_type=%q", seen, gotCandidateType)
		}
	}
	if gotCandidateType != wantCandidateType {
		t.Fatalf("candidate type=%q, want %q", gotCandidateType, wantCandidateType)
	}
}

func (p *pionTestPair) waitDirectFailure(t *testing.T) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case side := <-p.authenticated:
			t.Fatalf("%s authenticated on an unreachable route", side)
		case closed := <-p.closed:
			if errors.Is(closed.err, ErrDirectTransport) {
				return
			}
		case <-timer.C:
			t.Fatal("direct failure callback timeout")
		}
	}
}

func requirePionCandidateType(t *testing.T, pc *webrtc.PeerConnection, want webrtc.ICECandidateType) {
	t.Helper()
	for _, stat := range pc.GetStats() {
		candidate, ok := stat.(webrtc.ICECandidateStats)
		if ok && candidate.CandidateType == want {
			return
		}
	}
	t.Fatalf("peer connection did not gather a %s candidate", want.String())
}

func (p *pionTestPair) close() {
	if p.client != nil {
		_ = p.client.Close()
	}
	if p.host != nil {
		_ = p.host.Close()
	}
	if p.cancel != nil {
		p.cancel()
	}
}
