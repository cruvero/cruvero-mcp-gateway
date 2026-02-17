package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/cruvero/mcp-gateway/internal/types"
)

func TestPostgresServerStoreCreate(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresServerStore(db)

	record := sampleServerRecord()
	capabilitiesJSON, err := json.Marshal(record.Capabilities)
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}

	mock.ExpectExec("INSERT INTO mcp_servers").
		WithArgs(
			record.Name,
			record.SPIFFEID,
			record.Version,
			record.Host,
			record.Port,
			capabilitiesJSON,
			record.Status,
			record.PolicyProfile,
			record.LastHeartbeat,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := s.Create(context.Background(), record); err != nil {
		t.Fatalf("create server record: %v", err)
	}
}

func TestPostgresServerStoreCreateDuplicateError(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresServerStore(db)

	record := sampleServerRecord()

	mock.ExpectExec("INSERT INTO mcp_servers").
		WillReturnError(errors.New("duplicate key value violates unique constraint"))

	err := s.Create(context.Background(), record)
	if err == nil {
		t.Fatal("expected create error")
	}
	if !strings.Contains(err.Error(), "server store") {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestPostgresServerStoreGetAndNotFound(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresServerStore(db)
	now := fixedTime()

	expectedQuery := `SELECT ` + serverColumns + ` FROM mcp_servers WHERE id = $1`
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs("server-1").
		WillReturnRows(serverRows().AddRow(
			"server-1",
			"alpha",
			"spiffe://trust/ns/default/sa/alpha",
			"1.0.0",
			"alpha.svc.cluster.local",
			8080,
			[]byte(`{"tools":["tool.a"],"resources":["res://alpha"],"prompts":["prompt.a"]}`),
			"active",
			"default",
			now,
			now,
			now,
		))

	record, err := s.Get(context.Background(), "server-1")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if record.Name != "alpha" || record.Status != types.StatusActive {
		t.Fatalf("unexpected server record: %+v", record)
	}

	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs("missing").
		WillReturnError(sql.ErrNoRows)

	_, err = s.Get(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected not found error")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestPostgresServerStoreGetByNameAndSPIFFE(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresServerStore(db)
	now := fixedTime()

	queryByName := `SELECT ` + serverColumns + ` FROM mcp_servers WHERE name = $1`
	mock.ExpectQuery(regexp.QuoteMeta(queryByName)).
		WithArgs("alpha").
		WillReturnRows(serverRows().AddRow(
			"server-1",
			"alpha",
			"spiffe://trust/ns/default/sa/alpha",
			"1.0.0",
			"alpha.svc.cluster.local",
			8080,
			[]byte(`{"tools":[],"resources":[],"prompts":[]}`),
			"active",
			"default",
			now,
			now,
			now,
		))

	if _, err := s.GetByName(context.Background(), "alpha"); err != nil {
		t.Fatalf("get by name: %v", err)
	}

	queryBySPIFFE := `SELECT ` + serverColumns + ` FROM mcp_servers WHERE spiffe_id = $1`
	mock.ExpectQuery(regexp.QuoteMeta(queryBySPIFFE)).
		WithArgs("spiffe://trust/ns/default/sa/alpha").
		WillReturnRows(serverRows().AddRow(
			"server-1",
			"alpha",
			"spiffe://trust/ns/default/sa/alpha",
			"1.0.0",
			"alpha.svc.cluster.local",
			8080,
			[]byte(`{"tools":[],"resources":[],"prompts":[]}`),
			"active",
			"default",
			now,
			now,
			now,
		))

	if _, err := s.GetBySPIFFEID(context.Background(), "spiffe://trust/ns/default/sa/alpha"); err != nil {
		t.Fatalf("get by spiffe id: %v", err)
	}
}

func TestPostgresServerStoreList(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresServerStore(db)
	now := fixedTime()
	status := types.StatusActive

	expectedQuery := "SELECT " + serverColumns + " FROM mcp_servers WHERE status = $1 AND name ILIKE $2 ORDER BY created_at DESC LIMIT $3 OFFSET $4"
	mock.ExpectQuery(regexp.QuoteMeta(expectedQuery)).
		WithArgs(status.String(), "alpha%", 10, 2).
		WillReturnRows(serverRows().
			AddRow("server-1", "alpha", "spiffe://trust/ns/default/sa/alpha", "1.0.0", "alpha.svc", 8080, []byte(`{"tools":[],"resources":[],"prompts":[]}`), "active", "default", now, now, now).
			AddRow("server-2", "alpha-2", "spiffe://trust/ns/default/sa/alpha-2", "1.1.0", "alpha2.svc", 8081, []byte(`{"tools":[],"resources":[],"prompts":[]}`), "active", "default", now, now, now))

	records, err := s.List(context.Background(), types.ServerFilter{
		Status:      &status,
		NamePattern: "alpha%",
		Limit:       10,
		Offset:      2,
	})
	if err != nil {
		t.Fatalf("list servers: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
}

func TestPostgresServerStoreUpdateMethods(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresServerStore(db)

	record := sampleServerRecord()
	capabilitiesJSON, err := json.Marshal(record.Capabilities)
	if err != nil {
		t.Fatalf("marshal capabilities: %v", err)
	}

	mock.ExpectExec("UPDATE mcp_servers").
		WithArgs(
			record.Name,
			record.SPIFFEID,
			record.Version,
			record.Host,
			record.Port,
			capabilitiesJSON,
			record.Status,
			record.PolicyProfile,
			record.LastHeartbeat,
			record.ID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := s.Update(context.Background(), record); err != nil {
		t.Fatalf("update server: %v", err)
	}

	mock.ExpectExec(regexp.QuoteMeta("UPDATE mcp_servers SET status = $1, updated_at = now() WHERE id = $2")).
		WithArgs(types.StatusStale.String(), record.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := s.UpdateStatus(context.Background(), record.ID, types.StatusStale); err != nil {
		t.Fatalf("update status: %v", err)
	}

	mock.ExpectExec(regexp.QuoteMeta("UPDATE mcp_servers SET last_heartbeat = now(), updated_at = now() WHERE id = $1")).
		WithArgs(record.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := s.UpdateHeartbeat(context.Background(), record.ID); err != nil {
		t.Fatalf("update heartbeat: %v", err)
	}
}

func TestPostgresServerStoreDelete(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresServerStore(db)

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM mcp_servers WHERE id = $1")).
		WithArgs("server-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := s.Delete(context.Background(), "server-1"); err != nil {
		t.Fatalf("delete server: %v", err)
	}
}

func TestPostgresServerStoreListStaleAndExpired(t *testing.T) {
	db, mock := newMockDB(t)
	s := NewPostgresServerStore(db)
	now := fixedTime()
	threshold := 30 * time.Second

	mock.ExpectQuery("last_heartbeat < now\\(\\) - \\$1::interval").
		WithArgs(threshold.String()).
		WillReturnRows(serverRows().AddRow(
			"server-1",
			"alpha",
			"spiffe://trust/ns/default/sa/alpha",
			"1.0.0",
			"alpha.svc",
			8080,
			[]byte(`{"tools":[],"resources":[],"prompts":[]}`),
			"active",
			"default",
			now,
			now,
			now,
		))

	stale, err := s.ListStale(context.Background(), threshold)
	if err != nil {
		t.Fatalf("list stale: %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale record, got %d", len(stale))
	}

	mock.ExpectQuery("last_heartbeat < now\\(\\) - \\(\\$1::interval \\* 3\\)").
		WithArgs(threshold.String()).
		WillReturnRows(serverRows().AddRow(
			"server-2",
			"beta",
			"spiffe://trust/ns/default/sa/beta",
			"1.1.0",
			"beta.svc",
			8081,
			[]byte(`{"tools":[],"resources":[],"prompts":[]}`),
			"stale",
			"default",
			now,
			now,
			now,
		))

	expired, err := s.ListExpired(context.Background(), threshold)
	if err != nil {
		t.Fatalf("list expired: %v", err)
	}
	if len(expired) != 1 {
		t.Fatalf("expected 1 expired record, got %d", len(expired))
	}
}

func TestPostgresServerStoreThresholdValidation(t *testing.T) {
	db, _ := newMockDB(t)
	s := NewPostgresServerStore(db)

	if _, err := s.ListStale(context.Background(), 0); err == nil {
		t.Fatal("expected error for zero threshold in ListStale")
	}
	if _, err := s.ListExpired(context.Background(), -1*time.Second); err == nil {
		t.Fatal("expected error for negative threshold in ListExpired")
	}
}

func serverRows() *sqlmock.Rows {
	return sqlmock.NewRows(serverColumnNames)
}

func sampleServerRecord() *types.ServerRecord {
	now := fixedTime()
	status := types.StatusActive
	return &types.ServerRecord{
		ID:       "server-1",
		Name:     "alpha",
		SPIFFEID: "spiffe://trust/ns/default/sa/alpha",
		Version:  "1.0.0",
		Host:     "alpha.svc.cluster.local",
		Port:     8080,
		Capabilities: types.Capability{
			Tools:     []string{"tool.a"},
			Resources: []string{"res://alpha"},
			Prompts:   []string{"prompt.a"},
		},
		Status:        status,
		PolicyProfile: "default",
		LastHeartbeat: &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}
