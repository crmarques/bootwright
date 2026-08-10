import json
import os
import pathlib
import subprocess
import tempfile
import time
import unittest


_SCRIPT = pathlib.Path(__file__).resolve().parents[4] / "roles" / "storage_cluster_cephadm" / "files" / "ceph_lvm_quiet_scan.sh"


class CephLVMQuietScan(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.state = self.root / "state"
        self.ps_state = self.root / "ps-state"
        self._command(
            "pvs",
            """#!/bin/sh
count=0
[ ! -f "$SCAN_STATE" ] || count=$(cat "$SCAN_STATE")
count=$((count + 1))
printf '%s\n' "$count" > "$SCAN_STATE"
if [ "$SCAN_MODE" = late ] && [ "$count" -ge 3 ]; then
  printf ' /dev/nvme0n1|ceph-owned\n'
fi
if [ "$SCAN_MODE" = between ] && [ -f "$LATE_MARKER" ]; then
  for index in 0 1 2 3 4; do
    printf ' /dev/nvme%sn1|ceph-owned-%s\n' "$index" "$index"
  done
fi
""",
        )
        self._command(
            "lvs",
            """#!/bin/sh
count=0
[ ! -f "$SCAN_STATE" ] || count=$(cat "$SCAN_STATE")
if [ "$SCAN_MODE" = between ] && [ "$count" -eq 5 ]; then
  : > "$LATE_MARKER"
fi
if [ "$SCAN_MODE" = late ] && [ "$count" -ge 3 ]; then
  printf ' ceph-owned|ceph.cluster_fsid=owned\n'
fi
if [ "$SCAN_MODE" = between ] && [ -f "$LATE_MARKER" ]; then
  for index in 0 1 2 3 4; do
    printf ' ceph-owned-%s|ceph.cluster_fsid=owned\n' "$index"
  done
fi
""",
        )
        self._command(
            "ps",
            """#!/bin/sh
count=0
[ ! -f "$PS_STATE" ] || count=$(cat "$PS_STATE")
count=$((count + 1))
printf '%s\n' "$count" > "$PS_STATE"
if [ "$SCAN_MODE" = writer ] && [ "$count" -eq 3 ]; then
  printf '99 ceph-volume lvm batch\n'
fi
if [ "$SCAN_MODE" = continuous-writer ]; then
  printf '99 ceph-volume lvm batch\n'
fi
""",
        )
        self._command("sleep", "#!/bin/sh\n[ \"$REAL_SLEEP\" != 1 ] || exec /bin/sleep \"$@\"\nexit 0\n")

    def _command(self, name, content):
        path = self.bin / name
        path.write_text(content, encoding="utf-8")
        path.chmod(0o755)

    def _run(self, mode, samples=3, attempts=12, interval=0, quiet=0, real_sleep=False):
        env = os.environ.copy()
        env["PATH"] = f"{self.bin}:/usr/bin:/bin"
        env["SCAN_MODE"] = mode
        env["SCAN_STATE"] = str(self.state)
        env["PS_STATE"] = str(self.ps_state)
        env["LATE_MARKER"] = str(self.root / "late-marker")
        env["REAL_SLEEP"] = "1" if real_sleep else "0"
        return subprocess.run(
            [str(_SCRIPT), str(samples), str(attempts), str(interval), "30", str(quiet)],
            check=False,
            capture_output=True,
            text=True,
            env=env,
        )

    def test_healthy_node_needs_three_stable_samples(self):
        result = self._run("healthy")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "")
        self.assertEqual(self.state.read_text(encoding="utf-8").strip(), "6")

    def test_late_volume_group_resets_stability_and_is_reported(self):
        result = self._run("late")
        self.assertEqual(result.returncode, 0, result.stderr)
        rows = [json.loads(line) for line in result.stdout.splitlines()]
        self.assertEqual(rows[0]["pv"], "/dev/nvme0n1")
        self.assertEqual(rows[0]["fsid"], "owned")
        self.assertEqual(self.state.read_text(encoding="utf-8").strip(), "8")

    def test_row_created_between_pvs_and_lvs_forces_another_sample(self):
        result = self._run("between", attempts=12)
        self.assertEqual(result.returncode, 0, result.stderr)
        rows = [json.loads(line) for line in result.stdout.splitlines()]
        self.assertEqual([row["pv"] for row in rows], [f"/dev/nvme{index}n1" for index in range(5)])
        self.assertEqual(self.state.read_text(encoding="utf-8").strip(), "12")

    def test_writer_observation_resets_stability(self):
        result = self._run("writer")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.state.read_text(encoding="utf-8").strip(), "10")

    def test_continuous_writer_reaches_the_bound(self):
        result = self._run("continuous-writer", attempts=3)
        self.assertEqual(result.returncode, 75)
        self.assertIn("ceph-volume", result.stderr)

    def test_production_quiet_window_spans_two_seconds(self):
        started = time.monotonic()
        result = self._run("healthy", interval=1, quiet=2, real_sleep=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertGreaterEqual(time.monotonic() - started, 1.8)


if __name__ == "__main__":
    unittest.main()
