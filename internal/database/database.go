package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"review-service/internal/models"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"strings"
)

type DB struct {
	pool *pgxpool.Pool
}

func NewDB(connString string) (*DB, error) {
	pool, err := pgxpool.New(context.Background(), connString)
	if err != nil {
		return nil, err
	}

	// Test connection
	if err := pool.Ping(context.Background()); err != nil {
		return nil, err
	}

	return &DB{pool: pool}, nil
}

func (db *DB) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

// Team methods
func (db *DB) CreateTeam(ctx context.Context, team *models.Team) error {
	query := `INSERT INTO teams (name) VALUES ($1)`
	_, err := db.pool.Exec(ctx, query, team.TeamName)
	return err
}

func (db *DB) GetTeamByName(ctx context.Context, name string) (*models.Team, error) {
	var team models.Team
	query := `SELECT name FROM teams WHERE name = $1`
	err := db.pool.QueryRow(ctx, query, name).Scan(&team.TeamName)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("team not found")
		}
		return nil, err
	}

	// Get team members
	membersQuery := `SELECT user_id, username, is_active FROM users WHERE team_name = $1`
	rows, err := db.pool.Query(ctx, membersQuery, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	team.Members = []models.TeamMember{}
	for rows.Next() {
		var member models.TeamMember
		if err := rows.Scan(&member.UserID, &member.Username, &member.IsActive); err != nil {
			return nil, err
		}
		team.Members = append(team.Members, member)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &team, nil
}

func (db *DB) TeamExists(ctx context.Context, name string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM teams WHERE name = $1)`
	err := db.pool.QueryRow(ctx, query, name).Scan(&exists)
	return exists, err
}

// User methods
func (db *DB) CreateOrUpdateUser(ctx context.Context, user *models.User) error {
	query := `INSERT INTO users (user_id, username, team_name, is_active) 
              VALUES ($1, $2, $3, $4)
              ON CONFLICT (user_id) DO UPDATE SET 
              username = EXCLUDED.username, 
              team_name = EXCLUDED.team_name, 
              is_active = EXCLUDED.is_active`
	_, err := db.pool.Exec(ctx, query, user.UserID, user.Username, user.TeamName, user.IsActive)
	return err
}

func (db *DB) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	var user models.User
	query := `SELECT user_id, username, team_name, is_active FROM users WHERE user_id = $1`
	err := db.pool.QueryRow(ctx, query, userID).Scan(&user.UserID, &user.Username, &user.TeamName, &user.IsActive)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("user not found")
		}
		return nil, err
	}
	return &user, nil
}

func (db *DB) UpdateUser(ctx context.Context, user *models.User) error {
	query := `UPDATE users SET username = $1, team_name = $2, is_active = $3 WHERE user_id = $4`
	result, err := db.pool.Exec(ctx, query, user.Username, user.TeamName, user.IsActive, user.UserID)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}

func (db *DB) GetActiveUsersByTeam(ctx context.Context, teamName string, excludeUserID string) ([]models.User, error) {
	query := `SELECT user_id, username, team_name, is_active 
              FROM users 
              WHERE team_name = $1 AND is_active = true AND user_id != $2`
	rows, err := db.pool.Query(ctx, query, teamName, excludeUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(&user.UserID, &user.Username, &user.TeamName, &user.IsActive)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

func (db *DB) UserExists(ctx context.Context, userID string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM users WHERE user_id = $1)`
	err := db.pool.QueryRow(ctx, query, userID).Scan(&exists)
	return exists, err
}

