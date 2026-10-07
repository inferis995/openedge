package db

import "testing"

func TestPoolSize(t *testing.T) {
	cases := []struct {
		name            string
		cfg             Config
		envOpen, envIdl string
		open, idle      int
	}{
		{"defaults", Config{}, "", "", 25, 10},
		{"driver", ForDriver(Config{}), "", "", 2, 1},
		{"env overrides", ForDriver(Config{}), "5", "", 5, 1},
		{"idle never above open", Config{}, "4", "", 4, 4},
		{"bad env ignored", ForDriver(Config{}), "x", "-1", 2, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("DB_MAX_OPEN_CONNS", c.envOpen)
			t.Setenv("DB_MAX_IDLE_CONNS", c.envIdl)
			open, idle := poolSize(c.cfg)
			if open != c.open || idle != c.idle {
				t.Fatalf("got %d/%d, want %d/%d", open, idle, c.open, c.idle)
			}
		})
	}
	if !ForDriver(Config{}).SkipMigrations {
		t.Fatal("a driver must not run migrations")
	}
}
