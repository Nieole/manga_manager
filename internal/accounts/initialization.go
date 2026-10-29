// Package accounts owns site-user initialization and account use-case boundaries.
package accounts

import (
	"context"
	"errors"
	"manga-manager/internal/database"
	"strings"
)

// MinimumPasswordBytes is shared by site-account password write intents.
const MinimumPasswordBytes = 8

var ErrInitialized = errors.New("site already initialized")
var ErrUsername = errors.New("username is required")
var ErrPassword = errors.New("password must contain at least 8 bytes")

type InitializationStore interface {
	WithAccountInitialization(context.Context, func(database.AccountInitializationWrite) error) error
	MigrateGlobalProgressToUser(context.Context, int64) error
	AssignOrphanKOReaderAccountsToUser(context.Context, int64) error
	MigrateGlobalActivityToUser(context.Context, int64) error
}
type SetupInput struct{ Username, Password, DisplayName string }
type Warning struct {
	Step string
	Err  error
}
type SetupResult struct {
	User      database.User
	Committed bool
	Warnings  []Warning
}
type Initialization struct {
	store InitializationStore
	hash  func(string) (string, error)
}

func NewInitialization(store InitializationStore, hash func(string) (string, error)) *Initialization {
	return &Initialization{store, hash}
}
func (s *Initialization) Setup(ctx context.Context, in SetupInput) (SetupResult, error) {
	var out SetupResult
	if strings.TrimSpace(in.Username) == "" {
		return out, ErrUsername
	}
	if len(in.Password) < MinimumPasswordBytes {
		return out, ErrPassword
	}
	hash, err := s.hash(in.Password)
	if err != nil {
		return out, err
	}
	err = s.store.WithAccountInitialization(ctx, func(w database.AccountInitializationWrite) error {
		n, err := w.CountUsers(ctx)
		if err != nil {
			return err
		}
		if n != 0 {
			return ErrInitialized
		}
		out.User, err = w.CreateAdministrator(ctx, in.Username, hash, strings.TrimSpace(in.DisplayName))
		return err
	})
	if err != nil {
		return SetupResult{}, err
	}
	out.Committed = true
	for _, step := range []struct {
		name string
		run  func(context.Context, int64) error
	}{{"reading_progress", s.store.MigrateGlobalProgressToUser}, {"device_accounts", s.store.AssignOrphanKOReaderAccountsToUser}, {"reading_activity", s.store.MigrateGlobalActivityToUser}} {
		if err := step.run(ctx, out.User.ID); err != nil {
			out.Warnings = append(out.Warnings, Warning{step.name, err})
		}
	}
	return out, nil
}

// CredentialStore reads only site users, independently of protocol device accounts.
type CredentialStore interface {
	GetUserByUsername(context.Context, string) (database.User, error)
}

type Credentials struct{ Username, Password string }

func Authenticate(ctx context.Context, store CredentialStore, in Credentials, dummy string, verify func(string, string) bool) (database.User, bool) {
	user, err := store.GetUserByUsername(ctx, in.Username)
	if err != nil {
		user = database.User{}
	}
	hash := user.PasswordHash
	if hash == "" {
		hash = dummy
	}
	matched := verify(hash, in.Password)
	if err != nil || !matched {
		return database.User{}, false
	}
	return user, true
}
