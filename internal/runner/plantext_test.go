package runner

import (
	"strings"
	"testing"
)

const samplePlanText = `
Terraform used the selected providers to generate the following execution
plan. Resource actions are indicated with the following symbols:
  ~ update in-place
  + create

Terraform will perform the following actions:

  # module.s3_bucket.aws_s3_bucket.this will be updated in-place
  ~ resource "aws_s3_bucket" "this" {
        id                          = "geoserver-s3-files-poc"
      ~ tags                        = {
          - "Application"  = "app-shared" -> null
        }
        # (14 unchanged attributes hidden)

        # (3 unchanged blocks hidden)
    }

  # aws_iam_policy.s3_access will be created
  + resource "aws_iam_policy" "s3_access" {
      + name = "s3-access"
    }

Plan: 1 to add, 1 to change, 0 to destroy.
`

func TestParsePlanDetailsSplitsPerResource(t *testing.T) {
	got := parsePlanDetails(samplePlanText)

	if len(got) != 2 {
		t.Fatalf("got %d blocks, want 2: %#v", len(got), got)
	}

	upd, ok := got["module.s3_bucket.aws_s3_bucket.this"]
	if !ok {
		t.Fatal("missing s3_bucket block")
	}
	// header line preserved
	if !strings.HasPrefix(strings.TrimSpace(upd), "# module.s3_bucket.aws_s3_bucket.this will be updated in-place") {
		t.Errorf("block does not start with header:\n%s", upd)
	}
	// inner "# (14 unchanged...)" comment stays inside the block, not a new key
	if !strings.Contains(upd, "# (14 unchanged attributes hidden)") {
		t.Errorf("block missing inner hidden-attrs comment:\n%s", upd)
	}
	// trailing "Plan:" summary excluded
	if strings.Contains(upd, "Plan: 1 to add") {
		t.Errorf("block leaked trailing summary:\n%s", upd)
	}

	cre, ok := got["aws_iam_policy.s3_access"]
	if !ok {
		t.Fatal("missing iam_policy block")
	}
	if !strings.Contains(cre, `+ name = "s3-access"`) {
		t.Errorf("create block missing body:\n%s", cre)
	}
}
