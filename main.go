package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

// Schema
type Experiment struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	URL         string    `json:"url"`
	CreatedAt   time.Time `json:"created_at"`
}

type Event struct {
	ID           int       `json:"id"`
	ExperimentID int       `json:"experiment_id"`
	EventType    string    `json:"event_type"`
	Source       string    `json:"source"`
	Metadata     string    `json:"metadata"`
	CreatedAt    time.Time `json:"created_at"`
}

type InboundRequest struct {
	ExperimentSlug string `json:"experiment"`
	EventType      string `json:"event_type"`
	Source         string `json:"source"`
	Metadata       string `json:"metadata,omitempty"`
}

type DashboardRow struct {
	Experiment    Experiment
	Visitors      int
	Leads         int
	Conversions   int
	Engagements   int
	Score         float64
	TrendUp       bool
}

type ScoreSummary struct {
	AllInSlug string
	AllInName string
	AllInScore float64
	SecondSlug string
	SecondName string
	SecondScore float64
}

var version = "v1.0.0"

func main() {
	initDB()
	defer db.Close()
	seedDefaultExperiments()

	port := os.Getenv("DASHBOARD_PORT")
	if port == "" {
		port = "9090"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", dashboardHandler)
	mux.HandleFunc("/api/dashboard", apiDashboard)
	mux.HandleFunc("/api/event", logEventHandler)
	mux.HandleFunc("/api/events", listEventsHandler)
	mux.HandleFunc("/api/experiments", listExperimentsHandler)
	mux.HandleFunc("/api/db-view", dbViewHandler)

	fs := http.FileServer(http.Dir("./static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	log.Printf("Command Dashboard %s running on :%s", version, port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func initDB() {
	var err error
	os.MkdirAll("data", 0755)
	db, err = sql.Open("sqlite3", "data/dashboard.db?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		log.Fatal(err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS experiments (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		slug TEXT UNIQUE NOT NULL,
		description TEXT,
		status TEXT DEFAULT 'active',
		url TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		experiment_id INTEGER NOT NULL,
		event_type TEXT NOT NULL,
		source TEXT DEFAULT 'web',
		metadata TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (experiment_id) REFERENCES experiments(id)
	);

	CREATE INDEX IF NOT EXISTS idx_events_experiment ON events(experiment_id);
	CREATE INDEX IF NOT EXISTS idx_events_created ON events(created_at);
	CREATE INDEX IF NOT EXISTS idx_events_type ON events(event_type);
	`
	_, err = db.Exec(schema)
	if err != nil {
		log.Fatal(err)
	}
}

func seedDefaultExperiments() {
	defaults := []Experiment{
		{Name: "Plankro", Slug: "plankro", Description: "Delhi date-planning app — live, GA tracking, SEO enabled", Status: "active", URL: "https://plankro.com"},
		{Name: "ServiceNow Freelance", Slug: "servicenow-freelance", Description: "ServiceNow freelancing income path — 15 openings at ₹1.5L+/mo", Status: "active", URL: ""},
	}

	for _, e := range defaults {
		_, err := db.Exec(
			"INSERT OR IGNORE INTO experiments (name, slug, description, status, url) VALUES (?, ?, ?, ?, ?)",
			e.Name, e.Slug, e.Description, e.Status, e.URL,
		)
		if err != nil {
			log.Printf("WARN: seed %s: %v", e.Slug, err)
		}
	}
}

// --- Handlers ---

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	rows := getDashboardData()
	summary := computeScore(rows)
	scoreJSON, _ := json.Marshal(summary)

	tmpl := template.Must(template.New("dashboard").Parse(dashboardHTML))
	tmpl.Execute(w, map[string]interface{}{
		"Experiments": rows,
		"Version":     version,
		"ScoreJSON":   string(scoreJSON),
		"Now":         time.Now().Format("Jan 02, 2006 15:04"),
	})
}

func apiDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	rows := getDashboardData()
	summary := computeScore(rows)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"experiments": rows,
		"decision":    summary,
		"timestamp":   time.Now(),
	})
}

func logEventHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		// Support GET for image-pixel tracking (e.g. from Silk Gallery)
		slug := r.URL.Query().Get("exp")
		etype := r.URL.Query().Get("type")
		src := r.URL.Query().Get("src")
		meta := r.URL.Query().Get("meta")
		if slug == "" || etype == "" {
			http.Error(w, "missing exp or type", 400)
			return
		}

		var expID int
		err := db.QueryRow("SELECT id FROM experiments WHERE slug = ?", slug).Scan(&expID)
		if err != nil {
			http.Error(w, "unknown experiment", 400)
			return
		}

		db.Exec("INSERT INTO events (experiment_id, event_type, source, metadata) VALUES (?, ?, ?, ?)",
			expID, etype, src, meta)
		w.Header().Set("Content-Type", "image/gif")
		w.WriteHeader(200)
		w.Write([]byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x21, 0xf9, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b})
		return
	}

	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}

	var req InboundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}

	// Resolve slug to ID
	var expID int
	err := db.QueryRow("SELECT id FROM experiments WHERE slug = ?", req.ExperimentSlug).Scan(&expID)
	if err != nil {
		http.Error(w, fmt.Sprintf("unknown experiment slug: %s", req.ExperimentSlug), 400)
		return
	}

	_, err = db.Exec(
		"INSERT INTO events (experiment_id, event_type, source, metadata) VALUES (?, ?, ?, ?)",
		expID, req.EventType, req.Source, req.Metadata,
	)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.WriteHeader(201)
	json.NewEncoder(w).Encode(map[string]string{"status": "logged"})
}

func listEventsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	slug := r.URL.Query().Get("experiment")
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "50"
	}

	query := `
		SELECT e.id, e.experiment_id, e.event_type, e.source, e.metadata, e.created_at
		FROM events e
		JOIN experiments x ON x.id = e.experiment_id
	`
	args := []interface{}{}
	if slug != "" {
		query += " WHERE x.slug = ?"
		args = append(args, slug)
	}
	query += " ORDER BY e.created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var ev Event
		var meta sql.NullString
		rows.Scan(&ev.ID, &ev.ExperimentID, &ev.EventType, &ev.Source, &meta, &ev.CreatedAt)
		if meta.Valid {
			ev.Metadata = meta.String
		}
		events = append(events, ev)
	}
	if events == nil {
		events = []Event{}
	}
	json.NewEncoder(w).Encode(events)
}

func listExperimentsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	rows, err := db.Query("SELECT id, name, slug, description, status, url, created_at FROM experiments")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var exps []Experiment
	for rows.Next() {
		var e Experiment
		var desc, url sql.NullString
		rows.Scan(&e.ID, &e.Name, &e.Slug, &desc, &e.Status, &url, &e.CreatedAt)
		if desc.Valid {
			e.Description = desc.String
		}
		if url.Valid {
			e.URL = url.String
		}
		exps = append(exps, e)
	}
	if exps == nil {
		exps = []Experiment{}
	}
	json.NewEncoder(w).Encode(exps)
}

func dbViewHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	rows, err := db.Query("SELECT id, name, slug, status, created_at FROM experiments ORDER BY id")
	if err != nil {
		fmt.Fprintf(w, "Error: %v", err)
		return
	}
	defer rows.Close()

	fmt.Fprintf(w, "=== EXPERIMENTS ===\n")
	for rows.Next() {
		var id int
		var name, slug, status string
		var created time.Time
		rows.Scan(&id, &name, &slug, &status, &created)
		fmt.Fprintf(w, "%d | %s | %s | %s | %s\n", id, name, slug, status, created.Format("2006-01-02"))
	}

	fmt.Fprintf(w, "\n=== EVENT COUNTS (today) ===\n")
	rows2, err := db.Query(`
		SELECT x.name, x.slug, e.event_type, COUNT(*) as cnt
		FROM events e
		JOIN experiments x ON x.id = e.experiment_id
		WHERE date(e.created_at) = date('now')
		GROUP BY x.slug, e.event_type
		ORDER BY x.slug, cnt DESC
	`)
	if err == nil {
		defer rows2.Close()
		for rows2.Next() {
			var name, slug, etype string
			var cnt int
			rows2.Scan(&name, &slug, &etype, &cnt)
			fmt.Fprintf(w, "%s (%s) | %s: %d\n", name, slug, etype, cnt)
		}
	}

	fmt.Fprintf(w, "\n=== RAW EVENTS (last 20) ===\n")
	rows3, err := db.Query(`
		SELECT e.id, x.name, e.event_type, e.source, e.created_at
		FROM events e
		JOIN experiments x ON x.id = e.experiment_id
		ORDER BY e.created_at DESC LIMIT 20
	`)
	if err == nil {
		defer rows3.Close()
		for rows3.Next() {
			var id int
			var name, etype, source string
			var created time.Time
			rows3.Scan(&id, &name, &etype, &source, &created)
			fmt.Fprintf(w, "#%d | %s | %s | %s | %s\n", id, name, etype, source, created.Format("15:04"))
		}
	}
}

// --- Data ---

func getDashboardData() []DashboardRow {
	experiments, _ := db.Query("SELECT id, name, slug, description, status, url, created_at FROM experiments WHERE status = 'active'")
	if experiments != nil {
		defer experiments.Close()
	}

	var rows []DashboardRow
	if experiments != nil {
		for experiments.Next() {
			var e Experiment
			var desc, url sql.NullString
			experiments.Scan(&e.ID, &e.Name, &e.Slug, &desc, &e.Status, &url, &e.CreatedAt)
			if desc.Valid {
				e.Description = desc.String
			}
			if url.Valid {
				e.URL = url.String
			}

			row := DashboardRow{Experiment: e}
			row = computeMetrics(row)
			rows = append(rows, row)
		}
	}
	if rows == nil {
		rows = []DashboardRow{}
	}
	return rows
}

func computeMetrics(row DashboardRow) DashboardRow {
	eid := row.Experiment.ID

	// Last 7 days window
	weekAgo := time.Now().Add(-7 * 24 * 60 * 60 * time.Second).Format("2006-01-02 15:04:05")
	todayStart := time.Now().Format("2006-01-02") + " 00:00:00"
	yesterdayStart := time.Now().Add(-24 * time.Hour).Format("2006-01-02") + " 00:00:00"

	// Visitors = pageview events
	db.QueryRow("SELECT COUNT(*) FROM events WHERE experiment_id = ? AND event_type = 'pageview' AND created_at >= ?", eid, weekAgo).Scan(&row.Visitors)

	// Engagements = click, visit, interaction events
	db.QueryRow("SELECT COUNT(*) FROM events WHERE experiment_id = ? AND event_type IN ('click','visit','interaction','message') AND created_at >= ?", eid, weekAgo).Scan(&row.Engagements)

	// Leads = lead, inquiry, signup, waitlist
	db.QueryRow("SELECT COUNT(*) FROM events WHERE experiment_id = ? AND event_type IN ('lead','inquiry','signup','waitlist','meeting_booked') AND created_at >= ?", eid, weekAgo).Scan(&row.Leads)

	// Conversions = conversion, purchase, paid
	db.QueryRow("SELECT COUNT(*) FROM events WHERE experiment_id = ? AND event_type IN ('conversion','purchase','paid','deal_closed') AND created_at >= ?", eid, weekAgo).Scan(&row.Conversions)

	// Score = weighted formula
	row.Score = float64(row.Visitors)*0.05 +
		float64(row.Engagements)*0.15 +
		float64(row.Leads)*0.35 +
		float64(row.Conversions)*0.45

	// Trend check — more yesterday than day before?
	var todayCount, yesterdayCount int
	db.QueryRow("SELECT COUNT(*) FROM events WHERE experiment_id = ? AND created_at >= ?", eid, todayStart).Scan(&todayCount)
	db.QueryRow("SELECT COUNT(*) FROM events WHERE experiment_id = ? AND created_at >= ? AND created_at < ?", eid, yesterdayStart, todayStart).Scan(&yesterdayCount)
	row.TrendUp = todayCount >= yesterdayCount

	return row
}

func computeScore(rows []DashboardRow) ScoreSummary {
	var top, second *DashboardRow

	for i := range rows {
		if top == nil || rows[i].Score > top.Score {
			second = top
			top = &rows[i]
		} else if second == nil || rows[i].Score > second.Score {
			second = &rows[i]
		}
	}

	summary := ScoreSummary{}
	if top != nil {
		summary.AllInSlug = top.Experiment.Slug
		summary.AllInName = top.Experiment.Name
		summary.AllInScore = top.Score
	}
	if second != nil {
		summary.SecondSlug = second.Experiment.Slug
		summary.SecondName = second.Experiment.Name
		summary.SecondScore = second.Score
	}
	return summary
}

// --- Embedded HTML ---

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Command Dashboard</title>
<style>
:root {
  --bg: #0f1117; --card: #1a1d27; --border: #2a2d3a;
  --text: #e1e4ed; --muted: #888ca0; --accent: #6c63ff;
  --green: #4ade80; --amber: #fbbf24; --red: #f87171;
}
* { margin: 0; padding: 0; box-sizing: border-box; }
body {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
  background: var(--bg); color: var(--text); padding: 20px;
}
.container { max-width: 1200px; margin: 0 auto; }
header {
  display: flex; justify-content: space-between; align-items: center;
  margin-bottom: 30px; padding-bottom: 20px; border-bottom: 1px solid var(--border);
}
h1 { font-size: 24px; font-weight: 700; }
h1 span { color: var(--accent); }
.header-meta { color: var(--muted); font-size: 13px; text-align: right; }
.header-meta .version { display: block; }
.decision-banner {
  background: linear-gradient(135deg, #1a1d27 0%, #252840 100%);
  border: 2px solid var(--accent); border-radius: 12px;
  padding: 20px 24px; margin-bottom: 30px;
  display: flex; justify-content: space-between; align-items: center;
}
.decision-banner h2 { font-size: 18px; color: var(--accent); }
.decision-banner .lead-name { font-size: 28px; font-weight: 800; margin-top: 4px; }
.decision-banner .lead-score { font-size: 14px; color: var(--muted); margin-top: 2px; }
.decision-banner .runner-up { font-size: 13px; color: var(--muted); }
.leaderboard { display: grid; grid-template-columns: repeat(auto-fill, minmax(340px, 1fr)); gap: 16px; }
.card {
  background: var(--card); border: 1px solid var(--border); border-radius: 12px;
  padding: 20px; transition: transform 0.15s, box-shadow 0.15s;
}
.card:hover { transform: translateY(-2px); box-shadow: 0 8px 24px rgba(0,0,0,0.3); }
.card.leader {
  border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent), 0 4px 20px rgba(108,99,255,0.15);
}
.card-header {
  display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 16px;
}
.card-title { font-size: 16px; font-weight: 600; }
.card-url { font-size: 11px; color: var(--accent); text-decoration: none; display: block; margin-top: 2px; }
.card-badge {
  font-size: 10px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.5px;
  padding: 3px 8px; border-radius: 4px;
}
.badge-leader { background: var(--accent); color: #fff; }
.badge-watch { background: var(--amber); color: #000; }
.badge-lagging { background: var(--red); color: #fff; }
.metrics { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-bottom: 16px; }
.metric { text-align: center; }
.metric-value { font-size: 24px; font-weight: 700; }
.metric-label { font-size: 11px; color: var(--muted); text-transform: uppercase; letter-spacing: 0.3px; margin-top: 2px; }
.metric.up .metric-value { color: var(--green); }
.metric.down .metric-value { color: var(--red); }
.card-footer {
  display: flex; justify-content: space-between; align-items: center;
  padding-top: 12px; border-top: 1px solid var(--border);
}
.score-bar {
  flex: 1; height: 6px; background: var(--border); border-radius: 3px; overflow: hidden; margin: 0 12px 0 0;
}
.score-fill { height: 100%; border-radius: 3px; background: linear-gradient(90deg, var(--accent), #8b83ff); transition: width 0.5s; }
.score-label { font-size: 14px; font-weight: 600; white-space: nowrap; }
.actions { margin-top: 30px; display: flex; gap: 12px; flex-wrap: wrap; }
.btn {
  padding: 10px 20px; border-radius: 8px; border: 1px solid var(--border);
  background: var(--card); color: var(--text); font-size: 13px; cursor: pointer;
  text-decoration: none; display: inline-flex; align-items: center; gap: 6px;
}
.btn:hover { background: var(--border); }
.btn-primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.btn-primary:hover { background: #7b73ff; }
.quick-log { margin-top: 30px; background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 20px; }
.quick-log h3 { font-size: 14px; margin-bottom: 12px; color: var(--muted); }
.quick-log form { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; }
.quick-log select, .quick-log input, .quick-log button {
  padding: 8px 12px; border-radius: 6px; border: 1px solid var(--border);
  background: var(--bg); color: var(--text); font-size: 13px;
}
.quick-log button { background: var(--accent); border-color: var(--accent); cursor: pointer; }
.quick-log .status { font-size: 12px; color: var(--green); margin-left: 10px; }
</style>
</head>
<body>
<div class="container">
  <header>
    <h1>⚡ <span>Command</span> Dashboard</h1>
    <div class="header-meta">
      <span class="version">{{.Version}}</span>
      <span>{{.Now}}</span>
    </div>
  </header>

  <div class="decision-banner" id="decisionBanner">
    <div>
      <h2>🏆 Current Leader</h2>
      <div class="lead-name" id="leadName">—</div>
      <div class="lead-score" id="leadScore">Score: 0</div>
    </div>
    <div class="runner-up" id="runnerUp">Runner-up: —</div>
  </div>

  <div class="leaderboard" id="leaderboard">
    {{range .Experiments}}
    <div class="card" data-score="{{.Score}}" data-slug="{{.Experiment.Slug}}">
      <div class="card-header">
        <div>
          <div class="card-title">{{.Experiment.Name}}</div>
          {{if .Experiment.URL}}<a class="card-url" href="{{.Experiment.URL}}" target="_blank">{{.Experiment.URL}}</a>{{end}}
        </div>
        <span class="card-badge" id="badge-{{.Experiment.Slug}}">active</span>
      </div>
      <div class="metrics">
        <div class="metric {{if .TrendUp}}up{{else}}down{{end}}">
          <div class="metric-value">{{.Visitors}}</div>
          <div class="metric-label">Traffic</div>
        </div>
        <div class="metric">
          <div class="metric-value">{{.Engagements}}</div>
          <div class="metric-label">Engagements</div>
        </div>
        <div class="metric">
          <div class="metric-value">{{.Leads}}</div>
          <div class="metric-label">Leads</div>
        </div>
        <div class="metric">
          <div class="metric-value">{{.Conversions}}</div>
          <div class="metric-label">Conversions</div>
        </div>
      </div>
      <div class="card-footer">
        <div class="score-bar"><div class="score-fill" style="width:{{printf "%.0f" .Score}}%"></div></div>
        <div class="score-label">{{printf "%.0f" .Score}}</div>
      </div>
    </div>
    {{end}}
  </div>

  <div class="actions">
    <a class="btn btn-primary" href="/api/dashboard">📊 API (JSON)</a>
    <a class="btn" href="/api/db-view" target="_blank">🗄️ Raw DB</a>
    <a class="btn" href="/api/events">📝 All Events</a>
  </div>

  <div class="quick-log">
    <h3>📥 Quick Log Event</h3>
    <form id="logForm">
      <select name="experiment" id="expSelect">
        <option value="silk-gallery">Silk Gallery</option>
        <option value="whatsapp-os">WhatsApp Commerce OS</option>
        <option value="hair-care">Hair Care Trio</option>
        <option value="automation-boutique">AI Automation Boutique</option>
        <option value="cofounder">Co-founder Platform</option>
        <option value="stitch">Stitch Date Planner</option>
        <option value="ems-sme">EMS SME Education</option>
        <option value="servicenow-freelance">ServiceNow Freelance</option>
      </select>
      <select name="event_type" id="eventTypeSelect">
        <option value="pageview">Page View</option>
        <option value="lead">Lead / Inquiry</option>
        <option value="signup">Signup / Waitlist</option>
        <option value="message">Message / Contact</option>
        <option value="conversion">Conversion / Purchase</option>
      </select>
      <input type="text" name="source" placeholder="source (e.g. instagram)" id="sourceInput">
      <button type="submit">Log</button>
      <span class="status" id="logStatus"></span>
    </form>
  </div>
</div>

<script>
// Update leader badges
const scores = [];
document.querySelectorAll('.card').forEach(c => {
  const score = parseFloat(c.dataset.score);
  const slug = c.dataset.slug;
  scores.push({score, slug, name: c.querySelector('.card-title').textContent});
});
scores.sort((a, b) => b.score - a.score);

const maxScore = scores.length > 0 ? scores[0].score : 1;

// Normalize score bar widths
document.querySelectorAll('.card').forEach(c => {
  const score = parseFloat(c.dataset.score);
  const fill = c.querySelector('.score-fill');
  if (fill) fill.style.width = (maxScore > 0 ? (score/maxScore)*100 : 0) + '%';
});

// Badges and leader highlight
scores.forEach((s, i) => {
  const badge = document.getElementById('badge-' + s.slug);
  const card = document.querySelector('.card[data-slug="' + s.slug + '"]');
  if (badge) {
    if (i === 0) {
      badge.textContent = '🏆 LEADING';
      badge.className = 'card-badge badge-leader';
      if (card) card.classList.add('leader');
    } else if (i < 3) {
      badge.textContent = '👀 WATCH';
      badge.className = 'card-badge badge-watch';
    } else {
      badge.textContent = '⬇ LAGGING';
      badge.className = 'card-badge badge-lagging';
    }
  }
});

// Decision banner
if (scores.length > 0) {
  document.getElementById('leadName').textContent = scores[0].name;
  document.getElementById('leadScore').textContent = 'Score: ' + scores[0].score.toFixed(1);
  if (scores.length > 1) {
    document.getElementById('runnerUp').textContent = 'Runner-up: ' + scores[1].name + ' (' + scores[1].score.toFixed(1) + ')';
  }
}

// Quick log form
document.getElementById('logForm').addEventListener('submit', async function(e) {
  e.preventDefault();
  const status = document.getElementById('logStatus');
  status.textContent = 'logging...';
  const data = {
    experiment: document.getElementById('expSelect').value,
    event_type: document.getElementById('eventTypeSelect').value,
    source: document.getElementById('sourceInput').value || 'manual'
  };
  try {
    const res = await fetch('/api/event', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(data) });
    if (res.ok) {
      status.textContent = '✅ logged! Reload page to see update.';
    } else {
      status.textContent = '❌ error';
    }
  } catch(e) {
    status.textContent = '❌ ' + e.message;
  }
});
</script>
</body>
</html>`
