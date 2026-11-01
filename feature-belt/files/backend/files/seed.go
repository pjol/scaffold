package files

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/storage"
)

// SeedDev gives the orgs the orgs feature seeds a believable drive: shared
// folders for the access seed's teams (one with a restricted subfolder),
// private and shared My drive items, a few real files (CSV, markdown with
// two versions, a PNG drawn here) and one public link, whose URL is logged.
// users are admin, sam, jordan, priya. Safe to re-run.
func (s *Service) SeedDev(ctx context.Context, users []uuid.UUID) error {
	if len(users) < 4 {
		return nil
	}
	for _, m := range s.d.Modules {
		if seeder, ok := m.(app.DevSeeder); ok && m.Name() == "access" {
			if err := seeder.SeedDev(ctx, users); err != nil {
				return fmt.Errorf("access seed: %w", err)
			}
		}
	}
	sam, jordan, priya := users[1], users[2], users[3]
	var linkURL string
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		var club, owls uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM orgs WHERE slug = 'riverside-climbing'`).Scan(&club); err != nil {
			if db.IsNoRows(err) {
				return nil
			}
			return err
		}
		sd := &seeder{s: s, tx: tx, ctx: ctx}

		// Route setting: a shared folder for the route setters team.
		setting := sd.sharedFolder(club, "Route setting", jordan)
		sd.grantTeam(setting, club, "route-setters", access.Edit, jordan)
		sd.file(setting, "Wall plan — November.csv", "text/csv", []byte(wallPlanCSV), jordan)
		notes := sd.file(setting, "Setting notes.md", "text/markdown", []byte(settingNotesV1), jordan)
		sd.version(notes, "text/markdown", []byte(settingNotesV2), priya)
		sd.file(setting, "Cave wall reference.png", "image/png", wallPNG(), priya)
		inventory := sd.folder(setting, "Inventory", jordan)
		sd.file(inventory, "Holds.csv", "text/csv", []byte(holdsCSV), priya)
		budget := sd.restrictedFolder(setting, "Budget", jordan)
		sd.file(budget, "Hold order — Q1.csv", "text/csv", []byte(holdOrderCSV), jordan)

		// Coaching: the coaching team edits, everyone in the club can view.
		coaching := sd.sharedFolder(club, "Coaching", sam)
		sd.grantTeam(coaching, club, "coaching", access.Edit, sam)
		sd.grantOrg(coaching, club, access.View, sam)
		sd.file(coaching, "Youth team schedule.csv", "text/csv", []byte(youthScheduleCSV), sam)
		sd.file(coaching, "Technique session plan.md", "text/markdown", []byte(techniquePlanMD), jordan)

		// Sam's My drive: a private draft, a folder shared with Jordan, a
		// public visitor guide.
		samDrive := &placement{orgID: club, drive: DriveMy, owner: &sam}
		sd.file(samDrive, "Club handbook draft.md", "text/markdown", []byte(handbookMD), sam)
		board := sd.folder(samDrive, "Board meeting", sam)
		sd.grantUser(board, jordan, access.Comment, sam)
		sd.file(board, "Agenda — November.md", "text/markdown", []byte(agendaMD), sam)
		guide := sd.file(samDrive, "Visitor guide.md", "text/markdown", []byte(visitorGuideMD), sam)
		if guide != nil {
			linkURL = sd.publicLink(guide.ID, sam)
		}
		sd.star(sam, setting)
		sd.star(jordan, notes)

		// Priya's My drive.
		priyaDrive := &placement{orgID: club, drive: DriveMy, owner: &priya}
		sd.file(priyaDrive, "Grade conversion.csv", "text/csv", []byte(gradesCSV), priya)

		// Night Owls: one shared folder the whole club can view.
		if err := tx.QueryRow(ctx, `SELECT id FROM orgs WHERE slug = 'night-owls'`).Scan(&owls); err == nil {
			books := sd.sharedFolder(owls, "Book list", priya)
			sd.grantOrg(books, owls, access.View, priya)
			sd.file(books, "2026 picks.csv", "text/csv", []byte(bookPicksCSV), priya)
		} else if !db.IsNoRows(err) {
			return err
		}
		return sd.err
	})
	if err == nil && linkURL != "" {
		slog.Info("files: dev public link for “Visitor guide.md”", "url", linkURL)
	}
	return err
}

// seeder carries the transaction and the first error, so the seed reads as
// a list of what exists.
type seeder struct {
	s   *Service
	tx  pgx.Tx
	ctx context.Context
	err error
}

func (sd *seeder) fail(err error) {
	if sd.err == nil && err != nil {
		sd.err = err
	}
}

// existing finds a live item by name in a placement.
func (sd *seeder) existing(pl *placement, name string) *Item {
	var id uuid.UUID
	err := sd.tx.QueryRow(sd.ctx, `
		SELECT id FROM files_items WHERE org_id = $1 AND drive = $2 AND COALESCE(parent_id, owner_id, org_id) = $3
		  AND lower(name) = lower($4) AND trashed_at IS NULL`, pl.orgID, pl.drive, pl.siblingKey(), name).Scan(&id)
	if err != nil {
		if !db.IsNoRows(err) {
			sd.fail(err)
		}
		return nil
	}
	return &Item{ID: id, OrgID: pl.orgID, Drive: pl.drive, OwnerID: pl.owner, Name: name}
}

func (sd *seeder) into(it *Item) *placement {
	if it == nil {
		return nil
	}
	return &placement{orgID: it.OrgID, parent: it, drive: it.Drive, owner: it.OwnerID}
}

// where accepts a placement (the top of a drive) or a folder.
func (sd *seeder) where(where any) *placement {
	switch w := where.(type) {
	case *placement:
		return w
	case *Item:
		return sd.into(w)
	}
	return nil
}

func (sd *seeder) create(where any, typ, name string, by uuid.UUID, restricted bool) *Item {
	pl := sd.where(where)
	if sd.err != nil || pl == nil {
		return nil
	}
	if it := sd.existing(pl, name); it != nil {
		return it
	}
	id, err := sd.s.insertItemWith(sd.ctx, sd.tx, pl, typ, name, by, false, access.None, restricted)
	if err != nil {
		sd.fail(err)
		return nil
	}
	if typ != TypeFile { // files get their "uploaded" event with the first version
		sd.s.logActivity(sd.ctx, sd.tx, pl.orgID, id, pl.parentID(), by, "created", map[string]any{"type": typ})
	}
	return &Item{ID: id, OrgID: pl.orgID, Drive: pl.drive, OwnerID: pl.owner, Name: name}
}

func (sd *seeder) sharedFolder(orgID uuid.UUID, name string, by uuid.UUID) *Item {
	it := sd.create(&placement{orgID: orgID, drive: DriveShared}, TypeFolder, name, by, false)
	sd.grantUser(it, by, access.Manage, by)
	return it
}

func (sd *seeder) folder(where any, name string, by uuid.UUID) *Item {
	return sd.create(where, TypeFolder, name, by, false)
}

func (sd *seeder) restrictedFolder(parent *Item, name string, by uuid.UUID) *Item {
	return sd.create(parent, TypeFolder, name, by, true)
}

func (sd *seeder) file(where any, name, mt string, body []byte, by uuid.UUID) *Item {
	pl := sd.where(where)
	if sd.err != nil || pl == nil {
		return nil
	}
	if it := sd.existing(pl, name); it != nil {
		return it
	}
	it := sd.create(pl, TypeFile, name, by, false)
	sd.version(it, mt, body, by)
	return it
}

// version adds body as the file's next version, unless a version already
// has these bytes' size.
func (sd *seeder) version(it *Item, mt string, body []byte, by uuid.UUID) {
	if sd.err != nil || it == nil {
		return
	}
	var have int
	if err := sd.tx.QueryRow(sd.ctx, `SELECT count(*) FROM files_versions WHERE item_id = $1 AND size = $2`, it.ID, len(body)).Scan(&have); err != nil {
		sd.fail(err)
		return
	}
	if have > 0 {
		return
	}
	key, err := storage.NewKey(Purpose, it.OrgID, mt)
	if err != nil {
		sd.fail(err)
		return
	}
	if err := sd.s.d.Storage.Put(sd.ctx, key, mt, body); err != nil {
		sd.fail(err)
		return
	}
	_, textLike, err := classify(mt, it.Name, body)
	if err != nil {
		sd.fail(err)
		return
	}
	var content *string
	if textLike {
		t := string(body)
		content = &t
	}
	var before int
	_ = sd.tx.QueryRow(sd.ctx, `SELECT version_count FROM files_items WHERE id = $1`, it.ID).Scan(&before)
	if err := sd.s.addVersion(sd.ctx, sd.tx, it.ID, it.OrgID, by, newVersion{key: key, size: int64(len(body)), mime: mt, textLike: textLike, content: content}); err != nil {
		sd.fail(err)
		return
	}
	action := "uploaded"
	if before > 0 {
		action = "version"
	}
	sd.s.logActivity(sd.ctx, sd.tx, it.OrgID, it.ID, nil, by, action, map[string]any{"size": len(body)})
}

// grant shares target through access (the share dialog's checks; nobody is
// notified). A re-run never lowers a level.
func (sd *seeder) grant(target *Item, typ string, subject uuid.UUID, level access.Level, by uuid.UUID) {
	if sd.err != nil || target == nil {
		return
	}
	sd.fail(access.AddGrant(sd.ctx, sd.tx, access.Ref{Kind: Kind, ID: target.ID}, access.SubjectRef{Type: typ, ID: subject}, level, by))
}

func (sd *seeder) grantUser(target *Item, user uuid.UUID, level access.Level, by uuid.UUID) {
	sd.grant(target, "user", user, level, by)
}

func (sd *seeder) grantOrg(target *Item, orgID uuid.UUID, level access.Level, by uuid.UUID) {
	sd.grant(target, "org", orgID, level, by)
}

func (sd *seeder) grantTeam(target *Item, orgID uuid.UUID, handle string, level access.Level, by uuid.UUID) {
	if sd.err != nil || target == nil {
		return
	}
	t, err := access.TeamByHandle(sd.ctx, sd.tx, orgID, handle)
	if err != nil || t == nil {
		return // the access seed changed; share with nobody
	}
	sd.grant(target, "team", t.ID, level, by)
}

func (sd *seeder) star(user uuid.UUID, it *Item) {
	if sd.err != nil || it == nil {
		return
	}
	_, err := sd.tx.Exec(sd.ctx, `INSERT INTO files_stars (user_id, item_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, user, it.ID)
	sd.fail(err)
}

