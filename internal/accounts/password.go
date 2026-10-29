package accounts

import (
	"context"
	"errors"
	"manga-manager/internal/database"
)

var ErrCurrentPassword = errors.New("current password incorrect")

type PasswordStore interface {
	WithAccountPassword(context.Context, func(database.AccountPasswordWrite) error) error
}

// Passwords owns credential writes, old-session revocation and cached eligibility handoff.
// Replacement browser-session creation remains a transport operation after this commit.
type Passwords struct {
	store      PasswordStore
	hash       func(string) (string, error)
	verify     func(string, string) bool
	invalidate func(int64)
}

func NewPasswords(store PasswordStore, hash func(string) (string, error), verify func(string, string) bool, invalidate func(int64)) *Passwords {
	return &Passwords{store, hash, verify, invalidate}
}

type ChangePasswordInput struct{ CurrentPassword, NewPassword string }
type PasswordResult struct {
	User      database.User
	Committed bool
}

func (p *Passwords) Change(ctx context.Context, actor Actor, in ChangePasswordInput) (PasswordResult, error) {
	return p.write(ctx, actor, actor.ID, in.NewPassword, false, func(user database.User) error {
		if !p.verify(user.PasswordHash, in.CurrentPassword) {
			return ErrCurrentPassword
		}
		return nil
	})
}
func (p *Passwords) Reset(ctx context.Context, actor Actor, id int64, password string) (PasswordResult, error) {
	return p.write(ctx, actor, id, password, true, nil)
}
func (p *Passwords) write(ctx context.Context, actor Actor, id int64, password string, reset bool, check func(database.User) error) (PasswordResult, error) {
	var out PasswordResult
	err := p.store.WithAccountPassword(ctx, func(w database.AccountPasswordWrite) error {
		acting, err := w.GetUser(ctx, actor.ID)
		if errors.Is(err, database.ErrUserNotFound) {
			return ErrForbidden
		}
		if err != nil {
			return err
		}
		if reset && !acting.IsAdmin() {
			return ErrForbidden
		}
		target, err := w.GetUser(ctx, id)
		if err != nil {
			return err
		}
		if check != nil {
			if err := check(target); err != nil {
				return err
			}
		}
		if len(password) < MinimumPasswordBytes {
			return ErrPassword
		}
		hash, err := p.hash(password)
		if err != nil {
			return err
		}
		if err := w.ReplacePassword(ctx, id, hash, reset); err != nil {
			return err
		}
		if err := w.RevokeSessions(ctx, id); err != nil {
			return err
		}
		out.User, err = w.GetUser(ctx, id)
		return err
	})
	if err != nil {
		return PasswordResult{}, err
	}
	p.invalidate(id)
	out.Committed = true
	return out, nil
}
