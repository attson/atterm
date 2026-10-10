package main

import "github.com/attson/atterm/internal/appdir"

const peerTURNCredentialAccount = "active"

func peerTURNCredentialService() string {
	return "com.atterm.peer-turn.v1" + appdir.KeychainSuffix()
}

func peerTURNCredentialSlot() keychainSlot[string] {
	return keychainSlot[string]{
		service: peerTURNCredentialService(),
		account: peerTURNCredentialAccount,
		codec:   stringCodec,
	}
}

func loadPeerTURNCredential() (string, error) {
	return peerTURNCredentialSlot().Load()
}

func savePeerTURNCredential(credential string) error {
	return peerTURNCredentialSlot().Save(credential)
}

func clearPeerTURNCredential() error {
	return peerTURNCredentialSlot().Clear()
}
