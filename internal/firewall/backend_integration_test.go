//go:build linux && integration

package firewall

import (
	"context"
	"os"
	"testing"

	"github.com/google/nftables"
)

func TestNftBackendIntegrationCreatesAndDeletesOwnedTable(t *testing.T) {
	if os.Getenv("S12RYT_INTEGRATION_NETNS") != "1" {
		t.Skip("set S12RYT_INTEGRATION_NETNS=1 inside a disposable Linux network namespace")
	}
	if os.Geteuid() != 0 {
		t.Skip("integration test requires root or CAP_NET_ADMIN")
	}
	backend, err := NewNftBackend()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Delete(context.Background(), OwnedTableName) })
	rules := Ruleset{Family: TableFamilyINet, Table: OwnedTableName, Openings: []Opening{
		{Protocol: ProtocolTCP, Family: FamilyIPv4, Port: 34466, Purpose: "management"},
		{Protocol: ProtocolTCP, Family: FamilyIPv6, Port: 34466, Purpose: "management"},
	}}
	if err := backend.Apply(context.Background(), rules); err != nil {
		t.Fatal(err)
	}
	conn, err := nftables.New()
	if err != nil {
		t.Fatal(err)
	}
	if !integrationTableExists(t, conn) {
		t.Fatal("owned nftables table was not created")
	}
	if err := backend.Delete(context.Background(), OwnedTableName); err != nil {
		t.Fatal(err)
	}
	conn, err = nftables.New()
	if err != nil {
		t.Fatal(err)
	}
	if integrationTableExists(t, conn) {
		t.Fatal("owned nftables table remains after delete")
	}
}

func integrationTableExists(t *testing.T, conn *nftables.Conn) bool {
	t.Helper()
	tables, err := conn.ListTables()
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		if table.Family == nftables.TableFamilyINet && table.Name == OwnedTableName {
			return true
		}
	}
	return false
}
