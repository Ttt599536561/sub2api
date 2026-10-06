package repository

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Wei-Shaw/sub2api/migrations"
)

// These fingerprints describe released databases, not the current source tree.
// New migrations belong to the upgrade target and must not enter these fixtures.
type migrationUpgradeBaseline struct {
	commit   string
	count    int
	digest   string
	excluded []string
}

const migrationV028LastFilename = "240_welfare_subscription_rewards.sql"

var (
	upstreamV028MigrationBaseline = migrationUpgradeBaseline{
		commit: "a3eb7ef302961cba716dc78b39b93b60c467db0e", count: 289,
		digest:   "6075250885f45555d7671005dc80db75c8848e9066a0cc3d291cd36a80c30947",
		excluded: []string{"235_subscription_daily_reset.sql", "239_welfare_center.sql", "240_welfare_subscription_rewards.sql"},
	}
	customV027MigrationBaseline = migrationUpgradeBaseline{
		commit: "713d2852e8ea8addce365f595d95823d26bb5aa2", count: 289,
		digest:   "47bd915b3a9b9baf8b145c432617b26082f138dcc34170ccfeabdd4079484648",
		excluded: []string{"238b_content_moderation_engine_meta.sql", "239_channel_reasoning_effort_multipliers.sql", "240_affiliate_ledger_operation_id.sql"},
	}
	customV028MigrationBaseline = migrationUpgradeBaseline{
		commit: "ebaa8c0222b797930f7469bd68154d637d3b5a1a", count: 292,
		digest: "48aad43a88107ef6f79d07952c8c1e19347f7f498b8f21295bd71816c2afcb64",
	}
)

func migrationUpgradeFixture(t *testing.T, baseline migrationUpgradeBaseline) fstest.MapFS {
	t.Helper()
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	fixture := fstest.MapFS{}
	digest := sha256.New()
	for _, name := range files {
		if name > migrationV028LastFilename {
			continue
		}
		excluded := false
		for _, omitted := range baseline.excluded {
			if name == omitted {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		contents, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		fixture[name] = &fstest.MapFile{Data: contents}
		_, _ = fmt.Fprintf(digest, "%s\x00%s\x00", name, strings.TrimSpace(string(contents)))
	}
	if len(fixture) != baseline.count {
		t.Fatalf("migration fixture %s: got %d SQL files, want %d", baseline.commit, len(fixture), baseline.count)
	}
	if actual := fmt.Sprintf("%x", digest.Sum(nil)); actual != baseline.digest {
		t.Fatalf("migration fixture %s changed: got SHA-256 %s, want %s; preserve the released SQL files", baseline.commit, actual, baseline.digest)
	}
	return fixture
}

// This test runs without the integration build tag or Docker, so a baseline
// mismatch cannot be hidden by the integration harness's local Docker skip.
func TestMigrationUpgradeBaselines(t *testing.T) {
	for _, baseline := range []migrationUpgradeBaseline{
		upstreamV028MigrationBaseline, customV027MigrationBaseline, customV028MigrationBaseline,
	} {
		t.Run(baseline.commit[:9], func(t *testing.T) {
			migrationUpgradeFixture(t, baseline)
		})
	}
}
