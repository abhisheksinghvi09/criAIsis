package entity

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"criaisis/internal/domain/value"

	"github.com/google/uuid"
)

// User models an individual operator within a workspace authenticated via Slack identity.
type User struct {
	id          uuid.UUID
	workspaceID value.WorkspaceID
	slackUserID value.SlackUserID
	email       string
	name        string
	role        string
	createdAt   time.Time
	updatedAt   time.Time
}

func NewUser(
	workspaceID value.WorkspaceID,
	slackUserID value.SlackUserID,
	email string,
	name string,
	role string,
) (*User, error) {
	if workspaceID.IsZero() {
		return nil, errors.New("workspace id cannot be zero")
	}
	trimmedEmail := strings.TrimSpace(email)
	if trimmedEmail == "" {
		return nil, errors.New("email cannot be empty")
	}
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return nil, errors.New("name cannot be empty")
	}

	canonicalRole := strings.ToLower(strings.TrimSpace(role))
	if canonicalRole != "admin" && canonicalRole != "member" {
		return nil, fmt.Errorf("invalid role '%s': must be 'admin' or 'member'", role)
	}

	now := time.Now().UTC()
	return &User{
		id:          uuid.New(),
		workspaceID: workspaceID,
		slackUserID: slackUserID,
		email:       trimmedEmail,
		name:        trimmedName,
		role:        canonicalRole,
		createdAt:   now,
		updatedAt:   now,
	}, nil
}

func ReconstituteUser(
	id uuid.UUID,
	workspaceID value.WorkspaceID,
	slackUserID value.SlackUserID,
	email string,
	name string,
	role string,
	createdAt time.Time,
	updatedAt time.Time,
) *User {
	return &User{
		id:          id,
		workspaceID: workspaceID,
		slackUserID: slackUserID,
		email:       email,
		name:        name,
		role:        role,
		createdAt:   createdAt,
		updatedAt:   updatedAt,
	}
}

func (u *User) ID() uuid.UUID                  { return u.id }
func (u *User) WorkspaceID() value.WorkspaceID { return u.workspaceID }
func (u *User) SlackUserID() value.SlackUserID { return u.slackUserID }
func (u *User) Email() string                  { return u.email }
func (u *User) Name() string                   { return u.name }
func (u *User) Role() string                   { return u.role }
func (u *User) CreatedAt() time.Time           { return u.createdAt }
func (u *User) UpdatedAt() time.Time           { return u.updatedAt }
func (u *User) IsAdmin() bool                  { return u.role == "admin" }
