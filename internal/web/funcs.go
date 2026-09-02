package web

import (
	"fmt"
	"html/template"
	"math"
	"time"

	"github.com/dripips/span/internal/i18n"
	"github.com/dripips/span/internal/store"
)

func (s *Server) funcs() template.FuncMap {
	return template.FuncMap{
		"t":        i18n.T,
		"hm":       hoursMinutes,
		"money":    money,
		"day":      func(t time.Time) string { return t.Format("02.01") },
		"dayNum":   func(t time.Time) string { return t.Format("2") },
		"iso":      func(t time.Time) string { return t.Format("2006-01-02") },
		"clock":    func(t time.Time) string { return t.Local().Format("15:04") },
		"stamp":    func(t time.Time) string { return t.Local().Format("02.01.2006") },
		"monthly":  func(t time.Time) string { return t.Format("01.2006") },
		"addDays":  func(t time.Time, n int) time.Time { return t.AddDate(0, 0, n) },
		"today":    func(t time.Time) bool { n := time.Now(); return t.YearDay() == n.YearDay() && t.Year() == n.Year() },
		"weekend":  func(t time.Time) bool { d := t.Weekday(); return d == time.Saturday || d == time.Sunday },
		"elapsed":  elapsed,
		"unix":     func(t time.Time) int64 { return t.Unix() },
		"weeknum":  func(t time.Time) int { _, w := t.ISOWeek(); return w },
		"rounding": roundingLabel,
		"rate":     func(p *store.Project, c *store.Client) float64 { return p.EffectiveRate(c.Rate) },
		"projectsOf": func(all []*store.Project, clientID int64) []*store.Project {
			var out []*store.Project
			for _, p := range all {
				if p.ClientID == clientID {
					out = append(out, p)
				}
			}
			return out
		},
		"sub": func(a, b int) int { return a - b },
		"pct": func(part, whole int) int {
			if whole <= 0 {
				return 0
			}
			return int(math.Round(float64(part) / float64(whole) * 100))
		},
		"currencies": func() []string { return []string{"EUR", "USD", "RUB", "GBP", "CHF"} },
		"roundings": func() []store.Rounding {
			return []store.Rounding{store.RoundExact, store.RoundFive, store.RoundTen, store.RoundQuarter, store.RoundHalf}
		},
		"seq": func(n int) []int {
			out := make([]int, n)
			for i := range out {
				out[i] = i
			}
			return out
		},
	}
}

// hoursMinutes печатает минуты так, как их читают в табеле: 7:30, а не 7.5.
func hoursMinutes(minutes int) string {
	if minutes <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d:%02d", minutes/60, minutes%60)
}

func money(amount float64, currency string) string {
	symbol := map[string]string{"EUR": "€", "USD": "$", "RUB": "₽", "GBP": "£", "CHF": "Fr"}[currency]
	if symbol == "" {
		symbol = currency
	}
	return fmt.Sprintf("%s%s", symbol, humanAmount(amount))
}

func humanAmount(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.2f", v)
}

// elapsed — время идущего таймера в виде 1:23:45.
func elapsed(e *store.Entry) string {
	d := time.Since(e.StartedAt)
	if d < 0 {
		d = 0
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	sec := int(d.Seconds()) % 60
	return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
}

func roundingLabel(lang string, r store.Rounding) string {
	if r <= 0 {
		return i18n.T(lang, "clients.exact")
	}
	return fmt.Sprintf("%d %s", int(r), map[string]string{"ru": "мин", "en": "min", "de": "Min"}[lang])
}
