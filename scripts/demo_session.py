#!/usr/bin/env python3
"""Replay a TOML-described muxedo demo session in a color PTY."""

from __future__ import annotations

import argparse
import fcntl
import os
import pty
import re
import select
import struct
import subprocess
import sys
import tempfile
import termios
import time
import tty
from contextlib import contextmanager
from pathlib import Path

try:
    import tomllib
except ModuleNotFoundError:  # pragma: no cover - Python < 3.11 fallback.
    sys.stderr.write("error: Python 3.11+ is required for tomllib\n")
    sys.exit(2)


KEYS = {
    "enter": b"\r",
    "return": b"\r",
    "tab": b"\t",
    "backspace": b"\x7f",
    "esc": b"\x1b",
    "escape": b"\x1b",
    "space": b" ",
    "up": b"\x1b[A",
    "down": b"\x1b[B",
    "right": b"\x1b[C",
    "left": b"\x1b[D",
    "pgup": b"\x1b[5~",
    "pageup": b"\x1b[5~",
    "pgdn": b"\x1b[6~",
    "pagedown": b"\x1b[6~",
}

ANSI_RE = re.compile(rb"\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07]*(?:\x07|\x1b\\)")


def parse_duration(value: object) -> float:
    if value is None:
        return 0.0
    if isinstance(value, (int, float)):
        return float(value)
    text = str(value).strip()
    if not text:
        return 0.0
    units = {
        "ms": 0.001,
        "s": 1.0,
        "m": 60.0,
    }
    for suffix, multiplier in units.items():
        if text.endswith(suffix):
            return float(text[: -len(suffix)]) * multiplier
    return float(text)


def encode_key(name: str) -> bytes:
    lowered = name.lower()
    if lowered in KEYS:
        return KEYS[lowered]
    if lowered.startswith("ctrl+") and len(lowered) == 6:
        char = lowered[-1]
        if "a" <= char <= "z":
            return bytes([ord(char) - ord("a") + 1])
    if len(name) == 1:
        return name.encode()
    raise ValueError(f"unsupported key {name!r}")


def load_scenario(path: Path) -> dict:
    with path.open("rb") as handle:
        scenario = tomllib.load(handle)
    if "step" not in scenario or not isinstance(scenario["step"], list):
        raise ValueError(f"{path} must contain one or more [[step]] blocks")
    return scenario


def set_winsize(fd: int, rows: int, cols: int) -> None:
    packed = struct.pack("HHHH", rows, cols, 0, 0)
    fcntl.ioctl(fd, termios.TIOCSWINSZ, packed)


def set_raw_output(fd: int) -> None:
    tty.setraw(fd)
    attrs = termios.tcgetattr(fd)
    attrs[1] = attrs[1] & ~termios.OPOST
    if hasattr(termios, "ONLCR"):
        attrs[1] = attrs[1] & ~termios.ONLCR
    termios.tcsetattr(fd, termios.TCSANOW, attrs)


@contextmanager
def preserved_output_mode(fd: int):
    if not os.isatty(fd):
        yield
        return

    attrs = termios.tcgetattr(fd)
    raw_attrs = attrs[:]
    raw_attrs[1] = raw_attrs[1] & ~termios.OPOST
    if hasattr(termios, "ONLCR"):
        raw_attrs[1] = raw_attrs[1] & ~termios.ONLCR
    termios.tcsetattr(fd, termios.TCSANOW, raw_attrs)
    try:
        yield
    finally:
        termios.tcsetattr(fd, termios.TCSANOW, attrs)


def make_demo_home() -> tempfile.TemporaryDirectory[str]:
    temp_home = tempfile.TemporaryDirectory(prefix="muxedo-demo-home-")
    config_dir = Path(temp_home.name) / ".config" / "muxedo"
    config_dir.mkdir(parents=True)
    (config_dir / "config.toml").write_text(
        "[ui]\n"
        "show_exit_message = false\n"
        "check_updates_on_start = false\n",
        encoding="utf-8",
    )
    return temp_home


def prepare_child_pty() -> None:
    os.setsid()
    if hasattr(termios, "TIOCSCTTY"):
        fcntl.ioctl(0, termios.TIOCSCTTY, 0)


