package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"html/template"
	"net/http"
	"strings"
	"sync"
	"time"
)

// registerWebUIRoutes adds the admin web UI routes.
func (s *Server) registerWebUIRoutes() {
	s.mux.HandleFunc("GET /admin", s.requireAdminPassword(s.handleAdminDashboard))
	s.mux.HandleFunc("GET /admin/nodes", s.requireAdminPassword(s.handleNodeBrowser))
	s.mux.HandleFunc("GET /admin/repos", s.requireAdminPassword(s.handleRepoMappings))
	s.mux.HandleFunc("GET /admin/devices", s.requireAdminPassword(s.handleDeviceManagement))
	s.mux.HandleFunc("POST /admin/login", s.handleAdminLogin)
}

// --- Admin session management ---

var (
	adminSessions   = map[string]time.Time{}
	adminSessionsMu sync.Mutex
)

const adminSessionCookie = "ctx_admin_session"
const adminSessionTTL = 24 * time.Hour

func (s *Server) requireAdminPassword(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.config.AdminPassword == "" {
			next(w, r)
			return
		}

		cookie, err := r.Cookie(adminSessionCookie)
		if err == nil {
			adminSessionsMu.Lock()
			expiry, ok := adminSessions[cookie.Value]
			adminSessionsMu.Unlock()
			if ok && time.Now().Before(expiry) {
				next(w, r)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = adminLoginTmpl.Execute(w, map[string]string{"Redirect": r.URL.Path})
	}
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	password := r.FormValue("password")
	redirect := r.FormValue("redirect")
	if redirect == "" {
		redirect = "/admin"
	}

	if !s.verifyAdminPassword(password) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = adminLoginTmpl.Execute(w, map[string]string{"Error": "Invalid password.", "Redirect": redirect})
		return
	}

	// Create session
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	sessionID := hex.EncodeToString(b)

	adminSessionsMu.Lock()
	adminSessions[sessionID] = time.Now().Add(adminSessionTTL)
	adminSessionsMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    sessionID,
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(adminSessionTTL.Seconds()),
	})
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

