package accounts

import (
	"context"
	"errors"
	"manga-manager/internal/database"
	"strings"
)

var ErrForbidden = errors.New("administrator required")
var ErrLastAdmin = errors.New("cannot remove last administrator")
var ErrSelfDelete = errors.New("cannot delete own account")
var ErrRole = errors.New("invalid role")

// Actor identifies the site user requesting an account-management command.
type Actor struct{ ID int64 }
type ManagementStore interface {
	WithAccountManagement(context.Context, func(database.AccountManagementWrite) error) error
}
type Management struct {
	store ManagementStore
	hash  func(string) (string, error)
}

func NewManagement(store ManagementStore, hash func(string) (string, error)) *Management {
	return &Management{store, hash}
}
func authorize(ctx context.Context, w database.AccountManagementWrite, actor Actor) error {
	user, err := w.GetUser(ctx, actor.ID)
	if errors.Is(err, database.ErrUserNotFound) || err == nil && !user.IsAdmin() {
		return ErrForbidden
	}
	return err
}

type CreateInput struct{ Username, Password, DisplayName, Role string }
type UpdateInput struct{ DisplayName, Role string }

func (m *Management) List(ctx context.Context, actor Actor) ([]database.User, error) {
	var out []database.User
	err := m.store.WithAccountManagement(ctx, func(w database.AccountManagementWrite) error {
		if err := authorize(ctx, w, actor); err != nil {
			return err
		}
		var err error
		out, err = w.ListUsers(ctx)
		return err
	})
	return out, err
}
func (m *Management) Create(ctx context.Context, actor Actor, in CreateInput) (database.User, error) {
	var out database.User
	err := m.store.WithAccountManagement(ctx, func(w database.AccountManagementWrite) error {
		if err := authorize(ctx, w, actor); err != nil {
			return err
		}
		if strings.TrimSpace(in.Username) == "" {
			return ErrUsername
		}
		if len(in.Password) < MinimumPasswordBytes {
			return ErrPassword
		}
		role := in.Role
		if role == "" {
			role = database.RoleRegular
		}
		if role != database.RoleRegular && role != database.RoleAdmin {
			return ErrRole
		}
		hash, err := m.hash(in.Password)
		if err != nil {
			return err
		}
		out, err = w.CreateUser(ctx, database.CreateUserParams{Username: in.Username, PasswordHash: hash, Role: role, DisplayName: strings.TrimSpace(in.DisplayName), MustChangePassword: true})
		return err
	})
	if err != nil {
		return database.User{}, err
	}
	return out, nil
}
func (m *Management) Update(ctx context.Context, actor Actor, id int64, in UpdateInput) (database.User, error) {
	var out database.User
	err := m.store.WithAccountManagement(ctx, func(w database.AccountManagementWrite) error {
		if err := authorize(ctx, w, actor); err != nil {
			return err
		}
		target, err := w.GetUser(ctx, id)
		if err != nil {
			return err
		}
		role := in.Role
		if role == "" {
			role = target.Role
		}
		if role != database.RoleAdmin && role != database.RoleRegular {
			return ErrRole
		}
		if target.IsAdmin() && role != database.RoleAdmin {
			n, err := w.CountAdmins(ctx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return ErrLastAdmin
			}
		}
		if err := w.UpdateProfile(ctx, id, strings.TrimSpace(in.DisplayName), role); err != nil {
			return err
		}
		out, err = w.GetUser(ctx, id)
		return err
	})
	if err != nil {
		return database.User{}, err
	}
	return out, nil
}
func (m *Management) Delete(ctx context.Context, actor Actor, id int64) error {
	return m.store.WithAccountManagement(ctx, func(w database.AccountManagementWrite) error {
		if err := authorize(ctx, w, actor); err != nil {
			return err
		}
		if actor.ID == id {
			return ErrSelfDelete
		}
		target, err := w.GetUser(ctx, id)
		if err != nil {
			return err
		}
		if target.IsAdmin() {
			n, err := w.CountAdmins(ctx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return ErrLastAdmin
			}
		}
		return w.DeleteUser(ctx, id)
	})
}
