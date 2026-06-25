package userstore

import (
	"context"
	"fmt"
)

// RelayInstance is one row of the relay_instances heartbeat registry. The
// instance_id is the node's configured public URL.
type RelayInstance struct {
	InstanceID    string
	PublicURL     string
	LastHeartbeat int64
}

// UpsertInstanceHeartbeat records (or refreshes) this node's liveness row.
func (s *DBStore) UpsertInstanceHeartbeat(ctx context.Context, instanceID, publicURL string, nowUnix int64) error {
	_, err := s.db.ExecContext(ctx, s.dia.Rebind(
		`INSERT INTO relay_instances(instance_id, public_url, last_heartbeat)
		 VALUES (?, ?, ?)
		 ON CONFLICT(instance_id) DO UPDATE SET
		     public_url     = excluded.public_url,
		     last_heartbeat = excluded.last_heartbeat`),
		instanceID, publicURL, nowUnix)
	if err != nil {
		return fmt.Errorf("upsert instance heartbeat: %w", err)
	}
	return nil
}

// ListLiveInstances returns all instances whose last_heartbeat >= minHeartbeat,
// ordered by instance_id.
func (s *DBStore) ListLiveInstances(ctx context.Context, minHeartbeat int64) ([]RelayInstance, error) {
	rows, err := s.db.QueryContext(ctx, s.dia.Rebind(
		`SELECT instance_id, public_url, last_heartbeat
		 FROM relay_instances WHERE last_heartbeat >= ? ORDER BY instance_id`), minHeartbeat)
	if err != nil {
		return nil, fmt.Errorf("list live instances: %w", err)
	}
	defer rows.Close()
	var out []RelayInstance
	for rows.Next() {
		var inst RelayInstance
		if err := rows.Scan(&inst.InstanceID, &inst.PublicURL, &inst.LastHeartbeat); err != nil {
			return nil, fmt.Errorf("scan instance: %w", err)
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}