var adminLoginTmpl = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html><head><title>ctx — Admin Login</title>` + baseCSS + `
<style>.login { max-width: 400px; margin: 80px auto; }
.login input[type=password] { width: 100%; padding: 10px; margin: 10px 0; border: 1px solid #ddd; border-radius: 6px; font-size: 14px; }
.login button { width: 100%; padding: 10px; background: #1a1a2e; color: #fff; border: none; border-radius: 6px; cursor: pointer; font-size: 14px; }
.login .error { color: #ef4444; margin-bottom: 10px; }
</style></head><body>
<div class="login">
<h2>ctx Admin</h2>
{{if .Error}}<p class="error">{{.Error}}</p>{{end}}
<form method="POST" action="/admin/login">
<input type="hidden" name="redirect" value="{{.Redirect}}">
<p>Enter admin password:</p>
<input type="password" name="password" autofocus>
<button type="submit">Sign in</button>
</form>
</div>
</body></html>`))

// --- Dashboard ---

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	var totalNodes, totalTokens, edgeCount, tagCount, deviceCount int
	_ = s.store.QueryRow("SELECT COUNT(*) FROM nodes WHERE superseded_by IS NULL").Scan(&totalNodes)
	_ = s.store.QueryRow("SELECT COALESCE(SUM(token_estimate), 0) FROM nodes WHERE superseded_by IS NULL").Scan(&totalTokens)
	_ = s.store.QueryRow("SELECT COUNT(*) FROM edges").Scan(&edgeCount)
	_ = s.store.QueryRow("SELECT COUNT(DISTINCT tag) FROM tags").Scan(&tagCount)

	// Device count may fail if table doesn't exist (SQLite mode)
	_ = s.store.QueryRow("SELECT COUNT(*) FROM devices").Scan(&deviceCount)

	type recentNode struct {
		ID        string
		Type      string
		Content   string
		CreatedAt string
	}
	var recent []recentNode
	rows, err := s.store.Query(
		"SELECT id, type, substr(content, 1, 120), created_at FROM nodes WHERE superseded_by IS NULL ORDER BY created_at DESC LIMIT 10")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var n recentNode
			_ = rows.Scan(&n.ID, &n.Type, &n.Content, &n.CreatedAt)
			recent = append(recent, n)
		}
	}

	data := map[string]any{
		"TotalNodes":  totalNodes,
		"TotalTokens": totalTokens,
		"EdgeCount":   edgeCount,
		"TagCount":    tagCount,
		"DeviceCount": deviceCount,
		"Recent":      recent,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = dashboardTmpl.Execute(w, data)
}

// --- Node Browser ---

func (s *Server) handleNodeBrowser(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("q")
	typeFilter := r.URL.Query().Get("type")

	type nodeRow struct {
		ID        string
		Type      string
		Content   string
		Tokens    int
		CreatedAt string
		Tags      string
	}

	var nodes []nodeRow
	var queryStr string
	var args []any

	if search != "" {
		queryStr = `SELECT id, type, substr(content, 1, 200), token_estimate, created_at FROM nodes
			WHERE superseded_by IS NULL AND content LIKE $1 ORDER BY created_at DESC LIMIT 50`
		args = append(args, "%"+search+"%")
	} else if typeFilter != "" {
		queryStr = `SELECT id, type, substr(content, 1, 200), token_estimate, created_at FROM nodes
			WHERE superseded_by IS NULL AND type = $1 ORDER BY created_at DESC LIMIT 50`
		args = append(args, typeFilter)
	} else {
		queryStr = `SELECT id, type, substr(content, 1, 200), token_estimate, created_at FROM nodes
			WHERE superseded_by IS NULL ORDER BY created_at DESC LIMIT 50`
	}

	rows, err := s.store.Query(queryStr, args...)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var n nodeRow
			_ = rows.Scan(&n.ID, &n.Type, &n.Content, &n.Tokens, &n.CreatedAt)
			// Get tags
			tags, _ := s.store.GetTags(n.ID)
			n.Tags = strings.Join(tags, ", ")
			nodes = append(nodes, n)
		}
	}

	data := map[string]any{
		"Nodes":  nodes,
		"Search": search,
		"Type":   typeFilter,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = nodesBrowserTmpl.Execute(w, data)
}

// --- Repo Mappings ---

func (s *Server) handleRepoMappings(w http.ResponseWriter, r *http.Request) {
	type repoMapping struct {
		ID            string
		NormalizedURL string
		ProjectTag    string
		CreatedAt     string
	}

	var mappings []repoMapping
	rows, err := s.store.Query("SELECT id, normalized_url, project_tag, created_at FROM repo_mappings ORDER BY created_at DESC")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var m repoMapping
			_ = rows.Scan(&m.ID, &m.NormalizedURL, &m.ProjectTag, &m.CreatedAt)
			mappings = append(mappings, m)
		}
	}

	data := map[string]any{
		"Mappings": mappings,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = repoMappingsTmpl.Execute(w, data)
}

// --- Device Management ---

func (s *Server) handleDeviceManagement(w http.ResponseWriter, r *http.Request) {
	type device struct {
		ID        string
		Name      string
		LastSeen  string
		LastIP    string
		Revoked   bool
		CreatedAt string
	}

	var devices []device
	rows, err := s.store.Query("SELECT id, name, last_seen, last_ip, revoked, created_at FROM devices ORDER BY created_at DESC")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var d device
			var lastSeen, lastIP sql.NullString
			_ = rows.Scan(&d.ID, &d.Name, &lastSeen, &lastIP, &d.Revoked, &d.CreatedAt)
			if lastSeen.Valid {
				d.LastSeen = lastSeen.String
			}
			if lastIP.Valid {
				d.LastIP = lastIP.String
			}
			devices = append(devices, d)
		}
	}

	data := map[string]any{
		"Devices": devices,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = deviceMgmtTmpl.Execute(w, data)
}

// --- Templates ---

const baseCSS = `
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: system-ui, -apple-system, sans-serif; background: #f8f9fa; color: #1a1a2e; }
nav { background: #1a1a2e; padding: 12px 24px; display: flex; gap: 24px; align-items: center; }
nav a { color: #e0e0e0; text-decoration: none; font-size: 14px; }
nav a:hover, nav a.active { color: #fff; }
nav .brand { font-weight: 700; font-size: 18px; color: #fff; margin-right: 24px; }
.container { max-width: 1100px; margin: 24px auto; padding: 0 24px; }
.cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 16px; margin-bottom: 24px; }
.card { background: #fff; border-radius: 8px; padding: 20px; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
.card .label { font-size: 12px; text-transform: uppercase; color: #666; margin-bottom: 4px; }
.card .value { font-size: 28px; font-weight: 700; }
table { width: 100%; border-collapse: collapse; background: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
th { background: #f0f0f0; text-align: left; padding: 10px 14px; font-size: 12px; text-transform: uppercase; color: #666; }
td { padding: 10px 14px; border-top: 1px solid #eee; font-size: 14px; }
tr:hover td { background: #f8f8ff; }
.tag { display: inline-block; background: #e8e8ff; color: #4444aa; padding: 2px 8px; border-radius: 10px; font-size: 11px; margin: 1px; }
.type { display: inline-block; background: #e8ffe8; color: #228822; padding: 2px 8px; border-radius: 10px; font-size: 11px; }
.revoked { color: #ef4444; font-weight: 600; }
.active { color: #22c55e; font-weight: 600; }
h2 { margin-bottom: 16px; }
.search { margin-bottom: 16px; }
.search input { padding: 8px 14px; border: 1px solid #ddd; border-radius: 6px; width: 300px; font-size: 14px; }
.search button { padding: 8px 16px; background: #1a1a2e; color: #fff; border: none; border-radius: 6px; cursor: pointer; margin-left: 8px; }
.id { font-family: monospace; font-size: 12px; color: #666; }
.btn-revoke { padding: 4px 12px; background: #ef4444; color: #fff; border: none; border-radius: 4px; cursor: pointer; font-size: 12px; }
.empty { text-align: center; padding: 40px; color: #999; }
</style>
`

const navHTML = `
<nav>
<span class="brand">ctx</span>
<a href="/admin">Dashboard</a>
<a href="/admin/nodes">Nodes</a>
<a href="/admin/repos">Repos</a>
<a href="/admin/devices">Devices</a>
</nav>
`

var dashboardTmpl = template.Must(template.New("dashboard").Parse(`<!DOCTYPE html>
<html><head><title>ctx — Dashboard</title>` + baseCSS + `</head><body>
` + navHTML + `
<div class="container">
<h2>Dashboard</h2>
<div class="cards">
<div class="card"><div class="label">Nodes</div><div class="value">{{.TotalNodes}}</div></div>
<div class="card"><div class="label">Tokens</div><div class="value">{{.TotalTokens}}</div></div>
<div class="card"><div class="label">Edges</div><div class="value">{{.EdgeCount}}</div></div>
<div class="card"><div class="label">Tags</div><div class="value">{{.TagCount}}</div></div>
<div class="card"><div class="label">Devices</div><div class="value">{{.DeviceCount}}</div></div>
</div>
<h2>Recent Activity</h2>
{{if .Recent}}
<table>
<thead><tr><th>ID</th><th>Type</th><th>Content</th><th>Created</th></tr></thead>
<tbody>
{{range .Recent}}
<tr>
<td class="id">{{.ID}}</td>
<td><span class="type">{{.Type}}</span></td>
<td>{{.Content}}</td>
<td>{{.CreatedAt}}</td>
</tr>
{{end}}
</tbody>
</table>
{{else}}<div class="empty">No nodes yet.</div>{{end}}
</div>
</body></html>`))

var tmplFuncs = template.FuncMap{
	"split": strings.Split,
}

var nodesBrowserTmpl = template.Must(template.New("nodes").Funcs(tmplFuncs).Parse(`<!DOCTYPE html>
<html><head><title>ctx — Nodes</title>` + baseCSS + `</head><body>
` + navHTML + `
<div class="container">
<h2>Node Browser</h2>
<div class="search">
<form method="GET" action="/admin/nodes">
<input type="text" name="q" value="{{.Search}}" placeholder="Search nodes...">
<select name="type" onchange="this.form.submit()">
<option value="">All types</option>
<option value="fact" {{if eq .Type "fact"}}selected{{end}}>fact</option>
<option value="decision" {{if eq .Type "decision"}}selected{{end}}>decision</option>
<option value="pattern" {{if eq .Type "pattern"}}selected{{end}}>pattern</option>
<option value="observation" {{if eq .Type "observation"}}selected{{end}}>observation</option>
<option value="hypothesis" {{if eq .Type "hypothesis"}}selected{{end}}>hypothesis</option>
<option value="task" {{if eq .Type "task"}}selected{{end}}>task</option>
<option value="summary" {{if eq .Type "summary"}}selected{{end}}>summary</option>
</select>
<button type="submit">Search</button>
</form>
</div>
{{if .Nodes}}
<table>
<thead><tr><th>ID</th><th>Type</th><th>Content</th><th>Tokens</th><th>Tags</th><th>Created</th></tr></thead>
<tbody>
{{range .Nodes}}
<tr>
<td class="id">{{.ID}}</td>
<td><span class="type">{{.Type}}</span></td>
<td>{{.Content}}</td>
<td>{{.Tokens}}</td>
<td>{{range $i, $t := (split .Tags ", ")}}{{if $t}}<span class="tag">{{$t}}</span>{{end}}{{end}}</td>
<td>{{.CreatedAt}}</td>
</tr>
{{end}}
</tbody>
</table>
{{else}}<div class="empty">No nodes found.</div>{{end}}
</div>
</body></html>`))

var repoMappingsTmpl = template.Must(template.New("repos").Parse(`<!DOCTYPE html>
<html><head><title>ctx — Repo Mappings</title>` + baseCSS + `</head><body>
` + navHTML + `
<div class="container">
<h2>Repo Mappings</h2>
{{if .Mappings}}
<table>
<thead><tr><th>ID</th><th>Git Remote URL</th><th>Project Tag</th><th>Created</th></tr></thead>
<tbody>
{{range .Mappings}}
<tr>
<td class="id">{{.ID}}</td>
<td>{{.NormalizedURL}}</td>
<td><span class="tag">project:{{.ProjectTag}}</span></td>
<td>{{.CreatedAt}}</td>
</tr>
{{end}}
</tbody>
</table>
{{else}}<div class="empty">No repo mappings. Use <code>ctx sync register-repo</code> to register.</div>{{end}}
</div>
</body></html>`))

var deviceMgmtTmpl = template.Must(template.New("devices").Parse(`<!DOCTYPE html>
<html><head><title>ctx — Devices</title>` + baseCSS + `</head><body>
` + navHTML + `
<div class="container">
<h2>Device Management</h2>
{{if .Devices}}
<table>
<thead><tr><th>ID</th><th>Name</th><th>Status</th><th>Last Seen</th><th>Last IP</th><th>Created</th><th>Action</th></tr></thead>
<tbody>
{{range .Devices}}
<tr>
<td class="id">{{.ID}}</td>
<td>{{.Name}}</td>
<td>{{if .Revoked}}<span class="revoked">Revoked</span>{{else}}<span class="active">Active</span>{{end}}</td>
<td>{{.LastSeen}}</td>
<td>{{.LastIP}}</td>
<td>{{.CreatedAt}}</td>
<td>{{if not .Revoked}}<form method="POST" action="/api/devices/{{.ID}}/revoke" style="display:inline"><button class="btn-revoke" type="submit">Revoke</button></form>{{end}}</td>
</tr>
{{end}}
</tbody>
</table>
{{else}}<div class="empty">No devices registered. Use <code>ctx auth</code> from a device to register.</div>{{end}}
</div>
</body></html>`))
