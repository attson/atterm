package relay

import (
	"encoding/json"

	"github.com/attson/atterm/internal/proto"
)

const (
	replayPaceBytes = 64 * 1024
)

type replayPacer struct {
	active         bool
	thresholdBytes int
	pendingBytes   int
}

func newReplayPacer(thresholdBytes int) *replayPacer {
	if thresholdBytes <= 0 {
		thresholdBytes = replayPaceBytes
	}
	return &replayPacer{thresholdBytes: thresholdBytes}
}

func (p *replayPacer) observe(f proto.Frame) bool {
	switch f.Type {
	case proto.TypeReplayProgress:
		var progress proto.ReplayProgressPayload
		if err := json.Unmarshal(f.Payload, &progress); err != nil {
			return false
		}
		switch progress.Phase {
		case proto.ReplayProgressStart:
			p.active = true
			p.pendingBytes = 0
		case proto.ReplayProgressEnd:
			p.active = false
			p.pendingBytes = 0
		}
		return false
	case proto.TypeOut:
		if !p.active {
			return false
		}
		_, data, err := proto.DecodeOut(f.Payload)
		if err != nil {
			return false
		}
		p.pendingBytes += len(data)
		if p.pendingBytes < p.thresholdBytes {
			return false
		}
		p.pendingBytes = 0
		return true
	default:
		return false
	}
}
