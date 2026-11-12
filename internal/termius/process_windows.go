package termius

import (
	"context"
	"encoding/csv"
	"os/exec"
	"strings"
)

func processNames(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "tasklist.exe", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return nil, err
	}
	rows, err := csv.NewReader(strings.NewReader(string(out))).ReadAll()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) > 0 {
			names = append(names, row[0])
		}
	}
	return names, nil
}
