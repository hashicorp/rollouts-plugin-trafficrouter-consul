# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

# These scan results are run as part of CRT workflows.

# Un-triaged results will block release. See `security-scanner` docs for more
# information on how to add `triage` config to unblock releases for specific results.
# In most cases, we should not need to disable the entire scanner to unblock a release.

# To run manually, install scanner and then from the repository root run
# `SECURITY_SCANNER_CONFIG_FILE=.release/security-scan.hcl scan ...`
# To scan a local container, add `local_daemon = true` to the `container` block below.
# See `security-scanner` docs or run with `--help` for scan target syntax.

container {
  dependencies = true
  alpine_secdb = true

  secrets {
    all = true
  }
}

binary {
  go_modules   = true
  osv          = true

  secrets {
    all = true
  }

  triage {
    suppress {
      vulnerabilites = [
        "GO-2024-2611", #alias
      ]
    }
  }
}
