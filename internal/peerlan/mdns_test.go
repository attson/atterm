package peerlan

import (
	"bytes"
	"net"
	"testing"

	"github.com/attson/atterm/internal/configsync"
	"github.com/hashicorp/mdns"
)

func TestDeriveTagIsOpaqueAndBoundToEpochSpaceAndPeer(t *testing.T) {
	key, err := configsync.ParseEpochKey(configsync.KeyClassSync, 3, bytes.Repeat([]byte{7}, configsync.EpochKeySize))
	if err != nil {
		t.Fatal(err)
	}
	tag, err := DeriveTag(key, "space-one", "stable-peer-id")
	if err != nil {
		t.Fatal(err)
	}
	if !validTag(tag) || tag == "stable-peer-id" {
		t.Fatalf("tag=%q", tag)
	}
	again, _ := DeriveTag(key, "space-one", "stable-peer-id")
	otherPeer, _ := DeriveTag(key, "space-one", "other-peer")
	otherSpace, _ := DeriveTag(key, "space-two", "stable-peer-id")
	otherEpoch, _ := configsync.ParseEpochKey(configsync.KeyClassSync, 4, key.Bytes())
	rotated, _ := DeriveTag(otherEpoch, "space-one", "stable-peer-id")
	if again != tag || otherPeer == tag || otherSpace == tag || rotated == tag {
		t.Fatalf("tag binding failed: tag=%q again=%q peer=%q space=%q epoch=%q", tag, again, otherPeer, otherSpace, rotated)
	}
}

func TestParseEntryRequiresVersionTagIPv4AndPort(t *testing.T) {
	tag := "0123456789abcdef0123456789abcdef"
	entry, ok := parseEntry(&mdns.ServiceEntry{
		AddrV4: net.ParseIP("192.0.2.10"), Port: 8484,
		InfoFields: []string{"v=1", "tag=" + tag},
	})
	if !ok || entry.Tag != tag || entry.IP.String() != "192.0.2.10" || entry.Port != 8484 {
		t.Fatalf("entry=%+v ok=%t", entry, ok)
	}
	for _, invalid := range []*mdns.ServiceEntry{
		{AddrV4: net.ParseIP("192.0.2.10"), Port: 8484, InfoFields: []string{"v=2", "tag=" + tag}},
		{AddrV4: net.ParseIP("192.0.2.10"), Port: 8484, InfoFields: []string{"v=1", "tag=peer-id"}},
		{AddrV4: net.IPv4zero, Port: 8484, InfoFields: []string{"v=1", "tag=" + tag}},
		{AddrV4: net.ParseIP("192.0.2.10"), Port: 0, InfoFields: []string{"v=1", "tag=" + tag}},
	} {
		if _, ok := parseEntry(invalid); ok {
			t.Fatalf("accepted invalid entry %+v", invalid)
		}
	}
}