// PR methods
func (db *DB) CreatePR(ctx context.Context, pr *models.PullRequest) error {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Insert PR
	query := `INSERT INTO pull_requests (pull_request_id, pull_request_name, author_id, status, created_at) 
              VALUES ($1, $2, $3, $4, $5)`
	_, err = tx.Exec(ctx, query, pr.PullRequestID, pr.PullRequestName, pr.AuthorID, pr.Status, pr.CreatedAt)
	if err != nil {
		return err
	}

	// Insert reviewers
	for _, reviewerID := range pr.AssignedReviewers {
		_, err = tx.Exec(ctx,
			`INSERT INTO pr_reviewers (pr_id, reviewer_id) VALUES ($1, $2)`,
			pr.PullRequestID, reviewerID)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (db *DB) GetPRByID(ctx context.Context, prID string) (*models.PullRequest, error) {
	var pr models.PullRequest
	var createdAt, mergedAt sql.NullTime

	query := `SELECT pull_request_id, pull_request_name, author_id, status, created_at, merged_at 
              FROM pull_requests WHERE pull_request_id = $1`
	err := db.pool.QueryRow(ctx, query, prID).Scan(
		&pr.PullRequestID, &pr.PullRequestName, &pr.AuthorID, &pr.Status, &createdAt, &mergedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("PR not found")
		}
		return nil, err
	}

	// Set timestamps if they exist
	if createdAt.Valid {
		pr.CreatedAt = &createdAt.Time
	}
	if mergedAt.Valid {
		pr.MergedAt = &mergedAt.Time
	}

	// Get reviewers
	reviewersQuery := `SELECT reviewer_id FROM pr_reviewers WHERE pr_id = $1`
	rows, err := db.pool.Query(ctx, reviewersQuery, prID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pr.AssignedReviewers = []string{}
	for rows.Next() {
		var reviewerID string
		if err := rows.Scan(&reviewerID); err != nil {
			return nil, err
		}
		pr.AssignedReviewers = append(pr.AssignedReviewers, reviewerID)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &pr, nil
}

func (db *DB) UpdatePR(ctx context.Context, pr *models.PullRequest) error {
	query := `UPDATE pull_requests 
              SET pull_request_name = $1, author_id = $2, status = $3, merged_at = $4 
              WHERE pull_request_id = $5`

	result, err := db.pool.Exec(ctx, query,
		pr.PullRequestName, pr.AuthorID, pr.Status, pr.MergedAt, pr.PullRequestID)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("PR not found")
	}

	return nil
}

func (db *DB) UpdatePRStatus(ctx context.Context, prID string, status models.PullRequestStatus) error {
	var mergedAt interface{}
	if status == models.PRStatusMerged {
		mergedAt = time.Now()
	} else {
		mergedAt = nil
	}

	query := `UPDATE pull_requests SET status = $1, merged_at = $2 WHERE pull_request_id = $3`
	result, err := db.pool.Exec(ctx, query, status, mergedAt, prID)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("PR not found")
	}

	return nil
}

func (db *DB) UpdatePRReviewers(ctx context.Context, prID string, reviewers []string) error {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Delete existing reviewers
	_, err = tx.Exec(ctx, `DELETE FROM pr_reviewers WHERE pr_id = $1`, prID)
	if err != nil {
		return err
	}

	// Insert new reviewers
	for _, reviewerID := range reviewers {
		_, err = tx.Exec(ctx,
			`INSERT INTO pr_reviewers (pr_id, reviewer_id) VALUES ($1, $2)`,
			prID, reviewerID)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (db *DB) ReplaceReviewer(ctx context.Context, prID, oldReviewerID, newReviewerID string) error {
	query := `UPDATE pr_reviewers SET reviewer_id = $1 WHERE pr_id = $2 AND reviewer_id = $3`
	result, err := db.pool.Exec(ctx, query, newReviewerID, prID, oldReviewerID)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("reviewer not found in PR")
	}

	return nil
}

func (db *DB) GetPRsByReviewer(ctx context.Context, reviewerID string) ([]models.PullRequest, error) {
	query := `SELECT p.pull_request_id, p.pull_request_name, p.author_id, p.status, p.created_at, p.merged_at
              FROM pull_requests p
              JOIN pr_reviewers pr ON p.pull_request_id = pr.pr_id
              WHERE pr.reviewer_id = $1`

	rows, err := db.pool.Query(ctx, query, reviewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prs []models.PullRequest
	for rows.Next() {
		var pr models.PullRequest
		var createdAt, mergedAt sql.NullTime

		err := rows.Scan(&pr.PullRequestID, &pr.PullRequestName, &pr.AuthorID, &pr.Status, &createdAt, &mergedAt)
		if err != nil {
			return nil, err
		}

		// Set timestamps
		if createdAt.Valid {
			pr.CreatedAt = &createdAt.Time
		}
		if mergedAt.Valid {
			pr.MergedAt = &mergedAt.Time
		}

		// Get reviewers for this PR
		reviewerRows, err := db.pool.Query(ctx,
			`SELECT reviewer_id FROM pr_reviewers WHERE pr_id = $1`, pr.PullRequestID)
		if err != nil {
			return nil, err
		}

		pr.AssignedReviewers = []string{}
		for reviewerRows.Next() {
			var reviewerID string
			if err := reviewerRows.Scan(&reviewerID); err != nil {
				reviewerRows.Close()
				return nil, err
			}
			pr.AssignedReviewers = append(pr.AssignedReviewers, reviewerID)
		}
		reviewerRows.Close()

		prs = append(prs, pr)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return prs, nil
}

func (db *DB) PRExists(ctx context.Context, prID string) (bool, error) {
	var exists bool
	query := `SELECT EXISTS(SELECT 1 FROM pull_requests WHERE pull_request_id = $1)`
	err := db.pool.QueryRow(ctx, query, prID).Scan(&exists)
	return exists, err
}

func (db *DB) IsReviewerAssigned(ctx context.Context, prID, reviewerID string) (bool, error) {
	var assigned bool
	query := `SELECT EXISTS(SELECT 1 FROM pr_reviewers WHERE pr_id = $1 AND reviewer_id = $2)`
	err := db.pool.QueryRow(ctx, query, prID, reviewerID).Scan(&assigned)
	return assigned, err
}

// Health check
func (db *DB) HealthCheck(ctx context.Context) error {
	return db.pool.Ping(ctx)
}

// Migration and initialization
func (db *DB) InitSchema(ctx context.Context) error {
	// Check if tables already exist
	var tablesExist bool
	query := `SELECT EXISTS(
        SELECT FROM information_schema.tables 
        WHERE table_schema = 'public' AND table_name = 'teams'
    )`
	err := db.pool.QueryRow(ctx, query).Scan(&tablesExist)
	if err != nil {
		return err
	}

	if tablesExist {
		return nil // Tables already exist
	}

	// Execute SQL migration file
	sqlContent, err := os.ReadFile("migrations/001_init.sql")
	if err != nil {
		return fmt.Errorf("failed to read migration file: %w", err)
	}

	// Split SQL by queries
	queries := strings.Split(string(sqlContent), ";")

	for _, query := range queries {
		query = strings.TrimSpace(query)
		if query == "" {
			continue
		}

		// Add semicolon back
		query = query + ";"

		_, err := db.pool.Exec(ctx, query)
		if err != nil {
			return fmt.Errorf("failed to execute query: %s, error: %w", query, err)
		}
	}

	return nil
}
