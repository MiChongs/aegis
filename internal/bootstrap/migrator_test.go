package bootstrap

import "testing"

func TestMigrationPlanSkipsUnchangedAndRerunsEdited(t *testing.T) {
	files := []migrationFile{
		{Version: "000001_a", Checksum: "a1"},
		{Version: "000002_b", Checksum: "b2"}, // 被原地改过
		{Version: "000003_c", Checksum: "c1"}, // 新文件
	}
	recorded := map[string]string{"000001_a": "a1", "000002_b": "b1"}
	pending, skipped := migrationPlan(files, recorded)
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	if len(pending) != 2 || pending[0].Version != "000002_b" || pending[1].Version != "000003_c" {
		t.Fatalf("unexpected pending: %+v", pending)
	}
}

func TestMigrationPlanFirstAdoptionRunsAll(t *testing.T) {
	files := []migrationFile{{Version: "1", Checksum: "x"}, {Version: "2", Checksum: "y"}}
	pending, skipped := migrationPlan(files, map[string]string{})
	if skipped != 0 || len(pending) != 2 {
		t.Fatalf("first adoption should run everything, got pending=%d skipped=%d", len(pending), skipped)
	}
}
