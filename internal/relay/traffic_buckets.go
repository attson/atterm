package relay

import (
	"net/http"
	"sort"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/userstore"
)

// handleTrafficBucketsHTTP shares range validation and metadata between account
// and admin views. Personal queries always supply the authenticated user ID.
func (s *Server) handleTrafficBucketsHTTP(w http.ResponseWriter, r *http.Request, userID, bucket string) {
	now := time.Now().UTC()
	zoneName := r.URL.Query().Get("tz")
	var reportingZone *time.Location
	if zoneName != "" {
		var err error
		reportingZone, err = time.LoadLocation(zoneName)
		if err != nil {
			http.Error(w, "invalid time zone", http.StatusBadRequest)
			return
		}
	}
	seconds, retention := int64(60), 7
	if bucket == "hour" {
		seconds, retention = 3600, 90
	}
	from, errFrom := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	to, errTo := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	earliest := now.Truncate(24*time.Hour).AddDate(0, 0, -retention)
	latest := now.Truncate(24 * time.Hour).Add(24 * time.Hour)
	if reportingZone != nil {
		localNow := now.In(reportingZone)
		localToday := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, reportingZone)
		earliest = localToday.AddDate(0, 0, -retention)
		latest = localToday.AddDate(0, 0, 1)
	}
	if errFrom != nil || errTo != nil || !from.Before(to) || to.Sub(from) > 48*time.Hour || from.Before(earliest) || to.After(latest) {
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
	queryBucket := bucket
	querySeconds := seconds
	queryFrom := from.Unix() / seconds * seconds
	if zoneName != "" && bucket == "hour" {
		// Local midnights in half/quarter-hour zones do not align with UTC hour
		// buckets. Rebuild reporting hours from absolute quarter buckets.
		queryBucket = "quarter"
		querySeconds = 900
		queryFrom = from.Unix() / querySeconds * querySeconds
	}
	rows, err := s.cfg.Store.QueryTrafficBuckets(r.Context(), queryUser, queryBucket, queryFrom, to.Unix())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if queryBucket == "quarter" {
		rows = aggregateTrafficBuckets(rows, from.Unix(), seconds)
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
	if zoneName != "" {
		result["time_zone"] = zoneName
		result["day_basis"] = "time_zone"
		if coverage, err := s.cfg.Store.TrafficBucketCoverageStart(r.Context()); err == nil {
			result["local_coverage_from"] = time.Unix(coverage, 0).UTC().Format(time.RFC3339)
		}
	}
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

func aggregateTrafficBuckets(rows []userstore.TrafficRow, origin, seconds int64) []userstore.TrafficRow {
	type key struct {
		startedAt            int64
		userID, clientID     string
		frameType, direction int
	}
	aggregated := make(map[key]*userstore.TrafficRow)
	for _, row := range rows {
		startedAt, err := time.Parse(time.RFC3339, row.Day)
		if err != nil || startedAt.Unix() < origin {
			continue
		}
		bucketStart := origin + (startedAt.Unix()-origin)/seconds*seconds
		k := key{startedAt: bucketStart, userID: row.UserID, clientID: row.ClientID, frameType: row.FrameType, direction: row.Direction}
		cell := aggregated[k]
		if cell == nil {
			copyRow := row
			copyRow.Day = time.Unix(bucketStart, 0).UTC().Format(time.RFC3339)
			copyRow.Bytes = 0
			copyRow.Frames = 0
			cell = &copyRow
			aggregated[k] = cell
		}
		cell.Bytes += row.Bytes
		cell.Frames += row.Frames
		if row.UpdatedAt > cell.UpdatedAt {
			cell.UpdatedAt = row.UpdatedAt
		}
	}
	out := make([]userstore.TrafficRow, 0, len(aggregated))
	for _, row := range aggregated {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day < out[j].Day
		}
		if out[i].UserID != out[j].UserID {
			return out[i].UserID < out[j].UserID
		}
		if out[i].ClientID != out[j].ClientID {
			return out[i].ClientID < out[j].ClientID
		}
		if out[i].FrameType != out[j].FrameType {
			return out[i].FrameType < out[j].FrameType
		}
		return out[i].Direction < out[j].Direction
	})
	return out
}
