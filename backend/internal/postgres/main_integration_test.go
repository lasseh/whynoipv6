//go:build integration

package postgres

import (
	"os"
	"testing"

	"github.com/lasseh/whynoipv6/internal/postgres/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Main(m)) }