def output_available(master_fd: int, sink, transcript: bytearray, timeout: float) -> bool:
    readable, _, _ = select.select([master_fd], [], [], timeout)
    if not readable:
        return True
    try:
        data = os.read(master_fd, 8192)
    except OSError:
        return False
    if not data:
        return False
    transcript.extend(data)
    sink.write(data)
    sink.flush()
    return True


def pump_for(master_fd: int, sink, transcript: bytearray, seconds: float) -> bool:
    deadline = time.monotonic() + seconds
    while True:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            return True
        if not output_available(master_fd, sink, transcript, min(remaining, 0.05)):
            return False


def reap_child(proc: subprocess.Popen, master_fd: int, sink, transcript: bytearray, timeout: float) -> int:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        exit_code = proc.poll()
        if exit_code is not None:
            pump_for(master_fd, sink, transcript, 0.2)
            return exit_code
        if not output_available(master_fd, sink, transcript, 0.05):
            break

    try:
        proc.terminate()
    except ProcessLookupError:
        pass
    try:
        return proc.wait(timeout=2)
    except subprocess.TimeoutExpired:
        proc.kill()
        return proc.wait()


def run_session(args: argparse.Namespace) -> int:
    scenario_path = Path(args.scenario)
    scenario = load_scenario(scenario_path)
    rows = int(scenario.get("rows", 30))
    cols = int(scenario.get("cols", 100))
    profile = Path(args.profile or scenario.get("profile", "profile.toml.example"))
    if not profile.is_absolute():
        profile = Path.cwd() / profile

    muxedo = Path(args.muxedo)
    if not muxedo.is_absolute() and "/" in args.muxedo:
        muxedo = Path.cwd() / muxedo
    muxedo_cmd = str(muxedo) if "/" in args.muxedo else args.muxedo

    transcript = bytearray()
    with make_demo_home() as temp_home:
        master_fd, slave_fd = pty.openpty()
        set_winsize(slave_fd, rows, cols)
        set_raw_output(slave_fd)
        env = os.environ.copy()
        env["HOME"] = temp_home
        env["TERM"] = "xterm-256color"
        env["COLORTERM"] = "truecolor"
        env["COLUMNS"] = str(cols)
        env["LINES"] = str(rows)
        env.pop("NO_COLOR", None)
        proc = subprocess.Popen(
            [muxedo_cmd, "-profile", str(profile)],
            stdin=slave_fd,
            stdout=slave_fd,
            stderr=slave_fd,
            env=env,
            preexec_fn=prepare_child_pty,
            close_fds=True,
        )
        os.close(slave_fd)
        set_winsize(master_fd, rows, cols)
        with preserved_output_mode(sys.stdout.fileno()):
            sink = sys.stdout.buffer
            if not pump_for(master_fd, sink, transcript, parse_duration(scenario.get("startup_wait", "0s"))):
                return reap_child(proc, master_fd, sink, transcript, 1.0)

            for index, step in enumerate(scenario["step"], start=1):
                if not isinstance(step, dict):
                    raise ValueError(f"step {index} must be a TOML table")
                if "key" in step:
                    os.write(master_fd, encode_key(str(step["key"])))
                if "text" in step:
                    os.write(master_fd, str(step["text"]).encode())
                if "wait" in step:
                    if not pump_for(master_fd, sink, transcript, parse_duration(step["wait"])):
                        break

            exit_code = reap_child(proc, master_fd, sink, transcript, parse_duration(args.exit_timeout))
    if args.require_ansi and not ANSI_RE.search(transcript):
        sys.stderr.write("error: session output did not contain ANSI escape sequences\n")
        return 1
    return exit_code


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--muxedo", default="./muxedo", help="muxedo executable to run")
    parser.add_argument("--scenario", default="docs/demo/session.toml", help="demo scenario TOML")
    parser.add_argument("--profile", help="override profile path from the scenario")
    parser.add_argument("--exit-timeout", default="15s", help="time to wait for muxedo to exit")
    parser.add_argument("--require-ansi", action="store_true", help="fail when output lacks ANSI escapes")
    parser.add_argument("--print-window-size", action="store_true", help="print <cols>x<rows> and exit")
    args = parser.parse_args()

    try:
        if args.print_window_size:
            scenario = load_scenario(Path(args.scenario))
            print(f"{int(scenario.get('cols', 100))}x{int(scenario.get('rows', 30))}")
            return 0
        return run_session(args)
    except Exception as exc:
        sys.stderr.write(f"error: {exc}\n")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
