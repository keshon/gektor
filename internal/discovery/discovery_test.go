package discovery

import "testing"

func TestMetadataRanking(t *testing.T) {
	fields := []string{"native_ssh", "Connect to SSH hosts and upload files", "ssh_connections"}
	for _, query := range []string{"ssh connect host", "host connect ssh", "SSH SSH", "ssh_connections", ""} {
		if Score(query, fields...) == 0 {
			t.Errorf("missing match for %q", query)
		}
	}
	if Score("unrelated", fields...) != 0 {
		t.Fatal("unrelated query matched")
	}
	if Score("ssh_connections", fields...) <= Score("ssh_connections", "Find ssh_connections in a guide") {
		t.Fatal("exact tool name must rank before prose")
	}
	if Score("ssh ssh", fields...) != Score("ssh", fields...) {
		t.Fatal("duplicate words increased rank")
	}
}
