// Package web — HTTP-слой Span: неделя, отчёт, клиенты, таймер.
package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/dripips/span/internal/i18n"
	"github.com/dripips/span/internal/report"
	"github.com/dripips/span/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

const (
	sessionCookie = "span_session"
	langCookie    = "span_lang"
)

type Server struct {
	DB     *store.DB
	secret []byte
	tpl    *template.Template
}

func New(db *store.DB) (*Server, error) {
	s := &Server{DB: db}
	secret := db.Setting("session_secret", "")
	if secret == "" {
		secret = randomToken() + randomToken()
		if err := db.SetSetting("session_secret", secret); err != nil {
			return nil, err
		}
	}
	s.secret = []byte(secret)

	tpl, err := template.New("").Funcs(s.funcs()).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	s.tpl = tpl
	return s, nil
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Handle("/static/*", http.FileServer(http.FS(staticFS)))
	r.Get("/login", s.loginForm)
	r.Post("/login", s.loginSubmit)
	r.Post("/logout", s.logout)
	r.Post("/lang", s.setLang)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") })

	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/", s.week)
		r.Post("/timer/start", s.timerStart)
		r.Post("/timer/stop", s.timerStop)
		r.Get("/entries/new", s.entryForm)
		r.Post("/entries", s.entrySave)
		r.Get("/entries/{id}/edit", s.entryForm)
		r.Post("/entries/{id}", s.entrySave)
		r.Post("/entries/{id}/delete", s.entryDelete)
		r.Get("/report", s.reportPage)
		r.Get("/report.csv", s.reportCSV)
		r.Get("/clients", s.clients)
		r.Post("/clients", s.clientSave)
		r.Post("/clients/{id}", s.clientSave)
		r.Post("/clients/{id}/delete", s.clientDelete)
		r.Post("/projects", s.projectSave)
		r.Post("/projects/{id}", s.projectSave)
		r.Post("/projects/{id}/delete", s.projectDelete)
	})
	return r
}

// ── сессия ──────────────────────────────────────────────────────────────

func randomToken() string {
	return fmt.Sprintf("%x", time.Now().UnixNano()) + fmt.Sprintf("%x", time.Now().Unix())
}

func (s *Server) sign(v string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(v))
	return v + "." + hex.EncodeToString(m.Sum(nil))
}

func (s *Server) valid(token string) bool {
	i := strings.LastIndex(token, ".")
	if i < 0 {
		return false
	}
	return hmac.Equal([]byte(s.sign(token[:i])), []byte(token))
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || !s.valid(c.Value) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) lang(r *http.Request) string {
	if c, err := r.Cookie(langCookie); err == nil && i18n.Valid(c.Value) {
		return c.Value
	}
	return s.DB.Setting("lang", "ru")
}

// ── страницы ────────────────────────────────────────────────────────────

type pageData struct {
	Lang     string
	Path     string
	Title    string
	Weekdays [7]string
	Week     *report.Week
	Period   *report.Period
	Lines    []*report.Line
	Projects []*store.Project
	Clients  []*store.Client
	Entries  []*store.Entry
	Entry    *store.Entry
	Running  *store.Entry
	Day      time.Time
	From, To time.Time
	Error    string
	Langs    []string
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data pageData) {
	data.Lang = s.lang(r)
	data.Path = r.URL.Path
	data.Langs = i18n.Langs
	data.Weekdays = i18n.Weekdays(data.Lang)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) week(w http.ResponseWriter, r *http.Request) {
	start := report.StartOfWeek(time.Now())
	if v := r.URL.Query().Get("week"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
			start = report.StartOfWeek(t)
		}
	}
	projects, err := s.DB.Projects(false)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	entries, err := s.DB.Entries(start.UTC(), start.AddDate(0, 0, 7).UTC())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	running, _ := s.DB.Running()

	day := time.Now()
	if v := r.URL.Query().Get("day"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
			day = t
		}
	}
	dayEntries, _ := s.DB.Entries(
		time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local).UTC(),
		time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, 1).UTC())

	s.render(w, r, "week.html", pageData{
		Title:    i18n.T(s.lang(r), "week.title"),
		Week:     report.BuildWeek(start, projects, entries),
		Projects: projects, Running: running, Entries: dayEntries, Day: day,
	})
}

