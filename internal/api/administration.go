package api

import (
	"context"
	"manga-manager/internal/accounts"
	"manga-manager/internal/application/configuration"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/koreader"
	"sync"
	"time"
)

// AdministrationStore supplies session and diagnostic queries plus the existing
// atomic account capabilities; it exposes no catalogue writes or arbitrary SQL.
type AdministrationStore interface {
	accounts.InitializationStore
	accounts.ManagementStore
	accounts.PasswordStore
	accounts.CredentialStore
	CountUsers(context.Context) (int64, error)
	CreateKOReaderSyncEvent(context.Context, database.CreateKOReaderSyncEventParams) error
	CreateSession(ctx context.Context, sess database.Session) error
	DeleteSession(ctx context.Context, id string) error
	GetHealthReport(ctx context.Context, filters database.HealthIssueFilters) (database.HealthReport, error)
	GetKOReaderStats(ctx context.Context) (database.KOReaderStats, error)
	GetLatestKOReaderFailure(ctx context.Context) (database.KOReaderSyncEvent, error)
	GetSessionWithUser(ctx context.Context, id string, now time.Time) (database.Session, database.User, error)
	GetUserByID(ctx context.Context, id int64) (database.User, error)
	ListKOReaderDeviceConflicts(ctx context.Context, limit int) ([]database.KOReaderDeviceConflict, error)
	ListKOReaderDeviceDiagnostics(ctx context.Context) ([]database.KOReaderDeviceDiagnostic, error)
	ListKOReaderDeviceMatchMethods(ctx context.Context) ([]database.KOReaderDeviceMatchMethod, error)
	ListLibraries(ctx context.Context) ([]database.Library, error)
	ListUnmatchedKOReaderProgress(ctx context.Context, limit int) ([]database.KOReaderProgress, error)
	SetKOReaderAccountUser(ctx context.Context, accountID, userID int64) error
	TouchSession(ctx context.Context, id string, lastSeen, expiresAt time.Time) error
}

// administrationHTTP owns the administration transport family, not an application graph.
type administrationHTTP struct {
	initialization *accounts.Initialization
	management     *accounts.Management
	passwords      *accounts.Passwords

	store                  AdministrationStore
	config                 *config.Manager
	configuration          *configuration.Service
	auth                   *authState
	untrustedProtoWarnOnce *sync.Once
	koreader               *koreader.Service
}

func (c *administrationHTTP) currentConfig() config.Config {
	if c.config == nil {
		return config.Config{}
	}
	return c.config.Snapshot()
}

func newAdministration(d AdministrationDependencies, auth *authState, warning *sync.Once) *administrationHTTP {
	return &administrationHTTP{
		store: d.Store, config: d.Manager, configuration: d.Configuration, koreader: d.KOReader,
		auth: auth, untrustedProtoWarnOnce: warning,
		initialization: accounts.NewInitialization(d.Store, hashPassword),
		management:     accounts.NewManagement(d.Store, hashPassword),
		passwords:      accounts.NewPasswords(d.Store, hashPassword, verifyPassword, auth.invalidateBasicAuthForUser),
	}
}
