package docs

import (
	"net/http"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/orgs"
)

// hideTeamspace / showTeamspace: PUT / DELETE
// /docs/orgs/{orgID}/teamspaces/{teamID}/hidden (member, on the team): take a
// teamspace out of the caller's sidebar or bring it back. Access is
// unchanged (leaving the team is the team's setting).
func (s *Service) hideTeamspace(w http.ResponseWriter, r *http.Request) error {
	return s.setTeamspaceHidden(w, r, true)
}

func (s *Service) showTeamspace(w http.ResponseWriter, r *http.Request) error {
	return s.setTeamspaceHidden(w, r, false)
}

func (s *Service) setTeamspaceHidden(w http.ResponseWriter, r *http.Request, hidden bool) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	m, _ := orgs.FromContext(ctx)
	teamID, err := httpx.URLParamUUID(r, "teamID")
	if err != nil {
		return err
	}
	var on bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM access_team_members tm JOIN access_teams t ON t.id = tm.team_id
		WHERE tm.team_id = $1 AND tm.user_id = $2 AND t.org_id = $3)`, teamID, me, m.OrgID).Scan(&on); err != nil {
		return err
	}
	if !on {
		return httpx.NotFound("You aren't on that team.")
	}
	if hidden {
		_, err = s.d.Pool.Exec(ctx, `INSERT INTO docs_teamspace_prefs (user_id, team_id, hidden) VALUES ($1, $2, true)
			ON CONFLICT (user_id, team_id) DO UPDATE SET hidden = true`, me, teamID)
	} else {
		_, err = s.d.Pool.Exec(ctx, `DELETE FROM docs_teamspace_prefs WHERE user_id = $1 AND team_id = $2`, me, teamID)
	}
	if err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}
