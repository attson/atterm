package relay

import (
	"net/http"
	"sort"
	"time"

	"github.com/attson/atterm/internal/proto"
)

const meTrafficMaxDays = 180

type meRelayTrafficDay struct {
	Day       string `json:"day"`
	BytesIn   int64  `json:"bytes_in"`
	BytesOut  int64  `json:"bytes_out"`
	FramesIn  int64  `json:"frames_in"`
	FramesOut int64  `json:"frames_out"`
}

type meDirectTrafficDay struct {
	Day           string `json:"day"`
	Attempts      int64  `json:"attempts"`
	Successes     int64  `json:"successes"`
	Fallbacks     int64  `json:"fallbacks"`
	BytesSent     int64  `json:"bytes_sent"`
	BytesReceived int64  `json:"bytes_received"`
}

type meRelayTrafficDetail struct {
	Day           string `json:"day"`
	ClientID      string `json:"client_id"`
	ClientName    string `json:"client_name"`
	ClientKind    string `json:"client_kind"`
	FrameType     int    `json:"frame_type"`
	FrameTypeName string `json:"frame_type_name"`
	Category      string `json:"category"`
	Direction     int    `json:"direction"`
	Bytes         int64  `json:"bytes"`
	Frames        int64  `json:"frames"`
}

type meTrafficResponse struct {
	From              string                 `json:"from"`
	To                string                 `json:"to"`
	Relay             []meRelayTrafficDay    `json:"relay"`
	RelayDetail       []meRelayTrafficDetail `json:"relay_detail"`
	Direct            []meDirectTrafficDay   `json:"direct"`
	TimeZone          string                 `json:"time_zone,omitempty"`
	DayBasis          string                 `json:"day_basis,omitempty"`
	LocalCoverageFrom string                 `json:"local_coverage_from,omitempty"`
}

