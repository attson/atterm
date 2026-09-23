package peertransport

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestPionChromiumOrderedReliableDataChannel(t *testing.T) {
	chrome, err := findChrome()
	if err != nil {
		t.Skip("Chrome not installed; browser DataChannel spike skipped")
	}
	if testing.Short() {
		t.Skip("browser DataChannel spike skipped in short mode")
	}

	answerPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer answerPC.Close()
	errCh := make(chan error, 4)
	browserDone := make(chan struct{})
	var browserDoneOnce sync.Once
	var reassembler Reassembler
	answerPC.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != "atterm-terminal-v1" || !dc.Ordered() || dc.MaxPacketLifeTime() != nil || dc.MaxRetransmits() != nil {
			nonBlockingTestError(errCh, fmt.Errorf("unexpected Chromium channel reliability"))
			return
		}
		dc.OnMessage(func(message webrtc.DataChannelMessage) {
			frame, complete, addErr := reassembler.Add(message.Data, time.Now())
			if addErr != nil {
				nonBlockingTestError(errCh, addErr)
				return
			}
			if !complete {
				return
			}
			want := make([]byte, 10*1024*1024)
			for i := range want {
				want[i] = byte(i % 251)
			}
			gotHash := sha256.Sum256(frame)
			wantHash := sha256.Sum256(want)
			if !bytes.Equal(gotHash[:], wantHash[:]) {
				nonBlockingTestError(errCh, fmt.Errorf("Chromium payload hash differs"))
				return
			}
			if sendErr := dc.SendText("ok"); sendErr != nil {
				nonBlockingTestError(errCh, sendErr)
			}
		})
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(chromiumSpikePage))
	})
	mux.HandleFunc("/wait", func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-browserDone:
			w.WriteHeader(http.StatusNoContent)
		case <-time.After(25 * time.Second):
			http.Error(w, "browser timeout", http.StatusGatewayTimeout)
		}
	})
	mux.HandleFunc("/complete", func(w http.ResponseWriter, _ *http.Request) {
		browserDoneOnce.Do(func() { close(browserDone) })
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/offer", func(w http.ResponseWriter, r *http.Request) {
		var offer webrtc.SessionDescription
		if decodeErr := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024)).Decode(&offer); decodeErr != nil {
			http.Error(w, "bad offer", http.StatusBadRequest)
			nonBlockingTestError(errCh, decodeErr)
			return
		}
		if setErr := answerPC.SetRemoteDescription(offer); setErr != nil {
			http.Error(w, "bad remote description", http.StatusBadRequest)
			nonBlockingTestError(errCh, setErr)
			return
		}
		answer, createErr := answerPC.CreateAnswer(nil)
		if createErr != nil {
			http.Error(w, "create answer failed", http.StatusInternalServerError)
			nonBlockingTestError(errCh, createErr)
			return
		}
		gathered := webrtc.GatheringCompletePromise(answerPC)
		if setErr := answerPC.SetLocalDescription(answer); setErr != nil {
			http.Error(w, "set answer failed", http.StatusInternalServerError)
			nonBlockingTestError(errCh, setErr)
			return
		}
		<-gathered
		w.Header().Set("Content-Type", "application/json")
		if encodeErr := json.NewEncoder(w).Encode(answerPC.LocalDescription()); encodeErr != nil {
			nonBlockingTestError(errCh, encodeErr)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	started := time.Now()
	cmd := exec.Command(chrome,
		"--headless=new",
		"--no-sandbox",
		"--disable-gpu",
		"--disable-dev-shm-usage",
		"--disable-features=WebRtcHideLocalIpsWithMdns",
		"--no-proxy-server",
		"--user-data-dir="+t.TempDir(),
		"--virtual-time-budget=20000",
		"--dump-dom",
		server.URL,
	)
	output, runErr := cmd.CombinedOutput()
	select {
	case callbackErr := <-errCh:
		t.Fatal(callbackErr)
	default:
	}
	if runErr != nil {
		t.Fatalf("Chrome spike: %v\n%s", runErr, output)
	}
	if !strings.Contains(string(output), `data-result="pass"`) {
		t.Fatalf("Chrome did not complete DataChannel transfer:\n%s", output)
	}
	t.Logf("Chromium -> Pion 10 MiB transfer completed in %s", time.Since(started))
}

func findChrome() (string, error) {
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

func nonBlockingTestError(ch chan<- error, err error) {
	select {
	case ch <- err:
	default:
	}
}

const chromiumSpikePage = `<!doctype html>
<html><body data-result="running"><img src="/wait" hidden><script>
(async () => {
  const pc = new RTCPeerConnection({ iceServers: [] });
  const dc = pc.createDataChannel('atterm-terminal-v1');
  dc.binaryType = 'arraybuffer';
  dc.bufferedAmountLowThreshold = 256 * 1024;
  dc.onmessage = async (event) => {
    if (event.data === 'ok') {
      document.body.dataset.result = 'pass';
      await fetch('/complete', { method: 'POST' });
    }
  };
  dc.onopen = async () => {
    const total = 10 * 1024 * 1024;
    const chunkSize = 16 * 1024 - 16;
    for (let offset = 0; offset < total; offset += chunkSize) {
      while (dc.bufferedAmount > 1024 * 1024) {
        await new Promise((resolve) => dc.addEventListener('bufferedamountlow', resolve, { once: true }));
      }
      const size = Math.min(chunkSize, total - offset);
      const fragment = new Uint8Array(16 + size);
      const view = new DataView(fragment.buffer);
      view.setBigUint64(0, 1n, false);
      view.setUint32(8, offset, false);
      view.setUint32(12, total, false);
      for (let i = 0; i < size; i++) fragment[16 + i] = (offset + i) % 251;
      dc.send(fragment);
    }
  };
  const offer = await pc.createOffer();
  await pc.setLocalDescription(offer);
  if (pc.iceGatheringState !== 'complete') {
    await new Promise((resolve) => pc.addEventListener('icegatheringstatechange', () => {
      if (pc.iceGatheringState === 'complete') resolve();
    }));
  }
  const response = await fetch('/offer', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(pc.localDescription),
  });
  await pc.setRemoteDescription(await response.json());
})().catch((error) => {
  document.body.dataset.result = 'fail';
  document.body.textContent = String(error && (error.stack || error));
});
</script></body></html>`
