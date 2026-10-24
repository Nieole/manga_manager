package api

import (
	"context"
	"encoding/json"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"net/http"
	"testing"
)

func TestRegularUserCanReadButCannotMutateManualCollections(t *testing.T) {
	controller, srv := authTestServer(t)
	hash, err := hashPassword("password1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controller.store.CreateUser(context.Background(), database.CreateUserParams{Username: "reader", PasswordHash: hash, Role: database.RoleRegular}); err != nil {
		t.Fatal(err)
	}
	id, err := organize.NewCollections(controller.store).Create(context.Background(), "Picks", "")
	if err != nil {
		t.Fatal(err)
	}
	client := newAuthClient(t)
	response, data := authDo(t, client, http.MethodPost, srv.URL+"/api/auth/login", "", map[string]string{"username": "reader", "password": "password1"})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login %d: %s", response.StatusCode, data)
	}
	var session authSessionResponse
	if err := json.Unmarshal(data, &session); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/collections/", "/api/collection-views", "/api/collections/" + itoa(id) + "/series"} {
		response, data := authDo(t, client, http.MethodGet, srv.URL+path, "", nil)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("read %s: %d %s", path, response.StatusCode, data)
		}
	}
	for _, request := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/collections/", map[string]string{"name": "Other"}},
		{http.MethodPut, "/api/collections/" + itoa(id), map[string]string{"name": "Edited"}},
		{http.MethodDelete, "/api/collections/" + itoa(id), nil},
		{http.MethodPost, "/api/collections/" + itoa(id) + "/series", map[string]any{"series_ids": []int64{1}}},
		{http.MethodDelete, "/api/collections/" + itoa(id) + "/series/1", nil},
	} {
		response, data := authDo(t, client, request.method, srv.URL+request.path, session.CSRFToken, request.body)
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("mutation %s %s: %d %s", request.method, request.path, response.StatusCode, data)
		}
	}
}