func (s *Server) timerStart(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.FormValue("project_id"), 10, 64)
	if id > 0 {
		if err := s.DB.Start(id, r.FormValue("description")); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	http.Redirect(w, r, backTo(r), http.StatusFound)
}

func (s *Server) timerStop(w http.ResponseWriter, r *http.Request) {
	_ = s.DB.StopRunning()
	http.Redirect(w, r, backTo(r), http.StatusFound)
}

func (s *Server) entryForm(w http.ResponseWriter, r *http.Request) {
	projects, _ := s.DB.Projects(false)
	data := pageData{Title: i18n.T(s.lang(r), "entry.add"), Projects: projects, Day: time.Now()}
	if id := idParam(r); id > 0 {
		e, err := s.DB.Entry(id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		data.Entry = e
		data.Day = e.StartedAt.Local()
		data.Title = i18n.T(s.lang(r), "entry.edit")
	} else if v := r.URL.Query().Get("day"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
			data.Day = t
		}
	}
	if v := r.URL.Query().Get("project"); v != "" && data.Entry == nil {
		id, _ := strconv.ParseInt(v, 10, 64)
		data.Entry = &store.Entry{ProjectID: id}
	}
	s.render(w, r, "entry.html", data)
}

func (s *Server) entrySave(w http.ResponseWriter, r *http.Request) {
	projectID, _ := strconv.ParseInt(r.FormValue("project_id"), 10, 64)
	date := r.FormValue("date")
	start, err1 := time.ParseInLocation("2006-01-02 15:04", date+" "+r.FormValue("from"), time.Local)
	end, err2 := time.ParseInLocation("2006-01-02 15:04", date+" "+r.FormValue("to"), time.Local)
	if projectID == 0 || err1 != nil || err2 != nil {
		projects, _ := s.DB.Projects(false)
		s.render(w, r, "entry.html", pageData{Title: i18n.T(s.lang(r), "entry.add"),
			Projects: projects, Day: time.Now(), Error: "invalid"})
		return
	}
	if !end.After(start) { // ночная смена: окончание раньше начала — значит следующий день
		end = end.AddDate(0, 0, 1)
	}

	e := &store.Entry{
		ID: idParam(r), ProjectID: projectID,
		Description: strings.TrimSpace(r.FormValue("description")),
		StartedAt:   start.UTC(),
		EndedAt:     sql.NullTime{Time: end.UTC(), Valid: true},
		Billable:    r.FormValue("billable") != "",
	}
	if err := s.DB.SaveEntry(e); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	http.Redirect(w, r, "/?week="+start.Format("2006-01-02")+"&day="+start.Format("2006-01-02"), http.StatusFound)
}

func (s *Server) entryDelete(w http.ResponseWriter, r *http.Request) {
	_ = s.DB.DeleteEntry(idParam(r))
	http.Redirect(w, r, backTo(r), http.StatusFound)
}

func (s *Server) periodFromQuery(r *http.Request) (time.Time, time.Time) {
	now := time.Now()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	to := from.AddDate(0, 1, 0)
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
			to = t.AddDate(0, 0, 1)
		}
	}
	return from, to
}

func (s *Server) reportPage(w http.ResponseWriter, r *http.Request) {
	from, to := s.periodFromQuery(r)
	entries, err := s.DB.Entries(from.UTC(), to.UTC())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.render(w, r, "report.html", pageData{
		Title:  i18n.T(s.lang(r), "report.title"),
		Period: report.BuildPeriod(from, to, entries),
		Lines:  report.InvoiceLines(entries),
		From:   from, To: to.AddDate(0, 0, -1),
	})
}

