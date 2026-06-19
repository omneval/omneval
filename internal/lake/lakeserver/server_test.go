package lakeserver_test

import (
	"testing"

	"github.com/omneval/omneval/internal/config"
	"github.com/omneval/omneval/internal/lake/lakeserver"
)

func TestConfigFromApp_CatalogDriver(t *testing.T) {
	cases := []struct {
		name            string
		catalogDriver   string
		databaseDriver  string
		wantCatalog     string
		wantCatalogDSN  string
	}{
		{
			name:           "no explicit catalogDriver + postgres database → duckdb",
			databaseDriver: "postgres",
			wantCatalog:    lakeserver.CatalogDriverLocal,
			wantCatalogDSN: "lake/catalog.ducklake",
		},
		{
			name:           "no explicit catalogDriver + sqlite database → duckdb",
			databaseDriver: "sqlite",
			wantCatalog:    lakeserver.CatalogDriverLocal,
			wantCatalogDSN: "lake/catalog.ducklake",
		},
		{
			name:           "no explicit catalogDriver + empty database → duckdb",
			databaseDriver: "",
			wantCatalog:    lakeserver.CatalogDriverLocal,
			wantCatalogDSN: "lake/catalog.ducklake",
		},
		{
			name:           "explicit catalogDriver postgres + postgres database → postgres",
			catalogDriver:  lakeserver.CatalogDriverPostgres,
			databaseDriver: "postgres",
			wantCatalog:    lakeserver.CatalogDriverPostgres,
			wantCatalogDSN: "host=localhost dbname=omneval",
		},
		{
			name:           "explicit catalogDriver postgres + sqlite database → postgres (override)",
			catalogDriver:  lakeserver.CatalogDriverPostgres,
			databaseDriver: "sqlite",
			wantCatalog:    lakeserver.CatalogDriverPostgres,
			wantCatalogDSN: "host=localhost dbname=omneval",
		},
		{
			name:          "explicit catalogDriver duckdb → duckdb",
			catalogDriver: lakeserver.CatalogDriverLocal,
			wantCatalog:   lakeserver.CatalogDriverLocal,
			wantCatalogDSN: "lake/catalog.ducklake",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := &config.Config{
				Database: config.DatabaseConfig{
					Driver: tc.databaseDriver,
					DSN:    "host=localhost dbname=omneval",
				},
				Quack: config.QuackConfig{
					Server: config.QuackServerConfig{
						CatalogDriver: tc.catalogDriver,
					},
				},
			}
			cfg := lakeserver.ConfigFromApp(app)
			if cfg.CatalogDriver != tc.wantCatalog {
				t.Errorf("CatalogDriver: got %q, want %q", cfg.CatalogDriver, tc.wantCatalog)
			}
			if cfg.CatalogDSN != tc.wantCatalogDSN {
				t.Errorf("CatalogDSN: got %q, want %q", cfg.CatalogDSN, tc.wantCatalogDSN)
			}
		})
	}
}