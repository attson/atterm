package relay

import (
	"errors"
	"time"
	_ "time/tzdata"
)

type trafficDayRange struct {
	fromDay string
	toDay   string
	from    time.Time
	to      time.Time
	zone    *time.Location
}

func parseTrafficDayRange(from, to, zoneName string, maxDays int) (trafficDayRange, error) {
	zone, err := time.LoadLocation(zoneName)
	if err != nil || zoneName == "" {
		return trafficDayRange{}, errors.New("invalid time zone")
	}
	parse := func(value string) (time.Time, error) {
		parsed, err := time.ParseInLocation(time.DateOnly, value, zone)
		if err != nil || parsed.Format(time.DateOnly) != value {
			return time.Time{}, errors.New("invalid date")
		}
		return parsed, nil
	}
	fromTime, err := parse(from)
	if err != nil {
		return trafficDayRange{}, err
	}
	toTime, err := parse(to)
	if err != nil || fromTime.After(toTime) {
		return trafficDayRange{}, errors.New("invalid date range")
	}
	days := 1
	for cursor := fromTime; cursor.Before(toTime); days++ {
		cursor = cursor.AddDate(0, 0, 1)
		if days > maxDays {
			return trafficDayRange{}, errors.New("invalid date range")
		}
	}
	return trafficDayRange{
		fromDay: from,
		toDay:   to,
		from:    fromTime,
		to:      toTime.AddDate(0, 0, 1),
		zone:    zone,
	}, nil
}

func trafficLocalDay(timestamp int64, zone *time.Location) string {
	return time.Unix(timestamp, 0).In(zone).Format(time.DateOnly)
}
