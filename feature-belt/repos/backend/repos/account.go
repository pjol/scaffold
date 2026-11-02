package repos

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DeleteAccount removes the person's git credentials and review requests
// and takes their name off pull requests, reviews, comments and events.
// What they wrote stays (other people's reviews depend on it) without
// attribution. Commits keep the author name and address recorded in git
// (history can't be rewritten; in-app commits used a no-reply address).
// Repositories in their My drive go with the drive (files purges them,
// which deletes the repository through OnDelete).
func (s *Service) DeleteAccount(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	for _, q := range []string{
		`DELETE FROM repos_git_credentials WHERE user_id = $1`,
		`DELETE FROM repos_review_requests WHERE user_id = $1`,
		`UPDATE repos_review_requests SET requested_by = NULL WHERE requested_by = $1`,
		`UPDATE repos_comments SET author_id = NULL WHERE author_id = $1`,
		`UPDATE repos_comments SET resolved_by = NULL WHERE resolved_by = $1`,
		`UPDATE repos_reviews SET author_id = NULL WHERE author_id = $1`,
		`UPDATE repos_pulls SET author_id = NULL WHERE author_id = $1`,
		`UPDATE repos_pulls SET merged_by = NULL WHERE merged_by = $1`,
		`UPDATE repos_pulls SET closed_by = NULL WHERE closed_by = $1`,
		`UPDATE repos_pull_events SET actor_id = NULL WHERE actor_id = $1`,
		`UPDATE repos_repos SET created_by = NULL WHERE created_by = $1`,
		`UPDATE repos_links SET created_by = NULL WHERE created_by = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return err
		}
	}
	return nil
}

// ExportAccount returns the person's git credentials (never secrets),
// the repositories they made, and the pull requests, reviews and comments
// they wrote.
func (s *Service) ExportAccount(ctx context.Context, userID uuid.UUID) (any, error) {
	type credential struct {
		Name       string     `json:"name"`
		Prefix     string     `json:"prefix"`
		Access     string     `json:"access"`
		ExpiresAt  *time.Time `json:"expires_at"`
		LastUsedAt *time.Time `json:"last_used_at"`
		RevokedAt  *time.Time `json:"revoked_at"`
		CreatedAt  time.Time  `json:"created_at"`
	}
	type repo struct {
		ID        uuid.UUID `json:"id"`
		Slug      string    `json:"slug"`
		CreatedAt time.Time `json:"created_at"`
	}
	type pull struct {
		RepoID    uuid.UUID `json:"repo_id"`
		Number    int       `json:"number"`
		Title     string    `json:"title"`
		Body      string    `json:"body"`
		State     string    `json:"state"`
		Head      string    `json:"head_branch"`
		Base      string    `json:"base_branch"`
		CreatedAt time.Time `json:"created_at"`
	}
	type review struct {
		RepoID    uuid.UUID `json:"repo_id"`
		Number    int       `json:"number"`
		State     string    `json:"state"`
		Body      string    `json:"body"`
		CreatedAt time.Time `json:"created_at"`
	}
	type comment struct {
		RepoID    uuid.UUID `json:"repo_id"`
		Number    int       `json:"number"`
		Body      string    `json:"body"`
		Path      *string   `json:"path"`
		Line      *int      `json:"line"`
		CreatedAt time.Time `json:"created_at"`
	}
	type link struct {
		Provider  string    `json:"provider"`
		WebBase   string    `json:"web_base"`
		Path      string    `json:"path"`
		CreatedAt time.Time `json:"created_at"`
	}
	out := struct {
		Credentials  []credential `json:"git_credentials"`
		Repositories []repo       `json:"repositories_created"`
		Pulls        []pull       `json:"pull_requests"`
		Reviews      []review     `json:"reviews"`
		Comments     []comment    `json:"comments"`
		Links        []link       `json:"connected_links"`
	}{}
	collect := func(sql string, dst any, scan func(pgx.Rows) error) error {
		rows, err := s.d.Pool.Query(ctx, sql, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	if err := collect(`SELECT name, prefix, access, expires_at, last_used_at, revoked_at, created_at FROM repos_git_credentials WHERE user_id = $1 ORDER BY created_at`, nil,
		func(r pgx.Rows) error {
			var c credential
			err := r.Scan(&c.Name, &c.Prefix, &c.Access, &c.ExpiresAt, &c.LastUsedAt, &c.RevokedAt, &c.CreatedAt)
			out.Credentials = append(out.Credentials, c)
			return err
		}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT id, slug, created_at FROM repos_repos WHERE created_by = $1 ORDER BY created_at`, nil,
		func(r pgx.Rows) error {
			var x repo
			err := r.Scan(&x.ID, &x.Slug, &x.CreatedAt)
			out.Repositories = append(out.Repositories, x)
			return err
		}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT repo_id, number, title, body, state, head_branch, base_branch, created_at FROM repos_pulls WHERE author_id = $1 ORDER BY created_at`, nil,
		func(r pgx.Rows) error {
			var p pull
			err := r.Scan(&p.RepoID, &p.Number, &p.Title, &p.Body, &p.State, &p.Head, &p.Base, &p.CreatedAt)
			out.Pulls = append(out.Pulls, p)
			return err
		}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT p.repo_id, p.number, r.state, r.body, r.created_at FROM repos_reviews r JOIN repos_pulls p ON p.id = r.pull_id
		WHERE r.author_id = $1 ORDER BY r.created_at`, nil,
		func(r pgx.Rows) error {
			var x review
			err := r.Scan(&x.RepoID, &x.Number, &x.State, &x.Body, &x.CreatedAt)
			out.Reviews = append(out.Reviews, x)
			return err
		}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT p.repo_id, p.number, c.body, c.path, c.line, c.created_at FROM repos_comments c JOIN repos_pulls p ON p.id = c.pull_id
		WHERE c.author_id = $1 AND c.deleted_at IS NULL ORDER BY c.created_at`, nil,
		func(r pgx.Rows) error {
			var x comment
			err := r.Scan(&x.RepoID, &x.Number, &x.Body, &x.Path, &x.Line, &x.CreatedAt)
			out.Comments = append(out.Comments, x)
			return err
		}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT provider, web_base, path, created_at FROM repos_links WHERE created_by = $1 ORDER BY created_at`, nil,
		func(r pgx.Rows) error {
			var x link
			err := r.Scan(&x.Provider, &x.WebBase, &x.Path, &x.CreatedAt)
			out.Links = append(out.Links, x)
			return err
		}); err != nil {
		return nil, err
	}
	return out, nil
}
