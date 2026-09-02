// Package report собирает записи времени в недельную сетку, отчёт за период
// и позиции для счёта.
package report

import (
	"sort"
	"time"

	"github.com/dripips/span/internal/store"
)

// ── Недельная сетка ─────────────────────────────────────────────────────

type Cell struct {
	Minutes int
	Entries []*store.Entry
}

type Row struct {
	Project *store.Project
	Days    [7]Cell
	Total   int
}

type Week struct {
	Start     time.Time // понедельник, местное время
	Days      [7]time.Time
	Rows      []*Row
	DayTotals [7]int
	Total     int
	Amount    float64
	Currency  string
}

// StartOfWeek — понедельник недели, в которую попадает t.
func StartOfWeek(t time.Time) time.Time {
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	shift := (int(t.Weekday()) + 6) % 7 // воскресенье = 6, а не 0
	return t.AddDate(0, 0, -shift)
}

// BuildWeek раскладывает записи по проектам и дням. Проекты без часов в этой
// неделе всё равно показываются: пустая строка — тоже сведение.
func BuildWeek(start time.Time, projects []*store.Project, entries []*store.Entry) *Week {
	w := &Week{Start: start}
	for i := 0; i < 7; i++ {
		w.Days[i] = start.AddDate(0, 0, i)
	}

	index := map[int64]*Row{}
	for _, p := range projects {
		r := &Row{Project: p}
		index[p.ID] = r
		w.Rows = append(w.Rows, r)
	}

	for _, e := range entries {
		day := int(e.StartedAt.Local().Sub(start).Hours() / 24)
		if day < 0 || day > 6 {
			continue
		}
		r, ok := index[e.ProjectID]
		if !ok {
			// проект в архиве, но часы за эту неделю есть — строку показываем
			p := &store.Project{ID: e.ProjectID, Name: e.ProjectName, ClientName: e.ClientName, Color: e.Color}
			r = &Row{Project: p}
			index[e.ProjectID] = r
			w.Rows = append(w.Rows, r)
		}
		m := e.Minutes()
		r.Days[day].Minutes += m
		r.Days[day].Entries = append(r.Days[day].Entries, e)
		r.Total += m
		w.DayTotals[day] += m
		w.Total += m
		w.Amount += e.Amount()
		if w.Currency == "" {
			w.Currency = e.Currency
		}
	}

	// Пустые строки уезжают вниз: неделя читается сверху вниз по занятости.
	sort.SliceStable(w.Rows, func(i, j int) bool {
		if (w.Rows[i].Total > 0) != (w.Rows[j].Total > 0) {
			return w.Rows[i].Total > 0
		}
		return w.Rows[i].Total > w.Rows[j].Total
	})
	return w
}

// ── Отчёт за период ─────────────────────────────────────────────────────

type ProjectSum struct {
	Project  string
	Minutes  int
	Billable int
	Amount   float64
	Color    string
}

type ClientSum struct {
	Client   string
	Currency string
	Minutes  int
	Billable int
	Amount   float64
	Projects []*ProjectSum
}

type Period struct {
	From, To time.Time
	Clients  []*ClientSum
	Minutes  int
	Billable int
	Amount   float64
	Currency string
}

func BuildPeriod(from, to time.Time, entries []*store.Entry) *Period {
	p := &Period{From: from, To: to}
	byClient := map[string]*ClientSum{}
	byProject := map[string]*ProjectSum{}

	for _, e := range entries {
		cs, ok := byClient[e.ClientName]
		if !ok {
			cs = &ClientSum{Client: e.ClientName, Currency: e.Currency}
			byClient[e.ClientName] = cs
			p.Clients = append(p.Clients, cs)
		}
		key := e.ClientName + "\x00" + e.ProjectName
		ps, ok := byProject[key]
		if !ok {
			ps = &ProjectSum{Project: e.ProjectName, Color: e.Color}
			byProject[key] = ps
			cs.Projects = append(cs.Projects, ps)
		}

		m := e.Minutes()
		amount := e.Amount()
		ps.Minutes += m
		cs.Minutes += m
		p.Minutes += m
		if e.Billable {
			ps.Billable += m
			cs.Billable += m
			p.Billable += m
		}
		ps.Amount += amount
		cs.Amount += amount
		p.Amount += amount
		if p.Currency == "" {
			p.Currency = e.Currency
		}
	}

	sort.Slice(p.Clients, func(i, j int) bool { return p.Clients[i].Amount > p.Clients[j].Amount })
	for _, cs := range p.Clients {
		sort.Slice(cs.Projects, func(i, j int) bool { return cs.Projects[i].Amount > cs.Projects[j].Amount })
	}
	return p
}

// ── Позиции счёта ───────────────────────────────────────────────────────

type Line struct {
	Client      string
	Description string
	Hours       float64
	Rate        float64
	Amount      float64
	Currency    string
}

// InvoiceLines сворачивает оплачиваемые записи периода в строки счёта:
// одна строка на проект, часы суммой. Неоплачиваемое не попадает сюда
// вовсе — в отчёте оно видно, в счёте его быть не должно.
func InvoiceLines(entries []*store.Entry) []*Line {
	index := map[string]*Line{}
	var out []*Line
	for _, e := range entries {
		if !e.Billable {
			continue
		}
		key := e.ClientName + "\x00" + e.ProjectName
		l, ok := index[key]
		if !ok {
			l = &Line{Client: e.ClientName, Description: e.ProjectName, Rate: e.Rate, Currency: e.Currency}
			index[key] = l
			out = append(out, l)
		}
		l.Hours += e.Hours()
		l.Amount += e.Amount()
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Client != out[j].Client {
			return out[i].Client < out[j].Client
		}
		return out[i].Amount > out[j].Amount
	})
	return out
}
