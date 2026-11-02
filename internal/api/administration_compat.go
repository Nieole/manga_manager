package api

import (
	"context"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"net/http"
	"time"
)

// administration binds the legacy caller to the same stateful transport leaves.
func (c *Controller) administration() *administrationHTTP {
	return newAdministration(AdministrationDependencies{Store: c.store, Manager: c.config, Configuration: c.configuration, KOReader: c.koreader}, c.auth, &c.untrustedProtoWarnOnce)
}
func (c *Controller) authenticateUser(ctx context.Context, username, password string) (database.User, bool) {
	return c.administration().authenticateUser(ctx, username, password)
}
func (c *Controller) sessionCookieSecure(r *http.Request) bool {
	return c.administration().sessionCookieSecure(r)
}
func (c *Controller) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	c.administration().setSessionCookie(w, r, token, expires)
}
func (c *Controller) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	c.administration().clearSessionCookie(w, r)
}
func (c *Controller) resolveBasicAuthUser(ctx context.Context, username, password string) (int64, bool) {
	return c.administration().resolveBasicAuthUser(ctx, username, password)
}
func (c *Controller) requireBasicAuth(next http.Handler) http.Handler {
	return c.administration().requireBasicAuth(next)
}
func (c *Controller) authGate(next http.Handler) http.Handler {
	return c.administration().authGate(next)
}
func (c *Controller) setupAdmin(w http.ResponseWriter, r *http.Request) {
	c.administration().setupAdmin(w, r)
}
func (c *Controller) login(w http.ResponseWriter, r *http.Request) { c.administration().login(w, r) }
func (c *Controller) changePassword(w http.ResponseWriter, r *http.Request) {
	c.administration().changePassword(w, r)
}
func (c *Controller) updateUser(w http.ResponseWriter, r *http.Request) {
	c.administration().updateUser(w, r)
}
func (c *Controller) clientIP(r *http.Request) string { return c.administration().clientIP(r) }
func (c *Controller) browseDirs(w http.ResponseWriter, r *http.Request) {
	c.administration().browseDirs(w, r)
}
func (c *Controller) getSystemConfig(w http.ResponseWriter, r *http.Request) {
	c.administration().getSystemConfig(w, r)
}
func (c *Controller) updateSystemConfig(w http.ResponseWriter, r *http.Request) {
	c.administration().updateSystemConfig(w, r)
}
func (c *Controller) testLLMConfig(w http.ResponseWriter, r *http.Request) {
	c.administration().testLLMConfig(w, r)
}
func (c *Controller) getHealthReport(w http.ResponseWriter, r *http.Request) {
	c.administration().getHealthReport(w, r)
}
func (c *Controller) getClientConnections(w http.ResponseWriter, r *http.Request) {
	c.administration().getClientConnections(w, r)
}
func (c *Controller) getSystemLogs(w http.ResponseWriter, r *http.Request) {
	c.administration().getSystemLogs(w, r)
}
func (c *Controller) getKOReaderSettings(w http.ResponseWriter, r *http.Request) {
	c.administration().getKOReaderSettings(w, r)
}
func (c *Controller) listKOReaderUnmatched(w http.ResponseWriter, r *http.Request) {
	c.administration().listKOReaderUnmatched(w, r)
}
func (c *Controller) getKOReaderDeviceDiagnostics(w http.ResponseWriter, r *http.Request) {
	c.administration().getKOReaderDeviceDiagnostics(w, r)
}
func (c *Controller) listKOReaderAccounts(w http.ResponseWriter, r *http.Request) {
	c.administration().listKOReaderAccounts(w, r)
}
func (c *Controller) createKOReaderAccount(w http.ResponseWriter, r *http.Request) {
	c.administration().createKOReaderAccount(w, r)
}
func (c *Controller) rotateKOReaderAccountKey(w http.ResponseWriter, r *http.Request) {
	c.administration().rotateKOReaderAccountKey(w, r)
}
func (c *Controller) toggleKOReaderAccount(w http.ResponseWriter, r *http.Request) {
	c.administration().toggleKOReaderAccount(w, r)
}
func (c *Controller) updateKOReaderSettings(w http.ResponseWriter, r *http.Request) {
	c.administration().updateKOReaderSettings(w, r)
}
func (c *Controller) persistConfig(cfg *config.Config) error {
	return c.administration().persistConfig(cfg)
}
func (c *Controller) persistConfigExpected(expected, cfg *config.Config) error {
	return c.administration().persistConfigExpected(expected, cfg)
}
