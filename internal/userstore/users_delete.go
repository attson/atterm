package userstore

import (
	"context"
	"fmt"
)

// DeleteUser hard-deletes userID. Wraps the invitation-null + user-delete
// pair in a transaction so a partial failure doesn't leave the DB in a
// half-deleted state.
func (s *DBStore) DeleteUser(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx,
		s.dia.Rebind(`UPDATE invitations SET consumed_by = NULL WHERE consumed_by = ?`),
		userID,
	); err != nil {
		return fmt.Errorf("null invitations.consumed_by: %w", err)
	}
	if _, err = tx.ExecContext(ctx,
		s.dia.Rebind(`DELETE FROM users WHERE id = ?`),
		userID,
	); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit delete user: %w", err)
	}
	return nil
}
