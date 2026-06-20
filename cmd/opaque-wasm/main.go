//go:build js && wasm

// Command opaque-wasm is the browser-side OPAQUE client: the SAME
// github.com/bytemare/opaque client the desktop SDK (internal/e2eeclient) and
// the relay server use, compiled to WebAssembly so the web client interoperates
// byte-for-byte with them. It replaces @cloudflare/opaque-ts, which derives the
// client AKE keypair differently and is therefore NOT cross-client compatible.
//
// It exposes four functions on globalThis.attermOpaque. The JS side does all
// HTTP, base64, and the (separate, noble-based) account-key AEAD wrap; this
// module only runs the OPAQUE protocol crypto + message (de)serialization.
//
//	registerInit(password)                              -> {handle, ke1}
//	registerFinish(handle, ke2, serverId, clientId)     -> {record, exportKey}
//	loginInit(password)                                 -> {handle, ke1}
//	loginFinish(handle, ke2, serverId, clientId)        -> {ke3, exportKey, sessionKey}
//
// All byte payloads cross the boundary as standard-base64 strings. The bytemare
// client is stateful between init and finish (it holds the blind / ephemeral
// key / nonce), so init returns an integer handle into a client map and finish
// consumes it.
package main

import (
	"encoding/base64"
	"sync"
	"syscall/js"

	"github.com/attson/atterm/internal/opaquesuite"
	"github.com/bytemare/opaque"
)

var (
	mu      sync.Mutex
	clients = map[int]*opaque.Client{}
	nextH   int
)

func store(cl *opaque.Client) int {
	mu.Lock()
	defer mu.Unlock()
	nextH++
	clients[nextH] = cl
	return nextH
}

// take returns and removes the client for handle h (single-use init→finish).
func take(h int) *opaque.Client {
	mu.Lock()
	defer mu.Unlock()
	cl := clients[h]
	delete(clients, h)
	return cl
}

func b64e(b []byte) string          { return base64.StdEncoding.EncodeToString(b) }
func b64d(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }

func errResult(msg string) any { return map[string]any{"error": msg} }

func registerInit(_ js.Value, args []js.Value) (ret any) {
	defer func() {
		if r := recover(); r != nil {
			ret = errResult("registerInit failed")
		}
	}()
	if len(args) < 1 {
		return errResult("registerInit: missing password")
	}
	cl, err := opaquesuite.Config().Client()
	if err != nil {
		return errResult("opaque client: " + err.Error())
	}
	ke1 := cl.RegistrationInit([]byte(args[0].String()))
	return map[string]any{"handle": store(cl), "ke1": b64e(ke1.Serialize())}
}

func registerFinish(_ js.Value, args []js.Value) (ret any) {
	defer func() {
		if r := recover(); r != nil {
			ret = errResult("registerFinish failed")
		}
	}()
	if len(args) < 4 {
		return errResult("registerFinish: missing args")
	}
	cl := take(args[0].Int())
	if cl == nil {
		return errResult("registerFinish: unknown handle")
	}
	respBytes, err := b64d(args[1].String())
	if err != nil {
		return errResult("registerFinish: bad ke2")
	}
	resp, err := cl.Deserialize.RegistrationResponse(respBytes)
	if err != nil {
		return errResult("registerFinish: decode response: " + err.Error())
	}
	record, exportKey := cl.RegistrationFinalize(resp, opaque.ClientRegistrationFinalizeOptions{
		ServerIdentity: []byte(args[2].String()),
		ClientIdentity: []byte(args[3].String()),
	})
	return map[string]any{"record": b64e(record.Serialize()), "exportKey": b64e(exportKey)}
}

func loginInit(_ js.Value, args []js.Value) (ret any) {
	defer func() {
		if r := recover(); r != nil {
			ret = errResult("loginInit failed")
		}
	}()
	if len(args) < 1 {
		return errResult("loginInit: missing password")
	}
	cl, err := opaquesuite.Config().Client()
	if err != nil {
		return errResult("opaque client: " + err.Error())
	}
	ke1 := cl.LoginInit([]byte(args[0].String()))
	return map[string]any{"handle": store(cl), "ke1": b64e(ke1.Serialize())}
}

func loginFinish(_ js.Value, args []js.Value) (ret any) {
	defer func() {
		if r := recover(); r != nil {
			ret = errResult("loginFinish failed")
		}
	}()
	if len(args) < 4 {
		return errResult("loginFinish: missing args")
	}
	cl := take(args[0].Int())
	if cl == nil {
		return errResult("loginFinish: unknown handle")
	}
	ke2Bytes, err := b64d(args[1].String())
	if err != nil {
		return errResult("loginFinish: bad ke2")
	}
	ke2, err := cl.Deserialize.KE2(ke2Bytes)
	if err != nil {
		return errResult("loginFinish: decode ke2: " + err.Error())
	}
	ke3, exportKey, err := cl.LoginFinish(ke2, opaque.ClientLoginFinishOptions{
		ServerIdentity: []byte(args[2].String()),
		ClientIdentity: []byte(args[3].String()),
	})
	if err != nil {
		// Library-detected wrong password — server never finalizes.
		return errResult("invalid credentials")
	}
	return map[string]any{
		"ke3":        b64e(ke3.Serialize()),
		"exportKey":  b64e(exportKey),
		"sessionKey": b64e(cl.SessionKey()),
	}
}

func main() {
	js.Global().Set("attermOpaque", js.ValueOf(map[string]any{
		"registerInit":   js.FuncOf(registerInit),
		"registerFinish": js.FuncOf(registerFinish),
		"loginInit":      js.FuncOf(loginInit),
		"loginFinish":    js.FuncOf(loginFinish),
	}))
	// Keep the Go runtime alive so the exported funcs stay callable. The Set
	// above has already run, so JS can use globalThis.attermOpaque as soon as
	// go.run() yields here.
	select {}
}
