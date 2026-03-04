package routes

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/mailer"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/jfxdev/wireops/internal/config"
)

func RegisterUserRoutes(r *router.Router[*core.RequestEvent], app core.App) {
	r.POST("/api/custom/users/invite", handleCreateInvite(app))
	r.GET("/api/custom/users/invite/validate", handleValidateInvite(app))
	r.POST("/api/custom/users/invite/accept", handleAcceptInvite(app))
}

func handleCreateInvite(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		if !e.HasSuperuserAuth() {
			return e.JSON(http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		}

		var body struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil || body.Email == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "email is required"})
		}

		existing, _ := app.FindAllRecords(core.CollectionNameSuperusers, dbx.HashExp{"email": body.Email})
		if len(existing) > 0 {
			return e.JSON(http.StatusConflict, map[string]string{"error": "a user with this email already exists"})
		}

		token, err := generateSecureToken()
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to generate token"})
		}

		col, err := app.FindCollectionByNameOrId("invites")
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "invites collection not found"})
		}

		invite := core.NewRecord(col)
		invite.Set("email", body.Email)
		invite.Set("token", token)
		invite.Set("expires_at", time.Now().UTC().Add(24*time.Hour))
		invite.Set("used", false)
		if e.Auth != nil {
			invite.Set("created_by", e.Auth.Id)
		}
		if err := app.Save(invite); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		actionURL := config.GetAppURL() + "/invite?token=" + token
		senderAddr := app.Settings().Meta.SenderAddress
		if senderAddr == "" {
			senderAddr = "noreply@wireops.local"
		}
		msg := &mailer.Message{
			From:    mail.Address{Name: "wireops", Address: senderAddr},
			To:      []mail.Address{{Address: body.Email}},
			Subject: "You've been invited to wireops",
			HTML:    buildInviteEmailHTML(actionURL),
			Text:    "You've been invited to wireops. Set up your account here: " + actionURL,
		}
		if err := app.NewMailClient().Send(msg); err != nil {
			_ = app.Delete(invite)
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to send invite email: " + err.Error()})
		}

		return e.JSON(http.StatusOK, map[string]string{"status": "invited"})
	}
}

func handleValidateInvite(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		token := e.Request.URL.Query().Get("token")
		if token == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "token is required"})
		}

		invites, err := app.FindAllRecords("invites", dbx.HashExp{"token": token})
		if err != nil || len(invites) == 0 {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "invalid or expired invite"})
		}

		invite := invites[0]
		if invite.GetBool("used") {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "invite has already been used"})
		}
		if time.Now().After(invite.GetDateTime("expires_at").Time()) {
			return e.JSON(http.StatusGone, map[string]string{"error": "invite has expired"})
		}

		return e.JSON(http.StatusOK, map[string]string{
			"email":  invite.GetString("email"),
			"status": "valid",
		})
	}
}

func handleAcceptInvite(app core.App) func(*core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		var body struct {
			Token           string `json:"token"`
			Password        string `json:"password"`
			PasswordConfirm string `json:"password_confirm"`
		}
		if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if body.Token == "" || body.Password == "" || body.PasswordConfirm == "" {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "token, password and password_confirm are required"})
		}
		if body.Password != body.PasswordConfirm {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "passwords do not match"})
		}

		invites, err := app.FindAllRecords("invites", dbx.HashExp{"token": body.Token})
		if err != nil || len(invites) == 0 {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "invalid or expired invite"})
		}

		invite := invites[0]
		if invite.GetBool("used") {
			return e.JSON(http.StatusNotFound, map[string]string{"error": "invite has already been used"})
		}
		if time.Now().After(invite.GetDateTime("expires_at").Time()) {
			return e.JSON(http.StatusGone, map[string]string{"error": "invite has expired"})
		}

		email := invite.GetString("email")
		existing, _ := app.FindAllRecords(core.CollectionNameSuperusers, dbx.HashExp{"email": email})
		if len(existing) > 0 {
			invite.Set("used", true)
			_ = app.Save(invite)
			return e.JSON(http.StatusConflict, map[string]string{"error": "a user with this email already exists"})
		}

		superusers, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": "internal error"})
		}

		record := core.NewRecord(superusers)
		record.Set("email", email)
		record.Set("password", body.Password)
		if err := app.Save(record); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}

		invite.Set("used", true)
		_ = app.Save(invite)

		return e.JSON(http.StatusCreated, map[string]string{"status": "created"})
	}
}

func generateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func buildInviteEmailHTML(actionURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="UTF-8"></head>
<body style="font-family:sans-serif;background:#0f1117;color:#e1e4e8;padding:40px 20px;margin:0">
  <div style="max-width:480px;margin:0 auto;background:#1a1d24;border:1px solid #2d333b;border-radius:12px;padding:32px">
    <div style="text-align:center;margin-bottom:24px">
      <span style="font-size:24px;font-weight:900;letter-spacing:4px;color:#ffd700">wireops</span>
    </div>
    <h2 style="margin:0 0 12px;font-size:18px">You've been invited</h2>
    <p style="color:#8b949e;font-size:14px;margin:0 0 24px">
      You've been invited to join wireops. Click the button below to set up your account.
      This link expires in 24 hours.
    </p>
    <div style="text-align:center;margin-bottom:24px">
      <a href="%s" style="display:inline-block;background:#ffd700;color:#000;font-weight:700;font-size:14px;padding:12px 32px;border-radius:8px;text-decoration:none">
        Set Up Account
      </a>
    </div>
    <p style="color:#484f58;font-size:12px;margin:0;text-align:center">
      If you weren't expecting this invitation, you can safely ignore this email.
    </p>
  </div>
</body></html>`, actionURL)
}
