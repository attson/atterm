package peerlan

import (
	"bytes"
	"net"
	"strconv"
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

func TestParseEntryRequiresVersionTagAddressAndPort(t *testing.T) {
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

func TestParseEntryAcceptsIPv6AndPreservesLinkLocalZone(t *testing.T) {
	tag := "0123456789abcdef0123456789abcdef"
	for _, test := range []struct {
		name string
		addr *net.IPAddr
		zone string
	}{
		{name: "global", addr: &net.IPAddr{IP: net.ParseIP("2001:db8::10")}},
		{name: "link local", addr: &net.IPAddr{IP: net.ParseIP("fe80::10"), Zone: "en0"}, zone: "en0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry, ok := parseEntry(&mdns.ServiceEntry{
				AddrV6IPAddr: test.addr, Port: 8484,
				InfoFields: []string{"v=1", "tag=" + tag},
			})
			if !ok || entry.Tag != tag || !entry.IP.Equal(test.addr.IP) || entry.Zone != test.zone || entry.Port != 8484 {
				t.Fatalf("entry=%+v ok=%t", entry, ok)
			}
		})
	}

	for _, invalid := range []*net.IPAddr{
		{IP: net.IPv6zero},
		{IP: net.ParseIP("ff02::fb"), Zone: "en0"},
		{IP: net.ParseIP("fe80::10")},
		{IP: net.ParseIP("fe80::10"), Zone: "en 0"},
	} {
		if _, ok := parseEntry(&mdns.ServiceEntry{
			AddrV6IPAddr: invalid, Port: 8484,
			InfoFields: []string{"v=1", "tag=" + tag},
		}); ok {
			t.Fatalf("accepted invalid IPv6 address %+v", invalid)
		}
	}
}

func TestEntryKeyDistinguishesIPv6Zones(t *testing.T) {
	base := Entry{
		Tag: "0123456789abcdef0123456789abcdef",
		IP:  net.ParseIP("fe80::10"), Port: 8484,
	}
	first, second := base, base
	first.Zone = "en0"
	second.Zone = "en1"
	if entryKey(first) == entryKey(second) {
		t.Fatal("link-local entries on different interfaces were deduplicated")
	}
}

func TestInterfaceForZoneAcceptsNameAndNumericIndex(t *testing.T) {
	interfaces, err := net.Interfaces()
	if err != nil || len(interfaces) == 0 {
		t.Skip("no local network interface is available")
	}
	want := interfaces[0]
	for _, zone := range []string{want.Name, strconv.Itoa(want.Index)} {
		got, err := interfaceForZone(zone)
		if err != nil || got.Index != want.Index {
			t.Fatalf("interfaceForZone(%q)=%+v, %v; want index %d", zone, got, err, want.Index)
		}
	}
}
