package repository

import (
	"database/sql"
	"fmt"
)

// claimableTables lists every table that carries a nullable user_id for
// pre-account (legacy) rows. Order does not matter: each UPDATE only ever
// touches rows that are currently unowned.
var claimableTables = []string{
	"jobs", "search_periods", "job_technologies", "technologies", "technology_aliases", "cv_versions",
}

// ClaimUnownedData assigns every currently-unowned (user_id IS NULL) row in
// every per-user table to userID, and advances that user's CV version
// sequence past any claimed CV versions so the next upload cannot collide
// with a version number it just adopted.
//
// Call this exactly once, inside the same transaction that inserts the
// first-ever row in users; the caller is responsible for that precondition
// (this function does not check it). Under that precondition it cannot fail
// on a uniqueness constraint: the partial unique indexes on technologies,
// technology_aliases, and cv_versions already enforce install-wide
// uniqueness among unclaimed (user_id IS NULL) rows, and the claimant owns
// no rows yet, so assigning all of them the same new user_id introduces no
// duplicate (user_id, name) pair that was not already forbidden.
func ClaimUnownedData(tx *sql.Tx, userID string) error {
	for _, table := range claimableTables {
		if _, err := tx.Exec(fmt.Sprintf("UPDATE %s SET user_id = ? WHERE user_id IS NULL", table), userID); err != nil { // #nosec G201 -- table is from the fixed claimableTables list, never request input.
			return fmt.Errorf("claim unowned %s rows: %w", table, err)
		}
	}
	return claimCVVersionSequence(tx, userID)
}

func claimCVVersionSequence(tx *sql.Tx, userID string) error {
	var maxClaimedVersion int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM cv_versions WHERE user_id = ?`, userID).Scan(&maxClaimedVersion); err != nil {
		return fmt.Errorf("find highest claimed cv version: %w", err)
	}
	nextVersion := maxClaimedVersion + 1
	if _, err := tx.Exec(`
		INSERT INTO cv_version_sequence (user_id, next_version) VALUES (?, ?)
		ON CONFLICT(user_id) DO UPDATE SET next_version = MAX(next_version, excluded.next_version)
	`, userID, nextVersion); err != nil {
		return fmt.Errorf("seed cv version sequence: %w", err)
	}
	return nil
}
