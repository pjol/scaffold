package forum

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/moderation"
)

// SeedDev adds three communities, a pinned welcome post, a discussion with
// a threaded conversation and votes, a link post, and one post screening
// holds (it waits in Admin → Moderation). users are admin, sam, jordan,
// priya (auth.DevIdentities order). Skipped once the general community exists.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	admin, sam, jordan, priya := users[0], users[1], users[2], users[3]
	var seeded bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM forum_communities WHERE slug = 'general')`).Scan(&seeded); err != nil || seeded {
		return err
	}
	now := time.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		community := func(slug, title, desc string) (uuid.UUID, error) {
			var id uuid.UUID
			err := tx.QueryRow(ctx, `INSERT INTO forum_communities (slug, title, description, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
				slug, title, desc, admin).Scan(&id)
			return id, err
		}
		general, err := community("general", "General", "Anything about the app and the people who use it.")
		if err != nil {
			return err
		}
		help, err := community("help", "Help", "Questions about using the app, answered by the community and staff.")
		if err != nil {
			return err
		}
		show, err := community("show-and-tell", "Show and tell", "Share what you made, wrote or found useful.")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO forum_moderators (community_id, user_id, added_by) VALUES ($1, $2, $3)`, help, priya, admin); err != nil {
			return err
		}
		post := func(c uuid.UUID, author uuid.UUID, title, body string, url *string, at time.Time, pinned bool) (uuid.UUID, error) {
			id := uuid.New()
			var domain *string
			if url != nil {
				d := "example.org"
				domain = &d
			}
			_, err := tx.Exec(ctx, `INSERT INTO forum_posts (id, community_id, author_id, title, body, url, domain, status, hot, pinned, notified, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, 'visible', $8, $9, true, $10)`, id, c, author, title, body, url, domain, Hot(0, at, false), pinned, at)
			return id, err
		}
		comment := func(postID uuid.UUID, parent *uuid.UUID, depth int, author uuid.UUID, body string, at time.Time) (uuid.UUID, error) {
			id := uuid.New()
			if _, err := tx.Exec(ctx, `INSERT INTO forum_comments (id, post_id, parent_id, depth, author_id, body, status, notified, created_at)
				VALUES ($1, $2, $3, $4, $5, $6, 'visible', true, $7)`, id, postID, parent, depth, author, body, at); err != nil {
				return id, err
			}
			_, err := tx.Exec(ctx, `UPDATE forum_posts SET comment_count = comment_count + 1 WHERE id = $1`, postID)
			return id, err
		}
		vote := func(voter uuid.UUID, kind string, target uuid.UUID, value int) error {
			table := "forum_posts"
			if kind == "comment" {
				table = "forum_comments"
			}
			var author uuid.UUID
			if err := tx.QueryRow(ctx, `UPDATE `+table+` SET score = score + $2, ups = ups + greatest($2, 0), downs = downs + greatest(-$2, 0)
				WHERE id = $1 RETURNING author_id`, target, value).Scan(&author); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO forum_votes (user_id, target_kind, target_id, author_id, value, counts) VALUES ($1, $2, $3, $4, $5, true)`,
				voter, kind, target, author, value)
			return err
		}

		welcome, err := post(general, admin, "Welcome to the forum",
			"Introduce yourself, ask questions in **Help**, and share your work in **Show and tell**.\n\n"+
				"Each community lists its rules. Report anything that breaks them from the ⋯ menu; staff review reports within a day.",
			nil, ago(72*time.Hour), true)
		if err != nil {
			return err
		}
		week, err := post(general, sam, "What are you working on this week?",
			"I'm rewriting the onboarding emails for our team. Short ones, one action each.\n\nWhat's on your list?", nil, ago(20*time.Hour), false)
		if err != nil {
			return err
		}
		c1, err := comment(week, nil, 0, jordan, "Moving our docs to one place. Three wikis was two too many.", ago(19*time.Hour))
		if err != nil {
			return err
		}
		c2, err := comment(week, &c1, 1, priya, "Which one did you keep? We're about to do the same.", ago(18*time.Hour))
		if err != nil {
			return err
		}
		if _, err := comment(week, &c2, 2, jordan, "The one people actually searched. We checked the logs before deciding.", ago(17*time.Hour)); err != nil {
			return err
		}
		if _, err := comment(week, nil, 0, priya, "Planning a small meetup for next month. @Sam want to help with the venue?", ago(15*time.Hour)); err != nil {
			return err
		}
		howto, err := post(help, priya, "How do I stop email notifications but keep push?",
			"I want replies on my phone but not in my inbox. Is that a setting?", nil, ago(9*time.Hour), false)
		if err != nil {
			return err
		}
		answer, err := comment(howto, nil, 0, admin, "Settings → Notifications: each kind has its own switches for push and email.", ago(8*time.Hour))
		if err != nil {
			return err
		}
		link := "https://example.org/writing-bug-reports"
		guide, err := post(show, jordan, "A checklist for bug reports that get fixed",
			"Steps, expected result, actual result, one screenshot. It halved our back-and-forth.", &link, ago(5*time.Hour), false)
		if err != nil {
			return err
		}
		for _, v := range []struct {
			voter  uuid.UUID
			kind   string
			target uuid.UUID
			value  int
		}{
			{sam, "post", welcome, 1}, {jordan, "post", welcome, 1}, {priya, "post", welcome, 1},
			{jordan, "post", week, 1}, {priya, "post", week, 1}, {admin, "post", week, 1},
			{sam, "comment", c1, 1}, {priya, "comment", c1, 1}, {sam, "comment", c2, 1},
			{priya, "comment", answer, 1}, {sam, "comment", answer, 1},
			{sam, "post", guide, 1}, {priya, "post", guide, 1}, {admin, "post", guide, -1},
		} {
			if err := vote(v.voter, v.kind, v.target, v.value); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	// rank the seeded posts by their votes (Hot, in SQL)
	if _, err := s.d.Pool.Exec(ctx, `
		UPDATE forum_posts SET hot = sign(score) * log(greatest(abs(score), 1)) + (extract(epoch FROM created_at)::float8 - 1.7e9) / 45000
		WHERE community_id IN (SELECT id FROM forum_communities WHERE slug IN ('general', 'help', 'show-and-tell'))`); err != nil {
		return err
	}
	return s.seedHeld(ctx, jordan)
}

// seedHeld posts through screening like a real client: a new account's
// shortened link and off-platform contact are held for review.
func (s *Service) seedHeld(ctx context.Context, author uuid.UUID) error {
	c, err := s.communityBySlug(ctx, s.d.Pool, "general")
	if err != nil {
		return err
	}
	id := uuid.New()
	title, body := "Free gift cards for the first 10 people", "Message me on telegram and I'll send the code: bit.ly/3fRee-cards"
	k, err := Karma(ctx, s.d.Pool, author, time.Time{})
	if err != nil {
		return err
	}
	dec, err := moderation.Screen(ctx, s.d.Pool, moderation.Item{Kind: TargetPost, ID: id.String(), AuthorID: author, Title: title, Body: body,
		Place: s.placeWords(CommunityRef{ID: c.ID, Slug: c.Slug, Title: c.Title}, c.Description), Link: postPath(id), Karma: &k})
	if err != nil {
		return err
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO forum_posts (id, community_id, author_id, title, body, labels, mod_reasons, status, hot, notified)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, id, c.ID, author, title, body, nonNilStrings(dec.Labels), dec.Reasons(), dec.Status(),
			Hot(0, time.Now(), false), dec.Status() == StatusVisible); err != nil {
			return err
		}
		return moderation.Record(ctx, tx, dec)
	})
}
