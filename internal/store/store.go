// Package store держит схему и запросы Span: клиенты, проекты, записи времени.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Rounding — договорённость с клиентом о том, как считать неполные минуты.
type Rounding int

const (
	RoundExact   Rounding = 0  // по минутам
	RoundFive    Rounding = 5  // до 5 минут вверх
	RoundTen     Rounding = 10 // до 10 минут вверх
	RoundQuarter Rounding = 15
	RoundHalf    Rounding = 30
)

type Client struct {
	ID       int64
	Name     string
	Rate     float64
	Currency string
	Rounding Rounding
	Notes    string
	Archived bool
}

type Project struct {
	ID         int64
	ClientID   int64
	ClientName string
	Name       string
	Rate       sql.NullFloat64 // пусто — берём ставку клиента
	Color      string
	Archived   bool
}

// EffectiveRate: ставка проекта важнее ставки клиента — правки по старому
// договору могут стоить иначе, чем новая работа.
func (p *Project) EffectiveRate(clientRate float64) float64 {
	if p.Rate.Valid && p.Rate.Float64 > 0 {
		return p.Rate.Float64
	}
	return clientRate
}

type Entry struct {
	ID          int64
	ProjectID   int64
	ProjectName string
	ClientID    int64
	ClientName  string
	Color       string
	Description string
	StartedAt   time.Time
	EndedAt     sql.NullTime // пусто — таймер идёт
	Billable    bool
	Rate        float64
	Currency    string
	Rounding    Rounding
}

// Minutes — длительность записи с учётом округления клиента. Идущий таймер
// считается до текущего момента.
func (e *Entry) Minutes() int {
	end := time.Now().UTC()
	if e.EndedAt.Valid {
		end = e.EndedAt.Time
	}
	raw := end.Sub(e.StartedAt).Minutes()
	if raw < 0 {
		return 0
	}
	step := int(e.Rounding)
	if step <= 0 {
		return int(math.Round(raw))
	}
	return int(math.Ceil(raw/float64(step))) * step
}

func (e *Entry) Hours() float64 { return float64(e.Minutes()) / 60 }

func (e *Entry) Amount() float64 {
	if !e.Billable {
		return 0
	}
	return e.Hours() * e.Rate
}

func (e *Entry) Running() bool { return !e.EndedAt.Valid }

