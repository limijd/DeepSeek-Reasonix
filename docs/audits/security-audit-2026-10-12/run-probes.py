import json
import os
from pathlib import Path
import subprocess
import tempfile

evidence = Path(__file__).resolve().parent
root = evidence.parents[2]
env = {**os.environ, "GOTOOLCHAIN": "go1.26.9"}
module_root = subprocess.check_output(
    ["go", "list", "-m", "-f", "{{.Dir}}"], cwd=root, env=env, text=True
).strip()
pairs = {
    "internal/frontend/serve/cloud_desktop_test.go": "internal_frontend_serve_audit_test.go.txt",
    "internal/tools/builtin/bash_cancel_test.go": "internal_tools_builtin_audit_test.go.txt",
    "internal/ext/installsource/archive_test.go": "internal_ext_installsource_audit_test.go.txt",
    "internal/assembly/boot/effect_egress_unix_test.go": "internal_assembly_boot_audit_test.go.txt",
    "internal/contract/config/systemone_test.go": "config_audit_test.go.txt",
}
pattern = "^TestAudit(RemoteAttachment|HTTPSProxy|ProxyBypasses|DenyOnly|ProjectOverrides)"
with tempfile.TemporaryDirectory(prefix="reasonix-audit-overlay-") as directory:
    overlay = Path(directory) / "overlay.json"
    overlay.write_text(json.dumps({"Replace": {
        str(Path(module_root) / target): str(evidence / "probes" / source)
        for target, source in pairs.items()
    }}))
    result = subprocess.run(
        ["go", "test", "-mod=readonly", f"-overlay={overlay}",
         *dict.fromkeys("./" + str(Path(target).parent) for target in pairs),
         "-run", pattern, "-v", "-count=1"],
        cwd=root, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
    )
    print(result.stdout, end="")
    if result.returncode:
        raise SystemExit(result.returncode)
    if result.stdout.count("--- PASS: TestAudit") != 5:
        raise SystemExit("Expected five executed audit probes")
