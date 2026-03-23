package handler

import (
	"github.com/gofiber/fiber/v2"
)

type Preferences struct {
	Theme    string         `json:"theme"`
	Mode     string         `json:"mode"`
	Locale   string         `json:"locale"`
	Settings map[string]any `json:"settings"`
}

func (h *Handler) GetPreferences(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := c.UserContext()

	var prefs Preferences
	err := h.db.QueryRow(ctx,
		`SELECT theme, mode, locale, settings FROM user_preferences WHERE user_id = $1`,
		claims.UserID,
	).Scan(&prefs.Theme, &prefs.Mode, &prefs.Locale, &prefs.Settings)

	if err != nil {
		// Return defaults if no preferences exist
		prefs = Preferences{
			Theme:    "default-dark",
			Mode:     "dark",
			Locale:   "en",
			Settings: map[string]any{},
		}
	}

	return writeJSON(c, fiber.StatusOK, prefs)
}

func (h *Handler) UpdatePreferences(c *fiber.Ctx) error {
	claims := getClaims(c)
	ctx := c.UserContext()

	var prefs Preferences
	if err := readJSON(c, &prefs); err != nil {
		return writeError(c, fiber.StatusBadRequest, "invalid request body")
	}

	if prefs.Theme == "" {
		prefs.Theme = "default-dark"
	}
	if prefs.Mode == "" {
		prefs.Mode = "dark"
	}
	if prefs.Locale == "" {
		prefs.Locale = "en"
	}
	if prefs.Settings == nil {
		prefs.Settings = map[string]any{}
	}

	_, err := h.db.Exec(ctx,
		`INSERT INTO user_preferences (user_id, theme, mode, locale, settings)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id) DO UPDATE SET
		   theme = EXCLUDED.theme,
		   mode = EXCLUDED.mode,
		   locale = EXCLUDED.locale,
		   settings = EXCLUDED.settings`,
		claims.UserID, prefs.Theme, prefs.Mode, prefs.Locale, prefs.Settings,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "failed to save preferences")
	}

	return writeJSON(c, fiber.StatusOK, prefs)
}
