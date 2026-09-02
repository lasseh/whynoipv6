package migrations

import "testing"

// TestDriverURL: the documented pool-sized DSN (09-ops §2.1) must reach the
// migrate driver without its pool_* keys, which the pgx stdlib would
// otherwise send to the server as runtime parameters.
func TestDriverURL(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{
			in:   "postgres://whynoipv6:pw@db.internal:5432/whynoipv6?pool_max_conns=32&sslmode=verify-full",
			want: "pgx5://whynoipv6:pw@db.internal:5432/whynoipv6?sslmode=verify-full",
		},
		{
			in:   "postgresql://u:p@h/db?pool_min_conns=2&pool_max_conn_lifetime=1h",
			want: "pgx5://u:p@h/db",
		},
		{
			in:   "postgres://postgres:integration@127.0.0.1:15433/postgres?sslmode=disable",
			want: "pgx5://postgres:integration@127.0.0.1:15433/postgres?sslmode=disable",
		},
		{in: "host=db.internal user=app password=x dbname=whynoipv6", wantErr: true},
		{in: "mysql://u:p@h/db", wantErr: true},
	}
	for _, tc := range cases {
		got, err := DriverURL(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("DriverURL(%q) = %q, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("DriverURL(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("DriverURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
