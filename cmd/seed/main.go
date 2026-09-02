// Команда seed наполняет базу правдоподобной неделей: три клиента, шесть
// проектов, записи за две недели назад и один идущий таймер.
package main

import (
	"database/sql"
	"flag"
	"log"
	"math/rand"
	"time"

	"github.com/dripips/span/internal/store"
)

var seedsRU = []clientSeed{
	{
		name: "Nordvik Studio", rate: 65, currency: "EUR", rounding: store.RoundQuarter,
		notes: "Счёт до 5 числа, часы округляем до 15 минут.",
		projects: []projectSeed{
			{name: "Сайт коллекции", color: "#2563eb", tasks: []string{
				"Вёрстка карточки товара", "Правки по сетке каталога", "Разбор аналитики за месяц", "Созвон по релизу"}},
			{name: "Поддержка", color: "#0ea5e9", rate: 55, tasks: []string{
				"Обновление зависимостей", "Починил выгрузку остатков", "Разбор писем от менеджера"}},
		},
	},
	{
		name: "Halden Logistics", rate: 80, currency: "EUR", rounding: store.RoundHalf,
		notes: "Считают по получасам, платят раз в две недели.",
		projects: []projectSeed{
			{name: "Панель диспетчера", color: "#8b5cf6", tasks: []string{
				"Карта рейсов", "Фильтры по складам", "Тесты на импорт накладных", "Ревью с командой"}},
			{name: "Интеграция с 1С", color: "#a855f7", rate: 95, tasks: []string{
				"Синхронизация справочников", "Разбор ошибок обмена", "Документация для внедрения"}},
		},
	},
	{
		name: "Личное", rate: 0, currency: "EUR", rounding: store.RoundExact,
		notes: "Своё время: учится и пишется, в счёт не идёт.",
		projects: []projectSeed{
			{name: "Заметки и статьи", color: "#64748b", tasks: []string{
				"Черновик статьи про Go", "Разбор чужого кода", "Читал документацию"}},
			{name: "Обучение", color: "#94a3b8", tasks: []string{
				"Курс по системному дизайну", "Практика с профилировщиком"}},
		},
	},
}

var seedsEN = []clientSeed{
	{
		name: "Nordvik Studio", rate: 65, currency: "EUR", rounding: store.RoundQuarter,
		notes: "Invoice by the 5th, hours rounded up to 15 minutes.",
		projects: []projectSeed{
			{name: "Collection site", color: "#2563eb", tasks: []string{
				"Product card markup", "Catalogue grid fixes", "Monthly analytics review", "Release call"}},
			{name: "Support", color: "#0ea5e9", rate: 55, tasks: []string{
				"Dependency updates", "Fixed the stock export", "Went through the manager's email"}},
		},
	},
	{
		name: "Halden Logistics", rate: 80, currency: "EUR", rounding: store.RoundHalf,
		notes: "Counts in half hours, pays every second week.",
		projects: []projectSeed{
			{name: "Dispatcher panel", color: "#8b5cf6", tasks: []string{
				"Route map", "Warehouse filters", "Tests for waybill import", "Review with the team"}},
			{name: "ERP integration", color: "#a855f7", rate: 95, tasks: []string{
				"Reference data sync", "Exchange error triage", "Rollout documentation"}},
		},
	},
	{
		name: "Personal", rate: 0, currency: "EUR", rounding: store.RoundExact,
		notes: "Own time: reading and writing, never billed.",
		projects: []projectSeed{
			{name: "Notes and articles", color: "#64748b", tasks: []string{
				"Draft of the Go article", "Reading someone else's code", "Documentation"}},
			{name: "Learning", color: "#94a3b8", tasks: []string{
				"System design course", "Profiler practice"}},
		},
	},
}

type projectSeed struct {
	name  string
	color string
	rate  float64
	tasks []string
}

type clientSeed struct {
	name     string
	rate     float64
	currency string
	rounding store.Rounding
	notes    string
	projects []projectSeed
}

func main() {
	dbPath := flag.String("db", "span.db", "файл базы SQLite")
	lang := flag.String("lang", "ru", "язык демо-данных: ru или en")
	flag.Parse()

	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	seeds := seedsRU
	if *lang == "en" {
		seeds = seedsEN
	}

	rnd := rand.New(rand.NewSource(85574491))
	now := time.Now()
	var projects []*store.Project

	for _, cs := range seeds {
		c := &store.Client{Name: cs.name, Rate: cs.rate, Currency: cs.currency,
			Rounding: cs.rounding, Notes: cs.notes}
		if err := db.SaveClient(c); err != nil {
			log.Fatal(err)
		}
		for _, ps := range cs.projects {
			p := &store.Project{ClientID: c.ID, Name: ps.name, Color: ps.color}
			if ps.rate > 0 {
				p.Rate = sql.NullFloat64{Float64: ps.rate, Valid: true}
			}
			if err := db.SaveProject(p); err != nil {
				log.Fatal(err)
			}
			projects = append(projects, p)

			// Две недели назад: рабочие дни, один-два блока на проект в день.
			for back := 13; back >= 0; back-- {
				day := now.AddDate(0, 0, -back)
				if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
					if rnd.Intn(5) != 0 {
						continue
					}
				}
				if rnd.Intn(100) < 45 {
					continue
				}
				blocks := 1 + rnd.Intn(2)
				hour := 9 + rnd.Intn(3)
				for b := 0; b < blocks; b++ {
					minutes := 45 + rnd.Intn(150)
					start := time.Date(day.Year(), day.Month(), day.Day(), hour, rnd.Intn(4)*15, 0, 0, time.Local)
					if back == 0 && start.After(now) {
						break
					}
					end := start.Add(time.Duration(minutes) * time.Minute)
					if end.After(now) {
						end = now.Add(-20 * time.Minute)
						if !end.After(start) {
							break
						}
					}
					e := &store.Entry{
						ProjectID:   p.ID,
						Description: ps.tasks[rnd.Intn(len(ps.tasks))],
						StartedAt:   start.UTC(),
						EndedAt:     sql.NullTime{Time: end.UTC(), Valid: true},
						Billable:    cs.rate > 0,
					}
					if err := db.SaveEntry(e); err != nil {
						log.Fatal(err)
					}
					hour = end.Hour() + 1
					if hour > 18 {
						break
					}
				}
			}
		}
	}

	// Идущий таймер: сорок минут назад, чтобы строка вверху была живой.
	running := projects[0]
	if err := db.Start(running.ID, seeds[0].projects[0].tasks[1]); err != nil {
		log.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE entries SET started_at=? WHERE ended_at IS NULL`,
		time.Now().Add(-41*time.Minute).UTC()); err != nil {
		log.Fatal(err)
	}

	log.Printf("готово: %d клиентов, %d проектов", len(seeds), len(projects))
}
