package handlers

import (
	"context"
	"database/sql"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"bible-tracker/internal/db"
	"bible-tracker/internal/reading"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	queries   *db.Queries
	templates *template.Template
}

func New(queries *db.Queries, templates *template.Template) *Handler {
	return &Handler{
		queries:   queries,
		templates: templates,
	}
}

type PageData struct {
	User            db.User
	MonthInfo       reading.MonthInfo
	CompletedCount  int64
	RemainingDays   int64
	ProgressPercent int
	MissedCount     int
}

type MissedMonth struct {
	MonthName string
	Month     int
	Days      []reading.DayInfo
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromContext(r)

	now := time.Now()
	year := reading.GetCurrentYear()
	month := reading.GetCurrentMonth()
	completedDays := h.getCompletedDaysMap(r.Context(), userID)
	monthInfo := reading.GetMonthInfo(year, month, completedDays)
	completedCount, _ := h.queries.CountCompletedDays(r.Context(), userID)
	user, _ := h.queries.GetUserByID(r.Context(), userID)

	todayDOY := now.YearDay()
	missedCount := 0
	for doy := 1; doy <= todayDOY; doy++ {
		if !completedDays[doy] {
			missedCount++
		}
	}

	data := PageData{
		User:            user,
		MonthInfo:       monthInfo,
		CompletedCount:  completedCount,
		RemainingDays:   365 - completedCount,
		ProgressPercent: int(completedCount * 100 / 365),
		MissedCount:     missedCount,
	}

	setNoStore(w)
	h.templates.ExecuteTemplate(w, "layout.html", data)
}

func (h *Handler) GetMonth(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromContext(r)

	monthStr := r.URL.Query().Get("month")
	month, err := strconv.Atoi(monthStr)
	if err != nil || month < 1 || month > 12 {
		month = reading.GetCurrentMonth()
	}

	year := reading.GetCurrentYear()
	completedDays := h.getCompletedDaysMap(r.Context(), userID)
	monthInfo := reading.GetMonthInfo(year, month, completedDays)

	setNoStore(w)
	h.templates.ExecuteTemplate(w, "month_card", monthInfo)
}

func (h *Handler) ToggleDay(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromContext(r)

	dayStr := chi.URLParam(r, "day")
	dayOfYear, err := strconv.Atoi(dayStr)
	if err != nil || dayOfYear < 1 || dayOfYear > 365 {
		http.Error(w, "Invalid day", http.StatusBadRequest)
		return
	}

	progress, err := h.queries.GetProgressByDay(r.Context(), db.GetProgressByDayParams{
		UserID:    userID,
		DayOfYear: int64(dayOfYear),
	})

	var newCompleted bool
	if err == sql.ErrNoRows {
		newCompleted = true
	} else if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	} else {
		newCompleted = !progress.Completed.Bool
	}

	var completedAt sql.NullTime
	if newCompleted {
		completedAt = sql.NullTime{Time: time.Now(), Valid: true}
	}

	err = h.queries.UpsertProgress(r.Context(), db.UpsertProgressParams{
		UserID:      userID,
		DayOfYear:   int64(dayOfYear),
		Completed:   sql.NullBool{Bool: newCompleted, Valid: true},
		CompletedAt: completedAt,
	})
	if err != nil {
		http.Error(w, "Failed to update progress", http.StatusInternalServerError)
		return
	}

	year := reading.GetCurrentYear()
	date := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, dayOfYear-1)
	passage := reading.GetPassageByDayOfYear(dayOfYear)

	dayInfo := reading.DayInfo{
		Day:          date.Day(),
		DayOfYear:    dayOfYear,
		Passage:      passage,
		PassageLinks: reading.ParsePassages(passage),
		Completed:    newCompleted,
	}

	setNoStore(w)
	h.templates.ExecuteTemplate(w, "day_item", dayInfo)
}

func (h *Handler) getCompletedDaysMap(ctx context.Context, userID int64) map[int]bool {
	completed := make(map[int]bool)
	progress, err := h.queries.GetProgress(ctx, userID)
	if err != nil {
		return completed
	}

	for _, p := range progress {
		if p.Completed.Bool {
			completed[int(p.DayOfYear)] = true
		}
	}
	return completed
}

func (h *Handler) GetMissedDays(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromContext(r)

	now := time.Now()
	year := now.Year()
	todayDOY := now.YearDay()

	completedDays := h.getCompletedDaysMap(r.Context(), userID)

	var months []MissedMonth

	for doy := 1; doy <= todayDOY; doy++ {
		if completedDays[doy] {
			continue
		}
		date := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, doy-1)
		month := int(date.Month())

		if len(months) == 0 || months[len(months)-1].Month != month {
			months = append(months, MissedMonth{
				MonthName: time.Month(month).String(),
				Month:     month,
			})
		}

		passage := reading.GetPassageByDayOfYear(doy)
		months[len(months)-1].Days = append(months[len(months)-1].Days, reading.DayInfo{
			Day:          date.Day(),
			DayOfYear:    doy,
			Passage:      passage,
			PassageLinks: reading.ParsePassages(passage),
			Completed:    false,
		})
	}

	setNoStore(w)
	h.templates.ExecuteTemplate(w, "missed_days", months)
}

// setNoStore prevents browsers from serving stale personalized HTML from the
// HTTP cache, e.g. when navigating back from a passage link on browsers
// without tab support (Kindle).
func setNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

func getUserIDFromContext(r *http.Request) int64 {
	if id, ok := r.Context().Value("userID").(int64); ok {
		return id
	}
	return 0
}
