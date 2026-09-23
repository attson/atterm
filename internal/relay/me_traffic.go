package relay

import (
	"net/http"
	"sort"
	"time"
)

const meTrafficMaxDays = 90

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

type meTrafficResponse struct {
	From   string               `json:"from"`
	To     string               `json:"to"`
	Relay  []meRelayTrafficDay  `json:"relay"`
	Direct []meDirectTrafficDay `json:"direct"`
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
	for _, row := range relayRows {
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
		From: from, To: to, Relay: relayDays, Direct: directDays,
	})
}