// handleMeTrafficHTTP returns the authenticated account's own daily usage.
// The user ID comes exclusively from requireSession and is intentionally
// absent from the response, so the endpoint cannot become a cross-account
// lookup surface.
func (s *Server) handleMeTrafficHTTP(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok || user.ID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if bucket := r.URL.Query().Get("bucket"); bucket == "minute" || bucket == "hour" {
		s.handleTrafficBucketsHTTP(w, r, user.ID, bucket)
		return
	} else if bucket != "" && bucket != "day" {
		http.Error(w, "invalid bucket", http.StatusBadRequest)
		return
	}
	if zoneName := r.URL.Query().Get("tz"); zoneName != "" {
		s.handleMeTrafficInTimeZone(w, r, user.ID, zoneName)
		return
	}
	today := time.Now().UTC()
	to := r.URL.Query().Get("to")
	from := r.URL.Query().Get("from")
	if to == "" {
		to = today.Format("2006-01-02")
	}
	if from == "" {
		from = today.AddDate(0, 0, -6).Format("2006-01-02")
	}
	fromTime, fromErr := time.Parse("2006-01-02", from)
	toTime, toErr := time.Parse("2006-01-02", to)
	if fromErr != nil || toErr != nil {
		http.Error(w, "invalid date, use YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	if fromTime.After(toTime) || int(toTime.Sub(fromTime).Hours()/24)+1 > meTrafficMaxDays {
		http.Error(w, "invalid date range", http.StatusBadRequest)
		return
	}

	relayRows, err := s.cfg.Store.QueryTrafficForUser(r.Context(), user.ID, from, to)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	directRows, err := s.cfg.Store.QueryDirectTraffic(r.Context(), user.ID, from, to)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	relayByDay := make(map[string]*meRelayTrafficDay)
	relayDetail := make([]meRelayTrafficDetail, 0, len(relayRows))
	for _, row := range relayRows {
		relayDetail = append(relayDetail, meRelayTrafficDetail{
			Day:           row.Day,
			ClientID:      row.ClientID,
			ClientName:    row.ClientName,
			ClientKind:    row.ClientKind,
			FrameType:     row.FrameType,
			FrameTypeName: frameTypeName(proto.Type(row.FrameType)),
			Category:      frameCategory(proto.Type(row.FrameType)),
			Direction:     row.Direction,
			Bytes:         row.Bytes,
			Frames:        row.Frames,
		})
		cell := relayByDay[row.Day]
		if cell == nil {
			cell = &meRelayTrafficDay{Day: row.Day}
			relayByDay[row.Day] = cell
		}
		if row.Direction == trafficIn {
			cell.BytesIn += row.Bytes
			cell.FramesIn += row.Frames
		} else if row.Direction == trafficOut {
			cell.BytesOut += row.Bytes
			cell.FramesOut += row.Frames
		}
	}
	relayDays := make([]meRelayTrafficDay, 0, len(relayByDay))
	for _, row := range relayByDay {
		relayDays = append(relayDays, *row)
	}
	sort.Slice(relayDays, func(i, j int) bool { return relayDays[i].Day < relayDays[j].Day })

	directDays := make([]meDirectTrafficDay, 0, len(directRows))
	for _, row := range directRows {
		directDays = append(directDays, meDirectTrafficDay{
			Day:           row.Day,
			Attempts:      row.Attempts,
			Successes:     row.Successes,
			Fallbacks:     row.Fallbacks,
			BytesSent:     row.BytesSent,
			BytesReceived: row.BytesReceived,
		})
	}

	writeJSONStatus(w, http.StatusOK, meTrafficResponse{
		From: from, To: to, Relay: relayDays, RelayDetail: relayDetail, Direct: directDays,
	})
}

func (s *Server) handleMeTrafficInTimeZone(w http.ResponseWriter, r *http.Request, userID, zoneName string) {
	now := time.Now()
	zone, err := time.LoadLocation(zoneName)
	if err != nil {
		http.Error(w, "invalid time zone", http.StatusBadRequest)
		return
	}
	today := now.In(zone)
	to := r.URL.Query().Get("to")
	from := r.URL.Query().Get("from")
	if to == "" {
		to = today.Format(time.DateOnly)
	}
	if from == "" {
		from = today.AddDate(0, 0, -6).Format(time.DateOnly)
	}
	dayRange, err := parseTrafficDayRange(from, to, zoneName, meTrafficMaxDays)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	coverage, err := s.cfg.Store.TrafficBucketCoverageStart(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	queryFrom := dayRange.from.Unix()
	if queryFrom < coverage {
		queryFrom = coverage
	}
	relayRows, err := s.cfg.Store.QueryTrafficBuckets(r.Context(), userID, "quarter", queryFrom, dayRange.to.Unix())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	directRows, err := s.cfg.Store.QueryDirectTrafficBuckets(r.Context(), userID, queryFrom, dayRange.to.Unix())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	relayByDay := make(map[string]*meRelayTrafficDay)
	relayDetails := make(map[string]*meRelayTrafficDetail)
	for _, row := range relayRows {
		startedAt, parseErr := time.Parse(time.RFC3339, row.Day)
		if parseErr != nil {
			continue
		}
		day := trafficLocalDay(startedAt.Unix(), dayRange.zone)
		cell := relayByDay[day]
		if cell == nil {
			cell = &meRelayTrafficDay{Day: day}
			relayByDay[day] = cell
		}
		if row.Direction == trafficIn {
			cell.BytesIn += row.Bytes
			cell.FramesIn += row.Frames
		} else if row.Direction == trafficOut {
			cell.BytesOut += row.Bytes
			cell.FramesOut += row.Frames
		}
		key := day + "\x00" + row.ClientID + "\x00" + string(rune(row.FrameType)) + "\x00" + string(rune(row.Direction))
		detail := relayDetails[key]
		if detail == nil {
			ft := proto.Type(row.FrameType)
			detail = &meRelayTrafficDetail{Day: day, ClientID: row.ClientID, ClientName: row.ClientName, ClientKind: row.ClientKind,
				FrameType: row.FrameType, FrameTypeName: frameTypeName(ft), Category: frameCategory(ft), Direction: row.Direction}
			relayDetails[key] = detail
		}
		detail.Bytes += row.Bytes
		detail.Frames += row.Frames
	}
	relayDays := make([]meRelayTrafficDay, 0, len(relayByDay))
	for _, row := range relayByDay {
		relayDays = append(relayDays, *row)
	}
	sort.Slice(relayDays, func(i, j int) bool { return relayDays[i].Day < relayDays[j].Day })
	details := make([]meRelayTrafficDetail, 0, len(relayDetails))
	for _, row := range relayDetails {
		details = append(details, *row)
	}
	sort.Slice(details, func(i, j int) bool {
		if details[i].Day != details[j].Day {
			return details[i].Day < details[j].Day
		}
		if details[i].ClientID != details[j].ClientID {
			return details[i].ClientID < details[j].ClientID
		}
		if details[i].FrameType != details[j].FrameType {
			return details[i].FrameType < details[j].FrameType
		}
		return details[i].Direction < details[j].Direction
	})
	directByDay := make(map[string]*meDirectTrafficDay)
	for _, row := range directRows {
		day := trafficLocalDay(row.StartedAt, dayRange.zone)
		cell := directByDay[day]
		if cell == nil {
			cell = &meDirectTrafficDay{Day: day}
			directByDay[day] = cell
		}
		cell.Attempts += row.Attempts
		cell.Successes += row.Successes
		cell.Fallbacks += row.Fallbacks
		cell.BytesSent += row.BytesSent
		cell.BytesReceived += row.BytesReceived
	}
	directDays := make([]meDirectTrafficDay, 0, len(directByDay))
	for _, row := range directByDay {
		directDays = append(directDays, *row)
	}
	sort.Slice(directDays, func(i, j int) bool { return directDays[i].Day < directDays[j].Day })
	writeJSONStatus(w, http.StatusOK, meTrafficResponse{
		From: from, To: to, Relay: relayDays, RelayDetail: details, Direct: directDays,
		TimeZone: zoneName, DayBasis: "time_zone", LocalCoverageFrom: time.Unix(coverage, 0).UTC().Format(time.RFC3339),
	})
}
