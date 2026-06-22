package api

import (
	"log/slog"
	"net/http"
)

// auditLog emits a structured log entry for every admin action. This provides
// an audit trail of who did what, when, and from where.
func auditLog(r *http.Request, action, target, subject string) {
	slog.Info("audit",
		"action", action,
		"target", target,
		"subject", subject,
		"method", r.Method,
		"path", r.URL.Path,
		"remote_addr", r.RemoteAddr,
		"user_agent", r.UserAgent(),
	)
}
