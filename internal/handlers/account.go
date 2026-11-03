package handlers

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"html/template"
	"log"
	"net/http"

	"bible-tracker/internal/db"

	"github.com/gorilla/sessions"
)

// csrfSessionKey holds the session-bound token that POST /hapus-akun must echo.
// The session cookie is SameSite=Lax, but the token also covers same-site
// origins (other tuntasalkitab.com subdomains) and browsers that ignore Lax.
const csrfSessionKey = "csrfToken"

type AccountHandler struct {
	db        *sql.DB
	queries   *db.Queries
	store     *sessions.CookieStore
	templates *template.Template
}

func NewAccountHandler(sqlDB *sql.DB, queries *db.Queries, store *sessions.CookieStore, templates *template.Template) *AccountHandler {
	return &AccountHandler{
		db:        sqlDB,
		queries:   queries,
		store:     store,
		templates: templates,
	}
}

func (h *AccountHandler) DeletePage(w http.ResponseWriter, r *http.Request) {
	user, _ := h.queries.GetUserByID(r.Context(), getUserIDFromContext(r))
	data := infoPage{User: user, Page: "hapus-akun"}

	if user.GoogleID.Valid {
		session, _ := h.store.Get(r, "bible-tracker")
		token, _ := session.Values[csrfSessionKey].(string)
		if token == "" {
			buf := make([]byte, 32)
			if _, err := rand.Read(buf); err != nil {
				http.Error(w, "Failed to create session", http.StatusInternalServerError)
				return
			}
			token = hex.EncodeToString(buf)
			session.Values[csrfSessionKey] = token
			if err := session.Save(r, w); err != nil {
				http.Error(w, "Failed to create session", http.StatusInternalServerError)
				return
			}
		}
		data.CSRFToken = token
	}

	setNoStore(w)
	h.templates.ExecuteTemplate(w, "layout.html", data)
}

func (h *AccountHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user, err := h.queries.GetUserByID(r.Context(), getUserIDFromContext(r))
	if err != nil || !user.GoogleID.Valid {
		http.Error(w, "Silakan masuk dengan Google terlebih dahulu.", http.StatusForbidden)
		return
	}

	session, _ := h.store.Get(r, "bible-tracker")
	want, _ := session.Values[csrfSessionKey].(string)
	got := r.PostFormValue("csrf_token")
	if want == "" || subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
		http.Error(w, "Permintaan tidak valid. Muat ulang halaman lalu coba lagi.", http.StatusForbidden)
		return
	}

	if err := h.deleteUser(r, user.ID); err != nil {
		log.Printf("delete account %d: %v", user.ID, err)
		http.Error(w, "Gagal menghapus akun. Silakan coba lagi.", http.StatusInternalServerError)
		return
	}

	session.Values = map[interface{}]interface{}{}
	session.Options.MaxAge = -1
	session.Save(r, w)

	setNoStore(w)
	h.templates.ExecuteTemplate(w, "layout.html", infoPage{Page: "hapus-akun", Deleted: true})
}

func (h *AccountHandler) deleteUser(r *http.Request, userID int64) error {
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	qtx := h.queries.WithTx(tx)
	if err := qtx.DeleteUserProgress(r.Context(), userID); err != nil {
		return err
	}
	if err := qtx.DeleteUser(r.Context(), userID); err != nil {
		return err
	}
	return tx.Commit()
}
