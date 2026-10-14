"""Run Railway verification with normal orphan-process reaping."""
import ctypes
import os
import subprocess
import sys
import time

if ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) != 0:
    raise OSError(ctypes.get_errno(), "Cannot enable child subreaper")
child = subprocess.Popen(sys.argv[1:])
while True:
    try:
        pid, status = os.waitpid(-1, os.WNOHANG)
    except ChildProcessError:
        raise RuntimeError("Verification process was lost")
    if pid == child.pid:
        code = os.waitstatus_to_exitcode(status)
        child.returncode = code
        sys.exit(code if code >= 0 else 128 - code)
    if pid == 0:
        time.sleep(0.05)
