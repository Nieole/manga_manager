package api

import "github.com/go-chi/chi/v5"

// SetupAdministrationRoutes mounts the standalone administration family.
func (h *Host) SetupAdministrationRoutes(r chi.Router) {
	r.Route("/api", func(r chi.Router) { r.Use(h.administration.authGate); h.administration.mount(r) })
}
func (c *administrationHTTP) mount(r chi.Router) {
	r.Get("/system/performance", c.getSystemPerformance)
	r.Get("/auth/status", c.authStatus)
	r.Post("/auth/setup", c.setupAdmin)
	r.Post("/auth/login", c.login)
	r.Post("/auth/logout", c.logout)
	r.Get("/auth/me", c.authMe)
	r.Post("/auth/change-password", c.changePassword)
	r.Get("/users", c.listUsers)
	r.Post("/users", c.createUser)
	r.Patch("/users/{userId}", c.updateUser)
	r.Post("/users/{userId}/password", c.resetUserPassword)
	r.Delete("/users/{userId}", c.deleteUser)
	r.Get("/browse-dirs", c.browseDirs)
	r.Get("/health/report", c.getHealthReport)
	r.Get("/system/config", c.getSystemConfig)
	r.Get("/system/capabilities", c.getSystemCapabilities)
	r.Get("/system/client-connections", c.getClientConnections)
	r.Post("/system/config", c.updateSystemConfig)
	r.Get("/system/logs", c.getSystemLogs)
	r.Get("/system/koreader", c.getKOReaderSettings)
	r.Get("/system/koreader/accounts", c.listKOReaderAccounts)
	r.Get("/system/koreader/unmatched", c.listKOReaderUnmatched)
	r.Get("/system/koreader/devices", c.getKOReaderDeviceDiagnostics)
	r.Post("/system/koreader", c.updateKOReaderSettings)
	r.Post("/system/koreader/accounts", c.createKOReaderAccount)
	r.Post("/system/koreader/accounts/{accountId}/rotate-key", c.rotateKOReaderAccountKey)
	r.Post("/system/koreader/accounts/{accountId}/toggle", c.toggleKOReaderAccount)
	r.Delete("/system/koreader/accounts/{accountId}", c.deleteKOReaderAccount)
	r.Delete("/system/koreader/progress/{progressId}", c.resetKOReaderProgress)
	r.Post("/system/test-llm", c.testLLMConfig)
}