// publicLink makes a view link through access unless one works already.
// Returns its URL ("" when one existed: only its hash is stored, or when the
// org no longer lets by make public links).
func (sd *seeder) publicLink(itemID, by uuid.UUID) string {
	if sd.err != nil {
		return ""
	}
	var exists bool
	if err := sd.tx.QueryRow(sd.ctx, `SELECT EXISTS (SELECT 1 FROM access_links WHERE resource_kind = 'files.item' AND resource_id = $1
		AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now()))`, itemID).Scan(&exists); err != nil {
		sd.fail(err)
		return ""
	}
	if exists {
		return ""
	}
	l, err := access.CreateLink(sd.ctx, sd.tx, access.Ref{Kind: Kind, ID: itemID}, access.View, nil, by)
	var refused *httpx.Error
	if errors.As(err, &refused) {
		slog.Info("files: dev public link skipped", "reason", refused.Message)
		return ""
	}
	if err != nil {
		sd.fail(err)
		return ""
	}
	return *l.URL
}

// wallPNG draws a small climbing wall diagram: panels with coloured holds.
func wallPNG() []byte {
	const w, h = 480, 300
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	panelA := color.RGBA{214, 206, 194, 255}
	panelB := color.RGBA{200, 191, 178, 255}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := panelA
			if (x/120+y/100)%2 == 1 {
				c = panelB
			}
			if x%120 == 0 || y%100 == 0 {
				c = color.RGBA{150, 141, 128, 255}
			}
			img.Set(x, y, c)
		}
	}
	holds := []color.RGBA{{205, 56, 56, 255}, {44, 112, 196, 255}, {232, 184, 36, 255}, {52, 150, 84, 255}}
	seed := uint32(7)
	next := func(n int) int {
		seed = seed*1664525 + 1013904223
		return int(seed>>16) % n
	}
	for i := 0; i < 42; i++ {
		cx, cy, r := 16+next(w-32), 16+next(h-32), 5+next(7)
		c := holds[i%len(holds)]
		for y := -r; y <= r; y++ {
			for x := -r; x <= r; x++ {
				if x*x+y*y <= r*r {
					img.Set(cx+x, cy+y, c)
				}
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

const wallPlanCSV = `Wall,Grade,Hold colour,Setter,Set on,Strip by
Cave,V2,Yellow,Jordan Lee,2026-11-02,2026-11-30
Cave,V4,Red,Priya Shah,2026-11-02,2026-11-30
Slab,V1,Green,Jordan Lee,2026-11-03,2026-12-01
Slab,V3,Blue,Priya Shah,2026-11-03,2026-12-01
Arete,V5,Red,Jordan Lee,2026-11-09,2026-12-07
Arete,V6,Black,Jordan Lee,2026-11-09,2026-12-07
Overhang,V3,Blue,Priya Shah,2026-11-16,2026-12-14
Overhang,V7,Purple,Jordan Lee,2026-11-16,2026-12-14
`

const settingNotesV1 = `# Setting notes

## November reset

- Cave and Slab reset on the first weekend.
- Keep at least 3 problems at V0–V2 on every wall.
`

const settingNotesV2 = `# Setting notes

## November reset

- Cave and Slab reset on the first weekend.
- Keep at least 3 problems at V0–V2 on every wall.
- Arete gets the new dual-tex volumes.

## Safety

1. Check every bolt with the torque wrench before setting.
2. Two people on the floor whenever someone is on the ladder.
`

const holdsCSV = `Brand,Type,Colour,Count,Condition
Kilter,Jugs,Red,24,Good
Kilter,Crimps,Blue,40,Good
Teknik,Slopers,Yellow,12,Needs cleaning
Teknik,Pinches,Green,18,Good
Flathold,Volumes,Grey,6,Good
`

const holdOrderCSV = `Item,Supplier,Quantity,Unit price (USD),Total (USD)
Dual-tex volume set,Flathold,2,340,680
Crimp pack (40),Kilter,1,210,210
Bolt-on screws (500),Hardware Co,2,38,76
`

const youthScheduleCSV = `Day,Time,Group,Coach,Wall
Tuesday,16:30,Youth A (8–11),Sam Rivera,Slab
Tuesday,18:00,Technique session,Jordan Lee,Cave
Thursday,16:30,Youth B (12–15),Sam Rivera,Overhang
Saturday,10:00,Youth team,Sam Rivera,All walls
`

const techniquePlanMD = `# Technique session plan

Tuesdays, 18:00–19:30 in the Cave.

## Week 1: footwork

- Silent feet drill on V0–V1 (10 minutes)
- Edging versus smearing on the Slab

## Week 2: hips and body position

- Flagging on the Arete
- Drop knees on the Overhang
`

const handbookMD = `# Club handbook (draft)

Notes before this goes to the board.

- Membership tiers and the youth discount
- Guest policy: one free visit per guest per month
- Incident reporting steps
`

const agendaMD = `# Board meeting — November

1. Hold budget for Q1
2. Youth team travel to the regional comp
3. Volunteer schedule for the open day
`

const visitorGuideMD = `# Visitor guide

Welcome to Riverside Climbing Club.

## Opening hours

- Weekdays 07:00–22:00
- Weekends 09:00–20:00

## First visit

1. Sign the waiver at the front desk.
2. Rent shoes and chalk if you need them.
3. Ask a staff member for a short wall tour.
`

const gradesCSV = `V scale,Font scale
V0,4
V1,5
V2,5+
V3,6A
V4,6B
V5,6C
V6,7A
V7,7A+
`

const bookPicksCSV = `Month,Title,Author,Host
January,The Left Hand of Darkness,Ursula K. Le Guin,Priya Shah
February,Piranesi,Susanna Clarke,Sam Rivera
March,Klara and the Sun,Kazuo Ishiguro,Priya Shah
`
