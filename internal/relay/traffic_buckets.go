package relay

import (
	"net/http"
	"sort"
	"time"

	"github.com/attson/atterm/internal/proto"
)

// handleTrafficBucketsHTTP shares range validation and metadata between account
// and admin views. Personal queries always supply the authenticated user ID.
func (s *Server) handleTrafficBucketsHTTP(w http.ResponseWriter, r *http.Request, userID, bucket string) {
	now := time.Now().UTC()
	seconds, retention := int64(60), 7
	if bucket == "hour" {
		seconds, retention = 3600, 90
	}
	from, errFrom := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	to, errTo := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	earliest := now.Truncate(24*time.Hour).AddDate(0, 0, -retention)
	if errFrom != nil || errTo != nil || !from.Before(to) || to.Sub(from) > 48*time.Hour || from.Before(earliest) || to.After(now.Truncate(24*time.Hour).Add(24*time.Hour)) {
		http.Error(w, "invalid time range or outside retention", http.StatusBadRequest)
		return
	}
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "detail"
	}
	if view != "detail" && view != "group" && view != "summary" {
		http.Error(w, "invalid view", http.StatusBadRequest)
		return
	}
	queryUser := userID
	if queryUser == "" {
		queryUser = r.URL.Query().Get("user_id")
	}
	rows, err := s.cfg.Store.QueryTrafficBuckets(r.Context(), queryUser, bucket, from.Unix()/seconds*seconds, to.Unix())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	updated := int64(0)
	details := make([]meRelayTrafficDetail, 0, len(rows))
	adminRows := make([]trafficRow, 0, len(rows))
	emails := map[string]string{}
	if userID == "" {
		emails = s.userEmailMap(r)
	}
	type key struct {
		user, time, category string
		frame, direction     int
	}
	grouped := make(map[key]*trafficRow)
	for _, row := range rows {
		if row.UpdatedAt > updated {
			updated = row.UpdatedAt
		}
		ft := proto.Type(row.FrameType)
		if userID != "" {
			details = append(details, meRelayTrafficDetail{Day: row.Day, ClientID: row.ClientID, ClientName: row.ClientName, ClientKind: row.ClientKind, FrameType: row.FrameType, FrameTypeName: frameTypeName(ft), Category: frameCategory(ft), Direction: row.Direction, Bytes: row.Bytes, Frames: row.Frames})
		} else {
			k := key{user: row.UserID, time: row.Day, direction: row.Direction}
			if view == "detail" {
				k.frame = row.FrameType
			} else if view == "group" {
				k.category = frameCategory(ft)
			}
			cell := grouped[k]
			if cell == nil {
				cell = &trafficRow{UserID: row.UserID, Email: emails[row.UserID], Day: row.Day, Direction: row.Direction}
				if view == "detail" {
					cell.FrameType = row.FrameType
					cell.FrameTypeName = frameTypeName(ft)
					cell.Category = frameCategory(ft)
				} else {
					cell.Category = k.category
				}
				grouped[k] = cell
			}
			cell.Bytes += row.Bytes
			cell.Frames += row.Frames
		}
	}
	updatedAt := ""
	if updated != 0 {
		updatedAt = time.Unix(updated, 0).UTC().Format(time.RFC3339)
	}
	result := map[string]any{"from": from.Format(time.RFC3339), "to": to.Format(time.RFC3339), "bucket": bucket, "bucket_seconds": seconds,
		"updated_at": updatedAt, "server_time": now.Format(time.RFC3339), "retention_from": earliest.Format(time.RFC3339), "flush_interval_seconds": int(trafficFlushInterval.Seconds())}
	if userID != "" {
		result["relay_detail"] = details
		result["relay"] = []meRelayTrafficDay{}
		result["direct"] = []meDirectTrafficDay{}
	} else {
		for _, row := range grouped {
			adminRows = append(adminRows, *row)
		}
		sort.Slice(adminRows, func(i, j int) bool {
			if adminRows[i].Day != adminRows[j].Day {
				return adminRows[i].Day < adminRows[j].Day
			}
			if adminRows[i].UserID != adminRows[j].UserID {
				return adminRows[i].UserID < adminRows[j].UserID
			}
			if adminRows[i].FrameType != adminRows[j].FrameType {
				return adminRows[i].FrameType < adminRows[j].FrameType
			}
			if adminRows[i].Category != adminRows[j].Category {
				return adminRows[i].Category < adminRows[j].Category
			}
			return adminRows[i].Direction < adminRows[j].Direction
		})
		result["rows"] = adminRows
		result["view"] = view
	}
	writeJSONStatus(w, http.StatusOK, result)
}
