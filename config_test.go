package pglocker_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/ismurov/pglocker"
)

func TestConfig_SetDefault(t *testing.T) { //nolint:funlen // Test table is used.
	t.Parallel()

	tests := []struct {
		name string
		give pglocker.Config
		want pglocker.Config
	}{
		{
			name: "empty config",
			give: pglocker.Config{},
			want: pglocker.Config{
				Scheme: pglocker.DefaultScheme,
				Table:  pglocker.DefaultTable,
			},
		},
		{
			name: "scheme only",
			give: pglocker.Config{
				Scheme: "test_scheme",
			},
			want: pglocker.Config{
				Scheme: "test_scheme",
				Table:  pglocker.DefaultTable,
			},
		},
		{
			name: "table only",
			give: pglocker.Config{
				Table: "test_table",
			},
			want: pglocker.Config{
				Scheme: pglocker.DefaultScheme,
				Table:  "test_table",
			},
		},
		{
			name: "scheme and table",
			give: pglocker.Config{
				Scheme: "test_scheme",
				Table:  "test_table",
			},
			want: pglocker.Config{
				Scheme: "test_scheme",
				Table:  "test_table",
			},
		},
	}

	for _, tc := range tests {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.give
			got.SetDefault()

			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("mismatch (-want, +got):\n%s", diff)
			}
		})
	}
}
