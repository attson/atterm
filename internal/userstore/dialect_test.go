package userstore

import "testing"

func TestSQLiteRebindIsNoop(t *testing.T) {
	got := dialectSQLite.Rebind("SELECT a FROM t WHERE x = ? AND y = ?")
	want := "SELECT a FROM t WHERE x = ? AND y = ?"
	if got != want {
		t.Fatalf("sqlite rebind = %q, want %q", got, want)
	}
}

func TestPostgresRebindNumbersPlaceholders(t *testing.T) {
	got := dialectPostgres.Rebind("INSERT INTO t(a,b,c) VALUES(?, ?, ?)")
	want := "INSERT INTO t(a,b,c) VALUES($1, $2, $3)"
	if got != want {
		t.Fatalf("postgres rebind = %q, want %q", got, want)
	}
}

func TestDialectNames(t *testing.T) {
	if dialectSQLite.Name() != "sqlite" {
		t.Fatalf("sqlite name = %q", dialectSQLite.Name())
	}
	if dialectPostgres.Name() != "postgres" {
		t.Fatalf("postgres name = %q", dialectPostgres.Name())
	}
}
