package docs

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Embeds: a link from an allowlisted provider is shown as that provider's
// own player or viewer in a sandboxed frame. The server is the authority:
// normalizeProps stores the frame's src it computes here, and clients frame
// only that (and only on these hosts). Any other link is a bookmark, never
// an arbitrary frame. shared/blocks.ts mirrors the detection for the paste
// menu; keep the two lists in step.

// Embed is a matched provider link.
type Embed struct {
	Provider string // id ("youtube")
	URL      string // the link as written (normalised)
	Src      string // what the frame loads
}

type embedRule struct {
	id, name string
	hosts    []string // hostnames (without www.) the link may use
	re       *regexp.Regexp
	src      func(u *url.URL, m []string) string
}

var embedRules = []embedRule{
	{id: "youtube", name: "YouTube", hosts: []string{"youtube.com", "m.youtube.com", "youtu.be", "youtube-nocookie.com"},
		re: regexp.MustCompile(`^https://(?:www\.|m\.)?(?:youtube\.com/(?:watch\?(?:.*&)?v=|shorts/|embed/|live/)|youtu\.be/|youtube-nocookie\.com/embed/)([A-Za-z0-9_-]{11})`),
		src: func(u *url.URL, m []string) string {
			src := "https://www.youtube-nocookie.com/embed/" + m[1]
			if t := youtubeStart(u.Query().Get("t") + u.Query().Get("start")); t > 0 {
				src += "?start=" + strconv.Itoa(t)
			}
			return src
		}},
	{id: "vimeo", name: "Vimeo", hosts: []string{"vimeo.com", "player.vimeo.com"},
		re: regexp.MustCompile(`^https://(?:www\.)?(?:vimeo\.com/(?:video/)?|player\.vimeo\.com/video/)(\d{4,12})(?:/([0-9a-f]{6,20}))?`),
		src: func(u *url.URL, m []string) string {
			src := "https://player.vimeo.com/video/" + m[1] + "?dnt=1"
			if m[2] != "" {
				src += "&h=" + m[2]
			} else if h := u.Query().Get("h"); regexp.MustCompile(`^[0-9a-f]{6,20}$`).MatchString(h) {
				src += "&h=" + h
			}
			return src
		}},
	{id: "loom", name: "Loom", hosts: []string{"loom.com"},
		re:  regexp.MustCompile(`^https://(?:www\.)?loom\.com/(?:share|embed)/([0-9a-f]{32})`),
		src: func(_ *url.URL, m []string) string { return "https://www.loom.com/embed/" + m[1] }},
	{id: "figma", name: "Figma", hosts: []string{"figma.com"},
		re: regexp.MustCompile(`^https://(?:www\.)?figma\.com/(?:file|design|proto|board|slides|deck)/[A-Za-z0-9]{10,64}`),
		src: func(u *url.URL, _ []string) string {
			return "https://www.figma.com/embed?embed_host=share&url=" + url.QueryEscape(u.String())
		}},
	{id: "miro", name: "Miro", hosts: []string{"miro.com"},
		re:  regexp.MustCompile(`^https://(?:www\.)?miro\.com/app/(?:board|live-embed)/([A-Za-z0-9_=\-]{8,64})/?`),
		src: func(_ *url.URL, m []string) string { return "https://miro.com/app/live-embed/" + m[1] + "/" }},
	{id: "google_maps", name: "Google Maps", hosts: []string{"google.com", "maps.google.com"},
		re: regexp.MustCompile(`^https://(?:www\.|maps\.)?google\.com/maps(?:/embed\?pb=|/place/|/@|\?q=|/search/)`),
		src: func(u *url.URL, _ []string) string {
			p := u.EscapedPath()
			switch {
			case strings.HasPrefix(p, "/maps/embed"):
				return "https://www.google.com/maps/embed?pb=" + url.QueryEscape(u.Query().Get("pb"))
			case strings.HasPrefix(p, "/maps/place/"), strings.HasPrefix(p, "/maps/search/"):
				parts := strings.Split(strings.TrimPrefix(strings.TrimPrefix(p, "/maps/place/"), "/maps/search/"), "/")
				q, _ := url.PathUnescape(parts[0])
				return "https://maps.google.com/maps?output=embed&q=" + url.QueryEscape(strings.ReplaceAll(q, "+", " "))
			case strings.HasPrefix(p, "/maps/@"):
				parts := strings.Split(strings.TrimPrefix(p, "/maps/@"), ",")
				if len(parts) >= 2 {
					return "https://maps.google.com/maps?output=embed&q=" + url.QueryEscape(parts[0]+","+parts[1])
				}
			}
			return "https://maps.google.com/maps?output=embed&q=" + url.QueryEscape(u.Query().Get("q"))
		}},
	{id: "google_docs", name: "Google Docs", hosts: []string{"docs.google.com"},
		re:  regexp.MustCompile(`^https://docs\.google\.com/document/d/([A-Za-z0-9_-]{20,100})`),
		src: func(_ *url.URL, m []string) string { return "https://docs.google.com/document/d/" + m[1] + "/preview" }},
	{id: "google_sheets", name: "Google Sheets", hosts: []string{"docs.google.com"},
		re: regexp.MustCompile(`^https://docs\.google\.com/spreadsheets/d/([A-Za-z0-9_-]{20,100})`),
		src: func(_ *url.URL, m []string) string {
			return "https://docs.google.com/spreadsheets/d/" + m[1] + "/preview"
		}},
	{id: "google_slides", name: "Google Slides", hosts: []string{"docs.google.com"},
		re: regexp.MustCompile(`^https://docs\.google\.com/presentation/d/([A-Za-z0-9_-]{20,100})`),
		src: func(_ *url.URL, m []string) string {
			return "https://docs.google.com/presentation/d/" + m[1] + "/embed"
		}},
	{id: "google_forms", name: "Google Forms", hosts: []string{"docs.google.com"},
		re: regexp.MustCompile(`^https://docs\.google\.com/forms/d/e/([A-Za-z0-9_-]{20,120})`),
		src: func(_ *url.URL, m []string) string {
			return "https://docs.google.com/forms/d/e/" + m[1] + "/viewform?embedded=true"
		}},
	{id: "google_drive", name: "Google Drive", hosts: []string{"drive.google.com"},
		re:  regexp.MustCompile(`^https://drive\.google\.com/file/d/([A-Za-z0-9_-]{20,100})`),
		src: func(_ *url.URL, m []string) string { return "https://drive.google.com/file/d/" + m[1] + "/preview" }},
	{id: "google_calendar", name: "Google Calendar", hosts: []string{"calendar.google.com"},
		re:  regexp.MustCompile(`^https://calendar\.google\.com/calendar/(?:u/\d/)?embed\?`),
		src: func(u *url.URL, _ []string) string { return "https://calendar.google.com/calendar/embed?" + u.RawQuery }},
	{id: "codepen", name: "CodePen", hosts: []string{"codepen.io"},
		re: regexp.MustCompile(`^https://codepen\.io/([A-Za-z0-9_-]{1,64})/(?:pen|embed|full|details)/([A-Za-z0-9]{3,16})`),
		src: func(_ *url.URL, m []string) string {
			return "https://codepen.io/" + m[1] + "/embed/" + m[2] + "?default-tab=result"
		}},
	{id: "codesandbox", name: "CodeSandbox", hosts: []string{"codesandbox.io"},
		re:  regexp.MustCompile(`^https://codesandbox\.io/(?:s|embed|p/sandbox|p/devbox)/([A-Za-z0-9_-]{3,80})`),
		src: func(_ *url.URL, m []string) string { return "https://codesandbox.io/embed/" + m[1] }},
	{id: "gist", name: "GitHub Gist", hosts: []string{"gist.github.com"},
		re:  regexp.MustCompile(`^https://gist\.github\.com/([A-Za-z0-9-]{1,39})/([0-9a-f]{8,40})`),
		src: func(_ *url.URL, m []string) string { return "https://gist.github.com/" + m[1] + "/" + m[2] + ".pibb" }},
	{id: "twitter", name: "X", hosts: []string{"twitter.com", "x.com", "mobile.twitter.com"},
		re: regexp.MustCompile(`^https://(?:www\.|mobile\.)?(?:twitter|x)\.com/[A-Za-z0-9_]{1,15}/status/(\d{5,25})`),
		src: func(_ *url.URL, m []string) string {
			return "https://platform.twitter.com/embed/Tweet.html?dnt=true&id=" + m[1]
		}},
	{id: "spotify", name: "Spotify", hosts: []string{"open.spotify.com"},
		re: regexp.MustCompile(`^https://open\.spotify\.com/(?:intl-[a-z]{2}/)?(track|album|playlist|episode|show|artist)/([A-Za-z0-9]{10,40})`),
		src: func(_ *url.URL, m []string) string {
			return "https://open.spotify.com/embed/" + m[1] + "/" + m[2]
		}},
	{id: "soundcloud", name: "SoundCloud", hosts: []string{"soundcloud.com", "m.soundcloud.com"},
		re: regexp.MustCompile(`^https://(?:www\.|m\.)?soundcloud\.com/[A-Za-z0-9_-]{2,64}/[A-Za-z0-9_-]{1,128}`),
		src: func(u *url.URL, _ []string) string {
			clean := "https://soundcloud.com" + u.EscapedPath()
			return "https://w.soundcloud.com/player/?visual=false&url=" + url.QueryEscape(clean)
		}},
	{id: "typeform", name: "Typeform", hosts: []string{"typeform.com", "form.typeform.com"},
		re:  regexp.MustCompile(`^https://(?:[a-z0-9-]{1,63}\.)?typeform\.com/to/([A-Za-z0-9]{4,20})`),
		src: func(_ *url.URL, m []string) string { return "https://form.typeform.com/to/" + m[1] }},
	{id: "tally", name: "Tally", hosts: []string{"tally.so"},
		re:  regexp.MustCompile(`^https://tally\.so/(?:r|embed)/([A-Za-z0-9]{4,20})`),
		src: func(_ *url.URL, m []string) string { return "https://tally.so/embed/" + m[1] }},
	{id: "airtable", name: "Airtable", hosts: []string{"airtable.com"},
		re: regexp.MustCompile(`^https://airtable\.com/(?:embed/)?((?:app[A-Za-z0-9]{14}/)?shr[A-Za-z0-9]{14})`),
		src: func(_ *url.URL, m []string) string {
			return "https://airtable.com/embed/" + m[1]
		}},
	{id: "whimsical", name: "Whimsical", hosts: []string{"whimsical.com"},
		re:  regexp.MustCompile(`^https://whimsical\.com/(?:embed/)?([A-Za-z0-9-]{4,120})`),
		src: func(_ *url.URL, m []string) string { return "https://whimsical.com/embed/" + m[1] }},
	{id: "excalidraw", name: "Excalidraw", hosts: []string{"excalidraw.com"},
		re:  regexp.MustCompile(`^https://excalidraw\.com/#(?:json|room)=[A-Za-z0-9_,-]{10,200}$`),
		src: func(u *url.URL, _ []string) string { return "https://excalidraw.com/#" + u.Fragment }},
	{id: "framer", name: "Framer", hosts: []string{"framer.com", "framer.website"},
		re:  regexp.MustCompile(`^https://(?:[a-z0-9-]{1,63}\.framer\.website|framer\.com/embed)/`),
		src: func(u *url.URL, _ []string) string { return u.String() }},
	{id: "replit", name: "Replit", hosts: []string{"replit.com"},
		re:  regexp.MustCompile(`^https://replit\.com/@([A-Za-z0-9_-]{1,64})/([A-Za-z0-9_.-]{1,100})`),
		src: func(_ *url.URL, m []string) string { return "https://replit.com/@" + m[1] + "/" + m[2] + "?embed=true" }},
	{id: "lucidchart", name: "Lucidchart", hosts: []string{"lucid.app"},
		re:  regexp.MustCompile(`^https://lucid\.app/(?:lucidchart|lucidspark|documents/embedded)/([0-9a-f-]{36})`),
		src: func(_ *url.URL, m []string) string { return "https://lucid.app/documents/embedded/" + m[1] }},
	{id: "calendly", name: "Calendly", hosts: []string{"calendly.com"},
		re: regexp.MustCompile(`^https://calendly\.com/([A-Za-z0-9_-]{2,64})(/[A-Za-z0-9_-]{1,80})?/?(?:$|\?)`),
		src: func(_ *url.URL, m []string) string {
			return "https://calendly.com/" + m[1] + m[2] + "?embed_type=Inline&hide_gdpr_banner=1"
		}},
	{id: "canva", name: "Canva", hosts: []string{"canva.com"},
		re: regexp.MustCompile(`^https://(?:www\.)?canva\.com/design/([A-Za-z0-9_-]{8,20})/([A-Za-z0-9_-]{8,40})/(?:view|watch)`),
		src: func(_ *url.URL, m []string) string {
			return "https://www.canva.com/design/" + m[1] + "/" + m[2] + "/view?embed"
		}},
}

