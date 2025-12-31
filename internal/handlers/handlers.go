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
	ProgressPercent int
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	userID := getUserIDFromContext(r)

	year := reading.GetCurrentYear()
	month := reading.GetCurrentMonth()
	completedDays := h.getCompletedDaysMap(r.Context(), userID)
	monthInfo := reading.GetMonthInfo(year, month, completedDays)
	completedCount, _ := h.queries.CountCompletedDays(r.Context(), userID)
	user, _ := h.queries.GetUserByID(r.Context(), userID)

	data := PageData{
		User:            user,
		MonthInfo:       monthInfo,
		CompletedCount:  completedCount,
		ProgressPercent: int(completedCount * 100 / 365),
	}

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

	dayInfo := reading.DayInfo{
		Day:       date.Day(),
		DayOfYear: dayOfYear,
		Passage:   reading.GetPassageByDayOfYear(dayOfYear),
		Completed: newCompleted,
	}

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

func getUserIDFromContext(r *http.Request) int64 {
	if id, ok := r.Context().Value("userID").(int64); ok {
		return id
	}
	return 0
}