func (s *Server) reportCSV(w http.ResponseWriter, r *http.Request) {
	from, to := s.periodFromQuery(r)
	entries, _ := s.DB.Entries(from.UTC(), to.UTC())

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="span-%s.csv"`, from.Format("2006-01")))
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"date", "client", "project", "description", "hours", "billable", "rate", "amount", "currency"})
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		_ = cw.Write([]string{
			e.StartedAt.Local().Format("2006-01-02"), e.ClientName, e.ProjectName, e.Description,
			fmt.Sprintf("%.2f", e.Hours()), strconv.FormatBool(e.Billable),
			fmt.Sprintf("%.2f", e.Rate), fmt.Sprintf("%.2f", e.Amount()), e.Currency,
		})
	}
}

func (s *Server) clients(w http.ResponseWriter, r *http.Request) {
	clients, err := s.DB.Clients(true)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	projects, _ := s.DB.Projects(true)
	s.render(w, r, "clients.html", pageData{
		Title: i18n.T(s.lang(r), "clients.title"), Clients: clients, Projects: projects})
}

func (s *Server) clientSave(w http.ResponseWriter, r *http.Request) {
	rate, _ := strconv.ParseFloat(strings.Replace(r.FormValue("rate"), ",", ".", 1), 64)
	rounding, _ := strconv.Atoi(r.FormValue("rounding"))
	c := &store.Client{
		ID: idParam(r), Name: strings.TrimSpace(r.FormValue("name")), Rate: rate,
		Currency: r.FormValue("currency"), Rounding: store.Rounding(rounding),
		Notes: strings.TrimSpace(r.FormValue("notes")),
	}
	if c.Currency == "" {
		c.Currency = "EUR"
	}
	_ = s.DB.SaveClient(c)
	http.Redirect(w, r, "/clients", http.StatusFound)
}

func (s *Server) clientDelete(w http.ResponseWriter, r *http.Request) {
	_ = s.DB.DeleteClient(idParam(r))
	http.Redirect(w, r, "/clients", http.StatusFound)
}

func (s *Server) projectSave(w http.ResponseWriter, r *http.Request) {
	clientID, _ := strconv.ParseInt(r.FormValue("client_id"), 10, 64)
	p := &store.Project{
		ID: idParam(r), ClientID: clientID,
		Name:  strings.TrimSpace(r.FormValue("name")),
		Color: r.FormValue("color"),
	}
	if raw := strings.TrimSpace(strings.Replace(r.FormValue("rate"), ",", ".", 1)); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
			p.Rate = sql.NullFloat64{Float64: v, Valid: true}
		}
	}
	if p.Color == "" {
		p.Color = "#2563eb"
	}
	_ = s.DB.SaveProject(p)
	http.Redirect(w, r, "/clients", http.StatusFound)
}

func (s *Server) projectDelete(w http.ResponseWriter, r *http.Request) {
	_ = s.DB.DeleteProject(idParam(r))
	http.Redirect(w, r, "/clients", http.StatusFound)
}

// ── вход и язык ─────────────────────────────────────────────────────────

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "login.html", pageData{Title: i18n.T(s.lang(r), "login.title"),
		Error: r.URL.Query().Get("error")})
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	hash, err := s.DB.UserHash(email)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(r.FormValue("password"))) != nil {
		http.Redirect(w, r, "/login?error=1", http.StatusFound)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: s.sign(email), Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *Server) setLang(w http.ResponseWriter, r *http.Request) {
	lang := r.FormValue("lang")
	if i18n.Valid(lang) {
		http.SetCookie(w, &http.Cookie{Name: langCookie, Value: lang, Path: "/", MaxAge: 365 * 24 * 3600})
		_ = s.DB.SetSetting("lang", lang)
	}
	http.Redirect(w, r, backTo(r), http.StatusFound)
}

// ── помощники ───────────────────────────────────────────────────────────

func idParam(r *http.Request) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id
}

func backTo(r *http.Request) string {
	if ref := r.Header.Get("Referer"); ref != "" {
		return ref
	}
	return "/"
}

func Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}
