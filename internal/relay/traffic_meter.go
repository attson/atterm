package relay

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"github.com/attson/atterm/internal/logging"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/userstore"
)

const (
	trafficIn  = 0
	trafficOut = 1
)

// trafficDelta is one accumulated (user, login client, frame type, direction) bucket
// drained from a trafficMeter snapshot.
type trafficDelta struct {
	Minute     int64
	UserID     string
	ClientID   string
	ClientName string
	ClientKind string
	FrameType  int
	Direction  int
	Bytes      int64
	Frames     int64
}

type trafficKey struct {
	minute     int64
	userID     string
	clientID   string
	clientName string
	clientKind string
	ft         int
	dir        int
}

// trafficClient identifies one authenticated login session. ID is the
// existing sha256 session hash, never the plaintext bearer token.
type trafficClient struct {
	ID   string
	Name string
	Kind string
}

var desktopTrafficUserAgent = regexp.MustCompile(`^AT-Term-Desktop/([^\s]+) \(([^/]+)/([^)]+)\)$`)

func trafficClientFromContext(ctx context.Context) trafficClient {
	sess, ok := SessionFromContext(ctx)
	if !ok {
		return trafficClient{}
	}
	name, kind := describeTrafficClient(sess.UserAgent)
	return trafficClient{ID: sess.IDHash, Name: name, Kind: kind}
}

func describeTrafficClient(userAgent string) (string, string) {
	ua := strings.TrimSpace(userAgent)
	if match := desktopTrafficUserAgent.FindStringSubmatch(ua); match != nil {
		platform := map[string]string{"darwin": "macOS", "windows": "Windows", "linux": "Linux"}[strings.ToLower(match[2])]
		if platform == "" {
			platform = match[2]
		}
		if match[1] == "dev" {
			return fmt.Sprintf("AT Term Desktop · %s", platform), "desktop"
		}
		return fmt.Sprintf("AT Term Desktop · %s · %s", platform, match[1]), "desktop"
	}
	if ua == "Go-http-client/1.1" {
		return "AT Term Desktop (legacy)", "legacy"
	}
	lower := strings.ToLower(ua)
	device := ""
	kind := "web"
	switch {
	case strings.Contains(lower, "ipad"):
		device, kind = "iPad", "mobile"
	case strings.Contains(lower, "iphone"):
		device, kind = "iPhone", "mobile"
	case strings.Contains(lower, "android"):
		device, kind = "Android", "mobile"
	case strings.Contains(lower, "windows"):
		device = "Windows"
	case strings.Contains(lower, "macintosh") || strings.Contains(lower, "mac os x"):
		device = "macOS"
	case strings.Contains(lower, "linux"):
		device = "Linux"
	}
	browser := ""
	switch {
	case strings.Contains(lower, "edg/"):
		browser = "Edge"
	case strings.Contains(lower, "firefox/"):
		browser = "Firefox"
	case strings.Contains(lower, "crios/") || strings.Contains(lower, "chrome/"):
		browser = "Chrome"
	case strings.Contains(lower, "safari/"):
		browser = "Safari"
	}
	if device != "" || browser != "" {
		return strings.Join(nonEmptyStrings(device, browser), " · "), kind
	}
	if ua == "" {
		return "", "unknown"
	}
	// User-Agent is client-controlled. Keep it useful as a fallback label but
	// bound row growth and remove control/spacing noise before persistence.
	label := strings.Join(strings.Fields(ua), " ")
	runes := []rune(label)
	if len(runes) > 160 {
		label = string(runes[:160]) + "…"
	}
	return label, "unknown"
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

type trafficCell struct {
	bytes  int64
	frames int64
}

// trafficMeter accumulates per-(user, login client, frame type, direction) byte and frame
// counts in memory. It is written on the hot send/receive paths (add) and
// drained on a fixed interval (snapshot). A single mutex is sufficient:
// snapshot runs at flush cadence (~10s), so lock contention on add is
// negligible relative to the WebSocket I/O it accompanies.
type trafficMeter struct {
	mu    sync.Mutex
	cells map[trafficKey]*trafficCell
}

func newTrafficMeter() *trafficMeter {
	return &trafficMeter{cells: make(map[trafficKey]*trafficCell)}
}

// add records wireBytes for one frame. A zero-length userID (pre-auth /
// anonymous frames) is dropped: traffic is only meaningful per account.
func (m *trafficMeter) add(userID string, client trafficClient, t proto.Type, dir int, wireBytes int) {
	m.addAt(userID, client, t, dir, wireBytes, time.Now())
}

func (m *trafficMeter) addAt(userID string, client trafficClient, t proto.Type, dir int, wireBytes int, captured time.Time) {
	if userID == "" {
		return
	}
	k := trafficKey{minute: captured.Unix() / 60 * 60, userID: userID, clientID: client.ID, clientName: client.Name, clientKind: client.Kind, ft: int(t), dir: dir}
	m.mu.Lock()
	c := m.cells[k]
	if c == nil {
		c = &trafficCell{}
		m.cells[k] = c
	}
	c.bytes += int64(wireBytes)
	c.frames++
	m.mu.Unlock()
}

// snapshot returns all accumulated deltas and resets the meter. The caller
// (flush loop) is responsible for persisting them; a failed persist loses
// at most one interval's data (accepted trade-off, see spec §Error handling).
func (m *trafficMeter) snapshot() []trafficDelta {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]trafficDelta, 0, len(m.cells))
	for k, c := range m.cells {
		out = append(out, trafficDelta{
			Minute:     k.minute,
			UserID:     k.userID,
			ClientID:   k.clientID,
			ClientName: k.clientName,
			ClientKind: k.clientKind,
			FrameType:  k.ft,
			Direction:  k.dir,
			Bytes:      c.bytes,
			Frames:     c.frames,
		})
	}
	m.cells = make(map[trafficKey]*trafficCell)
	return out
}

