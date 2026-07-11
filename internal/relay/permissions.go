package relay

import (
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
)

type remotePermission uint8

const (
	permView remotePermission = iota
	permControl
	permFull
)

func parseRemotePermission(value string) remotePermission {
	switch value {
	case proto.RemotePermissionView:
		return permView
	case proto.RemotePermissionControl:
		return permControl
	case proto.RemotePermissionFull, "":
		return permFull
	default:
		return permView
	}
}

func sessionRemotePermission(sess *session.Session) remotePermission {
	if sess == nil {
		return permView
	}
	return parseRemotePermission(sess.Info().RemotePermission)
}

func frameAllowedByPermission(scope authScope, perm remotePermission, typ proto.Type) bool {
	if scope == authRead {
		switch typ {
		case proto.TypeIn, proto.TypeResize, proto.TypePasteImage, proto.TypePasteFile:
			return false
		default:
			return true
		}
	}
	switch typ {
	case proto.TypeIn, proto.TypeResize:
		return perm >= permControl
	case proto.TypePasteImage, proto.TypePasteFile:
		return perm >= permFull
	default:
		return true
	}
}