type DB struct{ *sql.DB }

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &DB{db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (d *DB) migrate() error {
	schema := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS clients (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			rate REAL NOT NULL DEFAULT 0,
			currency TEXT NOT NULL DEFAULT 'EUR',
			rounding INTEGER NOT NULL DEFAULT 0,
			notes TEXT NOT NULL DEFAULT '',
			archived INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS projects (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id INTEGER NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			rate REAL,
			color TEXT NOT NULL DEFAULT '#2563eb',
			archived INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			description TEXT NOT NULL DEFAULT '',
			started_at DATETIME NOT NULL,
			ended_at DATETIME,
			billable INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_started ON entries(started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_entries_project ON entries(project_id, started_at DESC)`,
		`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
	}
	for _, q := range schema {
		if _, err := d.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// ── Клиенты ─────────────────────────────────────────────────────────────

func (d *DB) Clients(includeArchived bool) ([]*Client, error) {
	q := `SELECT id, name, rate, currency, rounding, notes, archived FROM clients`
	if !includeArchived {
		q += ` WHERE archived = 0`
	}
	q += ` ORDER BY name COLLATE NOCASE`
	rows, err := d.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Client
	for rows.Next() {
		c := &Client{}
		if err := rows.Scan(&c.ID, &c.Name, &c.Rate, &c.Currency, &c.Rounding, &c.Notes, &c.Archived); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) Client(id int64) (*Client, error) {
	c := &Client{}
	err := d.QueryRow(`SELECT id, name, rate, currency, rounding, notes, archived FROM clients WHERE id=?`, id).
		Scan(&c.ID, &c.Name, &c.Rate, &c.Currency, &c.Rounding, &c.Notes, &c.Archived)
	return c, err
}

func (d *DB) SaveClient(c *Client) error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("name required")
	}
	if c.ID > 0 {
		_, err := d.Exec(`UPDATE clients SET name=?, rate=?, currency=?, rounding=?, notes=?, archived=? WHERE id=?`,
			c.Name, c.Rate, c.Currency, c.Rounding, c.Notes, c.Archived, c.ID)
		return err
	}
	res, err := d.Exec(`INSERT INTO clients (name, rate, currency, rounding, notes) VALUES (?,?,?,?,?)`,
		c.Name, c.Rate, c.Currency, c.Rounding, c.Notes)
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	return nil
}

func (d *DB) DeleteClient(id int64) error {
	_, err := d.Exec(`DELETE FROM clients WHERE id=?`, id)
	return err
}

// ── Проекты ─────────────────────────────────────────────────────────────

const projectColumns = `p.id, p.client_id, c.name, p.name, p.rate, p.color, p.archived`

func (d *DB) Projects(includeArchived bool) ([]*Project, error) {
	q := `SELECT ` + projectColumns + ` FROM projects p JOIN clients c ON c.id = p.client_id`
	if !includeArchived {
		q += ` WHERE p.archived = 0`
	}
	q += ` ORDER BY c.name COLLATE NOCASE, p.name COLLATE NOCASE`
	rows, err := d.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		p := &Project{}
		if err := rows.Scan(&p.ID, &p.ClientID, &p.ClientName, &p.Name, &p.Rate, &p.Color, &p.Archived); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (d *DB) Project(id int64) (*Project, error) {
	p := &Project{}
	err := d.QueryRow(`SELECT `+projectColumns+` FROM projects p JOIN clients c ON c.id = p.client_id WHERE p.id=?`, id).
		Scan(&p.ID, &p.ClientID, &p.ClientName, &p.Name, &p.Rate, &p.Color, &p.Archived)
	return p, err
}

func (d *DB) SaveProject(p *Project) error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("name required")
	}
	if p.ID > 0 {
		_, err := d.Exec(`UPDATE projects SET client_id=?, name=?, rate=?, color=?, archived=? WHERE id=?`,
			p.ClientID, p.Name, p.Rate, p.Color, p.Archived, p.ID)
		return err
	}
	res, err := d.Exec(`INSERT INTO projects (client_id, name, rate, color) VALUES (?,?,?,?)`,
		p.ClientID, p.Name, p.Rate, p.Color)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

func (d *DB) DeleteProject(id int64) error {
	_, err := d.Exec(`DELETE FROM projects WHERE id=?`, id)
	return err
}

// ── Записи времени ──────────────────────────────────────────────────────

const entryColumns = `e.id, e.project_id, p.name, p.client_id, c.name, p.color, e.description,
	e.started_at, e.ended_at, e.billable,
	COALESCE(NULLIF(p.rate, 0), c.rate), c.currency, c.rounding`

func scanEntry(row interface{ Scan(...any) error }) (*Entry, error) {
	e := &Entry{}
	err := row.Scan(&e.ID, &e.ProjectID, &e.ProjectName, &e.ClientID, &e.ClientName, &e.Color,
		&e.Description, &e.StartedAt, &e.EndedAt, &e.Billable, &e.Rate, &e.Currency, &e.Rounding)
	return e, err
}

const entryFrom = ` FROM entries e
	JOIN projects p ON p.id = e.project_id
	JOIN clients c ON c.id = p.client_id`

func (d *DB) Entries(from, to time.Time) ([]*Entry, error) {
	rows, err := d.Query(`SELECT `+entryColumns+entryFrom+`
		WHERE e.started_at >= ? AND e.started_at < ? ORDER BY e.started_at DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) Entry(id int64) (*Entry, error) {
	return scanEntry(d.QueryRow(`SELECT `+entryColumns+entryFrom+` WHERE e.id=?`, id))
}

// Running возвращает идущий таймер, если он есть. Их всегда не больше одного.
func (d *DB) Running() (*Entry, error) {
	e, err := scanEntry(d.QueryRow(`SELECT ` + entryColumns + entryFrom +
		` WHERE e.ended_at IS NULL ORDER BY e.started_at DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return e, err
}

// Start запускает таймер, предварительно остановив предыдущий: параллельные
// таймеры порождают часы, которых не было.
func (d *DB) Start(projectID int64, description string) error {
	if err := d.StopRunning(); err != nil {
		return err
	}
	_, err := d.Exec(`INSERT INTO entries (project_id, description, started_at, billable) VALUES (?,?,?,1)`,
		projectID, strings.TrimSpace(description), time.Now().UTC())
	return err
}

func (d *DB) StopRunning() error {
	_, err := d.Exec(`UPDATE entries SET ended_at=? WHERE ended_at IS NULL`, time.Now().UTC())
	return err
}

func (d *DB) SaveEntry(e *Entry) error {
	if e.ID > 0 {
		_, err := d.Exec(`UPDATE entries SET project_id=?, description=?, started_at=?, ended_at=?, billable=? WHERE id=?`,
			e.ProjectID, e.Description, e.StartedAt, e.EndedAt, e.Billable, e.ID)
		return err
	}
	res, err := d.Exec(`INSERT INTO entries (project_id, description, started_at, ended_at, billable) VALUES (?,?,?,?,?)`,
		e.ProjectID, e.Description, e.StartedAt, e.EndedAt, e.Billable)
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

func (d *DB) DeleteEntry(id int64) error {
	_, err := d.Exec(`DELETE FROM entries WHERE id=?`, id)
	return err
}

// ── Пользователь и настройки ────────────────────────────────────────────

func (d *DB) Setting(key, fallback string) string {
	var v string
	if err := d.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v); err != nil {
		return fallback
	}
	return v
}

func (d *DB) SetSetting(key, value string) error {
	_, err := d.Exec(`INSERT INTO settings (key, value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (d *DB) EnsureUser(email, hash string) (bool, error) {
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	_, err := d.Exec(`INSERT INTO users (email, password_hash, created_at) VALUES (?,?,?)`, email, hash, time.Now().UTC())
	return err == nil, err
}

func (d *DB) UserHash(email string) (string, error) {
	var h string
	err := d.QueryRow(`SELECT password_hash FROM users WHERE email=?`, email).Scan(&h)
	return h, err
}