// trafficFlushInterval is how often accumulated traffic is folded into the
// minute/hour/day rollups. A crash loses at most this window (see spec §Error handling).
const trafficFlushInterval = 10 * time.Second

// trafficFlushLoop periodically drains the meter into the minute/hour/day rollups, and
// does a final drain on stop. Started by NewServer only when a store exists.
func (s *Server) trafficFlushLoop(store userstore.Store) {
	defer close(s.flushDone)
	t := time.NewTicker(trafficFlushInterval)
	prune := time.NewTicker(time.Hour)
	defer prune.Stop()
	pruneBuckets := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := store.PruneTrafficBuckets(ctx, time.Now()); err != nil {
			logging.Warn("relay-traffic", "prune buckets: %v", err)
		}
	}
	pruneBuckets()
	defer t.Stop()
	for {
		select {
		case <-prune.C:
			pruneBuckets()
		case <-t.C:
			s.flushTraffic(store)
		case <-s.flushStop:
			s.flushTraffic(store) // final drain
			return
		}
	}
}

// flushTraffic persists each relay delta into its captured UTC minute/hour/day
// rollups, including windows crossing midnight. A failed persist logs a warning and drops the window's data
// (accepted trade-off; the snapshot has already reset the meter).
func (s *Server) flushTraffic(store userstore.Store) {
	s.trafficFlushMu.Lock()
	defer s.trafficFlushMu.Unlock()

	deltas := s.traffic.snapshot()
	directDeltas := s.directStats.drain()
	if len(deltas) == 0 && len(directDeltas) == 0 {
		return
	}
	day := time.Now().UTC().Format("2006-01-02")
	conv := make([]userstore.TrafficDelta, len(deltas))
	for i, d := range deltas {
		conv[i] = userstore.TrafficDelta{
			Minute:     d.Minute,
			UserID:     d.UserID,
			ClientID:   d.ClientID,
			ClientName: d.ClientName,
			ClientKind: d.ClientKind,
			FrameType:  d.FrameType,
			Direction:  d.Direction,
			Bytes:      d.Bytes,
			Frames:     d.Frames,
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if len(conv) > 0 {
		if err := store.AddTrafficDeltas(ctx, day, conv); err != nil {
			logging.Warn("relay-traffic", "flush failed, dropping %d deltas: %v", len(conv), err)
		}
	}
	if len(directDeltas) > 0 {
		if err := store.AddDirectTrafficDeltas(ctx, day, directDeltas); err != nil {
			logging.Warn("relay-traffic", "direct flush failed, dropping %d deltas: %v", len(directDeltas), err)
		}
	}
}

// Close stops the traffic flush loop and drains a final snapshot. Called from
// the binary's shutdown path. Safe to call when the loop never started (no
// store) and idempotent-safe for a single call.
func (s *Server) Close() {
	if s.flushStop != nil {
		close(s.flushStop)
		<-s.flushDone
	}
}

// recordTraffic folds one frame's wire size into the meter. Never blocks the
// caller beyond a mutex-guarded map write; a zero userID is dropped.
func (s *Server) recordTraffic(userID string, client trafficClient, t proto.Type, dir int, wireBytes int) {
	s.traffic.add(userID, client, t, dir, wireBytes)
}

// writeFrame marshals f, writes it as one binary WS message, and — on a
// successful write — records its wire size against userID for traffic
// accounting. It centralizes the previously inlined
// c.Write(ctx, MessageBinary, proto.Marshal(f)) calls for the simple client
// write paths so every outbound frame is metered at one place.
func (s *Server) writeFrame(ctx context.Context, userID string, client trafficClient, c *websocket.Conn, f proto.Frame) error {
	b := proto.Marshal(f)
	if err := c.Write(ctx, websocket.MessageBinary, b); err != nil {
		return err
	}
	s.recordTraffic(userID, client, f.Type, trafficOut, len(b))
	return nil
}