// embedHosts are the hosts frames may load (the srcs above).
var embedHosts = []string{
	"www.youtube-nocookie.com", "player.vimeo.com", "www.loom.com", "www.figma.com", "miro.com", "www.google.com", "maps.google.com",
	"docs.google.com", "drive.google.com", "calendar.google.com", "codepen.io", "codesandbox.io", "gist.github.com",
	"platform.twitter.com", "open.spotify.com", "w.soundcloud.com", "form.typeform.com", "tally.so", "airtable.com",
	"whimsical.com", "excalidraw.com", "framer.com", "framer.website", "replit.com", "lucid.app", "calendly.com", "www.canva.com",
}

// embedFor matches a link to an allowlisted provider.
func embedFor(raw string) (Embed, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) > maxLinkLen || strings.ContainsAny(raw, " \n\t<>\"'") {
		return Embed{}, false
	}
	if strings.HasPrefix(strings.ToLower(raw), "http://") {
		raw = "https://" + raw[len("http://"):]
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return Embed{}, false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if u.Port() != "" {
		return Embed{}, false
	}
	u.Host = strings.ToLower(u.Host)
	norm := u.String()
	for _, r := range embedRules {
		if !hostIn(host, r.hosts) {
			continue
		}
		m := r.re.FindStringSubmatch(norm)
		if m == nil {
			continue
		}
		src := r.src(u, m)
		su, err := url.Parse(src)
		if err != nil || su.Scheme != "https" || !hostIn(su.Hostname(), embedHosts) {
			continue
		}
		return Embed{Provider: r.id, URL: norm, Src: src}, true
	}
	return Embed{}, false
}

func hostIn(host string, list []string) bool {
	for _, h := range list {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

func embedName(provider string) string {
	for _, r := range embedRules {
		if r.id == provider {
			return r.name
		}
	}
	return "Embed"
}

// youtubeStart reads a start time: "90", "90s", "1m30s", "1h2m3s".
func youtubeStart(t string) int {
	if t == "" {
		return 0
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(t, "s")); err == nil {
		return max(0, n)
	}
	total := 0
	for _, part := range regexp.MustCompile(`(\d+)([hms])`).FindAllStringSubmatch(t, -1) {
		n, _ := strconv.Atoi(part[1])
		total += n * map[string]int{"h": 3600, "m": 60, "s": 1}[part[2]]
	}
	return total
}
