package main

import (
	"errors"

	"github.com/attson/atterm/internal/termius"
	"github.com/godbus/dbus/v5"
	ss "github.com/zalando/go-keyring/secret_service"
)

func termiusKeyAttributes(service string) map[string]string {
	// keytar/libsecret uses account, whereas go-keyring.Get searches username.
	return map[string]string{"service": service, "account": "localKey"}
}

func readTermiusKeys() ([][]byte, error) {
	svc, err := ss.NewSecretService()
	if err != nil {
		return nil, errors.New("termius_local_key_unavailable")
	}
	session, err := svc.OpenSession()
	if err != nil {
		return nil, errors.New("termius_local_key_unavailable")
	}
	defer svc.Close(session)
	keys := [][]byte{}
	seen := map[dbus.ObjectPath]bool{}
	for _, service := range []string{"termius-app", "Termius", "Termius (MAS)", "termius", "com.termius-dmg.mac", "com.termius.mac"} {
		var unlocked, locked []dbus.ObjectPath
		// Search the service rather than only the default collection: libsecret
		// entries can live in a user-selected collection.
		err := svc.Object("org.freedesktop.secrets", "/org/freedesktop/secrets").Call("org.freedesktop.Secret.Service.SearchItems", 0, termiusKeyAttributes(service)).Store(&unlocked, &locked)
		if err != nil {
			continue
		}
		for _, item := range locked {
			if svc.Unlock(item) == nil {
				unlocked = append(unlocked, item)
			}
		}
		for _, item := range unlocked {
			if seen[item] {
				continue
			}
			seen[item] = true
			secret, err := svc.GetSecret(item, session.Path())
			if err != nil {
				continue
			}
			key, err := termius.DecodeKey(string(secret.Value))
			clear(secret.Value)
			if err == nil {
				keys = append(keys, key)
			}
		}
	}
	if len(keys) == 0 {
		return nil, errors.New("termius_local_key_unavailable")
	}
	return keys, nil
}
