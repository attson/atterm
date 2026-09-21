package relay

import (
	"net/http"
	"sort"
	"time"

	"github.com/attson/atterm/internal/proto"
)

type trafficRow struct {
	UserID        string `json:"user_id"`
	Email         string `json:"email,omitempty"`
	Day           string `json:"day,omitempty"`
	FrameType     int    `json:"frame_type,omitempty"`
	FrameTypeName string `json:"frame_type_name,omitempty"`
	Category      string `json:"category,omitempty"`
	Direction     int    `json:"direction"`
	Bytes         int64  `json:"bytes"`
	Frames        int64  `json:"frames"`
}

type trafficResponse struct {
	View   string       `json:"view"`
	Bucket string       `json:"bucket"`
	From   string       `json:"from"`
	To     string       `json:"to"`
	Rows   []trafficRow `json:"rows"`
}

// handleAdminTrafficHTTP implements GET /admin/api/traffic. Query params:
//
//	view = detail | group | summary   (default: group)
//	bucket = range | day               (default: range)
//	from, to = YYYY-MM-DD              (default: last 7 days ending today UTC)
//
// It reads the daily rollup rows for [from, to] and aggregates them by the
// requested view: detail keeps per-frame-type granularity, group folds frame
// types into semantic categories (frameCategory), and summary collapses to
// per-user-per-direction totals.
func (s *Server) handleAdminTrafficHTTP(w http.ResponseWriter, r *http.Request) {
	view := r.URL.Query().Get("view")
	switch view {
	case "detail", "group", "summary":
	case "":
		view = "group"
	default:
		http.Error(w, "invalid view", http.StatusBadRequest)
		return
	}
	bucket := r.URL.Query().Get("bucket")
	switch bucket {
	case "range", "day":
	case "":
		bucket = "range"
	default:
		http.Error(w, "invalid bucket", http.StatusBadRequest)
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
	if !validDay(from) || !validDay(to) {
		http.Error(w, "invalid date, use YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	if from > to {
		http.Error(w, "invalid date range", http.StatusBadRequest)
		return
	}

	rows, err := s.cfg.Store.QueryTraffic(r.Context(), from, to)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	emails := s.userEmailMap(r)

	// aggKey's disc/cat fields select the grouping columns for the view. For
	// summary both stay zero-valued, so only user+direction distinguish rows.
	type aggKey struct {
		user string
		day  string
		disc int
		cat  string
		dir  int
	}
	agg := map[aggKey]*trafficRow{}
	for _, rr := range rows {
		k := aggKey{user: rr.UserID, dir: rr.Direction}
		if bucket == "day" {
			k.day = rr.Day
		}
		switch view {
		case "detail":
			k.disc = rr.FrameType
		case "group":
			k.cat = frameCategory(proto.Type(rr.FrameType))
		}
		cell := agg[k]
		if cell == nil {
			cell = &trafficRow{UserID: rr.UserID, Email: emails[rr.UserID], Day: k.day, Direction: rr.Direction}
			switch view {
			case "detail":
				cell.FrameType = rr.FrameType
				cell.FrameTypeName = frameTypeName(proto.Type(rr.FrameType))
				cell.Category = frameCategory(proto.Type(rr.FrameType))
			case "group":
				cell.Category = k.cat
			}
			agg[k] = cell
		}
		cell.Bytes += rr.Bytes
		cell.Frames += rr.Frames
	}
	out := make([]trafficRow, 0, len(agg))
	for _, c := range agg {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day < out[j].Day
		}
		if out[i].Email != out[j].Email {
			return out[i].Email < out[j].Email
		}
		if out[i].UserID != out[j].UserID {
			return out[i].UserID < out[j].UserID
		}
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		if out[i].FrameType != out[j].FrameType {
			return out[i].FrameType < out[j].FrameType
		}
		return out[i].Direction < out[j].Direction
	})
	writeJSONStatus(w, http.StatusOK, trafficResponse{View: view, Bucket: bucket, From: from, To: to, Rows: out})
}

func validDay(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// userEmailMap builds a best-effort id->email map for annotating rows. On
// error it returns an empty map (rows just lack emails).
func (s *Server) userEmailMap(r *http.Request) map[string]string {
	m := map[string]string{}
	users, err := s.cfg.Store.ListUsers(r.Context())
	if err != nil {
		return m
	}
	for _, u := range users {
		m[u.ID] = u.Email
	}
	return m
}
