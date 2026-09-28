"""Exercise actual terminal vs redirected/no-color installer output."""
import errno
import os
import pty
import select
import subprocess
from pathlib import Path

script = Path(__file__).resolve().parents[2] / 'scripts/install.sh'

def terminal_output(args, no_color=False):
    master, slave = pty.openpty()
    env = dict(os.environ, TERM='xterm-256color')
    env.pop('NO_COLOR', None)
    if no_color:
        env['NO_COLOR'] = ''
    process = subprocess.Popen(['bash', str(script), *args], stdin=subprocess.DEVNULL,
                               stdout=slave, stderr=slave, env=env)
    os.close(slave)
    output = bytearray()
    try:
        while True:
            if not select.select([master], [], [], 5)[0]:
                raise AssertionError('installer did not finish')
            try:
                chunk = os.read(master, 8192)
            except OSError as error:
                if error.errno == errno.EIO:
                    break
                raise
            if not chunk:
                break
            output.extend(chunk)
    finally:
        os.close(master)
        if process.poll() is None:
            process.wait(timeout=5)
    return process.returncode, bytes(output)

code, output = terminal_output(['--help'])
assert code == 0 and b'\x1b[1;36m' in output and b'\x1b[0m' in output
code, output = terminal_output(['--unknown'])
assert code == 2 and b'\x1b[1;31m' in output
code, output = terminal_output(['--help'], no_color=True)
assert code == 0 and b'\x1b[' not in output
result = subprocess.run(['bash', str(script), '--help'], capture_output=True)
assert result.returncode == 0 and b'\x1b[' not in result.stdout + result.stderr
print('Terminal colors, errors, NO_COLOR and redirected output passed')
