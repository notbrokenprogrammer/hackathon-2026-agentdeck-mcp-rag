//go:build !qatools

package app

import (
	"feeders/internal/auth"
	"net/http"
)

func qaHandler(h http.Handler, a *auth.Handler) http.Handler { return h }
