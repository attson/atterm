package peertransport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestPionOrderedReliableDataChannelTenMiB(t *testing.T) {
	const payloadSize = 10 * 1024 * 1024
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	offerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer offerPC.Close()
	answerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer answerPC.Close()

	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	wantHash := sha256.Sum256(payload)
	fragments, err := FragmentFrame(1, payload)
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 4)
	completeCh := make(chan struct{}, 1)
	var reassembler Reassembler
	answerPC.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != "atterm-terminal-v1" || !dc.Ordered() || dc.MaxPacketLifeTime() != nil || dc.MaxRetransmits() != nil {
			errCh <- fmt.Errorf("unexpected data channel reliability label=%q ordered=%v", dc.Label(), dc.Ordered())
			return
		}
		dc.OnMessage(func(message webrtc.DataChannelMessage) {
			if message.IsString {
				errCh <- fmt.Errorf("received text fragment")
				return
			}
			frame, complete, addErr := reassembler.Add(message.Data, time.Now())
			if addErr != nil {
				errCh <- addErr
				return
			}
			if !complete {
				return
			}
			gotHash := sha256.Sum256(frame)
			if !bytes.Equal(gotHash[:], wantHash[:]) {
				errCh <- fmt.Errorf("reassembled payload hash differs")
				return
			}
			if sendErr := dc.SendText("ok"); sendErr != nil {
				errCh <- sendErr
			}
		})
	})

	dc, err := offerPC.CreateDataChannel("atterm-terminal-v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	dc.OnMessage(func(message webrtc.DataChannelMessage) {
		if message.IsString && string(message.Data) == "ok" {
			select {
			case completeCh <- struct{}{}:
			default:
			}
		}
	})
	dc.OnOpen(func() {
		go func() {
			for _, fragment := range fragments {
				for dc.BufferedAmount() > 1024*1024 {
					select {
					case <-ctx.Done():
						errCh <- ctx.Err()
						return
					case <-time.After(time.Millisecond):
					}
				}
				if sendErr := dc.Send(fragment); sendErr != nil {
					errCh <- sendErr
					return
				}
			}
		}()
	})

	started := time.Now()
	if err := signalPeerConnections(offerPC, answerPC); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		t.Fatal(err)
	case <-completeCh:
		t.Logf("Pion loopback: setup + 10 MiB transfer completed in %s across %d fragments", time.Since(started), len(fragments))
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func signalPeerConnections(offerPC, answerPC *webrtc.PeerConnection) error {
	offer, err := offerPC.CreateOffer(nil)
	if err != nil {
		return err
	}
	offerGathered := webrtc.GatheringCompletePromise(offerPC)
	if err := offerPC.SetLocalDescription(offer); err != nil {
		return err
	}
	<-offerGathered
	if err := answerPC.SetRemoteDescription(*offerPC.LocalDescription()); err != nil {
		return err
	}
	answer, err := answerPC.CreateAnswer(nil)
	if err != nil {
		return err
	}
	answerGathered := webrtc.GatheringCompletePromise(answerPC)
	if err := answerPC.SetLocalDescription(answer); err != nil {
		return err
	}
	<-answerGathered
	return offerPC.SetRemoteDescription(*answerPC.LocalDescription())
}
